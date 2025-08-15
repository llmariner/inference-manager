package config

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/llmariner/cluster-manager/pkg/status"
	"github.com/llmariner/inference-manager/engine/internal/autoscaler"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	kyaml "sigs.k8s.io/yaml"
)

// preloadedModelIDsEnv is the environment variable for the preloaded model IDs.
// If set, the environment variable is used instead of the value in the configuration file.
// The value is a comma-separated list of model IDs.
const preloadedModelIDsEnv = "PRELOADED_MODEL_IDS"

// OllamaConfig is the Ollama configuration.
type OllamaConfig struct {
	// KeepAlive is the keep-alive duration for Ollama.
	// This controls how long Ollama keeps models in GPU memory.
	KeepAlive time.Duration `yaml:"keepAlive"`

	// NumParallel is the maximum number of requests procesed in parallel.
	NumParallel int `yaml:"numParallel"`

	// ForceSpreading is true if the models should be spread across all GPUs.
	ForceSpreading bool `yaml:"forceSpreading"`

	Debug bool `yaml:"debug"`

	RunnersDir string `yaml:"runnersDir"`

	// DynamicModelLoading is true if the model is loaded dynamically.
	// If this is set to true, the puller is run in the daemon mode.
	DynamicModelLoading bool `yaml:"dynamicModelLoading"`
}

func (c *OllamaConfig) validate() error {
	if c.KeepAlive <= 0 {
		return fmt.Errorf("keepAlive must be greater than 0")
	}
	if c.NumParallel < 0 {
		return fmt.Errorf("numParallel must be non-negative")
	}
	if c.RunnersDir == "" {
		return fmt.Errorf("runnerDir must be set")
	}
	return nil
}

// VLLMConfig is the VLLM configuration.
type VLLMConfig struct {
	DynamicLoRALoading bool `yaml:"dynamicLoRALoading"`

	LoggingLevel string `yaml:"loggingLevel"`
}

func (c VLLMConfig) validate() error {
	levels := []string{
		"DEBUG",
		"INFO",
		"WARNING",
		"ERROR",
	}
	levelsMap := map[string]bool{}
	for _, level := range levels {
		levelsMap[level] = true
	}
	if !levelsMap[c.LoggingLevel] {
		return fmt.Errorf("invalid loggingLevel: %q, must be one of %v", c.LoggingLevel, levels)
	}
	return nil
}

// NIMModelConfig is the model configuration.
type NIMModelConfig struct {
	LogLevel        string    `yaml:"logLevel"`
	Image           string    `yaml:"image"`
	ImagePullPolicy string    `yaml:"imagePullPolicy"`
	ModelName       string    `yaml:"modelName"`
	ModelVersion    string    `yaml:"modelVersion"`
	OpenAIPort      int       `yaml:"openaiPort"`
	Resources       Resources `yaml:"resources"`
}

func (c *NIMModelConfig) validate() error {
	levels := []string{
		"DEBUG",
		"INFO",
		"WARNING",
		"ERROR",
	}
	levelsMap := map[string]bool{}
	for _, level := range levels {
		levelsMap[level] = true
	}
	if c.LogLevel == "" {
		c.LogLevel = "INFO"
	}
	if !levelsMap[c.LogLevel] {
		return fmt.Errorf("invalid logLevel: %q, must be one of %v", c.LogLevel, levels)
	}
	if c.Image == "" {
		return fmt.Errorf("image must be set")
	}
	if err := validateImagePullPolicy(c.ImagePullPolicy); err != nil {
		return fmt.Errorf("imagePullPolicy: %s", err)
	}
	if c.ModelName == "" {
		return fmt.Errorf("modelName must be set")
	}
	if c.ModelVersion == "" {
		return fmt.Errorf("modelVersion must be set")
	}
	if c.OpenAIPort <= 0 {
		return fmt.Errorf("openaiPort must be greater than 0")
	}
	return nil
}

// NIMConfig is the NIM configuration.
type NIMConfig struct {
	NGCAPIKey string                    `yaml:"ngcApiKey"`
	Models    map[string]NIMModelConfig `yaml:"models"`
}

