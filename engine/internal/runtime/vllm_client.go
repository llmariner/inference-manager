package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	"github.com/llmariner/inference-manager/engine/internal/puller"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const vllmHTTPPort = 80

const (
	nvidiaGPUResource     = "nvidia.com/gpu"
	awsNeuroncoreResource = "aws.amazon.com/neuroncore"
)

type modelClient interface {
	GetBaseModelPath(ctx context.Context, in *mv1.GetBaseModelPathRequest, opts ...grpc.CallOption) (*mv1.GetBaseModelPathResponse, error)
	GetModelAttributes(ctx context.Context, in *mv1.GetModelAttributesRequest, opts ...grpc.CallOption) (*mv1.ModelAttributes, error)
}

// NewVLLMClient creates a new VLLM runtime client.
func NewVLLMClient(
	opts NewCommonClientOptions,
	modelClient modelClient,
	vLLMConfg *config.VLLMConfig,
) Client {
	return &vllmClient{
		commonClient: newCommonClient(opts, vllmHTTPPort),
		modelClient:  modelClient,
		vLLMConfig:   vLLMConfg,
	}
}

type vllmClient struct {
	*commonClient

	modelClient modelClient

	vLLMConfig *config.VLLMConfig
}

// DeployRuntime deploys the runtime for the given model.
func (v *vllmClient) DeployRuntime(ctx context.Context, modelID string, update bool) (*appsv1.StatefulSet, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Deploying VLLM runtime for model", "model", modelID)

	params, err := v.deployRuntimeParams(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("deploy runtime params: %s", err)
	}

	return v.deployRuntime(ctx, params, update)
}

func (v *vllmClient) deployRuntimeParams(ctx context.Context, modelID string) (deployRuntimeParams, error) {
	mci, err := v.commonClient.modelConfigItem(ctx, modelID)
	if err != nil {
		return deployRuntimeParams{}, fmt.Errorf("model config info: %s", err)
	}

	// Remove the "ft:" suffix if it exists. This is confusing, but we
	// need to do this because the processor does the same converesion when
	// processing requests (for Ollama)
	// TODO(kenji): Remove this once the processor does not require this conversion.
	oModelID := ollama.ModelName(modelID)

	args := []string{
		"--port", strconv.Itoa(vllmHTTPPort),
	}
	// Ultravox models is a model to handle audio input, which require a specific tokenizer.
	if strings.Contains(modelID, "ultravox") {
		args = append(args, "--tokenizer", "fixie-ai/ultravox-v0_3")
	}
	// We only set the chat template and do not set the tokenizer as the model files provide necessary information
	// such as stop tokens.
	if t := chatTemplate(modelID); t != "" {
		// Set --chat-template only if it is not explicitly set in the extra flags.
		var found = false
		for _, f := range mci.VLLMExtraFlags {
			if f == "--chat-template" {
				found = true
				break
			}
		}
		if !found {
			// TODO(kenji): Return an error if the model file doesn't include a template config and it must be
			// explicitly specified.
			args = append(args, "--chat-template", t)
		}
	}

	if isBaseModel, err := v.isBaseModel(ctx, modelID); err != nil {
		return deployRuntimeParams{}, err
	} else if isBaseModel {
		mPath, err := v.baseModelFilePath(ctx, modelID)
		if err != nil {
			return deployRuntimeParams{}, fmt.Errorf("base model file path: %s", err)
		}
		args = append(args,
			"--served-model-name", oModelID,
			"--model", mPath,
		)
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

		mPath, err := modeldownloader.ModelFilePath(puller.ModelDir(), modelID, format)
		if err != nil {
			return deployRuntimeParams{}, fmt.Errorf("model file path: %s", err)
		}

		if attr.Adapter == mv1.AdapterType_ADAPTER_TYPE_LORA {
			bmPath, err := v.baseModelFilePath(ctx, attr.BaseModel)
			if err != nil {
				return deployRuntimeParams{}, fmt.Errorf("base model file path: %s", err)
			}

			// When LoRA is used, set --served-model-name to the base model name ID, not to the fine-tuned model ID.
			// vLLM serves both the base model and the fined-tune model, and --served-model-name is used to specify
			// the base model name. See https://docs.vllm.ai/en/v0.5.5/models/lora.html#serving-lora-adapters.
			args = append(args,
				"--served-model-name", attr.BaseModel,
				"--model", bmPath,
				"--enable-lora",
				"--lora-modules", fmt.Sprintf("%s=%s", oModelID, mPath),
			)
		} else {
			// TODO(kenji): Verify this code path.
			args = append(args,
				"--served-model-name", oModelID,
				"--model", mPath,
			)
		}
	}

	if gpus, err := numGPUs(mci.Resources); err != nil {
		return deployRuntimeParams{}, err
	} else if gpus == 0 {
		args = append(args, "--device", "cpu")
	} else {
		args = append(args, "--tensor-parallel-size", strconv.Itoa(gpus))
	}

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

	if fs := mci.VLLMExtraFlags; len(fs) > 0 {
		args = append(args, fs...)
	}

	var image string
	if mci.Image != "" {
		image = mci.Image
	} else {
		var ok bool
		image, ok = v.rconfig.RuntimeImages[config.RuntimeNameVLLM]
		if !ok {
			return deployRuntimeParams{}, fmt.Errorf("runtime image not found for vllm")
		}
	}

	// TODO(kenji):This is a hack. Revisit.
	if strings.HasPrefix(image, "ghcr.io/llm-d/llm-d-inference-sim") {
		args = removeFlagsUnsupportedInInferenceSim(args)
	}

	envs := []*corev1apply.EnvVarApplyConfiguration{
		corev1apply.EnvVar().WithName("VLLM_LOGGING_LEVEL").WithValue(v.vLLMConfig.LoggingLevel),
		// Increase the timeout (default: 10 seconds) as we hit
		// https://github.com/vllm-project/vllm/discussions/9418 when
		// running the Nvidia Llama3.1 Nemotron 70B with a larger context size.
		corev1apply.EnvVar().WithName("VLLM_RPC_TIMEOUT").WithValue("60000"),
	}

	if v.vLLMConfig.DynamicLoRALoading {
		// Enable dynamically serving LoRA Adapters.
		// https://docs.vllm.ai/en/stable/features/lora.html
		envs = append(envs, corev1apply.EnvVar().WithName("VLLM_ALLOW_RUNTIME_LORA_UPDATING").WithValue("True"))
	}

	// If the puller daemon mode is enabled, create another init container
	// so that the VLLM container won't start until the base model is pulled.
	var initContainerSepc *initContainerSpec
	if v.vLLMConfig.DynamicLoRALoading {
		cpath := modeldownloader.CompletionIndicationFilePath(puller.ModelDir(), modelID)
		initContainerSepc = &initContainerSpec{
			name: "pull-waiter",
			// TODO(kenji): Fix. Use a better image.
			image:           image,
			imagePullPolicy: corev1.PullIfNotPresent,
			command:         []string{"/bin/sh", "-c"},
			// wait for the cpath file to be created
			args: []string{"while [ ! -f " + cpath + " ]; do echo 'Waiting for the model pull'; sleep 1; done"},
		}
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
		args:              args,
		initContainerSpec: initContainerSepc,
		pullerDaemonMode:  v.vLLMConfig.DynamicLoRALoading,
		pullerPort:        v.rconfig.PullerPort,
	}, nil
}

