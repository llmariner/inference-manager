package runtime

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NewPodMonitor constructs a PodMonitor.
func NewPodMonitor(
	k8sClient k8sclient.Client,
) *PodMonitor {
	return &PodMonitor{
		k8sClient:  k8sClient,
		podsByName: make(map[string]*podReadinessStatus),
	}
}

type podReadinessStatus struct {
	pod     *corev1.Pod
	modelID string
	ready   bool
}

// PodMonitor monitors the pods in the cluster.
type PodMonitor struct {
	k8sClient k8sclient.Client
	logger    logr.Logger

	podsByName map[string]*podReadinessStatus
	mu         sync.Mutex
}

// SetupWithManager sets up the runtime manager with the given controller manager.
func (m *PodMonitor) SetupWithManager(mgr ctrl.Manager) error {
	m.logger = mgr.GetLogger().WithName("podMonitor")

	filter := (predicate.NewPredicateFuncs(func(object k8sclient.Object) bool {
		return object.GetLabels()["app.kubernetes.io/created-by"] == managerName
	}))

	return ctrl.NewControllerManagedBy(mgr).
		Named("pod-monitor").
		For(&corev1.Pod{}, builder.WithPredicates(filter)).
		WithLogConstructor(func(req *reconcile.Request) logr.Logger {
			if req != nil {
				return mgr.GetLogger().WithValues("pod", req.NamespacedName)
			}
			return mgr.GetLogger()
		}).
		WithOptions(controller.Options{NeedLeaderElection: ptr.To(false)}).
		Complete(m)
}

// Reconcile updates the pods in the cluster.
func (m *PodMonitor) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	var pod corev1.Pod
	if err := m.k8sClient.Get(ctx, req.NamespacedName, &pod); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to get pod")
			return ctrl.Result{}, err
		}

		m.deletePod(req.Name)
		return ctrl.Result{}, nil
	}

	m.addOrUpdatePod(&pod)

	return ctrl.Result{}, nil
}

func (m *PodMonitor) addOrUpdatePod(pod *corev1.Pod) {
	m.mu.Lock()
	defer m.mu.Unlock()

	modelID := pod.Annotations[modelAnnotationKey]

	// TODO(kenji): Collect log if the pod is not ready.

	m.podsByName[pod.Name] = &podReadinessStatus{
		pod:     pod,
		modelID: modelID,
		ready:   isPodReady(pod),
	}
}

func (m *PodMonitor) deletePod(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.podsByName, name)
}

type modelStatus struct {
	numReadyPods int
	numTotalPods int

	statusMessage string
}

func (m *PodMonitor) modelStatus(modelID string) *modelStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	ms := &modelStatus{}
	for _, p := range m.podsByName {
		if p.modelID != modelID {
			continue
		}

		ms.numTotalPods++
		if p.ready {
			ms.numReadyPods++
		} else {
			// TODO(kenji): Collect logs.
			ms.statusMessage += "Pod " + p.pod.Name + " is not ready. "
		}
	}

	return ms

}