func (c *NIMConfig) validate() error {
	if c.NGCAPIKey == "" {
		if len(c.Models) > 0 {
			return fmt.Errorf("models must not be set when ngcApiKey is unset")
		}
		return nil
	}

	if len(c.Models) == 0 {
		return fmt.Errorf("models must be set")
	}

	for _, model := range c.Models {
		if err := model.validate(); err != nil {
			return fmt.Errorf("model %q: %s", model.ModelName, err)
		}
	}
	return nil
}

// TolerationConfig is the toleration configuration.
type TolerationConfig struct {
	Key               string `yaml:"key"`
	Operator          string `yaml:"operator"`
	Value             string `yaml:"value"`
	Effect            string `yaml:"effect"`
	TolerationSeconds int64  `yaml:"tolerationSeconds"`
}

const (
	// RuntimeNameOllama is the Ollama runtime name.
	RuntimeNameOllama string = "ollama"
	// RuntimeNameVLLM is the VLLM runtime name.
	RuntimeNameVLLM string = "vllm"
	// RuntimeNameTriton is the runtime name for Nvidia Triton Inference Server.
	RuntimeNameTriton string = "triton"
	// RuntimeNameNIM is the NIM runtime name.
	RuntimeNameNIM string = "nim"
)

// RuntimeConfig is the runtime configuration.
type RuntimeConfig struct {
	PullerImage                string            `yaml:"pullerImage"`
	TritonProxyImage           string            `yaml:"tritonProxyImage"`
	RuntimeImages              map[string]string `yaml:"runtimeImages"`
	PullerImagePullPolicy      string            `yaml:"pullerImagePullPolicy"`
	TritonProxyImagePullPolicy string            `yaml:"tritonProxyImagePullPolicy"`
	RuntimeImagePullPolicy     string            `yaml:"runtimeImagePullPolicy"`
	RuntimeImagePullSecrets    []string          `yaml:"runtimeImagePullSecrets"`

	ConfigMapName        string `yaml:"configMapName"`
	AWSSecretName        string `yaml:"awsSecretName"`
	AWSKeyIDEnvKey       string `yaml:"awsKeyIdEnvKey"`
	AWSAccessKeyEnvKey   string `yaml:"awsAccessKeyEnvKey"`
	LLMOWorkerSecretName string `yaml:"llmoWorkerSecretName"`
	LLMOKeyEnvKey        string `yaml:"llmoKeyEnvKey"`

	ServiceAccountName string `yaml:"serviceAccountName"`

	PodAnnotations map[string]string `yaml:"podAnnotations"`

	NodeSelector         map[string]string  `yaml:"nodeSelector"`
	Tolerations          []TolerationConfig `yaml:"tolerations"`
	UnstructuredAffinity any                `yaml:"affinity"`
	Affinity             *corev1.Affinity   `yaml:"-"`

	// PullerPort is the port for the puller. This is only used when
	// Ollama's DynamicModelLoading or vLLM's DynamicLoRALoading is enabled.
	PullerPort int `yaml:"pullerPort"`

	// Env and EnvFrom are lists of environment variables or env sources to set in runtime containers.
	UnstructuredEnv     any                    `yaml:"env"`
	Env                 []corev1.EnvVar        `yaml:"-"`
	UnstructuredEnvFrom any                    `yaml:"envFrom"`
	EnvFrom             []corev1.EnvFromSource `yaml:"-"`

	UseMemoryMediumForModelVolume bool `yaml:"useMemoryMediumForModelVolume"`
}

