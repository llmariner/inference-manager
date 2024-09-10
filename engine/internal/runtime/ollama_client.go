package runtime

import (
	"context"
	"fmt"
	"strconv"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RuntimeNameOllama is the name of the Ollama runtime.
const RuntimeNameOllama = "ollama"

const ollamaHTTPPort = 11434

// NewOllamaClient creates a new Ollama runtime client.
func NewOllamaClient(
	k8sClient client.Client,
	namespace string,
	rconfig config.RuntimeConfig,
	oconfig config.OllamaConfig,
) Client {
	return &ollamaClient{
		commonClient: &commonClient{
			k8sClient:     k8sClient,
			namespace:     namespace,
			servingPort:   ollamaHTTPPort,
			RuntimeConfig: rconfig,
		},
		config: oconfig,
	}
}

type ollamaClient struct {
	*commonClient

	config config.OllamaConfig
}

// DeployRuntime deploys the runtime for the given model.
func (o *ollamaClient) DeployRuntime(ctx context.Context, modelID string) (types.NamespacedName, error) {
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

	image, ok := o.RuntimeImages[RuntimeNameOllama]
	if !ok {
		return types.NamespacedName{}, fmt.Errorf("image not found for runtime %s", RuntimeNameOllama)
	}

	// Start an Ollama server process in background and create a modelfile.
	// Revisit once Ollama supports model file creation without server (https://github.com/ollama/ollama/issues/3369)
	script := fmt.Sprintf(`
ollama serve &
serve_pid=$!

while true; do
  ollama create %s -f %s && break
  sleep 1
done

kill ${serve_pid}
`, modelID, ollama.ModelfilePath(modelDir, modelID))

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
		},
	})
}
