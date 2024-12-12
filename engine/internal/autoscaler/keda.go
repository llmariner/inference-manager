package autoscaler

import (
	"bytes"
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	kedaclient "github.com/kedacore/keda/v2/pkg/generated/clientset/versioned/typed/keda/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NewKedaScaler creates a new KedaScaler.
func NewKedaScaler(config KedaConfig) Autoscaler {
	return &KedaScaler{config: config}
}

// KedaScaler is a scaler that uses KEDA to scale.
type KedaScaler struct {
	config KedaConfig

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

// Start starts the multi-autoscaler.
func (s *KedaScaler) Start(ctx context.Context) error {
	s.logger.Info("Starting keda-scaler")
	// TODO(aya): support KEDA external scaler
	return nil
}

// NeedLeaderElection implements LeaderElectionRunnable and always returns true.
func (s *KedaScaler) NeedLeaderElection() bool {
	return true
}

// Register registers a new scaler for the given runtime.
func (s *KedaScaler) Register(ctx context.Context, modelID string, target *appsv1.StatefulSet) error {
	s.logger.Info("Registering scaler", "modelID", modelID, "target", target.GetName())
	if err := s.deployScaledObject(ctx, modelID, target); err != nil {
		return fmt.Errorf("failed to deploy ScaledObject: %s", err)
	}
	return nil
}

// Unregister unregisters the scaler for the given runtime.
func (s *KedaScaler) Unregister(target types.NamespacedName) {
	// do nothing. ScaledObject has a owner reference so it will be deleted by GC.
}

func (s *KedaScaler) deployScaledObject(ctx context.Context, modelID string, target *appsv1.StatefulSet) error {
	log := ctrl.LoggerFrom(ctx).WithValues("name", target.GetName())

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

	obj := &kedav1alpha1.ScaledObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:            target.GetName(),
			Namespace:       target.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{ptr.Deref(metav1.NewControllerRef(target, target.GroupVersionKind()), metav1.OwnerReference{})},
		},
		Spec: kedav1alpha1.ScaledObjectSpec{
			PollingInterval:  s.config.PollingInterval,
			CooldownPeriod:   s.config.CooldownPeriod,
			IdleReplicaCount: s.config.IdleReplicaCount,
			MinReplicaCount:  s.config.MinReplicaCount,
			MaxReplicaCount:  s.config.MaxReplicaCount,
			ScaleTargetRef: &kedav1alpha1.ScaleTarget{
				Kind: target.GroupVersionKind().Kind,
				Name: target.GetName(),
			},
			Triggers: triggers,
		},
	}

	// TODO: support updating ScaledObject
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
