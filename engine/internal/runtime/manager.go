package runtime

import (
	"context"
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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	errMsgUnreachableRuntime = "runtime is unreachable"
	errMsgDeletedRuntime     = "runtime is deleted"
)

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
		eventCh:         make(chan interface{}),

		runtimeReadinessChecker: &runtimeReadinessCheckerImpl{},
		loraAdapterLoader:       &loraAdapterLoaderImpl{},

		readinessCheckMaxRetryCount: 3,
		readinessCheckRetryInterval: 500 * time.Millisecond,
	}
}

type pullModelEvent struct {
	modelID     string
	readyWaitCh chan string
}

type deleteModelEvent struct {
	modelID     string
	eventWaitCh chan struct{}
}

type reconcileStatefulSetEvent struct {
	namespacedName types.NamespacedName
	eventWaitCh    chan struct{}
}

type readinessCheckEvent struct {
	modelID string

	address  string
	gpu      int32
	replicas int32

	retryCount int
}

type loraAdapterLoader interface {
	load(ctx context.Context, modelID string, pullerAddr string, vllmAddr string) error
	unload(ctx context.Context, vllmAddr string, modelID string) error
}

type runtimeReadinessChecker interface {
	check(addr string) error
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

	eventCh chan interface{}

	mu sync.RWMutex

	runtimeReadinessChecker runtimeReadinessChecker
	loraAdapterLoader       loraAdapterLoader

	// readinessCheckMaxRetryCount is the maximum number of retries for the readiness check.
	readinessCheckMaxRetryCount int
	// readinessCheckRetryInterval is the interval for the readiness check.
	readinessCheckRetryInterval time.Duration
}

func (m *Manager) deleteRuntimeByName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, r := range m.runtimes {
		if r.name != name {
			continue
		}

		r.closeWaitChs(errMsgDeletedRuntime)
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
	r.closeWaitChs(errMsgDeletedRuntime)
	delete(m.runtimes, modelID)
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

// RunStateMachine runs the state machine for the manager.
func (m *Manager) RunStateMachine(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-m.eventCh:
			switch e := e.(type) {
			case *pullModelEvent:
				if err := m.processPullModelEvent(ctx, e); err != nil {
					return err
				}
			case *deleteModelEvent:
				if err := m.processDeleteModelEvent(ctx, e); err != nil {
					return err
				}
			case *reconcileStatefulSetEvent:
				if err := m.processReconcileStatefulSetEvent(ctx, e); err != nil {
					return err
				}
			case *readinessCheckEvent:
				if err := m.processReadinessCheckEvent(ctx, e); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown event type: %T", e)
			}
		}
	}
}

func (m *Manager) processPullModelEvent(ctx context.Context, e *pullModelEvent) error {
	log := ctrl.LoggerFrom(ctx)

	client, err := m.rtClientFactory.New(e.modelID)
	if err != nil {
		return err
	}

	m.mu.Lock()

	if r, ok := m.runtimes[e.modelID]; ok {
		// The runtime is ready, or the pull is already in progress.
		if r.ready {
			log.Info("Runtime is ready. No need to pull the model", "modelID", e.modelID)
			close(e.readyWaitCh)
		} else {
			log.Info("Pull is in progress. Waiting for the runtime to be ready", "modelID", e.modelID)
			r.waitChs = append(r.waitChs, e.readyWaitCh)
		}
		m.mu.Unlock()
		return nil
	}

	// TODO(kenji): Revisit the lockig if this takes a long time.
	isDynamicLoRAApplicable, baseModelID, err := m.isDynamicLoRARloadingApplicable(ctx, e.modelID)
	if err != nil {
		return err
	}

	if !isDynamicLoRAApplicable {
		log.Info("Creating a new pending runtime", "modelID", e.modelID)
		r := newPendingRuntime(client.GetName(e.modelID))
		r.waitChs = append(r.waitChs, e.readyWaitCh)

		m.runtimes[e.modelID] = r
		m.mu.Unlock()

		sts, err := client.DeployRuntime(ctx, e.modelID, false)
		if err != nil {
			return err
		}

		if err := m.autoscaler.Register(ctx, e.modelID, sts); err != nil {
			return err
		}

		go func() {
			m.eventCh <- &reconcileStatefulSetEvent{
				namespacedName: types.NamespacedName{
					Name:      sts.Name,
					Namespace: sts.Namespace,
				},
				eventWaitCh: make(chan struct{}),
			}
		}()

		return nil
	}

	br, ok := m.runtimes[baseModelID]
	if !ok {
		br = newPendingRuntime(client.GetName(baseModelID))
		m.runtimes[baseModelID] = br
	}

	if !br.ready {
		log.Info("Base model is not ready. Request a pull", "baseModelID", baseModelID, "modelID", e.modelID)
		br.addPendingPullModelRequest(e)
		return nil
	}

	log.Info("Base model is ready. Load LoRA adapter", "baseModelID", baseModelID, "modelID", e.modelID)

	r := newPendingRuntime(client.GetName(e.modelID))
	r.waitChs = append(r.waitChs, e.readyWaitCh)
	r.isDynamicallyLoadedLoRA = true
	m.runtimes[e.modelID] = r
	m.mu.Unlock()

	podIP, err := m.findLoRAAdapterLoadingTargetPod(ctx, e.modelID, br.name)
	if err != nil {
		return fmt.Errorf("find LoRA adapter loading target pod: %s", err)
	}

	log.Info("Found pod for LoRA adapter loading", "podIP", podIP)

	pullerAddr := fmt.Sprintf("%s:%d", podIP, m.vllmConfig.PullerPort)
	vllmAddr := client.GetAddress(podIP)
	if err := m.loraAdapterLoader.load(ctx, e.modelID, pullerAddr, vllmAddr); err != nil {
		return fmt.Errorf("load LoRA adapter: %s", err)
	}

	go func() {
		m.eventCh <- &readinessCheckEvent{
			modelID:  e.modelID,
			address:  vllmAddr,
			gpu:      br.gpu,
			replicas: 1,
		}
	}()

	return nil
}

