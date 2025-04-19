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
	"github.com/llmariner/inference-manager/engine/internal/config"
	mv1 "github.com/llmariner/model-manager/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ErrRequestCanceled is returned when the request is canceled.
var ErrRequestCanceled = errors.New("request is canceled")

func newPendingRuntime(name string) *runtime {
	return &runtime{
		name:   name,
		ready:  false,
		waitCh: make(chan struct{}),
	}
}

func newPendingRuntimeWithErrorReason(name, errReason string) *runtime {
	return &runtime{
		name:      name,
		ready:     false,
		waitCh:    make(chan struct{}),
		errReason: errReason,
	}
}

type runtime struct {
	name  string
	ready bool

	// waitCh is used when the runtime is not ready.
	waitCh    chan struct{}
	errReason string

	// address is empty when the runtime is not ready.
	address string
	// isDynamicallyLoadedLoRA is true if the model is dynamically loaded LoRA.
	isDynamicallyLoadedLoRA bool
	// replicas is the number of ready replicas.
	replicas int32
	// gpu is the GPU limit of the runtime.
	gpu int32
}

func (r *runtime) updateStateToPending() {
	r.ready = false
	r.waitCh = make(chan struct{})
}

func (r *runtime) setErrorReason(errReason string) {
	// cancel the current waiting channel, but recreate to avoid panic.
	close(r.waitCh)
	r.waitCh = make(chan struct{})
	r.errReason = errReason
}

func (r *runtime) updateStateToReady(
	address string,
	isDynamicallyLoadedLoRA bool,
	gpu,
	replicas int32,
) {
	close(r.waitCh)
	r.errReason = ""

	r.ready = true
	r.address = address
	r.isDynamicallyLoadedLoRA = isDynamicallyLoadedLoRA
	r.gpu = gpu
	r.replicas = replicas
}

func newReadyRuntime(
	name string,
	address string,
	isDynamicallyLoadedLoRA bool,
	gpu,
	replicas int32,
) *runtime {
	return &runtime{
		name:                    name,
		ready:                   true,
		address:                 address,
		isDynamicallyLoadedLoRA: isDynamicallyLoadedLoRA,
		gpu:                     gpu,
		replicas:                replicas,
	}
}

// NewManager creates a new runtime manager.
func NewManager(
	k8sClient client.Client,
	rtClientFactory ClientFactory,
	autoscaler autoscaler.Registerer,
	modelClient modelClient,
	vllmConfig config.VLLMConfig,
) *Manager {
	return &Manager{
		k8sClient:       k8sClient,
		rtClientFactory: rtClientFactory,
		autoscaler:      autoscaler,
		modelClient:     modelClient,
		vllmConfig:      vllmConfig,
		runtimes:        make(map[string]*runtime),
	}
}

// Manager manages runtimes.
type Manager struct {
	k8sClient       client.Client
	rtClientFactory ClientFactory
	autoscaler      autoscaler.Registerer

	modelClient modelClient
	vllmConfig  config.VLLMConfig

	// runtimes is keyed by model ID.
	runtimes map[string]*runtime
	mu       sync.RWMutex
}

func (m *Manager) deleteRuntimeByName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, r := range m.runtimes {
		if r.name != name {
			continue
		}

		if r.waitCh != nil {
			close(r.waitCh)
		}
		delete(m.runtimes, id)
		break
	}
}

func (m *Manager) deleteRuntimeByModelID(modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.runtimes[modelID]
	if !ok {
		return
	}
	if r.waitCh != nil {
		close(r.waitCh)
	}
	delete(m.runtimes, modelID)
}

func (m *Manager) markRuntimeReady(
	name,
	modelID,
	address string,
	isDynamicallyLoadedLoRA bool,
	gpu,
	replicas int32,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.runtimes[modelID]
	if !ok || r.ready {
		return
	}

	r.updateStateToReady(address, isDynamicallyLoadedLoRA, gpu, replicas)
}

