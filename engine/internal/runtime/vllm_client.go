package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const vllmHTTPPort = 80

const nvidiaGPUResource = "nvidia.com/gpu"

type modelClient interface {
	GetBaseModelPath(ctx context.Context, in *mv1.GetBaseModelPathRequest, opts ...grpc.CallOption) (*mv1.GetBaseModelPathResponse, error)
	GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error)
	GetModelAttributes(ctx context.Context, in *mv1.GetModelAttributesRequest, opts ...grpc.CallOption) (*mv1.ModelAttributes, error)
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
	log := ctrl.LoggerFrom(ctx)
	log.Info("Deploying VLLM runtime for model %s\n", modelID)

	params, err := v.deployRuntimeParams(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("deploy runtime params: %s", err)
	}

	return v.deployRuntime(ctx, params, update)
}

func (v *vllmClient) deployRuntimeParams(ctx context.Context, modelID string) (deployRuntimeParams, error) {
	model, err := v.modelClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: modelID,
	})
	if err != nil {
		return deployRuntimeParams{}, fmt.Errorf("get model: %s", err)
	}

	template, err := chatTemplate(modelID)
	if err != nil {
		return deployRuntimeParams{}, fmt.Errorf("get chat template: %s", err)
	}

	// Remove the "ft:" suffix if it exists. This is confusing, but we
	// need to do this because the processor does the same converesion when
	// processing requests (for Ollama)
	// TODO(kenji): Remove this once the processor does not require this conversion.
	oModelID := ollama.ModelName(modelID)

	args := []string{
		"--port", strconv.Itoa(vllmHTTPPort),
		"--served-model-name", oModelID,
		// We only set the chat template and do not set the tokenizer as the model files provide necessary information
		// such as stop tokens.
		"--chat-template", template,
	}

	if isBaseModel(model) {
		mPath, err := v.baseModelFilePath(ctx, modelID)
		if err != nil {
			return deployRuntimeParams{}, fmt.Errorf("base model file path: %s", err)
		}
		args = append(args, "--model", mPath)
	} else {
		attr, err := v.modelClient.GetModelAttributes(ctx, &mv1.GetModelAttributesRequest{
			Id: modelID,
		})
		if err != nil {
			return deployRuntimeParams{}, fmt.Errorf("get model attributes: %s", err)
		}
		if attr.BaseModel == "" {
			return deployRuntimeParams{}, fmt.Errorf("base model ID is not set for %q", modelID)
		}
		format, err := v.preferredBaseModelFormat(ctx, attr.BaseModel)
		if err != nil {
			return deployRuntimeParams{}, err
		}

		mPath, err := modeldownloader.ModelFilePath(modelDir, modelID, format)
		if err != nil {
			return deployRuntimeParams{}, fmt.Errorf("model file path: %s", err)
		}

		if attr.Adapter == mv1.AdapterType_ADAPTER_TYPE_LORA {
			bmPath, err := v.baseModelFilePath(ctx, attr.BaseModel)
			if err != nil {
				return deployRuntimeParams{}, fmt.Errorf("base model file path: %s", err)
			}
			args = append(args,
				"--model", bmPath,
				"--enable-lora",
				"--lora-modules", fmt.Sprintf("%s=%s", oModelID, mPath),
			)
		} else {
			args = append(args, "--model", mPath)
		}
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

	if q, ok := vllmQuantization(modelID); ok {
		args = append(args, "--quantization", q)
		if q == "bitsandbytes" {
			// BitsAndBytes quantization only supports 'bitsandbytes' load format.
			args = append(args, "--load-format", "bitsandbytes")
		}
	}

	envs := []*corev1apply.EnvVarApplyConfiguration{
		corev1apply.EnvVar().WithName("VLLM_ALLOW_RUNTIME_LORA_UPDATING").WithValue("true"),
		corev1apply.EnvVar().WithName("VLLM_LOGGING_LEVEL").WithValue("DEBUG"),
	}

	return deployRuntimeParams{
		modelID: modelID,
		envs:    envs,
		// Shared memory is required for Pytorch
		// (See https://docs.vllm.ai/en/latest/serving/deploying_with_docker.html#deploying-with-docker).
		volumes: []*corev1apply.VolumeApplyConfiguration{
			shmemVolume(),
		},
		volumeMounts: []*corev1apply.VolumeMountApplyConfiguration{
			shmemVolumeMount(),
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

func (v *vllmClient) preferredBaseModelFormat(ctx context.Context, modelID string) (mv1.ModelFormat, error) {
	// TODO(kenji): Support non-base model.
	resp, err := v.modelClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: modelID,
	})
	if err != nil {
		return mv1.ModelFormat_MODEL_FORMAT_UNSPECIFIED, fmt.Errorf("get base model path: %s", err)
	}
	return PreferredModelFormat(config.RuntimeNameVLLM, resp.Formats)
}

func (v *vllmClient) baseModelFilePath(ctx context.Context, modelID string) (string, error) {
	format, err := v.preferredBaseModelFormat(ctx, modelID)
	if err != nil {
		return "", err
	}
	return modeldownloader.ModelFilePath(modelDir, modelID, format)
}

func isBaseModel(model *mv1.Model) bool {
	const systemOwner = "system"
	return model.OwnedBy == systemOwner
}

// vllmQuantization returns the quantization type of the given model.
// The second return value is true if the model is quantized.
//
// TODO(kenji): Instead of using a model ID suffix, store the quantization type in the model attributes.
func vllmQuantization(modelID string) (string, bool) {
	if strings.HasSuffix(modelID, "-awq") {
		return "awq", true
	}

	if strings.HasSuffix(modelID, "-bnb-4bit") {
		return "bitsandbytes", true
	}

	return "", false
}

// chatTemplate returns the chat template for the given model.
func chatTemplate(modelID string) (string, error) {
	switch {
	case strings.HasPrefix(modelID, "meta-llama-Meta-Llama-3.1-"),
		strings.HasPrefix(modelID, "mattshumer-Reflection-Llama-3.1-70B"),
		strings.Contains(modelID, "TinyLlama-1.1B"),
		strings.HasPrefix(modelID, "nvidia-Llama-3.1-Nemotron-"):
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
