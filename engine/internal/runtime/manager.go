package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-logr/logr"
	iv1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/engine/internal/autoscaler"
	"github.com/llmariner/inference-manager/engine/internal/config"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
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
	enableDynamicLoRALoading bool,
	pullerPort int,
	nimModels map[string]bool,
) *Manager {
	return &Manager{
		k8sClient:       k8sClient,
		rtClientFactory: rtClientFactory,
		autoscaler:      autoscaler,
		modelClient:     modelClient,

		enableDynamicLoRALoading: enableDynamicLoRALoading,
		pullerPort:               pullerPort,

		runtimes: make(map[string]*runtime),
		eventCh:  make(chan interface{}),

		runtimeReadinessChecker: &runtimeReadinessCheckerImpl{},
		loraAdapterLoadingTargetSelector: &loraAdapterLoadingTargetSelectorImpl{
			k8sClient:       k8sClient,
			rtClientFactory: rtClientFactory,
		},
		loraAdapterLoader: &loraAdapterLoaderImpl{},

		readinessCheckMaxRetryCount: 3,
		readinessCheckRetryInterval: 500 * time.Millisecond,

		nimModels: nimModels,
	}
}

type runtimeReadinessChecker interface {
	check(addr string) error
}

type loraAdapterLoadingTargetSelector interface {
	selectTarget(ctx context.Context, modelID string, stsName string) (*corev1.Pod, error)
}

type loraAdapterLoader interface {
	pullModel(ctx context.Context, pullerAddr, modelID string) error
	checkModelPullStatus(ctx context.Context, pullerAddr string, modelID string) (bool, error)
	load(ctx context.Context, vllmAddr, modelID string) error
	unload(ctx context.Context, vllmAddr, modelID string) error
}

// Manager manages runtimes.
type Manager struct {
	k8sClient       client.Client
	rtClientFactory ClientFactory
	autoscaler      autoscaler.Registerer

	modelClient modelClient

	enableDynamicLoRALoading bool
	pullerPort               int

	// runtimes is keyed by model ID.
	runtimes map[string]*runtime

	eventCh chan interface{}

	mu sync.RWMutex

	runtimeReadinessChecker          runtimeReadinessChecker
	loraAdapterLoadingTargetSelector loraAdapterLoadingTargetSelector
	loraAdapterLoader                loraAdapterLoader

	// readinessCheckMaxRetryCount is the maximum number of retries for the readiness check.
	readinessCheckMaxRetryCount int
	// readinessCheckRetryInterval is the interval for the readiness check.
	readinessCheckRetryInterval time.Duration

	// nimModels is a map of models that use NIM as backend.
	nimModels map[string]bool
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
	r, ok := m.runtimes[modelID]
	if !ok {
		return "", fmt.Errorf("runtime for model %q does not exist", modelID)
	}
	if !r.ready {
		return "", fmt.Errorf("runtime for model %q is not ready", modelID)
	}

	addr, ok := r.addrSet.get(time.Now())
	if !ok {
		return "", fmt.Errorf("runtime for model %q has no address", modelID)
	}

	return addr, nil
}

// BlacklistLLMAddress blacklists the address of the LLM.
func (m *Manager) BlacklistLLMAddress(modelID, address string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runtimes[modelID]
	if !ok {
		return fmt.Errorf("runtime for model %q does not exist", modelID)
	}
	if !r.ready {
		return fmt.Errorf("runtime for model %q is not ready", modelID)
	}
	r.addrSet.blacklistAddress(address, time.Now())

	return nil
}

// ListModels returns the list of models.
func (m *Manager) ListModels() []*iv1.EngineStatus_Model {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var ms []*iv1.EngineStatus_Model
	for id, r := range m.runtimes {
		ms = append(ms, &iv1.EngineStatus_Model{
			Id:                      id,
			IsReady:                 r.ready,
			GpuAllocated:            r.gpu * r.replicas,
			IsDynamicallyLoadedLora: r.isDynamicallyLoadedLoRA,
		})
	}
	return ms
}