func (m *Manager) markRuntimeReadyFromStatefulSet(
	sts *appsv1.StatefulSet,
	modelID,
	address string,
) {
	m.markRuntimeReady(
		sts.Name,
		modelID,
		address,
		false,
		getGPU(sts),
		sts.Status.ReadyReplicas,
	)
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

// ModelRuntimeInfo is the info of a model runtime.
type ModelRuntimeInfo struct {
	// ID is model ID.
	ID string
	// GPU is the total GPU allocated for the model.
	GPU   int32
	Ready bool
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
		if r.ready != ready {
			continue
		}

		ms = append(ms, ModelRuntimeInfo{
			ID:    id,
			GPU:   r.gpu * r.replicas,
			Ready: r.ready,
		})
	}
	return ms
}

// PullModel pulls the model from the model manager.
func (m *Manager) PullModel(ctx context.Context, modelID string) error {
	log := ctrl.LoggerFrom(ctx)

	if err := m.createNewRuntimeToPull(ctx, modelID); err != nil {
		log.Error(err, "Failed to pull model")
		return err
	}

	if err := m.waitForRuntimeToBeReady(ctx, modelID); err != nil {
		return err
	}
	return nil
}

func (m *Manager) createNewRuntimeToPull(ctx context.Context, modelID string) error {
	log := ctrl.LoggerFrom(ctx)

	client, err := m.rtClientFactory.New(modelID)
	if err != nil {
		return err
	}

	m.mu.Lock()
	if _, ok := m.runtimes[modelID]; ok {
		m.mu.Unlock()
		// The runtime is ready or the pull is already in progress.
		return nil
	}
	// First create a pending runtime.
	m.runtimes[modelID] = newPendingRuntime(client.GetName(modelID))
	m.mu.Unlock()

	cleanup := func() {
		m.deleteRuntimeByModelID(modelID)
	}

	if client.RuntimeName() == config.RuntimeNameVLLM && m.vllmConfig.DynamicLoRALoading {
		ok, err := m.deployToExistingRuntimeIfFineTunedModel(ctx, modelID, client)
		if err != nil {
			cleanup()
			return err
		}
		if ok {
			// The runtime is already registered.
			return nil
		}
		// Fall back to the regular deployment flow.
	}

	sts, err := client.DeployRuntime(ctx, modelID, false)
	if err != nil {
		cleanup()
		return err
	}

	if err := m.autoscaler.Register(ctx, modelID, sts); err != nil {
		log.Error(err, "Failed to register autoscaler")
		cleanup()
		return err
	}

	if sts.Status.ReadyReplicas == 0 {
		return nil
	}

	// If this is called before the first cache sync of the reconciler
	// is complete, the existing statefulset(STS) for the runtime is not
	// registered in the manager's managed runtime map.

	// If the runtime's STS is already ready, mark the runtime as
	// ready without waiting for the reconciler (leader-election
	// component) to process it.
	m.markRuntimeReadyFromStatefulSet(sts, modelID, client.GetAddress(sts.Name))

	return nil
}

func (m *Manager) waitForRuntimeToBeReady(ctx context.Context, modelID string) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Waiting for runtime to be ready", "model", modelID)

	m.mu.Lock()
	r, ok := m.runtimes[modelID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("runtime for model %q is not found", modelID)
	}
	if r.ready {
		log.V(4).Info("Runtime is already ready", "model", modelID)
		return nil
	}

	if reason, ok := m.errReason(modelID); ok {
		// The runtime is already in an error state.
		log.V(4).Info("Runtime is in an error state", "reason", reason)
		return ErrRequestCanceled
	}

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

