package runtime

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const vllmHTTPPort = 80

const nvidiaGPUResource = "nvidia.com/gpu"

type modelClient interface {
	GetBaseModelPath(ctx context.Context, in *mv1.GetBaseModelPathRequest, opts ...grpc.CallOption) (*mv1.GetBaseModelPathResponse, error)
}

// NewVLLMClient creates a new VLLM runtime client.
func NewVLLMClient(
	k8sClient client.Client,
	namespace string,
	rconfig *config.RuntimeConfig,
	mconfig *config.ProcessedModelConfig,
	modelClient modelClient,
) Client {
	return &vllmClient{
		commonClient: &commonClient{
			k8sClient:   k8sClient,
			namespace:   namespace,
			servingPort: vllmHTTPPort,
			rconfig:     rconfig,
			mconfig:     mconfig,
		},
		modelClient: modelClient,
	}
}

type vllmClient struct {
	*commonClient

	modelClient modelClient
}

// DeployRuntime deploys the runtime for the given model.
func (v *vllmClient) DeployRuntime(ctx context.Context, modelID string, update bool) (*appsv1.StatefulSet, error) {
	log.Printf("Deploying VLLM runtime for model %s\n", modelID)

	params, err := v.deployRuntimeParams(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("deploy runtime params: %s", err)
	}

	return v.deployRuntime(ctx, params, update)
}

func (v *vllmClient) deployRuntimeParams(ctx context.Context, modelID string) (deployRuntimeParams, error) {
	modelFilePath, err := v.modelFilePath(ctx, modelID)
	if err != nil {
		return deployRuntimeParams{}, fmt.Errorf("model file path: %s", err)
	}

	template, err := chatTemplate(modelID)
	if err != nil {
		return deployRuntimeParams{}, fmt.Errorf("get chat template: %s", err)
	}

	args := []string{
		"--port", strconv.Itoa(vllmHTTPPort),
		"--model", modelFilePath,
		"--served-model-name", modelID,
		// We only set the chat template and do not set the tokenizer as the model files provide necessary information
		// such as stop tokens.
		"--chat-template", template,
	}

	if gpus, err := v.numGPUs(modelID); err != nil {
		return deployRuntimeParams{}, err
	} else if gpus == 0 {
		args = append(args, "--device", "cpu")
	} else {
		args = append(args, "--tensor-parallel-size", strconv.Itoa(gpus))
	}

	mci := v.mconfig.ModelConfigItem(modelID)
	if mci.ContextLength > 0 {
		args = append(args, "--max-model-len", strconv.Itoa(mci.ContextLength))
	}

	if isAWQQuantizedModel(modelID) {
		args = append(args, "--quantization", "awq")
	}

	shmVolName := "devshm"
	return deployRuntimeParams{
		modelID: modelID,
		// Shared memory is required for Pytorch
		// (See https://docs.vllm.ai/en/latest/serving/deploying_with_docker.html#deploying-with-docker).
		volumes: []*corev1apply.VolumeApplyConfiguration{
			corev1apply.Volume().WithName(shmVolName).
				// TODO(kenji): Set the limit.
				WithEmptyDir(corev1apply.EmptyDirVolumeSource()),
		},
		volumeMounts: []*corev1apply.VolumeMountApplyConfiguration{
			corev1apply.VolumeMount().
				WithName(shmVolName).
				WithMountPath("/dev/shm"),
		},
		// TODO(kenji): Fix the readiness check. A 200 response from the /health endpoint does not indicate
		// that the model has been loaded (https://github.com/vllm-project/vllm/issues/6073), but
		// we want to make the pod ready after the model has been loaded.
		readinessProbe: corev1apply.Probe().
			WithHTTPGet(corev1apply.HTTPGetAction().
				WithPort(intstr.FromInt(vllmHTTPPort)).
				WithPath("/health")),
		args: args,
	}, nil
}

func (v *vllmClient) numGPUs(modelID string) (int, error) {
	resConf := v.getResouces(modelID)
	r, ok := resConf.Limits[nvidiaGPUResource]
	if !ok {
		return 0, nil
	}

	val, err := resource.ParseQuantity(r)
	if err != nil {
		return 0, fmt.Errorf("invalid resource limit: %s", err)
	}
	return int(val.Value()), nil
}

func (v *vllmClient) modelFilePath(ctx context.Context, modelID string) (string, error) {
	// TODO(kenji): Support non-base model.
	resp, err := v.modelClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: modelID,
	})
	if err != nil {
		return "", err
	}
	format, err := PreferredModelFormat(config.RuntimeNameVLLM, resp.Formats)
	if err != nil {
		return "", err
	}
	return modeldownloader.ModelFilePath(modelDir, modelID, format)
}

// isAWQQuantizedModel returns true if the model name is an AWQ quantized model.
func isAWQQuantizedModel(modelID string) bool {
	return strings.HasSuffix(modelID, "-awq")
}

// chatTemplate returns the chat template for the given model.
func chatTemplate(modelID string) (string, error) {
	switch {
	case strings.HasPrefix(modelID, "meta-llama-Meta-Llama-3.1-"),
		strings.HasPrefix(modelID, "TinyLlama-TinyLlama-1.1B-Chat-v1.0"),
		strings.HasPrefix(modelID, "mattshumer-Reflection-Llama-3.1-70B"):
		// This is a simplified template that does not support functions etc.
		// Please see https://llama.meta.com/docs/model-cards-and-prompt-formats/llama3_1/ for the spec.
		return `
<|begin_of_text|>
{% for message in messages %}
{{'<|start_header_id|>' + message['role'] + '<|end_header_id|>\n' + message['content'] + '\n<|eot_id|>\n'}}
{% endfor %}
`, nil
	case strings.HasPrefix(modelID, "deepseek-ai-deepseek-coder-6.7b-base"),
		strings.HasPrefix(modelID, "deepseek-ai-DeepSeek-Coder-V2-Lite-Base"):
		// This is a simplified template that works for auto code completion.
		// See https://huggingface.co/deepseek-ai/deepseek-coder-6.7b-instruct/blob/main/tokenizer_config.json#L34.
		return `
{% for message in messages %}
{{message['content']}}
{% endfor %}
`, nil
	case modelID == "intfloat-e5-mistral-7b-instruct":
		// This model is for embedding.
		return `
{% for message in messages %}
{{message['content']}}
{% endfor %}
`, nil
	default:
		return "", fmt.Errorf("unsupported model: %q", modelID)
	}
}
