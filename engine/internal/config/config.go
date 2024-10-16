package config

import (
	"fmt"
	"os"
	"strings"
	"time"

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
)

// RuntimeConfig is the runtime configuration.
type RuntimeConfig struct {
	PullerImage                string            `yaml:"pullerImage"`
	TritonProxyImage           string            `yaml:"tritonProxyImage"`
	RuntimeImages              map[string]string `yaml:"runtimeImages"`
	PullerImagePullPolicy      string            `yaml:"pullerImagePullPolicy"`
	TritonProxyImagePullPolicy string            `yaml:"tritonProxyImagePullPolicy"`
	RuntimeImagePullPolicy     string            `yaml:"runtimeImagePullPolicy"`

	ConfigMapName        string `yaml:"configMapName"`
	AWSSecretName        string `yaml:"awsSecretName"`
	AWSKeyIDEnvKey       string `yaml:"awsKeyIdEnvKey"`
	AWSAccessKeyEnvKey   string `yaml:"awsAccessKeyEnvKey"`
	LLMOWorkerSecretName string `yaml:"llmoWorkerSecretName"`
	LLMOKeyEnvKey        string `yaml:"llmoKeyEnvKey"`

	ServiceAccountName string `yaml:"serviceAccountName"`

	NodeSelector         map[string]string  `yaml:"nodeSelector"`
	Tolerations          []TolerationConfig `yaml:"tolerations"`
	UnstructuredAffinity any                `yaml:"affinity"`
	Affinity             *corev1.Affinity   `yaml:"-"`

	// TODO(kenji): Remove the following fields once every env uses ModelConfig.
	Name string `yaml:"name"`

	ModelResources   map[string]Resources `yaml:"modelResources"`
	DefaultResources Resources            `yaml:"defaultResources"`

	// DefaultReplicas specifies the number of replicas of the runtime (per model).
	// TODO(kenji): Revisit this once we support autoscaling.
	DefaultReplicas int `yaml:"defaultReplicas"`
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

	if n := c.Name; n != "" && !isValidRuntimeName(n) {
		return fmt.Errorf("invalid name: %q", n)
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

	if vol := c.DefaultResources.Volume; vol != nil {
		if err := vol.validate(); err != nil {
			return fmt.Errorf("defaultResources.volume: %s", err)
		}
	}
	for model, res := range c.ModelResources {
		if vol := res.Volume; vol != nil {
			if err := vol.validate(); err != nil {
				return fmt.Errorf("modelResources[%q].volume: %s", model, err)
			}
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
	EndpointURL string `yaml:"endpointUrl"`
	Region      string `yaml:"region"`
	Bucket      string `yaml:"bucket"`

	AssumeRole *AssumeRoleConfig `yaml:"assumeRole"`
}

// ObjectStoreConfig is the object store configuration.
type ObjectStoreConfig struct {
	S3 S3Config `yaml:"s3"`
}

// Validate validates the object store configuration.
func (c *ObjectStoreConfig) Validate() error {
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

// AutoscalerConfig is the autoscaler configuration.
type AutoscalerConfig struct {
	Enable bool `yaml:"enable"`

	// InitialDelay is the initial delay before starting the autoscaler.
	InitialDelay time.Duration `yaml:"initialDelay"`
	// SyncPeriod is the period for calculating the scaling.
	SyncPeriod time.Duration `yaml:"syncPeriod"`
	// ScaleToZeroGracePeriod is the grace period before scaling to zero.
	ScaleToZeroGracePeriod time.Duration `yaml:"scaleToZeroGracePeriod"`
	// MetricsWindow is the window size for metrics.
	// e.g., if it's 5 minutes, we'll use the 5-minute average as the metric.
	MetricsWindow time.Duration `yaml:"metricsWindow"`

	RuntimeScalers map[string]ScalingConfig `yaml:"runtimeScalers"`
	DefaultScaler  ScalingConfig            `yaml:"defaultScaler"`
}

func (c *AutoscalerConfig) validate() error {
	if !c.Enable {
		return nil
	}
	if c.SyncPeriod <= 0 {
		return fmt.Errorf("syncPeriod must be greater than 0")
	}
	if c.ScaleToZeroGracePeriod < 0 {
		return fmt.Errorf("scaleToZeroGracePeriod must be non-negative")
	}
	if c.MetricsWindow <= 0 {
		return fmt.Errorf("metricsWindow must be greater than 0")
	}
	for id, sc := range c.RuntimeScalers {
		if err := sc.validate(); err != nil {
			return fmt.Errorf("runtimeScalers[%q]: %s", id, err)
		}
	}
	if err := c.DefaultScaler.validate(); err != nil {
		return fmt.Errorf("defaultScaler: %s", err)
	}
	return nil
}

// ScalingConfig is the scaling configuration.
type ScalingConfig struct {
	// TargetValue is the per-pod metric value that we target to maintain.
	// Currently, this is the concurrent requests per model runtime.
	TargetValue float64 `yaml:"targetValue"`

	// MaxReplicas is the maximum number of replicas.
	// e.g., if this is 10, the pod can be scaled up to 10.
	MaxReplicas int32 `yaml:"maxReplicas"`
	// MinReplicas is the minimum number of replicas.
	// e.g., if this is 0, the pod can be scaled down to 0.
	MinReplicas int32 `yaml:"minReplicas"`

	// MaxScaleUpRate is the maximum rate of scaling up.
	// e.g., current replicas is 2 and this rate is 3.0,
	// the pod can be scaled up to 6. (ceil(2 * 3.0) = 6)
	MaxScaleUpRate float64 `yaml:"maxScaleUpRate"`
	// MaxScaleDownRate is the maximum rate of scaling down.
	// e.g., current replicas is 6 and this rate is 0.5,
	// the pod can be scaled down to 3. (floor(6 * 0.5) = 3)
	MaxScaleDownRate float64 `yaml:"maxScaleDownRate"`
}

func (c *ScalingConfig) validate() error {
	if c.TargetValue <= 0 {
		return fmt.Errorf("targetValue must be greater than 0")
	}
	if c.MaxReplicas < 0 {
		return fmt.Errorf("maxReplicas must be non-negative")
	}
	if c.MinReplicas < 0 {
		return fmt.Errorf("minReplicas must be non-negative")
	}
	if c.MaxReplicas != 0 && c.MinReplicas > c.MaxReplicas {
		return fmt.Errorf("minReplicas must be less than or equal to maxReplicas")
	}
	if c.MaxScaleUpRate <= 0 {
		return fmt.Errorf("maxScaleUpRate must be greater than 0")
	}
	if c.MaxScaleDownRate <= 0 {
		return fmt.Errorf("maxScaleDownRate must be greater than 0")
	}
	return nil
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
	Model   ModelConfig   `yaml:"model"`

	HealthPort int `yaml:"healthPort"`

	// GracefulShutdownTimeout is the duration given to runnable to stop
	// before the manager actually returns on stop. Default is 30 seconds.
	GracefulShutdownTimeout time.Duration `yaml:"gracefulShutdownTimeout"`

	LeaderElection LeaderElectionConfig `yaml:"leaderElection"`

	Autoscaler AutoscalerConfig `yaml:"autoscaler"`

	ObjectStore ObjectStoreConfig `yaml:"objectStore"`

	// PreloadedModelIDs is a list of model IDs to preload. These models are downloaded locally
	// at the startup time.
	// TODO(kenji):Remove once every env uses ModelConfig.
	PreloadedModelIDs []string `yaml:"preloadedModelIds"`

	// ModelContextLengths is a map of model ID to context length. If not specified, the default
	// context length is used.
	// TODO(kenji):Remove once every env uses ModelConfig.
	ModelContextLengths map[string]int `yaml:"modelContextLengths"`

	Debug DebugConfig `yaml:"debug"`

	InferenceManagerServerWorkerServiceAddr string `yaml:"inferenceManagerServerWorkerServiceAddr"`
	ModelManagerServerWorkerServiceAddr     string `yaml:"modelManagerServerWorkerServiceAddr"`

	Worker WorkerConfig `yaml:"worker"`
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

		if err := c.ObjectStore.Validate(); err != nil {
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

	if err := c.Model.validate(); err != nil {
		return fmt.Errorf("model: %s", err)
	}

	if err := c.Runtime.validate(); err != nil {
		return fmt.Errorf("runtime: %s", err)
	}

	if err := c.Autoscaler.validate(); err != nil {
		return fmt.Errorf("autoscaler: %s", err)
	}

	if err := c.LeaderElection.validate(); err != nil {
		return fmt.Errorf("leaderElection: %s", err)
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

	if val := os.Getenv(preloadedModelIDsEnv); val != "" {
		config.PreloadedModelIDs = strings.Split(val, ",")
	}

	return &config, nil
}
