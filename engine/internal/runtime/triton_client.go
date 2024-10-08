package runtime

import (
	"context"
	"fmt"

	"github.com/llmariner/inference-manager/engine/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	tritonHTTPPort = 8000
	proxyHTTPPort  = 8001
)

// NewTritonClient creates a new Triton runtime client.
func NewTritonClient(
	k8sClient client.Client,
	namespace string,
	rconfig *config.RuntimeConfig,
	mconfig *config.ProcessedModelConfig,
) Client {
	return &tritonClient{
		commonClient: &commonClient{
			k8sClient:   k8sClient,
			namespace:   namespace,
			servingPort: tritonHTTPPort,
			rconfig:     rconfig,
			mconfig:     mconfig,
		},
	}
}

type tritonClient struct {
	*commonClient
}

// DeployRuntime deploys the runtime for the given model.
func (c *tritonClient) DeployRuntime(ctx context.Context, modelID string, update bool) (*appsv1.StatefulSet, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Deploying Triton runtime for model", "model", modelID)

	params, err := c.deployRuntimeParams(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("deploy runtime params: %s", err)
	}

	return c.deployRuntime(ctx, params, update)
}

func (c *tritonClient) deployRuntimeParams(ctx context.Context, modelID string) (deployRuntimeParams, error) {
	// TOOD(kenji): Remove this once Triton Inference Server supports OpenAI-compatible API
	// (https://github.com/triton-inference-server/server/pull/7561).
	proxyContainer := corev1apply.Container().
		WithName("proxy").
		WithImage(c.rconfig.TritonProxyImage).
		WithImagePullPolicy(corev1.PullPolicy(c.rconfig.TritonProxyImagePullPolicy)).
		WithArgs(
			"--port", fmt.Sprintf("%d", proxyHTTPPort),
			"--triton-server-base-url", fmt.Sprintf("http://localhost:%d", tritonHTTPPort)).
		WithPorts(corev1apply.ContainerPort().
			WithName("http").
			WithContainerPort(int32(proxyHTTPPort)).
			WithProtocol(corev1.ProtocolTCP))

	return deployRuntimeParams{
		modelID: modelID,
		// Shared memory is required for Pytorch
		// (See https://docs.triton.ai/en/latest/serving/deploying_with_docker.html#deploying-with-docker).
		volumes: []*corev1apply.VolumeApplyConfiguration{
			shmemVolume(),
		},
		volumeMounts: []*corev1apply.VolumeMountApplyConfiguration{
			shmemVolumeMount(),
		},
		readinessProbe: corev1apply.Probe().
			WithHTTPGet(corev1apply.HTTPGetAction().
				WithPort(intstr.FromInt(tritonHTTPPort)).
				WithPath("/v2/health/ready")),
		args: []string{
			"--model-repository", modelDir,
		},
		additionalContainers: []*corev1apply.ContainerApplyConfiguration{proxyContainer},
	}, nil
}
