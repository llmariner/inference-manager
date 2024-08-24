package runtime

import (
	"context"
	"fmt"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RuntimeNameVLLM is the name of the VLLM runtime.
const RuntimeNameVLLM = "vllm"

const vllmHTTPPort = 80

// NewVLLMClient creates a new VLLM runtime client.
func NewVLLMClient(
	k8sClient client.Client,
	namespace string,
	rconfig config.RuntimeConfig,
	vconfig config.VLLMConfig,
) Client {
	return &vllmClient{
		commonClient: &commonClient{
			k8sClient:     k8sClient,
			namespace:     namespace,
			servingPort:   vllmHTTPPort,
			RuntimeConfig: rconfig,
		},
		config: vconfig,
	}
}

type vllmClient struct {
	*commonClient

	config config.VLLMConfig
}

// DeployRuntime deploys the runtime for the given model.
func (v *vllmClient) DeployRuntime(ctx context.Context, modelID string) error {
	return fmt.Errorf("not implemented")
}
