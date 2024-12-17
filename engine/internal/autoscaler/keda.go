package autoscaler

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strconv"

	"github.com/go-logr/logr"
	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	kedaclient "github.com/kedacore/keda/v2/pkg/generated/clientset/versioned/typed/keda/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
)

const modelAnnotationKey = "llmariner/model"

// NewKedaScaler creates a new KedaScaler.
func NewKedaScaler(config KedaConfig, namespace string) Autoscaler {
	return &KedaScaler{
		config:    config,
		namespace: namespace,
	}
}

// KedaScaler is a scaler that uses KEDA to scale.
type KedaScaler struct {
	config    KedaConfig
	namespace string

	client   kedaclient.KedaV1alpha1Interface
	recorder record.EventRecorder
	logger   logr.Logger
}

// SetupWithManager sets up the multi-autoscaler with the Manager.
func (s *KedaScaler) SetupWithManager(mgr ctrl.Manager) error {
	s.logger = mgr.GetLogger().WithName("kedascaler")
	s.recorder = mgr.GetEventRecorderFor("inference-scaler")

	c, err := kedaclient.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("failed to create KEDA client: %s", err)
	}
	s.client = c

	return mgr.Add(s)
}

// NeedLeaderElection implements LeaderElectionRunnable and always returns true.
func (s *KedaScaler) NeedLeaderElection() bool {
	// Enable leader-election to update scaled object only from the leader.
	// Register() is called by runtime manager, and is unaffected by this leader election.
	return true
}

// Start starts the multi-autoscaler.
func (s *KedaScaler) Start(ctx context.Context) error {
	s.logger.Info("Updating KEDA ScaledObjects")

	soList, err := s.client.ScaledObjects(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: s.getScaledObjectLabels().String(),
	})
	if err != nil {
		return fmt.Errorf("failed to list ScaledObjects: %s", err)
	}

	var updated, skipped int
	for _, so := range soList.Items {
		modelID, ok := so.Annotations[modelAnnotationKey]
		if !ok {
			s.logger.Info("Skipping ScaledObject without model annotation", "name", so.Name)
			continue
		}
		newSo := so.DeepCopy()
		if err := s.updateScaledObjectSpec(newSo, modelID); err != nil {
			return fmt.Errorf("failed to update ScaledObject spec: %s", err)
		}
		if reflect.DeepEqual(so.Spec, newSo.Spec) {
			s.logger.Info("ScaledObject is up-to-date", "name", so.Name)
			skipped++
			continue
		}
		if _, err := s.client.ScaledObjects(so.Namespace).Update(ctx, newSo, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update ScaledObject: %s", err)
		}
		s.recorder.Eventf(&so, "Normal", "ScaledObjectUpdated", "ScaledObject %s updated", so.Name)
		s.logger.Info("ScaledObject updated", "name", so.Name)
		updated++
	}

	s.logger.Info("Updated KEDA ScaledObjects", "updated", updated, "skipped", skipped)
	return nil
}

// Register registers a new scaler for the given runtime.
func (s *KedaScaler) Register(ctx context.Context, modelID string, target *appsv1.StatefulSet) error {
	s.logger.Info("Registering scaler", "modelID", modelID, "target", target.GetName())
	if err := s.createScaledObject(ctx, modelID, target); err != nil {
		return fmt.Errorf("failed to deploy ScaledObject: %s", err)
	}
	return nil
}

// Unregister unregisters the scaler for the given runtime.
func (s *KedaScaler) Unregister(target types.NamespacedName) {
	// do nothing. ScaledObject has an owner reference so it will be deleted by GC.
}

func (s *KedaScaler) createScaledObject(ctx context.Context, modelID string, target *appsv1.StatefulSet) error {
	log := ctrl.LoggerFrom(ctx).WithValues("name", target.GetName())

	obj := &kedav1alpha1.ScaledObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:            target.GetName(),
			Namespace:       target.GetNamespace(),
			Labels:          s.getScaledObjectLabels(),
			Annotations:     map[string]string{modelAnnotationKey: modelID},
			OwnerReferences: []metav1.OwnerReference{ptr.Deref(metav1.NewControllerRef(target, target.GroupVersionKind()), metav1.OwnerReference{})},
		},
		Spec: kedav1alpha1.ScaledObjectSpec{
			ScaleTargetRef: &kedav1alpha1.ScaleTarget{
				Kind: target.GroupVersionKind().Kind,
				Name: target.GetName(),
			},
		},
	}
	if err := s.updateScaledObjectSpec(obj, modelID); err != nil {
		return fmt.Errorf("failed to update ScaledObject spec: %s", err)
	}

	if _, err := s.client.ScaledObjects(target.Namespace).Create(ctx, obj, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.V(4).Info("ScaledObject already exists")
			return nil
		}
		return fmt.Errorf("failed to create ScaledObject: %s", err)
	}
	s.recorder.Eventf(target, "Normal", "ScaledObjectCreated", "ScaledObject %s created", obj.Name)
	log.V(1).Info("ScaledObject created")
	return nil
}

func (s *KedaScaler) getScaledObjectLabels() labels.Set {
	return labels.Set{
		"app.kubernetes.io/name":       "runtime-scaler",
		"app.kubernetes.io/created-by": "inference-engine",
	}
}

func (s *KedaScaler) updateScaledObjectSpec(obj *kedav1alpha1.ScaledObject, modelID string) error {
	var triggers []kedav1alpha1.ScaleTriggers
	for _, t := range s.config.PromTriggers {
		var buf bytes.Buffer
		if err := t.queryTemplate.Execute(&buf, modelID); err != nil {
			return fmt.Errorf("failed to execute template: %s", err)
		}
		meta := map[string]string{
			"serverAddress": s.config.PromServerAddress,
			"threshold":     strconv.FormatFloat(t.Threshold, 'f', -1, 64),
			"query":         buf.String(),
		}
		if t.ActivationThreshold != 0 {
			meta["activationThreshold"] = strconv.FormatFloat(t.ActivationThreshold, 'f', -1, 64)
		}
		triggers = append(triggers, kedav1alpha1.ScaleTriggers{
			Type:     "prometheus",
			Metadata: meta,
		})
	}

	target := obj.Spec.ScaleTargetRef
	obj.Spec = kedav1alpha1.ScaledObjectSpec{
		PollingInterval:  s.config.PollingInterval,
		CooldownPeriod:   s.config.CooldownPeriod,
		IdleReplicaCount: s.config.IdleReplicaCount,
		MinReplicaCount:  s.config.MinReplicaCount,
		MaxReplicaCount:  s.config.MaxReplicaCount,
		Triggers:         triggers,
		ScaleTargetRef:   target,
	}
	return nil
}
