package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type loraAdapterPullAndLoader interface {
	loadLoRAAdapter(ctx context.Context, modelID, podIP string) error
}

// NewLoRARebalancer creates a new LoRARebalancer.
func NewLoRARebalancer(
	k8sClient k8sclient.Client,
	loraAdapterPullAndLoader loraAdapterPullAndLoader,
) *LoRARebalancer {
	return newLoRARebalancer(
		k8sClient,
		loraAdapterPullAndLoader,
		&loraAdapterStatusGetterImpl{},
	)
}

func newLoRARebalancer(
	k8sClient k8sclient.Client,
	loraAdapterPullAndLoader loraAdapterPullAndLoader,
	loraAdapterStatusGetter loraAdapterStatusGetter,
) *LoRARebalancer {
	return &LoRARebalancer{
		k8sClient:                k8sClient,
		loraAdapterStatusGetter:  loraAdapterStatusGetter,
		loraAdapterPullAndLoader: loraAdapterPullAndLoader,

		podsByName: make(map[string]*corev1.Pod),
	}
}

// LoRARebalancer is a controller that rebalances LoRA adapters across pods.
type LoRARebalancer struct {
	k8sClient                k8sclient.Client
	loraAdapterPullAndLoader loraAdapterPullAndLoader
	loraAdapterStatusGetter  loraAdapterStatusGetter

	logger logr.Logger

	podsByName map[string]*corev1.Pod
	mu         sync.Mutex
}

// SetupWithManager sets up the runtime manager with the given controller manager.
func (r *LoRARebalancer) SetupWithManager(mgr ctrl.Manager) error {
	r.logger = mgr.GetLogger().WithName("loraRebalancer")

	filter := (predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetLabels()["app.kubernetes.io/created-by"] == managerName
	}))

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}, builder.WithPredicates(filter)).
		WithLogConstructor(func(req *reconcile.Request) logr.Logger {
			if req != nil {
				return mgr.GetLogger().WithValues("pod", req.NamespacedName)
			}
			return mgr.GetLogger()
		}).
		Complete(r)
}

// NeedLeaderElection implements LeaderElectionRunnable and always returns true.
func (r *LoRARebalancer) NeedLeaderElection() bool {
	return true
}

// Reconcile updates the pods in the cluster.
func (r *LoRARebalancer) Reconcile(
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
		r.deletePod(ctx, req.Name)

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

func (r *LoRARebalancer) addPod(pod *corev1.Pod) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.podsByName[pod.Name] = pod
}

func (r *LoRARebalancer) deletePod(ctx context.Context, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.podsByName, name)
}

// Run periodically checks the status of the pods and loaded LoRA adapters.
func (r *LoRARebalancer) Run(ctx context.Context, interval time.Duration) error {
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

func (r *LoRARebalancer) run(ctx context.Context) error {
	statusesByBaseModelID, err := r.buildStatusMap(ctx)
	if err != nil {
		return fmt.Errorf("build status map: %s", err)
	}

	for baseModelID, podStatuses := range statusesByBaseModelID {
		r.logger.Info("Rebalancing LoRA adapters", "baseModelID", baseModelID, "statusLen", len(podStatuses))
		r.rebalance(ctx, podStatuses)
	}

	return nil
}

func (r *LoRARebalancer) rebalance(
	ctx context.Context,
	podStatuses []*podStatus,
) {
	podStatusesByName := make(map[string]*podStatus)
	for _, ps := range podStatuses {
		podStatusesByName[ps.pod.Name] = ps
	}

	// Group pod statuses by adapter ID.
	podsByAdapterID := make(map[string]map[string]*podStatus)
	for _, ps := range podStatuses {
		for aid := range ps.lstatus.adapterIDs {
			m, ok := podsByAdapterID[aid]
			if !ok {
				m = make(map[string]*podStatus)
				podsByAdapterID[aid] = m
			}
			m[ps.pod.Name] = ps
		}
	}

	// TODO(kenji): Currently we simply load adapters in all pods. Revisit the logic.
	for adapterID, podStatuses := range podsByAdapterID {
		r.logger.Info("Rebalancing LoRA adapters", "adapterID", adapterID, "statusLen", len(podStatuses))

		for name, ps := range podStatusesByName {
			if _, ok := podStatuses[name]; ok {
				// Already loaded.
				continue
			}

			r.logger.Info("Loading LoRA adapter", "adapterID", adapterID, "podName", name)
			if err := r.loraAdapterPullAndLoader.loadLoRAAdapter(
				ctx,
				adapterID,
				ps.pod.Status.PodIP,
			); err != nil {
				// Gracefully handle the error as vLLM might not be ready yet.
				r.logger.Error(err, "Failed to load LoRA adapter", "adapterID", adapterID, "podName", name)
			}
		}
	}
}

func (r *LoRARebalancer) buildStatusMap(ctx context.Context) (map[string][]*podStatus, error) {
	var pods []*corev1.Pod
	r.mu.Lock()
	for _, pod := range r.podsByName {
		pods = append(pods, pod)
	}
	r.mu.Unlock()

	statusesByBaseModelID := make(map[string][]*podStatus)
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

		statusesByBaseModelID[lstatus.baseModelID] = append(statusesByBaseModelID[lstatus.baseModelID], &podStatus{
			pod:     pod,
			lstatus: lstatus,
		})
	}

	return statusesByBaseModelID, nil
}
