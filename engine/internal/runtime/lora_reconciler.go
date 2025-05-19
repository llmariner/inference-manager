package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

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

type loRAAdapterStatusUpdate struct {
	podName string
	podIP   string
	gpu     int32

	baseModelID       string
	addedAdapterIDs   []string
	removedAdapterIDs []string
}

type updateProcessor interface {
	processLoRAAdapterUpdate(ctx context.Context, update *loRAAdapterStatusUpdate) error
}

type loRAAdapterStatus struct {
	baseModelID string
	adapterIDs  map[string]struct{}
}

type loraAdapterStatusGetter interface {
	get(ctx context.Context, addr string) (*loRAAdapterStatus, error)
}

// NewLoRAReconciler creates a new LoRAReconciler.
func NewLoRAReconciler(
	k8sClient k8sclient.Client,
	updateProcessor updateProcessor,
) *LoRAReconciler {
	return newLoRAReconciler(
		k8sClient,
		updateProcessor,
		&loraAdapterStatusGetterImpl{},
	)
}

func newLoRAReconciler(
	k8sClient k8sclient.Client,
	updateProcessor updateProcessor,
	loraAdapterStatusGetter loraAdapterStatusGetter,
) *LoRAReconciler {
	return &LoRAReconciler{
		k8sClient:               k8sClient,
		updateProcessor:         updateProcessor,
		loraAdapterStatusGetter: loraAdapterStatusGetter,
		podsByName:              make(map[string]*podStatus),
	}
}

type podStatus struct {
	pod     *corev1.Pod
	lstatus *loRAAdapterStatus
}

// LoRAReconciler reconciles the LoRA adapters loading status.
type LoRAReconciler struct {
	k8sClient               k8sclient.Client
	updateProcessor         updateProcessor
	loraAdapterStatusGetter loraAdapterStatusGetter
	logger                  logr.Logger

	podsByName map[string]*podStatus
	mu         sync.Mutex
}

// SetupWithManager sets up the runtime manager with the given controller manager.
func (r *LoRAReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.logger = mgr.GetLogger().WithName("loraReconciler")

	filter := (predicate.NewPredicateFuncs(func(object k8sclient.Object) bool {
		return object.GetLabels()["app.kubernetes.io/created-by"] == managerName
	}))

	return ctrl.NewControllerManagedBy(mgr).
		Named("lora-reconciler").
		For(&corev1.Pod{}, builder.WithPredicates(filter)).
		WithLogConstructor(func(req *reconcile.Request) logr.Logger {
			if req != nil {
				return mgr.GetLogger().WithValues("pod", req.NamespacedName)
			}
			return mgr.GetLogger()
		}).
		WithOptions(controller.Options{NeedLeaderElection: ptr.To(false)}).
		Complete(r)
}

// Reconcile updates the pods in the cluster.
func (r *LoRAReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	var pod corev1.Pod
	if err := r.k8sClient.Get(ctx, req.NamespacedName, &pod); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to get pod")
			return ctrl.Result{}, err
		}

		log.Info("Pod deleted", "pod", pod.Name)
		if err := r.deletePod(ctx, req.Name); err != nil {
			log.Error(err, "Failed to delete pod")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	if pod.Status.PodIP == "" {
		// IP has not yet been bound.
		return ctrl.Result{}, nil
	}

	log.Info("Pod updated", "pod", pod.Name)
	r.addPod(&pod)

	return ctrl.Result{}, nil
}

func (r *LoRAReconciler) addPod(pod *corev1.Pod) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.podsByName[pod.Name]; ok {
		// Pod already exists, no need to add it again.
		return
	}
	r.podsByName[pod.Name] = &podStatus{
		pod: pod,
	}

	// TODO(kenji): Trigger processLoRAAdapterUpdate here so that
	// we don't need to wait for the next invocation of Run.
}

func (r *LoRAReconciler) deletePod(ctx context.Context, name string) error {
	r.mu.Lock()
	s, ok := r.podsByName[name]
	if !ok {
		r.mu.Unlock()
		return nil
	}
	delete(r.podsByName, name)
	r.mu.Unlock()

	var ids []string
	if s.lstatus != nil {
		for id := range s.lstatus.adapterIDs {
			ids = append(ids, id)
		}
	}
	if err := r.updateProcessor.processLoRAAdapterUpdate(ctx, &loRAAdapterStatusUpdate{
		podName:           name,
		podIP:             s.pod.Status.PodIP,
		gpu:               getGPUForPod(s.pod),
		removedAdapterIDs: ids,
	}); err != nil {
		return err
	}

	return nil
}