// RunStateMachine runs the state machine for the manager.
func (m *Manager) RunStateMachine(ctx context.Context) error {
	// Append auth token here so that it can be used in all the events.
	ctx = auth.AppendWorkerAuthorization(ctx)
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
			case *loraAdapterPullStatusCheckEvent:
				if err := m.processLoRAAdapterPullStatusCheckEvent(ctx, e); err != nil {
					return err
				}
			case *loraAdapterStatusUpdateEvent:
				if err := m.processLoRAAdapterStatusUpdateEvent(ctx, e); err != nil {
					return err
				}
			case *loadLoRAAdapterEvent:
				if err := m.processLoadLoRAAdapterEvent(ctx, e); err != nil {
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
		defer m.mu.Unlock()
		// The runtime is ready, or the pull is already in progress.
		if r.ready {
			log.V(1).Info("Runtime is ready. No need to pull the model", "modelID", e.modelID)
			if e.readyWaitCh != nil {
				close(e.readyWaitCh)
			}
			return nil
		}

		if r.lastErrReason != "" {
			log.Info("Runtime is not ready. Closing the wait channel", "modelID", e.modelID)
			if e.readyWaitCh != nil {
				e.readyWaitCh <- r.lastErrReason
				close(e.readyWaitCh)
			}
			return nil
		}

		log.Info("Pull is in progress. Waiting for the runtime to be ready", "modelID", e.modelID)

		if e.readyWaitCh != nil {
			r.waitChs = append(r.waitChs, e.readyWaitCh)
		}

		return nil
	}

	log.Info("Pulling model...", "modelID", e.modelID)
	log.Info("Runtime is not ready. Checking if LoRA adapter loading is applicable", "modelID", e.modelID)

	// TODO(kenji): Revisit the locking if this takes a long time.
	isDynamicLoRAApplicable, baseModelID, err := m.isDynamicLoRAloadingApplicable(ctx, e.modelID)
	if err != nil {
		return err
	}

	if !isDynamicLoRAApplicable {
		log.Info("Creating a new pending runtime", "modelID", e.modelID)
		r := newPendingRuntime(client.GetName(e.modelID))
		if e.readyWaitCh != nil {
			r.waitChs = append(r.waitChs, e.readyWaitCh)
		}
		m.runtimes[e.modelID] = r
		m.mu.Unlock()

		if err := m.deployRuntime(ctx, e.modelID); err != nil {
			return fmt.Errorf("deploy runtime: %s", err)
		}
		return nil
	}

	br, ok := m.runtimes[baseModelID]
	if !ok {
		log.Info("Creating a new pending runtime for the base model", "baseModelID", baseModelID)
		br = newPendingRuntime(client.GetName(baseModelID))
		m.runtimes[baseModelID] = br

		// TODO(kenji): Revisit the locking if this takes a long time.
		if err := m.deployRuntime(ctx, baseModelID); err != nil {
			return fmt.Errorf("deploy runtime: %s", err)
		}
	}

	if !br.ready {
		log.Info("Base model is not ready. Request a pull", "baseModelID", baseModelID, "modelID", e.modelID)
		br.addPendingPullModelRequest(e)
		m.mu.Unlock()
		return nil
	}

	log.Info("Base model is ready. Load LoRA adapter", "baseModelID", baseModelID, "modelID", e.modelID)

	r := newPendingRuntime(client.GetName(e.modelID))
	if e.readyWaitCh != nil {
		r.waitChs = append(r.waitChs, e.readyWaitCh)
	}
	r.isDynamicallyLoadedLoRA = true
	m.runtimes[e.modelID] = r
	m.mu.Unlock()

	// TODO(kenji): Consider sending the request to all pods.
	pod, err := m.loraAdapterLoadingTargetSelector.selectTarget(ctx, e.modelID, br.name)
	if err != nil {
		return fmt.Errorf("find LoRA adapter loading target pod: %s", err)
	}

	log.Info("Found pod for LoRA adapter loading", "podIP", pod.Status.PodIP)

	pullerAddr := fmt.Sprintf("%s:%d", pod.Status.PodIP, m.pullerPort)
	if err := m.loraAdapterLoader.pullModel(ctx, pullerAddr, e.modelID); err != nil {
		return fmt.Errorf("pull model: %s", err)
	}

	go func() {
		m.eventCh <- &loraAdapterPullStatusCheckEvent{
			modelID:     e.modelID,
			pod:         pod,
			gpu:         br.gpu,
			eventWaitCh: make(chan struct{}),
		}
	}()

	return nil
}

