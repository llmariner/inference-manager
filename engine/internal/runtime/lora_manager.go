package runtime

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	"github.com/llmariner/inference-manager/engine/internal/puller"
	"github.com/llmariner/inference-manager/engine/internal/vllm"
	mv1 "github.com/llmariner/model-manager/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type loRAAdapterStatusUpdate struct {
	podName           string
	baseModelID       string
	addedAdapterIDs   []string
	removedAdapterIDs []string
}

type updateProcessor interface {
	processLoRAAdapterUpdate(update *loRAAdapterStatusUpdate)
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
		r.deletePod(req.Name)

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
}

func (r *LoRAReconciler) deletePod(name string) {
	r.mu.Lock()
	s, ok := r.podsByName[name]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.podsByName, name)
	r.mu.Unlock()

	var ids []string
	if s.lstatus != nil {
		for id := range s.lstatus.adapterIDs {
			ids = append(ids, id)
		}
	}
	r.updateProcessor.processLoRAAdapterUpdate(&loRAAdapterStatusUpdate{
		podName:           name,
		removedAdapterIDs: ids,
	})
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
		r.updateProcessor.processLoRAAdapterUpdate(u)
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
	pod := oldS.pod

	if newS == nil {
		log.Info("Pod not found", "pod", pod.Name)

		// Pod not found or vLLM is unreachable. Consider that all LoRA adapters are deleted.
		var ids []string
		for id := range oldS.lstatus.adapterIDs {
			ids = append(ids, id)
		}
		return &loRAAdapterStatusUpdate{
			podName:           pod.Name,
			baseModelID:       oldS.lstatus.baseModelID,
			removedAdapterIDs: ids,
		}, true, nil
	}

	if oldS.lstatus == nil {
		log.Info("New status found", "pod", pod.Name, "new adapters", newS.lstatus.adapterIDs)
		var ids []string
		for id := range newS.lstatus.adapterIDs {
			ids = append(ids, id)
		}
		return &loRAAdapterStatusUpdate{
			podName:         pod.Name,
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
		baseModelID:       newS.lstatus.baseModelID,
		addedAdapterIDs:   added,
		removedAdapterIDs: removed,
	}, true, nil
}

func loadLoRAAdapter(
	ctx context.Context,
	modelID string,
	pullerAddr string,
	vllmAddr string,
) error {
	log := ctrl.LoggerFrom(ctx)

	pclient := puller.NewClient(pullerAddr)
	if err := pclient.PullModel(ctx, modelID); err != nil {
		return err
	}

	const retryInterval = 2 * time.Second

	for i := 0; ; i++ {
		status, err := pclient.GetModel(ctx, modelID)
		if err != nil {
			return err
		}

		if status == http.StatusOK {
			break
		}

		log.Info("Waiting for the model to be pulled", "modelID", modelID, "status", status, "retryCount", i)
		time.Sleep(retryInterval)
	}

	log.Info("Model has been pulled", "modelID", modelID)

	vclient := vllm.NewHTTPClient(vllmAddr)

	path, err := modeldownloader.ModelFilePath(
		puller.ModelDir(),
		modelID,
		// Fine-tuned models always have the Hugging Face format.
		mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE,
	)
	if err != nil {
		return fmt.Errorf("model file path: %s", err)
	}

	// Convert the model name as we do the same conversion in processor.
	// TODO(kenji): Revisit.
	omid := ollama.ModelName(modelID)
	status, err := vclient.LoadLoRAAdapter(ctx, omid, path)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("load LoRA adapter: %d", status)
	}

	return nil
}

func unloadLoRAAdapter(
	ctx context.Context,
	vllmAddr string,
	modelID string,
) error {
	vclient := vllm.NewHTTPClient(vllmAddr)

	omid := ollama.ModelName(modelID)
	status, err := vclient.UnloadLoRAAdapter(ctx, omid)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("lbunoad LoRA adapter: %d", status)
	}
	return nil
}

// LoRAAdapterStatusGetter is a getter for LoRA adapter status.
type LoRAAdapterStatusGetter struct {
}

func (*LoRAAdapterStatusGetter) get(ctx context.Context, addr string) (*loRAAdapterStatus, error) {
	vclient := vllm.NewHTTPClient(addr)
	resp, err := vclient.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	s := loRAAdapterStatus{
		adapterIDs: make(map[string]struct{}),
	}

	for _, model := range resp.Data {
		if model.Parent == nil {
			s.baseModelID = model.ID
			continue
		}

		s.adapterIDs[model.ID] = struct{}{}
	}

	if s.baseModelID == "" && len(s.adapterIDs) > 0 {
		return nil, fmt.Errorf("only adapter IDs found: %v", s.adapterIDs)
	}

	return &s, nil
}