func (c *RuntimeConfig) validate() error {
	// Do not check name and DefaultReplicas as they can be specified in the model config.

	if c.PullerImage == "" {
		return fmt.Errorf("pullerImage must be set")
	}
	if c.TritonProxyImage == "" {
		return fmt.Errorf("tritonProxyImage must be set")
	}
	if len(c.RuntimeImages) == 0 {
		return fmt.Errorf("runtimeImages must be set")
	}
	if err := validateImagePullPolicy(c.PullerImagePullPolicy); err != nil {
		return fmt.Errorf("pullerImagePullPolicy: %s", err)
	}
	if err := validateImagePullPolicy(c.TritonProxyImagePullPolicy); err != nil {
		return fmt.Errorf("tritonProxyImagePullPolicy: %s", err)
	}
	if err := validateImagePullPolicy(c.RuntimeImagePullPolicy); err != nil {
		return fmt.Errorf("runtimeImagePullPolicy: %s", err)
	}

	if c.ConfigMapName == "" {
		return fmt.Errorf("configMapName must be set")
	}
	if c.AWSSecretName != "" {
		if c.AWSKeyIDEnvKey == "" {
			return fmt.Errorf("awsKeyIdEnvKey must be set")
		}
		if c.AWSAccessKeyEnvKey == "" {
			return fmt.Errorf("awsAccessKeyEnvKey must be set")
		}
	}
	if c.LLMOWorkerSecretName == "" {
		return fmt.Errorf("llmoWorkerSecretName must be set")
	}
	if c.LLMOKeyEnvKey == "" {
		return fmt.Errorf("llmoKeyEnvKey must be set")
	}

	// Validate env variables
	reservedNames := []string{"INDEX", "LLMO_CLUSTER_REGISTRATION_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}
	for i, env := range c.Env {
		if env.Name == "" {
			return fmt.Errorf("runtime.env[%d].name must be set", i)
		}
		// Check for reserved environment variable names that might conflict with system vars
		if slices.Contains(reservedNames, env.Name) {
			return fmt.Errorf("name '%s' is reserved and cannot be overridden", env.Name)
		}
	}

	// Validate envFrom sources
	for i, envFrom := range c.EnvFrom {
		if envFrom.ConfigMapRef == nil && envFrom.SecretRef == nil {
			return fmt.Errorf("runtime.envFrom[%d] must specify either configMapRef or secretRef", i)
		}
		if envFrom.ConfigMapRef != nil && envFrom.SecretRef != nil {
			return fmt.Errorf("runtime.envFrom[%d] cannot specify both configMapRef and secretRef", i)
		}
		if envFrom.ConfigMapRef != nil && envFrom.ConfigMapRef.Name == "" {
			return fmt.Errorf("runtime.envFrom[%d].configMapRef.name must be set", i)
		}
		if envFrom.SecretRef != nil && envFrom.SecretRef.Name == "" {
			return fmt.Errorf("runtime.envFrom[%d].secretRef.name must be set", i)
		}
	}

	return nil
}

func validateImagePullPolicy(policy string) error {
	switch corev1.PullPolicy(policy) {
	case "":
		return fmt.Errorf("imagePullPolicy must be set")
	case corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever:
		return nil
	default:
		return fmt.Errorf("invalid imagePullPolicy: %q", policy)
	}
}

// ModelConfig is the model configuration.
type ModelConfig struct {
	Default ModelConfigItem `yaml:"default"`
	// Overrides is a map of model ID to the model configuration item to be overriden. Only
	// fields that are set in the overrides are applied.
	Overrides map[string]ModelConfigItem `yaml:"overrides"`
}

func (c *ModelConfig) validate() error {
	if err := c.Default.validate(); err != nil {
		return fmt.Errorf("default: %s", err)
	}
	for id, i := range c.Overrides {
		if err := i.validate(); err != nil {
			return fmt.Errorf("overrides[%q]: %s", id, err)
		}
	}
	return nil
}

// ModelConfigItem is the model configuration item.
type ModelConfigItem struct {
	RuntimeName string `yaml:"runtimeName"`

	Resources Resources `yaml:"resources"`
	Replicas  int       `yaml:"replicas"`

	// Preloaded is true if the model is preloaded.
	// If this is set to true in the the default model item, all models that are specified in override items
	// are preloaded.
	Preloaded bool `yaml:"preloaded"`

	// ContextLength is the context length for the model. If the value is 0,
	// the default context length is used.
	ContextLength int `yaml:"contextLength"`

	// VLLMExtraFlags is the extra flags for VLLM.
	VLLMExtraFlags []string `yaml:"vllmExtraFlags"`

	// SchedulerName is the name of the scheduler to use.
	// This is set when a vLLM runs on Inferentia instances and
	// requires Neuron scheduling extension.
	// See https://awsdocs-neuron.readthedocs-hosted.com/en/latest/containers/tutorials/k8s-setup.html.
	SchedulerName string `yaml:"schedulerName"`

	// ContainerRuntimeClassName is the name of a K8s Runtime Class
	// (https://kubernetes.io/docs/concepts/containers/runtime-class/) used by model runtime.
	// This is set the Runtime Class of Nvidia container runtime if it is not a cluster default.
	ContainerRuntimeClassName string `yaml:"containerRuntimeClassName"`

	// Image is the docker image to use for the model. If empty, use the default runtime image.
	Image string `yaml:"image"`
}

func (c *ModelConfigItem) validate() error {
	if n := c.RuntimeName; n != "" && !isValidRuntimeName(n) {
		return fmt.Errorf("invalid runtimeName: %q", n)
	}

	if c.Replicas < 0 {
		return fmt.Errorf("replicas must be non-negative")
	}
	if c.ContextLength < 0 {
		return fmt.Errorf("contextLength must be non-negative")
	}

	if c.Resources.Volume != nil {
		if err := c.Resources.Volume.validate(); err != nil {
			return fmt.Errorf("volume: %s", err)
		}
	}
	return nil
}

func isValidRuntimeName(name string) bool {
	switch name {
	case RuntimeNameOllama, RuntimeNameVLLM, RuntimeNameTriton:
		return true
	}
	return false
}

// Resources is the resources configuration.
type Resources struct {
	Requests map[string]string `yaml:"requests"`
	Limits   map[string]string `yaml:"limits"`
	Volume   *PersistentVolume `yaml:"volume"`
}

// PersistentVolume is the persistent volume configuration.
type PersistentVolume struct {
	// ShareWithReplicas sets whether to share the volume among replicas.
	ShareWithReplicas bool   `yaml:"shareWithReplicas"`
	StorageClassName  string `yaml:"storageClassName"`
	Size              string `yaml:"size"`
	AccessMode        string `yaml:"accessMode"`
}

func (c *PersistentVolume) validate() error {
	if c.AccessMode == "" {
		return fmt.Errorf("accessMode must be set")
	}
	return nil
}

// AssumeRoleConfig is the assume role configuration.
type AssumeRoleConfig struct {
	RoleARN    string `yaml:"roleArn"`
	ExternalID string `yaml:"externalId"`
}

func (c *AssumeRoleConfig) validate() error {
	if c.RoleARN == "" {
		return fmt.Errorf("roleArn must be set")
	}
	return nil
}

// S3Config is the S3 configuration.
type S3Config struct {
	EndpointURL        string `yaml:"endpointUrl"`
	Region             string `yaml:"region"`
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify"`
	Bucket             string `yaml:"bucket"`

	AssumeRole *AssumeRoleConfig `yaml:"assumeRole"`
}

// ObjectStoreConfig is the object store configuration.
type ObjectStoreConfig struct {
	S3 S3Config `yaml:"s3"`
}

func (c *ObjectStoreConfig) validate() error {
	if c.S3.Region == "" {
		return fmt.Errorf("s3 region must be set")
	}
	if c.S3.Bucket == "" {
		return fmt.Errorf("s3 bucket must be set")
	}
	if ar := c.S3.AssumeRole; ar != nil {
		if err := ar.validate(); err != nil {
			return fmt.Errorf("assumeRole: %s", err)
		}
	}
	return nil
}

// EngineHeartbeatConfig is the configuration for the engine heartbeat.
type EngineHeartbeatConfig struct {
	ReconnectOnNoHeartbeat bool          `yaml:"reconnectOnNoHeartbeat"`
	HeartbeatTimeout       time.Duration `yaml:"heartbeatTimeout"`
}

func (c *EngineHeartbeatConfig) validate() error {
	if !c.ReconnectOnNoHeartbeat {
		return nil
	}

	if c.HeartbeatTimeout <= 0 {
		return fmt.Errorf("heartbeatTimeout must be greater than 0 when reconnectOnNoHeartbeat is true")
	}

	return nil
}

// DriftedPodUpdaterConfig is the configuration for the drifted pod updater.
type DriftedPodUpdaterConfig struct {
	Enable bool `yaml:"enable"`
}

// DebugConfig is the debug configuration.
type DebugConfig struct {
	// Standalone is true if the service is running in standalone mode (except the
	// dependency to inference-manager-server).
	Standalone bool `yaml:"standalone"`
}

// WorkerTLSConfig is the worker TLS configuration.
type WorkerTLSConfig struct {
	Enable bool `yaml:"enable"`
}

// WorkerConfig is the worker configuration.
type WorkerConfig struct {
	TLS WorkerTLSConfig `yaml:"tls"`
}

// LeaderElectionConfig is the leader election configuration.
type LeaderElectionConfig struct {
	ID string `yaml:"id"`
	// LeaseDuration is the duration that non-leader candidates will
	// wait to force acquire leadership. This is measured against time of
	// last observed ack. Default is 15 seconds.
	LeaseDuration *time.Duration `yaml:"leaseDuration"`
	// RenewDeadline is the duration that the acting controlplane will retry
	// refreshing leadership before giving up. Default is 10 seconds.
	RenewDeadline *time.Duration `yaml:"renewDeadline"`
	// RetryPeriod is the duration the LeaderElector clients should wait
	// between tries of actions. Default is 2 seconds.
	RetryPeriod *time.Duration `yaml:"retryPeriod"`
}

func (c *LeaderElectionConfig) validate() error {
	if c.ID == "" {
		return fmt.Errorf("id must be set")
	}
	return nil
}

// Config is the configuration.
type Config struct {
	Runtime RuntimeConfig `yaml:"runtime"`
	Ollama  OllamaConfig  `yaml:"ollama"`
	VLLM    VLLMConfig    `yaml:"vllm"`
	NIM     NIMConfig     `yaml:"nim"`
	Model   ModelConfig   `yaml:"model"`

	HealthPort  int `yaml:"healthPort"`
	MetricsPort int `yaml:"metricsPort"`

	// GracefulShutdownTimeout is the duration given to runnable to stop
	// before the manager actually returns on stop. Default is 30 seconds.
	GracefulShutdownTimeout time.Duration `yaml:"gracefulShutdownTimeout"`

	LeaderElection LeaderElectionConfig `yaml:"leaderElection"`

	Autoscaler autoscaler.Config `yaml:"autoscaler"`

	ObjectStore ObjectStoreConfig `yaml:"objectStore"`

	// PreloadedModelIDs is a list of model IDs to preload. These models are downloaded locally
	// at the startup time.
	// TODO(kenji):Remove once every env uses ModelConfig.
	PreloadedModelIDs []string `yaml:"preloadedModelIds"`

	// ModelContextLengths is a map of model ID to context length. If not specified, the default
	// context length is used.
	// TODO(kenji):Remove once every env uses ModelConfig.
	ModelContextLengths map[string]int `yaml:"modelContextLengths"`

	EngineHeartbeat EngineHeartbeatConfig `yaml:"engineHeartbeat"`

	DriftedPodUpdater DriftedPodUpdaterConfig `yaml:"driftedPodUpdater"`

	Debug DebugConfig `yaml:"debug"`

	InferenceManagerServerWorkerServiceAddr string `yaml:"inferenceManagerServerWorkerServiceAddr"`
	ModelManagerServerWorkerServiceAddr     string `yaml:"modelManagerServerWorkerServiceAddr"`

	Worker WorkerConfig `yaml:"worker"`

	// ComponentStatusSender is the configuration for the component status sender.
	ComponentStatusSender status.Config `yaml:"componentStatusSender"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.HealthPort <= 0 {
		return fmt.Errorf("healthPort must be greater than 0")
	}

	if c.InferenceManagerServerWorkerServiceAddr == "" {
		return fmt.Errorf("inference manager server worker service address must be set")
	}
	if !c.Debug.Standalone {
		if c.ModelManagerServerWorkerServiceAddr == "" {
			return fmt.Errorf("model manager server worker service address must be set")
		}

		if err := c.ObjectStore.validate(); err != nil {
			return fmt.Errorf("object store: %s", err)
		}
	}

	for id, length := range c.ModelContextLengths {
		if length <= 0 {
			return fmt.Errorf("model context length for model %q must be greater than 0", id)
		}
	}

	if c.GracefulShutdownTimeout <= 0 {
		// default period is same as the default value in ctrl.Manager.
		c.GracefulShutdownTimeout = 30 * time.Second
	}

	if err := c.Ollama.validate(); err != nil {
		return fmt.Errorf("ollama: %s", err)
	}

	if err := c.VLLM.validate(); err != nil {
		return fmt.Errorf("vllm: %s", err)
	}

	if err := c.NIM.validate(); err != nil {
		return fmt.Errorf("nim: %s", err)
	}

	if err := c.Model.validate(); err != nil {
		return fmt.Errorf("model: %s", err)
	}

	if err := c.Runtime.validate(); err != nil {
		return fmt.Errorf("runtime: %s", err)
	}

	if c.Ollama.DynamicModelLoading || c.VLLM.DynamicLoRALoading {
		if c.Runtime.PullerPort <= 0 {
			return fmt.Errorf("runtime: pullerPort must be set when dynamicModelLoading or dynamicLoRALoading is true")
		}
	}

	if err := c.Autoscaler.Validate(); err != nil {
		return fmt.Errorf("autoscaler: %s", err)
	}

	if err := c.EngineHeartbeat.validate(); err != nil {
		return fmt.Errorf("engineHeartbeat: %s", err)
	}

	if err := c.LeaderElection.validate(); err != nil {
		return fmt.Errorf("leaderElection: %s", err)
	}

	if err := c.ComponentStatusSender.Validate(); err != nil {
		return fmt.Errorf("componentStatusSender: %s", err)
	}

	return nil
}

func formatModelID(id string) string {
	// model-manager-loader convers "/" in the model ID to "-". Do the same conversion here
	// so that end users can use the consistent format.
	return strings.ReplaceAll(id, "/", "-")
}

// Parse parses the configuration file at the given path, returning a new
// Config struct.
func Parse(path string) (*Config, error) {
	var config Config

	b, err := os.ReadFile(path)
	if err != nil {
		return &config, fmt.Errorf("config: read: %s", err)
	}

	if err = yaml.Unmarshal(b, &config); err != nil {
		return &config, fmt.Errorf("config: unmarshal: %s", err)
	}

	if config.Runtime.UnstructuredAffinity != nil {
		data, err := yaml.Marshal(config.Runtime.UnstructuredAffinity)
		if err != nil {
			return nil, fmt.Errorf("config: marshal affinity: %s", err)
		}
		var affinity corev1.Affinity
		if err := kyaml.Unmarshal(data, &affinity); err != nil {
			return nil, fmt.Errorf("config: unmarshal affinity: %s", err)
		}
		config.Runtime.Affinity = &affinity
	}

	if config.Runtime.UnstructuredEnv != nil {
		data, err := yaml.Marshal(config.Runtime.UnstructuredEnv)
		if err != nil {
			return nil, fmt.Errorf("config: marshal env: %s", err)
		}
		var env []corev1.EnvVar
		if err := kyaml.Unmarshal(data, &env); err != nil {
			return nil, fmt.Errorf("config: unmarshal env: %s", err)
		}
		config.Runtime.Env = env
	}

	if config.Runtime.UnstructuredEnvFrom != nil {
		data, err := yaml.Marshal(config.Runtime.UnstructuredEnvFrom)
		if err != nil {
			return nil, fmt.Errorf("config: marshal envFrom: %s", err)
		}
		var envFrom []corev1.EnvFromSource
		if err := kyaml.Unmarshal(data, &envFrom); err != nil {
			return nil, fmt.Errorf("config: unmarshal envFrom: %s", err)
		}
		config.Runtime.EnvFrom = envFrom
	}

	if val := os.Getenv(preloadedModelIDsEnv); val != "" {
		config.PreloadedModelIDs = strings.Split(val, ",")
	}

	return &config, nil
}
