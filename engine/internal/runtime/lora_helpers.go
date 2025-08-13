package runtime

import (
	"context"
	"fmt"
	"net/http"

	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	"github.com/llmariner/inference-manager/engine/internal/puller"
	"github.com/llmariner/inference-manager/engine/internal/runtime/vllm"
	mv1 "github.com/llmariner/model-manager/api/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type loraAdapterLoaderImpl struct {
}

func (*loraAdapterLoaderImpl) pullModel(
	ctx context.Context,
	pullerAddr string,
	modelID string,
) error {
	pclient := puller.NewClient(pullerAddr)
	if err := pclient.PullModel(ctx, modelID); err != nil {
		return err
	}
	return nil
}

func (*loraAdapterLoaderImpl) checkModelPullStatus(
	ctx context.Context,
	pullerAddr string,
	modelID string,
) (bool, error) {
	pclient := puller.NewClient(pullerAddr)

	status, err := pclient.GetModel(ctx, modelID)
	if err != nil {
		return false, err
	}

	return status == http.StatusOK, nil
}

func (*loraAdapterLoaderImpl) load(
	ctx context.Context,
	vllmAddr string,
	modelID string,
) error {
	// Convert the model name as we do the same conversion in processor.
	// TODO(kenji): Revisit.
	omid := ollama.ModelName(modelID)

	vclient := vllm.NewHTTPClient(vllmAddr)

	// Check if the model is already loaded. If so, do not load it again.
	resp, err := vclient.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("list models: %s", err)
	}
	for _, model := range resp.Data {
		if model.ID == omid {
			// Model is already loaded.
			return nil
		}
	}

	path, err := modeldownloader.ModelFilePath(
		puller.ModelDir(),
		modelID,
		// Fine-tuned models always have the Hugging Face format.
		mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE,
	)
	if err != nil {
		return fmt.Errorf("model file path: %s", err)
	}

	code, err := vclient.LoadLoRAAdapter(ctx, omid, path)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("load LoRA adapter: code=%d", code)
	}

	return nil
}

func (*loraAdapterLoaderImpl) unload(
	ctx context.Context,
	vllmAddr string,
	modelID string,
) error {
	vclient := vllm.NewHTTPClient(vllmAddr)

	// Convert the model name as we do the same conversion in processor.
	// TODO(kenji): Revisit.
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

type loraAdapterStatusGetterImpl struct {
}

func (*loraAdapterStatusGetterImpl) get(ctx context.Context, addr string) (*loRAAdapterStatus, error) {
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

		// Convert the model name back to the original ID as we do the conversion when loading the LoRA adapter.
		// TODO(kenji): Revisit.
		origID := ollama.OriginalFineTuningModelName(model.ID)
		s.adapterIDs[origID] = struct{}{}
	}

	if s.baseModelID == "" && len(s.adapterIDs) > 0 {
		return nil, fmt.Errorf("only adapter IDs found: %v", s.adapterIDs)
	}

	return &s, nil
}

type loraAdapterLoadingTargetSelectorImpl struct {
	rtClientFactory ClientFactory
	k8sClient       client.Client
}

func (s *loraAdapterLoadingTargetSelectorImpl) selectTarget(
	ctx context.Context,
	modelID string,
	stsName string,
) (*corev1.Pod, error) {
	pods, err := s.listPods(ctx, modelID, stsName)
	if err != nil {
		return nil, err
	}

	var found, foundReady *corev1.Pod
	// TODO(kenji): Pick up the least-loaded ready pod.
	for _, pod := range pods {
		p := pod
		if p.Status.PodIP == "" {
			continue
		}

		if found == nil {
			// Set the first available pod as fallback target.
			found = &p
		}

		if isPodReady(&p) {
			foundReady = &p
			break
		}
	}

	if found == nil {
		// TODO(kenji): Add a retry or gracefully handle.
		return nil, fmt.Errorf("no pod with IP address found")
	}

	if foundReady != nil {
		// Prefer a ready pod.
		return foundReady, nil
	}

	return found, nil
}

func (s *loraAdapterLoadingTargetSelectorImpl) targetExists(
	ctx context.Context,
	modelID string,
	pod *corev1.Pod,
) (bool, error) {
	stsName, ok := pod.Labels["app.kubernetes.io/instance"]
	if !ok {
		return false, fmt.Errorf("pod %s does not have app.kubernetes.io/instance label", pod.Name)
	}

	pods, err := s.listPods(ctx, modelID, stsName)
	if err != nil {
		return false, err
	}

	var found bool
	// TODO(kenji): Pick up the least-loaded ready pod.
	for _, p := range pods {
		if p.UID == pod.UID {
			found = true
			break
		}
	}

	return found, nil
}

func (s *loraAdapterLoadingTargetSelectorImpl) listPods(
	ctx context.Context,
	modelID string,
	stsName string,
) ([]corev1.Pod, error) {
	client, err := s.rtClientFactory.New(modelID)
	if err != nil {
		return nil, err
	}

	pods, err := listPods(ctx, s.k8sClient, client.Namespace(), stsName)
	if err != nil {
		return nil, err
	}

	return pods, nil
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