func (m *Manager) deployToExistingRuntimeIfFineTunedModel(
	ctx context.Context,
	modelID string,
	rclient Client,
) (bool, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Deploying fine-tuned model to existing runtime", "modelID", modelID)
	model, err := m.modelClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: modelID,
	})
	if err != nil {
		return false, err
	}

	if model.IsBaseModel {
		log.Info("Model is a base model, skipping deployment to existing runtime", "modelID", modelID)
		return false, nil
	}

	// Check if there is already a runtime for the base model.
	m.mu.Lock()
	br, ok := m.runtimes[model.BaseModelId]
	m.mu.Unlock()
	if !ok {
		log.Info("No existing runtime for base model", "baseModel", model.BaseModelId)
		return false, nil
	}

	log.Info("Found existing runtime for base model", "baseModel", model.BaseModelId)

	if err := m.waitForRuntimeToBeReady(ctx, model.BaseModelId); err != nil {
		log.Error(err, "Failed to wait for runtime to be ready", "baseModel", model.BaseModelId)
		return false, err
	}

	// TODO(kenji): Create a service and endpoint?
	// TODO(kenji): Be able to register the existing fine-tuned models when the engine starts up.
	// TODO(kenji): Dynamic rebalancing.

	// TODO(kenji): When other engine loads a LoRA adapter, the engine won't know it.
	// There is some periodic syncing to check the LoRA adapter status.

	log.Info("Loading LoRA adapter to existing runtime", "modelID", modelID)

	pods, err := listPods(ctx, m.k8sClient, rclient.Namespace(), br.name)
	if err != nil {
		return false, err
	}

	var podIP string
	// TODO(kenji): Pick up the least-loaded ready pod.
	for _, pod := range pods {
		if ip := pod.Status.PodIP; ip != "" {
			podIP = ip
			break
		}
	}

	if podIP == "" {
		return false, fmt.Errorf("no ready pod found")
	}

	log.Info("Found pod for LoRA adapter loading", "podIP", podIP)

	pullerAddr := fmt.Sprintf("%s:%d", podIP, m.vllmConfig.PullerPort)
	vllmAddr := rclient.GetAddress(podIP)
	if err := loadLoRAAdapter(ctx, modelID, pullerAddr, vllmAddr); err != nil {
		return false, fmt.Errorf("load LoRA adapter: %s", err)
	}

	log.Info("Model is ready", "modelID", modelID)

	m.markRuntimeReady(
		rclient.GetName(modelID),
		modelID,
		vllmAddr,
		true, /* isDynamicallyLoadedLoRA */
		br.gpu,
		1, /* replicas */
	)
	return true, nil
}

// DeleteModel deletes the model from the model manager.
func (m *Manager) DeleteModel(ctx context.Context, modelID string) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Deleting model", "model", modelID)

	m.mu.Lock()
	r, ok := m.runtimes[modelID]
	m.mu.Unlock()
	if !ok {
		log.V(4).Info("Runtime does not exist", "model", modelID)
		return nil
	}

	if r.isDynamicallyLoadedLoRA {
		log.Info("Unloading the LoRA adapter from the runtime", "model", modelID)
		if err := unloadLoRAAdapter(ctx, r.address, modelID); err != nil {
			return fmt.Errorf("unload LoRA adapter: %s", err)
		}
	} else {
		// TODO(kenji): Revisit how to handle the deletion of the base-model when
		// its runtime has a LoRA adapter. Deleting the statefulset will delete
		// both the base model and the LoRA adapter.

		client, err := m.rtClientFactory.New(modelID)
		if err != nil {
			return err
		}

		if err := client.DeleteRuntime(ctx, modelID); err != nil {
			return err
		}
	}

	// No need to call m.deleteRuntimeByModelID() as Reconcile will delete the runtime.

	log.Info("Deleted model", "model", modelID)
	return nil
}