func (v *vllmClient) preferredBaseModelFormat(ctx context.Context, modelID string) (mv1.ModelFormat, error) {
	// TODO(kenji): Support non-base model.
	resp, err := v.modelClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: modelID,
	})
	if err != nil {
		return mv1.ModelFormat_MODEL_FORMAT_UNSPECIFIED, fmt.Errorf("get base model path: %s", err)
	}
	return puller.PreferredModelFormat(config.RuntimeNameVLLM, resp.Formats)
}

func (v *vllmClient) isBaseModel(ctx context.Context, modelID string) (bool, error) {
	model, err := v.modelGetter.GetModel(ctx, &mv1.GetModelRequest{
		Id: modelID,
	})
	if err != nil {
		return false, fmt.Errorf("get model: %s", err)
	}
	return model.IsBaseModel, nil
}

func (v *vllmClient) baseModelFilePath(ctx context.Context, modelID string) (string, error) {
	format, err := v.preferredBaseModelFormat(ctx, modelID)
	if err != nil {
		return "", err
	}
	return modeldownloader.ModelFilePath(puller.ModelDir(), modelID, format)
}

func numGPUs(resConf config.Resources) (int, error) {
	for _, resName := range []string{nvidiaGPUResource, awsNeuroncoreResource} {
		r, ok := resConf.Limits[resName]
		if !ok {
			continue
		}
		val, err := resource.ParseQuantity(r)
		if err != nil {
			return 0, fmt.Errorf("invalid resource limit: %s", err)
		}
		return int(val.Value()), nil
	}

	return 0, nil
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
func chatTemplate(modelID string) string {
	switch {
	case strings.HasPrefix(modelID, "meta-llama-Meta-Llama-3.1-"),
		strings.HasPrefix(modelID, "meta-llama-Meta-Llama-3.3-"),
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
`
	case strings.HasPrefix(modelID, "deepseek-ai-deepseek-coder-6.7b-base"),
		strings.HasPrefix(modelID, "deepseek-ai-DeepSeek-Coder-V2-Lite-Base"):
		// This is a simplified template that works for auto code completion.
		// See https://huggingface.co/deepseek-ai/deepseek-coder-6.7b-instruct/blob/main/tokenizer_config.json#L34.
		return `
{% for message in messages %}
{{message['content']}}
{% endfor %}
`
	case modelID == "intfloat-e5-mistral-7b-instruct":
		// This model is for embedding.
		return `
{% for message in messages %}
{{message['content']}}
{% endfor %}
`
	case strings.Contains(modelID, "phi-4"):
		// Follow https://huggingface.co/microsoft/phi-4.
		return `
{% for message in messages %}
{{'<|im_start|>' + message['role'] + '<|im_sep|>\n' + message['content'] + '\n<|im_end|>\n'}}
{% endfor %}
`
	case strings.Contains(modelID, "ultravox"):
		return ""
	default:
		return ""
	}
}

// RuntimeName returns the runtime name.
func (v *vllmClient) RuntimeName() string {
	return config.RuntimeNameVLLM
}

func removeFlagsUnsupportedInInferenceSim(args []string) []string {
	ignore := map[string]bool{
		"--device":               true,
		"--chat-template":        true,
		"--tensor-parallel-size": true,
	}

	var (
		newArgs  []string
		skipNext bool
	)
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if ignore[arg] {
			skipNext = true
			continue
		}
		newArgs = append(newArgs, arg)

	}
	return newArgs
}