func (m *Manager) deployRuntime(ctx context.Context, modelID string) error {
	client, err := m.rtClientFactory.New(modelID)
	if err != nil {
		return err
	}

	sts, err := client.DeployRuntime(ctx, modelID, false)
	if err != nil {
		return err
	}

	if err := m.autoscaler.Register(ctx, modelID, sts); err != nil {
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

func (m *Manager) isDynamicLoRAloadingApplicable(ctx context.Context, modelID string) (bool, string, error) {
	if !m.enableDynamicLoRALoading {
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

func (m *Manager) processDeleteModelEvent(ctx context.Context, e *deleteModelEvent) error {
	log := ctrl.LoggerFrom(ctx)

	defer close(e.eventWaitCh)

	m.mu.Lock()
	r, ok := m.runtimes[e.modelID]
	m.mu.Unlock()
	if !ok {
		log.V(4).Info("Runtime does not exist", "modelID", e.modelID)
		return nil
	}

	log.Info("Deleting model...", "modelID", e.modelID)

	if r.isDynamicallyLoadedLoRA {
		log.Info("Unloading the LoRA adapter from the runtime", "modelID", e.modelID)
		for _, addr := range r.addresses() {
			if err := m.loraAdapterLoader.unload(ctx, addr, e.modelID); err != nil {
				return fmt.Errorf("unload LoRA adapter: %s", err)
			}
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

	log.Info("Deleting runtime...", "modelID", e.modelID, "runtime name", client.GetName(e.modelID))
	if err := client.DeleteRuntime(ctx, client.GetName(e.modelID), e.modelID); err != nil {
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
			// Create a new one instead of reusing the one in the event so that
			// we don't block reconciler until the readiness check is done.
			eventWaitCh: make(chan struct{}),
		}
	}()

	return nil
}

func (m *Manager) processReadinessCheckEvent(ctx context.Context, e *readinessCheckEvent) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Checking runtime readiness...", "modelID", e.modelID, "address", e.address)

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
			close(e.eventWaitCh)
			// TODO(kenji): Delete the runtime here?
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

				eventWaitCh: e.eventWaitCh,
			}
		}()
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	rt.becomeReady(e.address, e.gpu, e.replicas, log)

	log.Info("Runtime is ready", "modelID", e.modelID)

	go func(es []*pullModelEvent) {
		for _, e := range es {
			log.Info("Requeuing pending pull request", "modelID", e.modelID)
			m.eventCh <- e
		}
	}(rt.dequeuePendingPullModelRequests())

	rt.closeWaitChs("")

	close(e.eventWaitCh)

	return nil
}

func (m *Manager) processLoRAAdapterPullStatusCheckEvent(ctx context.Context, e *loraAdapterPullStatusCheckEvent) error {
	log := ctrl.LoggerFrom(ctx)

	// TODO(kenji): Check if the pod still exists. If not, we should stop retrying.

	pullerAddr := fmt.Sprintf("%s:%d", e.pod.Status.PodIP, m.pullerPort)
	ok, err := m.loraAdapterLoader.checkModelPullStatus(ctx, pullerAddr, e.modelID)
	if err != nil {
		return fmt.Errorf("check model pull status: %s", err)
	}
	if !ok {
		// Retry. We repeat without the max limit as we don't know how long the pull will take.
		// TODO(kenji): Revisit. We should stop retry if the pod no longer exists.
		log.Info("LoRA adapter pull is not finished. Retrying...", "modelID", e.modelID)
		time.Sleep(m.readinessCheckRetryInterval)
		go func() {
			m.eventCh <- e
		}()
		return nil
	}

	log.Info("LoRA adapter has been pulled", "modelID", e.modelID)

	client, err := m.rtClientFactory.New(e.modelID)
	if err != nil {
		return err
	}
	vllmAddr := client.GetAddress(e.pod.Status.PodIP)
	if err := m.loraAdapterLoader.load(ctx, vllmAddr, e.modelID); err != nil {
		// The loading fails if the pod no longer exists or the pod is crashing.
		// TODO(kenji): Revisit. We should stop retry if the pod no longer exists.
		log.Error(err, "Failed to load LoRA adapter. Retrying...", "modelID", e.modelID)
		time.Sleep(m.readinessCheckRetryInterval)
		go func() {
			m.eventCh <- e
		}()
		return nil
	}

	go func() {
		m.eventCh <- &readinessCheckEvent{
			modelID:     e.modelID,
			address:     vllmAddr,
			gpu:         e.gpu,
			replicas:    1, // TODO(kenji): Fix this.
			eventWaitCh: e.eventWaitCh,
		}
	}()

	return nil
}

func (m *Manager) processLoRAAdapterStatusUpdateEvent(ctx context.Context, e *loraAdapterStatusUpdateEvent) error {
	defer close(e.eventWaitCh)

	if e.update.podIP == "" {
		return fmt.Errorf("podIP is empty")
	}

	log := ctrl.LoggerFrom(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, modelID := range e.update.addedAdapterIDs {
		client, err := m.rtClientFactory.New(modelID)
		if err != nil {
			return err
		}
		vllmAddr := client.GetAddress(e.update.podIP)

		log.Info("Adding a new LoRA adapter", "modelID", modelID, "vllmAddr", vllmAddr)

		r, ok := m.runtimes[modelID]
		if !ok {
			log.Info("Creating a new runtime", "modelID", modelID)
			r = newPendingRuntime(modelID)
			r.isDynamicallyLoadedLoRA = true
			r.becomeReady(vllmAddr, e.update.gpu, 1, log)
			m.runtimes[modelID] = r
		}

		r.addAddress(vllmAddr)
	}

	for _, modelID := range e.update.removedAdapterIDs {
		client, err := m.rtClientFactory.New(modelID)
		if err != nil {
			return err
		}
		vllmAddr := client.GetAddress(e.update.podIP)

		log.Info("Removing a LoRA adapter", "modelID", modelID, "vllmAddr", vllmAddr)
		r, ok := m.runtimes[modelID]
		if !ok {
			continue
		}

		r.removeAddress(vllmAddr)

		if len(r.addresses()) != 0 {
			continue
		}

		log.Info("Removing the runtime", "modelID", modelID)
		r.closeWaitChs(errMsgDeletedRuntime)
		delete(m.runtimes, modelID)
	}

	return nil
}

func (m *Manager) processLoadLoRAAdapterEvent(ctx context.Context, e *loadLoRAAdapterEvent) error {
	log := ctrl.LoggerFrom(ctx)

	pullerAddr := fmt.Sprintf("%s:%d", e.pod.Status.PodIP, m.pullerPort)

	log.Info("Pulling LoRA adapter", "modelID", e.modelID, "pullerAddr", pullerAddr)
	if err := m.loraAdapterLoader.pullModel(ctx, pullerAddr, e.modelID); err != nil {
		return fmt.Errorf("pull model: %s", err)
	}

	go func() {
		m.eventCh <- &loraAdapterPullStatusCheckEvent{
			modelID:     e.modelID,
			pod:         e.pod,
			gpu:         0, /* TODO(kenji): Fix */
			eventWaitCh: e.eventWaitCh,
		}
	}()

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

// PullModelUnblocked pulls the model from the model manager without waiting for its completion.
func (m *Manager) PullModelUnblocked(ctx context.Context, modelID string) error {
	m.eventCh <- &pullModelEvent{
		modelID:     modelID,
		readyWaitCh: nil,
	}
	return nil
}

// DeleteModel deletes the model from the model manager.
func (m *Manager) DeleteModel(ctx context.Context, modelID string) error {
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

func (m *Manager) processLoRAAdapterUpdate(ctx context.Context, update *loRAAdapterStatusUpdate) error {
	waitCh := make(chan struct{})
	go func() {
		m.eventCh <- &loraAdapterStatusUpdateEvent{
			update:      update,
			eventWaitCh: waitCh,
		}
	}()

	select {
	case <-waitCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// LoadLoRAAdapter loads the LoRA adapter.
func (m *Manager) loadLoRAAdapter(ctx context.Context, modelID string, pod *corev1.Pod) error {
	waitCh := make(chan struct{})
	go func() {
		m.eventCh <- &loadLoRAAdapterEvent{
			modelID:     modelID,
			pod:         pod,
			eventWaitCh: waitCh,
		}
	}()

	select {
	case <-waitCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
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
