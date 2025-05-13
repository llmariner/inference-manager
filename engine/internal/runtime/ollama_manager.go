package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/llmariner/inference-manager/engine/internal/autoscaler"
	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	"github.com/llmariner/inference-manager/engine/internal/puller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// NewOllamaManager creates a new ollama runtime manager.
func NewOllamaManager(
	k8sClient client.Client,
	client Client,
	autoscaler autoscaler.Registerer,
	pullerAddr string,
) *OllamaManager {
	return &OllamaManager{
		k8sClient:    k8sClient,
		ollamaClient: client,
		autoscaler:   autoscaler,
		pullerAddr:   pullerAddr,
		runtime:      newPendingRuntime(client.GetName("")),
		models:       make(map[string]*ollamaModel),
	}
}

// OllamaManager manages multiple models in a single ollama runtime.
type OllamaManager struct {
	k8sClient    client.Client
	ollamaClient Client
	autoscaler   autoscaler.Registerer

	pullerAddr string

	runtime *runtime
	// models is keyed by model ID.
	models map[string]*ollamaModel
	mu     sync.RWMutex
}

type ollamaModel struct {
	id    string
	ready bool
	// waitChs is used when the model is not ready.
	waitChs []chan struct{}
}

// ListSyncedModels returns the list of models that are synced.
func (m *OllamaManager) ListSyncedModels() []ModelRuntimeInfo {
	return m.listModels(true)
}

// ListInProgressModels returns the list of models that are in progress.
func (m *OllamaManager) ListInProgressModels() []ModelRuntimeInfo {
	return m.listModels(false)
}

func (m *OllamaManager) listModels(ready bool) []ModelRuntimeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var ms []ModelRuntimeInfo
	if ready && !m.runtime.ready {
		return ms
	}
	for id, m := range m.models {
		if m.ready == ready {
			ms = append(ms, ModelRuntimeInfo{
				ID:    id,
				Ready: m.ready,
			})
		}
	}
	return ms
}

func (m *OllamaManager) cleanupModels() {
	for _, model := range m.models {
		for _, ch := range model.waitChs {
			close(ch)
		}
	}
	m.models = make(map[string]*ollamaModel)
}

// Start deploys the ollama runtime.
func (m *OllamaManager) Start(ctx context.Context) error {
	sts, err := m.ollamaClient.DeployRuntime(ctx, "", true)
	if err != nil {
		return fmt.Errorf("deploy runtime: %s", err)
	}
	// The autoscaler supports separate scaling settings for each model.
	// However, the dynamic model loading mode always uses the default
	// scaler settings because it can load multiple models.
	return m.autoscaler.Register(ctx, "", sts)
}

// NeedLeaderElection implements LeaderElectionRunnable and always returns true.
func (m *OllamaManager) NeedLeaderElection() bool {
	return true
}

// GetLLMAddress returns the address of the LLM for the given model.
func (m *OllamaManager) GetLLMAddress(_ string) (string, error) {
	return m.ollamaClient.GetAddress(m.ollamaClient.GetName("")), nil
}

