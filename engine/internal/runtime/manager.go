package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/llmariner/inference-manager/engine/internal/autoscaler"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ErrRequestCanceled is returned when the request is canceled.
var ErrRequestCanceled = errors.New("request is canceled")

// NewManager creates a new runtime manager.
func NewManager(
	k8sClient client.Client,
	rtClientFactory ClientFactory,
	autoscaler autoscaler.Registerer,
) *Manager {
	return &Manager{
		k8sClient:       k8sClient,
		rtClientFactory: rtClientFactory,
		autoscaler:      autoscaler,
		runtimes:        make(map[string]runtime),
	}
}

// Manager manages runtimes.
type Manager struct {
	k8sClient       client.Client
	rtClientFactory ClientFactory
	autoscaler      autoscaler.Registerer

	// runtimes is keyed by model ID.
	runtimes map[string]runtime
	mu       sync.RWMutex
}

func newPendingRuntime(name string) runtime {
	return runtime{name: name, ready: false, replicas: int32(0), waitCh: make(chan struct{})}
}

func newReadyRuntime(name string, address string, gpu, replicas int32) runtime {
	return runtime{name: name, ready: true, address: address, gpu: gpu, replicas: replicas}
}

// ModelRuntimeInfo is the info of a model runtime.
type ModelRuntimeInfo struct {
	// ID is model ID.
	ID string
	// GPU is the total GPU allocated for the model.
	GPU   int32
	Ready bool
}

type runtime struct {
	name  string
	ready bool
	// replicas is the number of ready replicas.
	replicas int32
	// gpu is the GPU limit of the runtime.
	gpu       int32
	errReason string
	// address is empty when the runtime is not ready.
	address string
	// waitCh is used when the runtime is not ready.
	waitCh chan struct{}
}

func getGPU(sts *appsv1.StatefulSet) int32 {
	gpu := int32(0)
	for _, con := range sts.Spec.Template.Spec.Containers {
		limit := con.Resources.Limits
		if limit == nil {
			continue
		}

		// TODO(guangrui): Support non-Nvidia GPU.
		v, ok := limit[nvidiaGPUResource]
		if !ok {
			continue
		}
		count, ok := v.AsInt64()
		if !ok {
			continue
		}
		gpu += int32(count)
	}
	return gpu
}

func (m *Manager) addRuntime(modelID string, sts appsv1.StatefulSet) (added bool, ready bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runtimes[modelID]; ok {
		return false, false, nil
	}
	var rt runtime
	ready = sts.Status.ReadyReplicas > 0
	if ready {
		c, err := m.rtClientFactory.New(modelID)
		if err != nil {
			return false, false, err
		}
		rt = newReadyRuntime(sts.Name, c.GetAddress(sts.Name), getGPU(&sts), sts.Status.ReadyReplicas)
	} else {
		rt = newPendingRuntime(sts.Name)
	}
	m.runtimes[modelID] = rt
	return true, ready, nil
}

func (m *Manager) deleteRuntime(name string) {
	var modelID string
	for id, r := range m.runtimes {
		if r.name == name {
			modelID = id
			break
		}
	}
	if modelID == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runtimes[modelID]; ok {
		if r.waitCh != nil {
			close(r.waitCh)
		}
		delete(m.runtimes, modelID)
	}
}

func (m *Manager) updateRuntimeReplicas(modelID string, replicas int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runtimes[modelID]
	if !ok {
		return fmt.Errorf("runtime for model %q is not found", modelID)
	}
	r.replicas = replicas
	m.runtimes[modelID] = r
	return nil
}

func (m *Manager) markRuntimeReady(name, modelID, address string, gpu, replicas int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runtimes[modelID]; ok && !m.runtimes[modelID].ready {
		if r.waitCh != nil {
			close(r.waitCh)
		}
		m.runtimes[modelID] = newReadyRuntime(name, address, gpu, replicas)
	}
}

func (m *Manager) markRuntimeIsPending(name, modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runtimes[modelID]; ok && r.ready {
		m.runtimes[modelID] = newPendingRuntime(name)
	}
}

func (m *Manager) cancelWaitingRequests(modelID, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runtimes[modelID]; ok && !m.runtimes[modelID].ready {
		// cancel the current waiting channel, but recreate to avoid panic.
		close(r.waitCh)
		r.waitCh = make(chan struct{})
		r.errReason = reason
		m.runtimes[modelID] = r
	}
}

func (m *Manager) isReady(modelID string) (bool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runtimes[modelID]
	return r.ready, ok
}

func (m *Manager) errReason(modelID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runtimes[modelID]
	if ok {
		return r.errReason, r.errReason != ""
	}
	return "", false
}

