package runtime

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/llmariner/inference-manager/engine/internal/autoscaler"
	"github.com/llmariner/inference-manager/engine/internal/config"
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
		models:       make(map[string]ollamaModel),
	}
}

// OllamaManager manages multiple models in a single ollama runtime.
type OllamaManager struct {
	k8sClient    client.Client
	ollamaClient Client
	autoscaler   autoscaler.Registerer

	pullerAddr string

	runtime runtime
	// models is keyed by model ID.
	models map[string]ollamaModel
	mu     sync.RWMutex
}

type ollamaModel struct {
	id    string
	ready bool
	// waitCh is used when the model is not ready.
	waitCh chan struct{}
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
	m.mu.RLock()
	runtimeAddr := m.runtime.address
	if m.runtime.ready {
		log.V(2).Info("Runtime is ready", "address", runtimeAddr)
		m.mu.RUnlock()
	} else {
		log.Info("Waiting for the runtime to be ready", "address", runtimeAddr)
		ch := m.runtime.waitCh
		m.mu.RUnlock()
		select {
		case <-ch:
		// TODO(aya): check error reason
		case <-ctx.Done():
			return ctx.Err()
		}
		log.Info("Runtime is ready", "address", runtimeAddr)
	}

	// check if the model is already pulled.
	m.mu.Lock()
	model, ok := m.models[modelID]
	if ok {
		m.mu.Unlock()
		if !model.ready {
			log.Info("Waiting for the model to be ready", "modelID", modelID)
			select {
			case <-model.waitCh:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		log.Info("Model is pulled", "modelID", modelID)
		return nil
	}
	m.models[modelID] = ollamaModel{id: modelID, waitCh: make(chan struct{})}
	m.mu.Unlock()
	log.Info("Model is being pulled", "modelID", modelID)

	// request to pull the model.
	pullURL := &url.URL{Scheme: "http", Host: m.pullerAddr, Path: "/pull"}
	resp, err := http.Post(pullURL.String(), "application/json",
		bytes.NewBuffer(fmt.Appendf([]byte{}, `{"modelID": "%s"}`, modelID)))
	if err != nil {
		return fmt.Errorf("failed to pull model: %s", err)
	}
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("failed to close response body: %s", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	// wait until the model is ready.
	log.Info("Waiting for the model to be ready", "modelID", modelID)
	const retry = 2 * time.Second
	showURL := &url.URL{Scheme: "http", Host: runtimeAddr, Path: "/api/show"}
	for {
		resp, err := http.Post(showURL.String(), "application/json",
			bytes.NewBuffer(fmt.Appendf([]byte{}, `{"model": "%s"}`, modelID)))
		if err != nil {
			return fmt.Errorf("failed to check model: %s", err)
		}
		if err := resp.Body.Close(); err != nil {
			return fmt.Errorf("failed to close response body: %s", err)
		}
		if resp.StatusCode == http.StatusOK {
			break
		}
		log.V(2).Info("Model is not ready yet", "url", showURL, "retry-after", retry, "status", resp.Status)
		time.Sleep(retry)
	}

	log.Info("Model is ready", "modelID", modelID)
	m.mu.Lock()
	if r, ok := m.models[modelID]; ok && !m.models[modelID].ready {
		if r.waitCh != nil {
			close(r.waitCh)
		}
		m.models[modelID] = ollamaModel{id: modelID, ready: true}
	}
	m.mu.Unlock()
	return nil
}

// Reconcile reconciles the runtime.
func (m *OllamaManager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	var sts appsv1.StatefulSet
	if err := m.k8sClient.Get(ctx, req.NamespacedName, &sts); err != nil {
		if apierrors.IsNotFound(err) {
			m.mu.Lock()
			m.runtime = runtime{}
			m.mu.Unlock()
			m.autoscaler.Unregister(req.NamespacedName)
			log.Info("Runtime is deleted")
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
			m.runtime = newPendingRuntime(sts.Name)
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
		if m.runtime.waitCh != nil {
			close(m.runtime.waitCh)
		}
		m.runtime = newReadyRuntime(sts.Name, m.ollamaClient.GetAddress(sts.Name), getGPU(&sts), sts.Status.ReadyReplicas)
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
			// cancel the current waiting channel, but recreate to avoid panic.
			close(r.waitCh)
			r.waitCh = make(chan struct{})
			r.errReason = corev1.PodReasonUnschedulable
			m.runtime = r
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