// PullModel pulls the model from the model manager.
func (m *OllamaManager) PullModel(ctx context.Context, modelID string) error {
	log := ctrl.LoggerFrom(ctx)

	// check if the runtime is ready.
	m.mu.Lock()

	var runtimeAddr string
	if m.runtime.ready {
		if len(m.runtime.addresses) != 1 {
			m.mu.Unlock()
			return fmt.Errorf("expected only one address: %v", m.runtime.addresses)
		}
		runtimeAddr = m.runtime.addresses[0]

		log.V(2).Info("Runtime is ready", "addresses", runtimeAddr)
	} else {
		log.Info("Waiting for the runtime to be ready", "addresses", m.runtime.addresses)
		ch := make(chan string)
		m.runtime.waitChs = append(m.runtime.waitChs, ch)
		m.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}

		m.mu.Lock()
		if !m.runtime.ready {
			err := fmt.Errorf("runtime is not ready")
			log.Error(err, "Runtime is not ready", "addresses", m.runtime.addresses)
			m.mu.Unlock()
			return err
		}

		if len(m.runtime.addresses) != 1 {
			m.mu.Unlock()
			return fmt.Errorf("expected only one address: %v", m.runtime.addresses)
		}
		runtimeAddr = m.runtime.addresses[0]

		log.Info("Runtime is ready", "address", runtimeAddr)
	}

	// check if the model is already pulled.
	model, ok := m.models[modelID]
	if ok {
		if model.ready {
			log.Info("Model is pulled", "modelID", modelID)
			m.mu.Unlock()
			return nil
		}

		ch := make(chan struct{})
		model.waitChs = append(model.waitChs, ch)
		m.mu.Unlock()

		log.Info("Waiting for the model to be ready", "modelID", modelID)
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}

		m.mu.Lock()
		if m.models[modelID].ready {
			m.mu.Unlock()
			log.Info("Model is pulled", "modelID", modelID)
			return nil
		}
		m.models[modelID] = &ollamaModel{id: modelID}
		m.mu.Unlock()
		log.Info("Model pulling is canceled, retrying", "modelID", modelID)
	} else {
		m.models[modelID] = &ollamaModel{id: modelID}
		m.mu.Unlock()
	}
	log.Info("Model is being pulled", "modelID", modelID)

	pc := puller.NewClient(m.pullerAddr)
	if err := pc.PullModel(ctx, modelID); err != nil {
		return err
	}

	// wait until the model is ready.
	log.Info("Waiting for the model to be ready", "modelID", modelID)

	oc := ollama.NewClient(runtimeAddr, log)
	if err := oc.Show(ctx, modelID); err != nil {
		return fmt.Errorf("check model: %s", err)
	}

	log.Info("Model is ready", "modelID", modelID)
	m.mu.Lock()
	if r, ok := m.models[modelID]; ok && !m.models[modelID].ready {
		for _, ch := range r.waitChs {
			close(ch)
		}
		m.models[modelID] = &ollamaModel{id: modelID, ready: true}
	}
	m.mu.Unlock()
	return nil
}

// DeleteModel deletes the model from the model manager.
func (m *OllamaManager) DeleteModel(ctx context.Context, modelID string) error {
	return fmt.Errorf("unsupported operation in ollama manager: delete model %s", modelID)
}

// Reconcile reconciles the runtime.
func (m *OllamaManager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	var sts appsv1.StatefulSet
	if err := m.k8sClient.Get(ctx, req.NamespacedName, &sts); err != nil {
		if apierrors.IsNotFound(err) {
			m.mu.Lock()
			m.runtime.ready = false
			m.runtime.closeWaitChs("")
			m.cleanupModels()
			m.mu.Unlock()
			m.autoscaler.Unregister(req.NamespacedName)
			log.Info("Runtime is deleted")

			// Re-deploy the runtime.
			sts, err := m.ollamaClient.DeployRuntime(ctx, "", false)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("deploy runtime: %s", err)
			}
			return ctrl.Result{}, m.autoscaler.Register(ctx, "", sts)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling runtime...")
	var ready bool
	m.mu.RLock()
	ready = m.runtime.ready
	m.mu.RUnlock()
	if ready {
		// The runtime has already been ready.
		if sts.Status.Replicas == 0 {
			m.mu.Lock()
			m.runtime.ready = false
			m.cleanupModels()
			m.mu.Unlock()
			log.Info("Runtime is scale down to zero")
		} else {
			m.mu.Lock()
			m.runtime.replicas = sts.Status.ReadyReplicas
			m.mu.Unlock()
			log.V(10).Info("Runtime replicas are updated", "replicas", sts.Status.ReadyReplicas)
		}
		return ctrl.Result{}, nil
	}

	if sts.Status.ReadyReplicas > 0 {
		m.mu.Lock()
		m.runtime.becomeReady(
			m.ollamaClient.GetAddress(sts.Name),
			getGPU(&sts),
			sts.Status.ReadyReplicas,
		)
		m.runtime.closeWaitChs("")
		m.mu.Unlock()

		log.Info("Runtime is ready")
		return ctrl.Result{}, nil
	}

	// The runtime is still not ready.
	if yes, err := allChildrenUnschedulable(ctx, m.k8sClient, sts); err != nil {
		log.V(2).Error(err, "Failed to check unschedulable children")
		return ctrl.Result{}, err
	} else if yes {
		m.mu.Lock()
		if r := m.runtime; !r.ready {
			r.closeWaitChs(corev1.PodReasonUnschedulable)
		}
		m.mu.Unlock()
		log.V(1).Info("Pod is unschedulable")
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the runtime manager with the given controller manager.
func (m *OllamaManager) SetupWithManager(mgr ctrl.Manager, leaderElection bool) error {
	if err := mgr.Add(m); err != nil {
		return fmt.Errorf("add manager: %s", err)
	}

	filterByLabel := (predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetAnnotations()[runtimeAnnotationKey] == config.RuntimeNameOllama
	}))
	return setupWithManager(mgr, leaderElection, m, filterByLabel)
}
