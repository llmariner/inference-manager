package runtime

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/puller"
	mv1 "github.com/llmariner/model-manager/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	tritonHTTPPort = 8000
	proxyHTTPPort  = 9000
)

// NewTritonClient creates a new Triton runtime client.
func NewTritonClient(opts NewCommonClientOptions) Client {
	return &tritonClient{
		// Set the servingPort to the proxy port so that requests first hit the proxy (and then the proxy forwards to Triton).
		commonClient: newCommonClient(opts, proxyHTTPPort),
	}
}

type tritonClient struct {
	*commonClient
}

// DeployRuntime deploys the runtime for the given model.
func (c *tritonClient) DeployRuntime(ctx context.Context, model *mv1.Model, update bool) (*appsv1.StatefulSet, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Deploying Triton runtime for model", "model", model.Id)

	params, err := c.deployRuntimeParams(ctx, model)
	if err != nil {
		return nil, fmt.Errorf("deploy runtime params: %s", err)
	}

	return c.deployRuntime(ctx, params, update)
}

func (c *tritonClient) deployRuntimeParams(ctx context.Context, model *mv1.Model) (deployRuntimeParams, error) {
	// TOOD(kenji): Remove this once Triton Inference Server supports OpenAI-compatible API
	// (https://github.com/triton-inference-server/server/pull/7561).
	proxyContainer := corev1apply.Container().
		WithName("proxy").
		WithImage(c.rconfig.TritonProxyImage).
		WithImagePullPolicy(corev1.PullPolicy(c.rconfig.TritonProxyImagePullPolicy)).
		WithArgs(
			"run",
			"--port", fmt.Sprintf("%d", proxyHTTPPort),
			"--triton-server-base-url", fmt.Sprintf("http://localhost:%d", tritonHTTPPort)).
		WithPorts(corev1apply.ContainerPort().
			WithName("http").
			WithContainerPort(int32(proxyHTTPPort)).
			WithProtocol(corev1.ProtocolTCP)).
		WithTerminationMessagePolicy(corev1.TerminationMessageFallbackToLogsOnError)

	return deployRuntimeParams{
		model: model,
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
			"tritonserver",
			// TODO(kenji): Make Model Manager tracks the path and and returns.
			// This is a hack to make this work for the model we compiled for LLama3.1 7B.
			"--model-repository", filepath.Join(puller.ModelDir(), "repo/llama3"),
		},
		runtimePort:          tritonHTTPPort,
		additionalContainers: []*corev1apply.ContainerApplyConfiguration{proxyContainer},
	}, nil
}

// RuntimeName returns the runtime name.
func (c *tritonClient) RuntimeName() string {
	return config.RuntimeNameTriton
}
