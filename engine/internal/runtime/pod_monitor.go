package runtime

import (
	"context"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
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
	clientset kubernetes.Interface,
) *PodMonitor {
	return &PodMonitor{
		k8sClient:  k8sClient,
		clientset:  clientset,
		podsByName: make(map[string]*podReadinessStatus),
	}
}

type podReadinessStatus struct {
	pod     *corev1.Pod
	modelID string
	ready   bool

	errLogMessage string
}

// PodMonitor monitors the pods in the cluster.
type PodMonitor struct {
	k8sClient k8sclient.Client
	clientset kubernetes.Interface

	logger logr.Logger

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

	m.addOrUpdatePod(ctx, &pod)

	return ctrl.Result{}, nil
}

func (m *PodMonitor) addOrUpdatePod(ctx context.Context, pod *corev1.Pod) {
	m.mu.Lock()
	defer m.mu.Unlock()

	modelID := pod.Annotations[modelAnnotationKey]

	ready := isPodReady(pod)

	var errLogMessage string
	if !ready {
		var ts []*corev1.ContainerStateTerminated
		for _, c := range pod.Status.InitContainerStatuses {
			if t := c.LastTerminationState.Terminated; t != nil && t.ExitCode != 0 {
				ts = append(ts, t)
			}
		}
		for _, c := range pod.Status.ContainerStatuses {
			if t := c.LastTerminationState.Terminated; t != nil && t.ExitCode != 0 {
				ts = append(ts, t)
			}
		}

		// TODO(kenji): Have a better way to get the error message.
		for _, t := range ts {
			errLogMessage = extractErrMsg(t.Message)
			if errLogMessage != "" {
				break
			}
		}
	}

	m.podsByName[pod.Name] = &podReadinessStatus{
		pod:           pod,
		modelID:       modelID,
		ready:         ready,
		errLogMessage: errLogMessage,
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
			ms.statusMessage += p.errLogMessage + "\n"
		}
	}

	return ms
}

// extractErrMsg extracts the last ERROR line from the given log lines.
func extractErrMsg(msg string) string {
	lines := strings.Split(msg, "\n")
	var lastError string
	for _, line := range lines {
		if strings.HasPrefix(line, "ERROR") {
			lastError = line
		}
	}

	if lastError == "" {
		// If there is no ERROR line, return the last non-empty line.
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.TrimSpace(lines[i]) != "" {
				lastError = lines[i]
				break
			}
		}
	}

	return strings.TrimSpace(lastError)
}