// GetLLMAddress returns the address of the LLM.
func (m *Manager) GetLLMAddress(modelID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.runtimes[modelID]; ok && r.ready {
		return r.address, nil
	}
	return "", fmt.Errorf("runtime for model %q is not ready", modelID)
}

// ListSyncedModels returns the list of models that are synced.
func (m *Manager) ListSyncedModels() []ModelRuntimeInfo {
	return m.listModels(true)
}

// ListInProgressModels returns the list of models that are in progress.
func (m *Manager) ListInProgressModels() []ModelRuntimeInfo {
	return m.listModels(false)
}

func (m *Manager) listModels(ready bool) []ModelRuntimeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var ms []ModelRuntimeInfo
	for id, r := range m.runtimes {
		if r.ready == ready {
			ms = append(ms, ModelRuntimeInfo{
				ID:    id,
				GPU:   r.gpu * r.replicas,
				Ready: r.ready,
			})
		}
	}
	return ms
}

// PullModel pulls the model from the model manager.
func (m *Manager) PullModel(ctx context.Context, modelID string) error {
	log := ctrl.LoggerFrom(ctx)
	m.mu.Lock()
	r, ok := m.runtimes[modelID]
	if ok {
		m.mu.Unlock()
		if r.ready {
			log.V(4).Info("Runtime is already ready", "model", modelID)
			return nil
		}
	} else {
		client, err := m.rtClientFactory.New(modelID)
		if err != nil {
			return err
		}
		name := client.GetName(modelID)

		r = newPendingRuntime(name)
		m.runtimes[modelID] = r
		m.mu.Unlock()

		cleanup := func() {
			m.mu.Lock()
			delete(m.runtimes, modelID)
			if r.waitCh != nil {
				close(r.waitCh)
			}
			m.mu.Unlock()
		}

		sts, err := client.DeployRuntime(ctx, modelID, false)
		if err != nil {
			cleanup()
			return err
		}
		if err := m.autoscaler.Register(ctx, modelID, sts); err != nil {
			log.Error(err, "Failed to register autoscaler")
			return err
		}
		if sts.Status.ReadyReplicas > 0 {
			// If this is called before the first cache sync of the reconciler
			// is complete, the existing statefulset(STS) for the runtime is not
			// registered in the manager's managed runtime map. If the runtime's
			// STS is already ready, mark the runtime as ready without waiting
			// for the reconciler (leader-election component) to process it.
			m.markRuntimeReady(sts.Name, modelID, client.GetAddress(sts.Name), getGPU(sts), sts.Status.ReadyReplicas)
		}
	}
	if reason, ok := m.errReason(modelID); ok {
		// The runtime is already in an error state.
		log.V(4).Info("Runtime is in an error state", "reason", reason)
		return ErrRequestCanceled
	}
	log.Info("Waiting for runtime to be ready", "model", modelID)
	select {
	case <-r.waitCh:
		if _, ok := m.errReason(modelID); ok {
			// This will happen when the `cancelWaitingRequests` is called.
			return ErrRequestCanceled
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Reconcile reconciles the runtime.
func (m *Manager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	var sts appsv1.StatefulSet
	if err := m.k8sClient.Get(ctx, req.NamespacedName, &sts); err != nil {
		if apierrors.IsNotFound(err) {
			m.deleteRuntime(req.Name)
			m.autoscaler.Unregister(req.NamespacedName)
			log.Info("Runtime is deleted")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	modelID := sts.GetAnnotations()[modelAnnotationKey]

	// TODO(aya): remove this block after a few releases.
	// This is for the backward compatibility. The controller no longer
	// adds the finalizer to the statefulset.
	if controllerutil.ContainsFinalizer(&sts, finalizerKey) {
		patch := client.MergeFrom(&sts)
		newSts := sts.DeepCopy()
		controllerutil.RemoveFinalizer(newSts, finalizerKey)
		if err := client.IgnoreNotFound(m.k8sClient.Patch(ctx, newSts, patch)); err != nil {
			log.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	log.Info("Reconciling runtime...", "model", modelID)
	ready, ok := m.isReady(modelID)
	if !ok {
		// If an statefulset for the unregistered runtime is found,
		// the manager registers it to the managed runtime map.
		// This would call when the manager synchronizes the cache
		// for the first time or when another engine creates a runtime.
		log.V(4).Info("Registering runtime", "model", modelID)
		if added, ready, err := m.addRuntime(modelID, sts); err != nil {
			log.Error(err, "Failed to add runtime")
			return ctrl.Result{}, err
		} else if added {
			if err := m.autoscaler.Register(ctx, modelID, &sts); err != nil {
				log.Error(err, "Failed to register autoscaler")
				return ctrl.Result{}, err
			}
			if !ready {
				if yes, err := allChildrenUnschedulable(ctx, m.k8sClient, sts); err != nil {
					log.V(2).Error(err, "Failed to check unschedulable children")
					return ctrl.Result{}, err
				} else if yes {
					m.cancelWaitingRequests(modelID, corev1.PodReasonUnschedulable)
					log.V(1).Info("Pod is unschedulable")
				}
			}
		}
		return ctrl.Result{}, nil
	}

	if ready {
		// The runtime has already been ready.
		if sts.Status.Replicas == 0 {
			m.markRuntimeIsPending(sts.Name, modelID)
			log.Info("Runtime is scale down to zero")
		} else {
			if err := m.updateRuntimeReplicas(modelID, sts.Status.ReadyReplicas); err != nil {
				log.Error(err, "Failed to update runtime replicas")
				return ctrl.Result{}, err
			}
			log.V(10).Info("Runtime replicas are updated", "modelID", modelID, "replicas", sts.Status.ReadyReplicas)
		}
		return ctrl.Result{}, nil
	}

	if sts.Status.ReadyReplicas > 0 {
		// The runtime has just became ready.
		client, err := m.rtClientFactory.New(modelID)
		if err != nil {
			log.Error(err, "Failed to create runtime client")
			return ctrl.Result{}, err
		}
		addr := client.GetAddress(sts.Name)
		// Double check if the statefulset is reachable as it might take some time for the service is being updated.
		req := &http.Request{
			Method: http.MethodGet,
			URL:    &url.URL{Scheme: "http", Host: addr},
		}
		if _, err := http.DefaultClient.Do(req); err != nil {
			retryAfter := 500 * time.Millisecond
			log.Error(err, "Still unable to reach the runtime endpoint", "retry-after", retryAfter)
			return ctrl.Result{RequeueAfter: retryAfter}, err
		}
		m.markRuntimeReady(sts.Name, modelID, addr, getGPU(&sts), sts.Status.ReadyReplicas)
		log.Info("Runtime is ready")
		return ctrl.Result{}, nil
	}

	// The runtime is still not ready.

	if yes, err := allChildrenUnschedulable(ctx, m.k8sClient, sts); err != nil {
		log.V(2).Error(err, "Failed to check unschedulable children")
		return ctrl.Result{}, err
	} else if yes {
		m.cancelWaitingRequests(modelID, corev1.PodReasonUnschedulable)
		log.V(1).Info("Pod is unschedulable")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the runtime manager with the given controller manager.
func (m *Manager) SetupWithManager(mgr ctrl.Manager, leaderElection bool) error {
	filterByLabel := (predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetLabels()["app.kubernetes.io/created-by"] == managerName
	}))
	return setupWithManager(mgr, leaderElection, m, filterByLabel)
}

func setupWithManager(mgr ctrl.Manager, leaderElection bool, r reconcile.Reconciler, predicates predicate.Predicate) error {
	constructer := func(r *reconcile.Request) logr.Logger {
		if r != nil {
			return mgr.GetLogger().WithValues("runtime", r.NamespacedName)
		}
		return mgr.GetLogger()
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}, builder.WithPredicates(predicates)).
		Watches(&corev1.Pod{},
			handler.TypedEnqueueRequestForOwner[client.Object](mgr.GetScheme(), mgr.GetRESTMapper(), &appsv1.StatefulSet{}, handler.OnlyControllerOwner()),
			builder.WithPredicates(predicates)).
		WithLogConstructor(constructer).
		// To share the runtime deletion event, disable the leader election
		// for this controller if the processor disables the leader election.
		WithOptions(controller.Options{NeedLeaderElection: ptr.To(leaderElection)}).
		Complete(r)
}

func allChildrenUnschedulable(ctx context.Context, k8sClient client.Client, sts appsv1.StatefulSet) (bool, error) {
	var podList corev1.PodList
	if err := k8sClient.List(ctx, &podList,
		client.InNamespace(sts.Namespace),
		client.MatchingLabels{"app.kubernetes.io/instance": sts.Name},
	); err != nil {
		return false, err
	}
	var cnt, unschedulable int
	for _, pod := range podList.Items {
		if pod.Labels["controller-revision-hash"] != sts.Status.CurrentRevision {
			continue
		}
		cnt++
		if yes, _ := isPodUnschedulable(pod); yes {
			unschedulable++
		}
	}
	return unschedulable > 0 && cnt == unschedulable, nil
}

func isPodUnschedulable(pod corev1.Pod) (bool, string) {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled {
			return cond.Status == corev1.ConditionFalse &&
					cond.Reason == corev1.PodReasonUnschedulable,
				cond.Message
		}
	}
	return false, ""
}
