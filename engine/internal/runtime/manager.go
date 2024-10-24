package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ScalerRegisterer is an interface for registering and unregistering scalers.
type ScalerRegisterer interface {
	Register(modelID string, target types.NamespacedName)
	Unregister(target types.NamespacedName)
}

// NewManager creates a new runtime manager.
func NewManager(
	k8sClient client.Client,
	rtClientFactory ClientFactory,
	autoscaler ScalerRegisterer,
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
	autoscaler      ScalerRegisterer

	runtimes map[string]runtime
	mu       sync.RWMutex
}

func newPendingRuntime() runtime {
	return runtime{ready: false, waitCh: make(chan struct{})}
}

func newReadyRuntime(address string) runtime {
	return runtime{ready: true, address: address}
}

type runtime struct {
	ready bool
	// address is empty when the runtime is not ready.
	address string
	// waitCh is used when the runtime is not ready.
	waitCh chan struct{}
}

func (m *Manager) addRuntime(modelID string, sts appsv1.StatefulSet) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runtimes[modelID]; ok {
		return false, nil
	}
	if sts.Status.ReadyReplicas > 0 {
		c, err := m.rtClientFactory.New(modelID)
		if err != nil {
			return false, err
		}
		m.runtimes[modelID] = newReadyRuntime(c.GetAddress(sts.Name))
	} else {
		m.runtimes[modelID] = newPendingRuntime()
	}
	return true, nil
}

func (m *Manager) deleteRuntime(modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runtimes[modelID]; ok {
		if r.waitCh != nil {
			close(r.waitCh)
		}
		delete(m.runtimes, modelID)
	}
}

func (m *Manager) markRuntimeReady(modelID, address string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runtimes[modelID]; ok && !m.runtimes[modelID].ready {
		if r.waitCh != nil {
			close(r.waitCh)
		}
		m.runtimes[modelID] = newReadyRuntime(address)
	}
}

func (m *Manager) markRuntimeIsPending(modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runtimes[modelID]; ok && r.ready {
		m.runtimes[modelID] = newPendingRuntime()
	}
}

func (m *Manager) isReady(modelID string) (bool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runtimes[modelID]
	return r.ready, ok
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

// ListSyncedModelIDs returns the list of models that are synced.
func (m *Manager) ListSyncedModelIDs() []string {
	return m.listModels(true)
}

// ListInProgressModels returns the list of models that are in progress.
func (m *Manager) ListInProgressModels() []string {
	return m.listModels(false)
}

func (m *Manager) listModels(ready bool) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var modelIDs []string
	for id, r := range m.runtimes {
		if r.ready == ready {
			modelIDs = append(modelIDs, id)
		}
	}
	return modelIDs
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
		r = newPendingRuntime()
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

		client, err := m.rtClientFactory.New(modelID)
		if err != nil {
			cleanup()
			return err
		}
		sts, err := client.DeployRuntime(ctx, modelID, false)
		if err != nil {
			cleanup()
			return err
		}
		m.autoscaler.Register(modelID, types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace})
		if sts.Status.ReadyReplicas > 0 {
			// If this is called before the first cache sync of the reconciler
			// is complete, the existing statefulset(STS) for the runtime is not
			// registered in the manager's managed runtime map. If the runtime's
			// STS is already ready, mark the runtime as ready without waiting
			// for the reconciler (leader-election component) to process it.
			m.markRuntimeReady(modelID, client.GetAddress(sts.Name))
		}
	}
	log.Info("Waiting for runtime to be ready", "model", modelID)
	select {
	case <-r.waitCh:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Reconcile reconciles the runtime.
func (m *Manager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	var sts appsv1.StatefulSet
	if err := m.k8sClient.Get(ctx, req.NamespacedName, &sts); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	modelID := sts.GetAnnotations()[modelAnnotationKey]

	if !sts.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&sts, finalizerKey) {
			return ctrl.Result{}, nil
		}
		m.deleteRuntime(modelID)
		m.autoscaler.Unregister(req.NamespacedName)

		patch := client.MergeFrom(&sts)
		newSts := sts.DeepCopy()
		controllerutil.RemoveFinalizer(newSts, finalizerKey)
		if err := client.IgnoreNotFound(m.k8sClient.Patch(ctx, newSts, patch)); err != nil {
			log.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	ready, ok := m.isReady(modelID)
	if !ok {
		// If an statefulset for the unregistered runtime is found,
		// the manager registers it to the managed runtime map.
		// This would call when the manager synchronizes the cache
		// for the first time or when another engine creates a runtime.
		log.V(4).Info("Registering runtime", "model", modelID)
		if added, err := m.addRuntime(modelID, sts); err != nil {
			log.Error(err, "Failed to add runtime")
			return ctrl.Result{}, err
		} else if added {
			m.autoscaler.Register(modelID, types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace})
		}
		return ctrl.Result{}, nil
	} else if ready {
		if sts.Status.Replicas == 0 {
			m.markRuntimeIsPending(modelID)
			log.Info("Runtime is pending")
		}
	} else if sts.Status.ReadyReplicas > 0 {
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
			log.Error(err, "Failed to reach the runtime")
			return ctrl.Result{}, err
		}
		m.markRuntimeReady(modelID, addr)
		log.Info("Runtime is ready")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the runtime manager with the given controller manager.
func (m *Manager) SetupWithManager(mgr ctrl.Manager) error {
	filterByAnno := (predicate.NewPredicateFuncs(func(object client.Object) bool {
		_, ok := object.GetAnnotations()[runtimeAnnotationKey]
		return ok
	}))
	constructer := func(r *reconcile.Request) logr.Logger {
		if r != nil {
			return mgr.GetLogger().WithValues("runtime", r.NamespacedName)
		}
		return mgr.GetLogger()
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}, builder.WithPredicates(filterByAnno)).
		WithLogConstructor(constructer).
		Complete(m)
}
