package runtime

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	"github.com/llmariner/inference-manager/engine/internal/puller"
	"github.com/llmariner/inference-manager/engine/internal/vllm"
	mv1 "github.com/llmariner/model-manager/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func loadLoRAAdapter(
	ctx context.Context,
	modelID string,
	pullerAddr string,
	vllmAddr string,
) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Loading LoRA adapter to existing runtime", "modelID", modelID)

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

		log.Info("Waiting for the model to be pulled", "modelID", modelID, "status", status)
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
	r runtime,
	modelID string,
) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Unloading the LoRA adapter from the runtime", "model", modelID)

	vclient := vllm.NewHTTPClient(r.address)

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
