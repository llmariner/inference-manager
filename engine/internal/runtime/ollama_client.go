package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	models "github.com/llm-operator/inference-manager/engine/internal/models"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ollamaHTTPPort = 11434

type modelGetter interface {
	GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error)
}

// NewOllamaClient creates a new Ollama runtime client.a
func NewOllamaClient(
	k8sClient client.Client,
	namespace string,
	rconfig *config.RuntimeConfig,
	mconfig *config.ProcessedModelConfig,
	oconfig config.OllamaConfig,
	modelClient modelGetter,
) Client {
	return &ollamaClient{
		commonClient: &commonClient{
			k8sClient:   k8sClient,
			namespace:   namespace,
			servingPort: ollamaHTTPPort,
			rconfig:     rconfig,
			mconfig:     mconfig,
		},
		config:      oconfig,
		modelClient: modelClient,
	}
}

type ollamaClient struct {
	*commonClient

	config      config.OllamaConfig
	modelClient modelGetter
}

// DeployRuntime deploys the runtime for the given model.
func (o *ollamaClient) DeployRuntime(ctx context.Context, modelID string, update bool) (*appsv1.StatefulSet, error) {
	initEnvs := []*corev1apply.EnvVarApplyConfiguration{
		corev1apply.EnvVar().WithName("OLLAMA_MODELS").WithValue(modelDir),

		// Ollama creates a payload in a temporary directory by default, and a new temporary directory is created
		// whenever Ollama restarts. This is a problem when a persistent volume is mounted.
		// To avoid this, we set the directory to a fixed path.
		//
		// TODO(kenji): Make sure there is no issue when multiple pods start at the same time.
		corev1apply.EnvVar().WithName("OLLAMA_RUNNERS_DIR").WithValue(o.config.RunnersDir),
	}

	envs := []*corev1apply.EnvVarApplyConfiguration{
		corev1apply.EnvVar().WithName("OLLAMA_MODELS").WithValue(modelDir),
		corev1apply.EnvVar().WithName("OLLAMA_KEEP_ALIVE").WithValue(o.config.KeepAlive.String()),
		corev1apply.EnvVar().WithName("OLLAMA_RUNNERS_DIR").WithValue(o.config.RunnersDir),
	}
	if o.config.NumParallel > 0 {
		envs = append(envs, corev1apply.EnvVar().WithName("OLLAMA_NUM_PARALLEL").WithValue(strconv.Itoa(o.config.NumParallel)))
	}
	if o.config.ForceSpreading {
		envs = append(envs, corev1apply.EnvVar().WithName("OLLAMA_FORCE_SPREADING").WithValue("true"))
	}
	if o.config.Debug {
		envs = append(envs, corev1apply.EnvVar().WithName("OLLAMA_DEBUG").WithValue("true"))
	}

	args := []string{
		"serve",
	}

	image, ok := o.rconfig.RuntimeImages[config.RuntimeNameOllama]
	if !ok {
		return nil, fmt.Errorf("image not found for runtime %s", config.RuntimeNameOllama)
	}

	isBase, err := models.IsBaseModel(ctx, o.modelClient, modelID)
	if err != nil {
		return nil, err
	}

	var modelIDs []string
	if isBase {
		modelIDs = append(modelIDs, modelID)
	} else {
		baseModelID, err := models.ExtractBaseModel(modelID)
		if err != nil {
			return nil, err
		}
		modelIDs = append(modelIDs, baseModelID, modelID)
	}

	// Create an init container that starts an Ollama server process in background and create a modelfile.
	// TODO(kenji): Revisit once Ollama supports model file creation without server (https://github.com/ollama/ollama/issues/3369)
	var createCmds []string
	for _, id := range modelIDs {
		createCmds = append(createCmds, fmt.Sprintf(`
while true; do
  ollama create %s -f %s && break
  sleep 1
done
`, ollama.ModelName(id), ollama.ModelfilePath(modelDir, id)))
	}

	// Start an Ollama server process in background and create a modelfile.
	// Revisit once Ollama supports model file creation without server (https://github.com/ollama/ollama/issues/3369)
	script := fmt.Sprintf(`
ollama serve &
serve_pid=$!

%s

kill ${serve_pid}
`, strings.Join(createCmds, "\n"))

	return o.deployRuntime(ctx, deployRuntimeParams{
		modelID:  modelID,
		initEnvs: initEnvs,
		envs:     envs,
		readinessProbe: corev1apply.Probe().
			WithHTTPGet(corev1apply.HTTPGetAction().
				WithPort(intstr.FromInt(ollamaHTTPPort))),
		args: args,
		initContainerSpec: &initContainerSpec{
			name:    "ollama-init",
			image:   image,
			command: []string{"/bin/bash"},
			args:    []string{"-c", script},
			// Use the same policy as the runtime as the container image is the same as the runtime.
			imagePullPolicy: corev1.PullPolicy(o.rconfig.RuntimeImagePullPolicy),
		},
	}, update)
}