// Run periodically checks the status of the pods and loaded LoRA adapters.
func (r *LoRAReconciler) Run(ctx context.Context, interval time.Duration) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
			if err := r.run(ctx); err != nil {
				return err
			}
		}
	}
}

func (r *LoRAReconciler) run(ctx context.Context) error {
	podsByName := r.getLoRALoadingStatus(ctx)

	updates, err := r.updateLoRALoadingStatus(podsByName)
	if err != nil {
		return err
	}

	for _, u := range updates {
		if err := r.updateProcessor.processLoRAAdapterUpdate(ctx, u); err != nil {
			return err
		}
	}
	return nil
}

func (r *LoRAReconciler) getLoRALoadingStatus(ctx context.Context) map[string]*podStatus {
	var pods []*corev1.Pod
	r.mu.Lock()
	for _, podStatus := range r.podsByName {
		pods = append(pods, podStatus.pod)
	}
	r.mu.Unlock()

	podsByName := make(map[string]*podStatus)
	for _, pod := range pods {
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, vllmHTTPPort)
		lstatus, err := r.loraAdapterStatusGetter.get(ctx, addr)
		if err != nil {
			// Gracefully handle the error as vLLM might not be ready yet.
			r.logger.Error(err, "Failed to list LoRA adapters", "pod", pod.Name)
			continue
		}

		if lstatus.baseModelID == "" {
			r.logger.Info("No base model ID found", "pod", pod.Name)
			continue
		}

		podsByName[pod.Name] = &podStatus{
			pod:     pod,
			lstatus: lstatus,
		}
	}

	return podsByName
}

func (r *LoRAReconciler) updateLoRALoadingStatus(podsByName map[string]*podStatus) ([]*loRAAdapterStatusUpdate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var updates []*loRAAdapterStatusUpdate
	for name, oldS := range r.podsByName {
		newS := podsByName[name]

		u, hasUpdate, err := updateLoRALoadingStatusForPod(oldS, newS, r.logger)
		if err != nil {
			return nil, err
		}

		if hasUpdate {
			updates = append(updates, u)
		}
	}

	r.podsByName = podsByName

	return updates, nil
}

func updateLoRALoadingStatusForPod(
	oldS,
	newS *podStatus,
	log logr.Logger,
) (*loRAAdapterStatusUpdate, bool, error) {
	if newS == nil {
		pod := oldS.pod
		log.Info("Pod not found", "pod", pod.Name)

		if oldS.lstatus == nil {
			// No previous status. Do nothing.
			return nil, false, nil
		}

		// Pod not found or vLLM is unreachable. Consider that all LoRA adapters are deleted.
		var ids []string
		for id := range oldS.lstatus.adapterIDs {
			ids = append(ids, id)
		}
		return &loRAAdapterStatusUpdate{
			podName:           pod.Name,
			podIP:             pod.Status.PodIP,
			gpu:               getGPUForPod(pod),
			baseModelID:       oldS.lstatus.baseModelID,
			removedAdapterIDs: ids,
		}, true, nil
	}

	pod := newS.pod

	if oldS.lstatus == nil {
		log.Info("New status found", "pod", pod.Name, "new adapters", newS.lstatus.adapterIDs)
		var ids []string
		for id := range newS.lstatus.adapterIDs {
			ids = append(ids, id)
		}
		return &loRAAdapterStatusUpdate{
			podName:         pod.Name,
			podIP:           pod.Status.PodIP,
			gpu:             getGPUForPod(pod),
			baseModelID:     newS.lstatus.baseModelID,
			addedAdapterIDs: ids,
		}, true, nil
	}

	if oldS.lstatus.baseModelID != newS.lstatus.baseModelID {
		return nil, false, fmt.Errorf("unexpected base model ID change: %s -> %s", oldS.lstatus.baseModelID, newS.lstatus.baseModelID)
	}

	var added, removed []string
	for id := range newS.lstatus.adapterIDs {
		if _, ok := oldS.lstatus.adapterIDs[id]; !ok {
			added = append(added, id)
		}
	}
	for id := range oldS.lstatus.adapterIDs {
		if _, ok := newS.lstatus.adapterIDs[id]; !ok {
			removed = append(removed, id)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		// No change in the LoRA adapters.
		return nil, false, nil
	}

	log.Info("LoRA adapter status changed", "pod", pod.Name, "added", added, "removed", removed)
	return &loRAAdapterStatusUpdate{
		podName:           pod.Name,
		podIP:             pod.Status.PodIP,
		gpu:               getGPUForPod(pod),
		baseModelID:       newS.lstatus.baseModelID,
		addedAdapterIDs:   added,
		removedAdapterIDs: removed,
	}, true, nil
}