func (m *Manager) isDynamicLoRARloadingApplicable(ctx context.Context, modelID string) (bool, string, error) {
	if !m.vllmConfig.DynamicLoRALoading {
		return false, "", nil
	}

	client, err := m.rtClientFactory.New(modelID)
	if err != nil {
		return false, "", err
	}

	if client.RuntimeName() != config.RuntimeNameVLLM {
		return false, "", nil
	}

	model, err := m.modelClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: modelID,
	})
	if err != nil {
		return false, "", err
	}

	if model.IsBaseModel {
		return false, "", nil
	}

	return true, model.BaseModelId, nil
}

func (m *Manager) findLoRAAdapterLoadingTargetPod(
	ctx context.Context,
	modelID string,
	stsName string,
) (string, error) {
	client, err := m.rtClientFactory.New(modelID)
	if err != nil {
		return "", err
	}

	pods, err := listPods(ctx, m.k8sClient, client.Namespace(), stsName)
	if err != nil {
		return "", err
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
		// TODO(kenji): Add a retry or gracefully handle.
		return "", fmt.Errorf("no ready pod found")
	}

	return podIP, nil
}

func (m *Manager) processDeleteModelEvent(ctx context.Context, e *deleteModelEvent) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Deleting model...", "modelID", e.modelID)

	defer close(e.eventWaitCh)

	m.mu.Lock()
	r, ok := m.runtimes[e.modelID]
	m.mu.Unlock()
	if !ok {
		log.V(4).Info("Runtime does not exist", "modelID", e.modelID)
		return nil
	}

	if r.isDynamicallyLoadedLoRA {
		log.Info("Unloading the LoRA adapter from the runtime", "modelID", e.modelID)
		if err := m.loraAdapterLoader.unload(ctx, r.address, e.modelID); err != nil {
			return fmt.Errorf("unload LoRA adapter: %s", err)
		}
		m.deleteRuntimeByModelID(e.modelID)
		return nil
	}

	// TODO(kenji): Revisit how to handle the deletion of the base-model when
	// its runtime has a LoRA adapter. Deleting the statefulset will delete
	// both the base model and the LoRA adapter.

	client, err := m.rtClientFactory.New(e.modelID)
	if err != nil {
		return err
	}

	log.Info("Deleting runtime...", "modelID", e.modelID)
	if err := client.DeleteRuntime(ctx, e.modelID); err != nil {
		return err
	}

	// No need to call m.deleteRuntimeByModelID() as Reconcile will delete the runtime.

	return nil
}

