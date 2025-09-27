package runtime

import (
	"context"
	"fmt"
	"strconv"

	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	"github.com/llmariner/inference-manager/engine/internal/puller"
	mv1 "github.com/llmariner/model-manager/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const sglangHTTPPort = 80

// NewSGLangClient creates a new SGLang runtime client.
func NewSGLangClient(
	opts NewCommonClientOptions,
	modelClient modelClient,
) Client {
	return &sglangClient{
		commonClient: newCommonClient(opts, sglangHTTPPort),
		modelClient:  modelClient,
	}
}

type sglangClient struct {
	*commonClient

	modelClient modelClient
}

// DeployRuntime deploys the runtime for the given model.
func (s *sglangClient) DeployRuntime(ctx context.Context, model *mv1.Model, update bool) (*appsv1.StatefulSet, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Deploying SGLang runtime for model", "model", model.Id)

	params, err := s.deployRuntimeParams(ctx, model)
	if err != nil {
		return nil, fmt.Errorf("deploy runtime params: %s", err)
	}

	return s.deployRuntime(ctx, params, update)
}

func (s *sglangClient) deployRuntimeParams(ctx context.Context, model *mv1.Model) (deployRuntimeParams, error) {
	mci := s.ModelConfigItem(model)

	// Remove the "ft:" suffix if it exists. This is confusing, but we
	// need to do this because the processor does the same conversion when
	// processing requests (for Ollama)
	// TODO(kenji): Remove this once the processor does not require this conversion.
	oModelID := ollama.ModelName(model.Id)

	args := []string{
		"-m",
		"sglang.launch_server",
		// To accept requests from outside the container.
		"--host", "0.0.0.0",
		"--port", strconv.Itoa(sglangHTTPPort),
	}

	if model.IsBaseModel {
		mPath, err := s.baseModelFilePath(ctx, model.Id)
		if err != nil {
			return deployRuntimeParams{}, fmt.Errorf("base model file path: %s", err)
		}
		args = append(args,
			"--served-model-name", oModelID,
			"--model-path", mPath,
		)
	} else {
		return deployRuntimeParams{}, fmt.Errorf("non-base model is not supported for SGLang runtime")
	}

	if gpus, err := numGPUs(mci.Resources); err != nil {
		return deployRuntimeParams{}, err
	} else if gpus > 0 {
		args = append(args, "--tensor-parallel-size", strconv.Itoa(gpus))
	}

	return deployRuntimeParams{
		model: model,
		// Shared memory is required for Pytorch
		// (See https://docs.vllm.ai/en/latest/serving/deploying_with_docker.html#deploying-with-docker).
		volumes: []*corev1apply.VolumeApplyConfiguration{
			shmemVolume(),
		},
		volumeMounts: []*corev1apply.VolumeMountApplyConfiguration{
			shmemVolumeMount(),
		},
		readinessProbe: corev1apply.Probe().
			WithHTTPGet(corev1apply.HTTPGetAction().
				WithPort(intstr.FromInt(sglangHTTPPort)).
				WithPath("/health")).
			// Have a longer timeout thant default (1 second) as the health endpoint sometimes responds slowly.
			WithTimeoutSeconds(3),
		command:    []string{"python3"},
		args:       args,
		pullerPort: s.rconfig.PullerPort,
	}, nil
}

func (s *sglangClient) preferredBaseModelFormat(ctx context.Context, modelID string) (mv1.ModelFormat, error) {
	// TODO(kenji): Support non-base model.
	resp, err := s.modelClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: modelID,
	})
	if err != nil {
		return mv1.ModelFormat_MODEL_FORMAT_UNSPECIFIED, fmt.Errorf("get base model path: %s", err)
	}
	return puller.PreferredModelFormat(config.RuntimeNameSGLang, resp.Formats)
}

func (s *sglangClient) baseModelFilePath(ctx context.Context, modelID string) (string, error) {
	format, err := s.preferredBaseModelFormat(ctx, modelID)
	if err != nil {
		return "", err
	}
	return modeldownloader.ModelFilePath(puller.ModelDir(), modelID, format)
}

// RuntimeName returns the runtime name.
func (s *sglangClient) RuntimeName() string {
	return config.RuntimeNameSGLang
}
