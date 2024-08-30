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

// NewManager creates a new runtime manager.
func NewManager(k8sClient client.Client, rtClient Client) *Manager {
	return &Manager{
		k8sClient:       k8sClient,
		rtClient:        rtClient,
		readyRuntimes:   make(map[string]runtime),
		pendingRuntimes: make(map[string]chan struct{}),
	}
}

// Manager manages runtimes.
type Manager struct {
	k8sClient client.Client
	rtClient  Client

	readyRuntimes   map[string]runtime
	pendingRuntimes map[string]chan struct{}
	mu              sync.RWMutex
}

type runtime struct {
	address string
}

// Initialize initializes ready and pending runtimes.
// This function is not thread-safe.
func (m *Manager) Initialize(ctx context.Context, apiReader client.Reader, autoscaler scalerRegisterer, namespace string) error {
	var stsList appsv1.StatefulSetList
	if err := apiReader.List(ctx, &stsList,
		client.InNamespace(namespace),
		client.MatchingLabels{"app.kubernetes.io/name": "runtime"},
	); err != nil {
		return fmt.Errorf("failed to list runtimes: %s", err)
	}

	for _, sts := range stsList.Items {
		if !sts.DeletionTimestamp.IsZero() {
			continue
		}
		modelID := sts.GetAnnotations()[modelAnnotationKey]
		if sts.Status.ReadyReplicas > 0 {
			m.readyRuntimes[modelID] = runtime{address: m.rtClient.GetAddress(sts.Name)}
		} else {
			m.pendingRuntimes[modelID] = make(chan struct{})
		}
		autoscaler.Register(modelID, types.NamespacedName{Name: sts.Name, Namespace: namespace})
	}
	return nil
}

func (m *Manager) deleteRuntime(modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.pendingRuntimes[modelID]; ok {
		close(r)
		delete(m.pendingRuntimes, modelID)
		return
	}
	delete(m.readyRuntimes, modelID)
}

func (m *Manager) markRuntimeReady(modelID, address string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.pendingRuntimes[modelID]; ok {
		close(r)
		delete(m.pendingRuntimes, modelID)
		m.readyRuntimes[modelID] = runtime{address: address}
	}
}

func (m *Manager) markRuntimeIsPending(modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.readyRuntimes[modelID]; ok {
		delete(m.readyRuntimes, modelID)
		m.pendingRuntimes[modelID] = make(chan struct{})
	}
}

func (m *Manager) isPending(modelID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.pendingRuntimes[modelID]
	return ok
}

// GetLLMAddress returns the address of the LLM.
func (m *Manager) GetLLMAddress(modelID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.readyRuntimes[modelID]; ok {
		return r.address, nil
	}
	return "", fmt.Errorf("runtime for model %q is not ready", modelID)
}

// ListSyncedModelIDs returns the list of models that are synced.
func (m *Manager) ListSyncedModelIDs(ctx context.Context) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var modelIDs []string
	for id := range m.readyRuntimes {
		modelIDs = append(modelIDs, id)
	}
	return modelIDs
}

// ListInProgressModels returns the list of models that are in progress.
func (m *Manager) ListInProgressModels() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var modelIDs []string
	for id := range m.pendingRuntimes {
		modelIDs = append(modelIDs, id)
	}
	return modelIDs
}

// PullModel pulls the model from the model manager.
func (m *Manager) PullModel(ctx context.Context, modelID string) error {
	m.mu.Lock()
	if _, ok := m.readyRuntimes[modelID]; ok {
		m.mu.Unlock()
		return nil
	}

	var done chan struct{}
	if ch, ok := m.pendingRuntimes[modelID]; ok {
		m.mu.Unlock()
		done = ch
	} else {
		done = make(chan struct{})
		m.pendingRuntimes[modelID] = done
		m.mu.Unlock()
		if err := m.rtClient.DeployRuntime(ctx, modelID); err != nil {
			m.mu.Lock()
			delete(m.pendingRuntimes, modelID)
			m.mu.Unlock()
			close(done)
			return err
		}
	}

	ctrl.LoggerFrom(ctx).Info("Waiting for runtime to be ready", "model", modelID)
	select {
	case <-done:
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

		patch := client.MergeFrom(&sts)
		newSts := sts.DeepCopy()
		controllerutil.RemoveFinalizer(newSts, finalizerKey)
		if err := client.IgnoreNotFound(m.k8sClient.Patch(ctx, newSts, patch)); err != nil {
			log.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if m.isPending(modelID) {
		if sts.Status.ReadyReplicas > 0 {
			addr := m.rtClient.GetAddress(sts.Name)
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
	} else if sts.Status.Replicas == 0 {
		m.markRuntimeIsPending(modelID)
		log.Info("Runtime is pending")
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