// Reconcile reconciles the runtime.
func (m *Manager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	var sts appsv1.StatefulSet
	if err := m.k8sClient.Get(ctx, req.NamespacedName, &sts); err != nil {
		if apierrors.IsNotFound(err) {
			m.deleteRuntimeByName(req.Name)
			m.autoscaler.Unregister(req.NamespacedName)
			log.Info("Runtime is deleted")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var unschedulable bool
	if sts.Status.ReadyReplicas == 0 {
		var err error
		unschedulable, err = allChildrenUnschedulable(ctx, m.k8sClient, sts)
		if err != nil {
			log.V(2).Error(err, "Failed to check unschedulable children")
			return ctrl.Result{}, err
		}
	}

	modelID := sts.GetAnnotations()[modelAnnotationKey]
	log.Info("Reconciling runtime...", "model", modelID)

	m.mu.Lock()
	rt, ok := m.runtimes[modelID]
	if !ok {
		// If an statefulset for the unregistered runtime is found,
		// the manager registers it to the managed runtime map.
		//
		// This would happen when the manager synchronizes the cache
		// for the first time or when another engine creates a runtime.
		log.V(4).Info("Registering runtime", "model", modelID)

		rt, err := m.createNewRuntime(modelID, &sts, unschedulable)
		if err != nil {
			log.Error(err, "Failed to create new runtime")
			m.mu.Unlock()
			return ctrl.Result{}, err
		}
		m.runtimes[modelID] = rt

		m.mu.Unlock()

		// TODO(kenji): reconcile LoRA adapters?

		if err := m.autoscaler.Register(ctx, modelID, &sts); err != nil {
			log.Error(err, "Failed to register autoscaler")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	if rt.ready {
		// The runtime has already been ready.

		defer m.mu.Unlock()

		if sts.Status.Replicas == 0 {
			log.Info("Runtime is scale down to zero")
			rt.updateStateToPending()
			return ctrl.Result{}, nil
		}

		// Update the runtime replicas.
		log.V(10).Info("Runtime replicas are updated", "modelID", modelID, "replicas", sts.Status.ReadyReplicas)
		rt.replicas = sts.Status.Replicas
		return ctrl.Result{}, nil
	}

	if sts.Status.ReadyReplicas == 0 {
		// The runtime is still not ready.
		defer m.mu.Unlock()

		if !unschedulable {
			return ctrl.Result{}, nil
		}

		log.V(1).Info("Pod is unschedulable")
		rt.setErrorReason(corev1.PodReasonUnschedulable)

		return ctrl.Result{}, nil
	}

	m.mu.Unlock()

	// The runtime has just became ready.

	addr, err := m.getAddress(modelID, sts.Name)
	if err != nil {
		log.Error(err, "Failed to get addr")
		return ctrl.Result{}, err
	}

	// Double check if the statefulset is reachable as it might take some time for the service is being updated.
	hreq := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Scheme: "http", Host: addr},
	}
	if _, err := http.DefaultClient.Do(hreq); err != nil {
		retryAfter := 500 * time.Millisecond
		log.Error(err, "Still unable to reach the runtime endpoint", "retry-after", retryAfter)
		return ctrl.Result{RequeueAfter: retryAfter}, err
	}

	// TODO(kenji): Find LoRA adapters.

	m.markRuntimeReadyFromStatefulSet(&sts, modelID, addr)

	log.Info("Runtime is ready")
	return ctrl.Result{}, nil
}

func (m *Manager) createNewRuntime(
	modelID string,
	sts *appsv1.StatefulSet,
	unschedulable bool,
) (*runtime, error) {
	if sts.Status.ReadyReplicas == 0 {
		var errReason string
		if unschedulable {
			errReason = corev1.PodReasonUnschedulable
		}
		return newPendingRuntimeWithErrorReason(sts.Name, errReason), nil
	}

	address, err := m.getAddress(modelID, sts.Name)
	if err != nil {
		return nil, err
	}

	return newReadyRuntime(
		sts.Name,
		address,
		false,
		getGPU(sts),
		sts.Status.ReadyReplicas,
	), nil
}

func (m *Manager) getAddress(modelID, stsName string) (string, error) {
	client, err := m.rtClientFactory.New(modelID)
	if err != nil {
		return "", err
	}
	return client.GetAddress(stsName), nil
}

func (m *Manager) processLoRAAdapterUpdate(update *loRAAdapterStatusUpdate) {
	// TODO(kenji): Implement.
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
	pods, err := listPods(ctx, k8sClient, sts.Namespace, sts.Name)
	if err != nil {
		return false, err
	}
	var cnt, unschedulable int
	for _, pod := range pods {
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

func listPods(ctx context.Context, k8sClient client.Client, namespace, name string) ([]corev1.Pod, error) {
	var podList corev1.PodList
	if err := k8sClient.List(ctx, &podList,
		client.InNamespace(namespace),
		client.MatchingLabels{"app.kubernetes.io/instance": name},
	); err != nil {
		return nil, err
	}
	return podList.Items, nil
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