func (m *Manager) processReconcileStatefulSetEvent(ctx context.Context, e *reconcileStatefulSetEvent) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling statefulset...", "modelID", e.namespacedName.Name)

	defer close(e.eventWaitCh)

	var sts appsv1.StatefulSet
	if err := m.k8sClient.Get(ctx, e.namespacedName, &sts); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		log.Info("Deleting runtime...", "modelID", e.namespacedName.Name)
		m.deleteRuntimeByName(e.namespacedName.Name)
		m.autoscaler.Unregister(e.namespacedName)
		return nil
	}

	modelID := sts.GetAnnotations()[modelAnnotationKey]

	var unschedulable bool
	if sts.Status.ReadyReplicas == 0 {
		var err error
		unschedulable, err = allChildrenUnschedulable(ctx, m.k8sClient, sts)
		if err != nil {
			return err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	rt, ok := m.runtimes[modelID]
	if !ok {
		// Create a new pending runtime and follow the same flow.
		rt = newPendingRuntime(sts.Name)
		m.runtimes[modelID] = rt

		// TODO(kenji): Reconsider if Register blocks other calls for a long of time.
		if err := m.autoscaler.Register(ctx, modelID, &sts); err != nil {
			return err
		}
	}

	if rt.ready {
		// The runtime has already been ready.

		if sts.Status.Replicas == 0 {
			log.Info("Runtime is scale down to zero")
			rt.ready = false
			return nil
		}

		// Update the runtime replicas.
		log.V(10).Info("Runtime replicas are updated", "modelID", modelID, "replicas", sts.Status.ReadyReplicas)
		rt.replicas = sts.Status.Replicas
		return nil
	}

	if sts.Status.ReadyReplicas == 0 {
		// The runtime is still not ready.
		if !unschedulable {
			return nil
		}

		log.V(1).Info("Pod is unschedulable")
		rt.closeWaitChs(corev1.PodReasonUnschedulable)

		return nil
	}

	// The statefulset is ready. Move to the final readiness check.

	addr, err := m.getAddress(modelID, rt.name)
	if err != nil {
		return err
	}

	go func() {
		m.eventCh <- &readinessCheckEvent{
			modelID:  modelID,
			address:  addr,
			gpu:      getGPU(&sts),
			replicas: sts.Status.ReadyReplicas,
		}
	}()

	return nil
}

func (m *Manager) processReadinessCheckEvent(ctx context.Context, e *readinessCheckEvent) error {
	log := ctrl.LoggerFrom(ctx)

	// If this is a Lora adapter, we need to check if the address is reachable.
	m.mu.Lock()
	rt, ok := m.runtimes[e.modelID]
	m.mu.Unlock()
	if !ok {
		log.Info("Runtime does not exist", "modelID", e.modelID)
		return nil
	}

	// The runtime has just became ready.
	if err := m.runtimeReadinessChecker.check(e.address); err != nil {
		log.Error(err, "Runtime is not ready", "modelID", e.modelID, "address", e.address)
		if e.retryCount >= m.readinessCheckMaxRetryCount {
			log.Info("runtime is not reachable", "modelID", e.modelID, "retryCount", e.retryCount)
			rt.closeWaitChs(errMsgUnreachableRuntime)
			return nil
		}

		log.Info("Runtime is not reachable. Retrying...", "modelID", e.modelID, "retryCount", e.retryCount)
		go func() {
			time.Sleep(m.readinessCheckRetryInterval)
			m.eventCh <- &readinessCheckEvent{
				modelID:    e.modelID,
				address:    e.address,
				gpu:        e.gpu,
				replicas:   e.replicas,
				retryCount: e.retryCount + 1,
			}
		}()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	log.Info("Runtime is ready")

	rt.becomeReady(e.address, e.gpu, e.replicas)

	go func(es []*pullModelEvent) {
		for _, e := range es {
			log.Info("Requeuing pending pull request", "modelID", e.modelID)
			m.eventCh <- e
		}
	}(rt.dequeuePendingPullModelRequests())

	rt.closeWaitChs("")

	return nil
}

// PullModel pulls the model from the model manager.
func (m *Manager) PullModel(ctx context.Context, modelID string) error {
	waitCh := make(chan string)

	m.eventCh <- &pullModelEvent{
		modelID:     modelID,
		readyWaitCh: waitCh,
	}

	select {
	case errReason := <-waitCh:
		if errReason != "" {
			// This will happen when the `cancelWaitingRequests` is called.
			return ErrRequestCanceled
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// DeleteModel deletes the model from the model manager.
func (m *Manager) DeleteModel(ctx context.Context, modelID string) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Deleting model", "modelID", modelID)

	waitCh := make(chan struct{})
	m.eventCh <- &deleteModelEvent{
		modelID:     modelID,
		eventWaitCh: waitCh,
	}

	select {
	case <-waitCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	log.Info("Deleted model", "modelID", modelID)
	return nil
}

// Reconcile reconciles the runtime.
func (m *Manager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	waitCh := make(chan struct{})
	m.eventCh <- &reconcileStatefulSetEvent{
		namespacedName: req.NamespacedName,
		eventWaitCh:    waitCh,
	}

	select {
	case <-waitCh:
	case <-ctx.Done():
		return ctrl.Result{}, ctx.Err()
	}

	return ctrl.Result{}, nil
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

type runtimeReadinessCheckerImpl struct {
}

func (*runtimeReadinessCheckerImpl) check(addr string) error {
	// TODO(kenji): Replace this with get model?
	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Scheme: "http", Host: addr},
	}
	_, err := http.DefaultClient.Do(req)
	return err
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
