package runtime

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/vllm"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RuntimeNameVLLM is the name of the VLLM runtime.
const RuntimeNameVLLM = "vllm"

const vllmHTTPPort = 80

const nvidiaGPUResource = "nvidia.com/gpu"

// NewVLLMClient creates a new VLLM runtime client.
func NewVLLMClient(
	k8sClient client.Client,
	namespace string,
	rconfig config.RuntimeConfig,
	vconfig config.VLLMConfig,
	modelContextLengths map[string]int,
) Client {
	return &vllmClient{
		commonClient: &commonClient{
			k8sClient:     k8sClient,
			namespace:     namespace,
			servingPort:   vllmHTTPPort,
			RuntimeConfig: rconfig,
		},
		config:              vconfig,
		modelContextLengths: modelContextLengths,
	}
}

type vllmClient struct {
	*commonClient

	config              config.VLLMConfig
	modelContextLengths map[string]int
}

// DeployRuntime deploys the runtime for the given model.
func (v *vllmClient) DeployRuntime(ctx context.Context, modelID string) (types.NamespacedName, error) {
	log.Printf("Deploying VLLM runtime for model %s\n", modelID)

	template, err := vllm.ChatTemplate(modelID)
	if err != nil {
		return types.NamespacedName{}, fmt.Errorf("get chat template: %s", err)
	}

	args := []string{
		"--port", strconv.Itoa(vllmHTTPPort),
		"--model", vllm.ModelFilePath(modelDir, modelID),
		"--served-model-name", modelID,
		// We only set the chat template and do not set the tokenizer as the GGUI file provides necessary information
		// such as stop tokens.
		"--chat-template", template,
	}

	if gpus, err := v.numGPUs(modelID); err != nil {
		return types.NamespacedName{}, err
	} else if gpus == 0 {
		args = append(args, "--device", "cpu")
	} else {
		args = append(args, "--tensor-parallel-size", strconv.Itoa(gpus))
	}

	if len, ok := v.modelContextLengths[modelID]; ok {
		args = append(args, "--max-model-len", strconv.Itoa(len))
	}

	if vllm.IsAWQQuantizedModel(modelID) {
		args = append(args, "--quantization", "awq")
	}

	shmVolName := "devshm"
	return v.deployRuntime(ctx, deployRunTimeParams{
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
	})
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
