package config

import (
	"fmt"
	"os"
	"time"

	"github.com/llm-operator/inference-manager/engine/internal/llmkind"
	"gopkg.in/yaml.v3"
)

// OllamaConfig is the Ollama configuration.
type OllamaConfig struct {
	// Port is the port to listen on.
	Port int `yaml:"port"`

	// KeepAlive is the keep-alive duration for Ollama.
	// This controls how long Ollama keeps models in GPU memory.
	KeepAlive time.Duration `yaml:"keepAlive"`
}

func (c *OllamaConfig) validate() error {
	if c.Port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}
	if c.KeepAlive <= 0 {
		return fmt.Errorf("keepAlive must be greater than 0")
	}
	return nil
}

// VLLMConfig is the configuration for vLLM.
type VLLMConfig struct {
	// Port is the port to listen on.
	Port    int    `yaml:"port"`
	Model   string `yaml:"model"`
	NumGPUs int    `yaml:"numGpus"`
}

func (c *VLLMConfig) validate() error {
	if c.Port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}
	if c.Model == "" {
		return fmt.Errorf("vllm model must be set")
	}
	if c.NumGPUs <= 0 {
		return fmt.Errorf("numGPUs must be positive")
	}
	return nil
}

// S3Config is the S3 configuration.
type S3Config struct {
	EndpointURL string `yaml:"endpointUrl"`
	Region      string `yaml:"region"`
	Bucket      string `yaml:"bucket"`
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

// Config is the configuration.
type Config struct {
	Ollama    OllamaConfig `yaml:"ollama"`
	VLLM      VLLMConfig   `yaml:"vllm"`
	LLMEngine llmkind.K    `yaml:"llmEngine"`

	ObjectStore ObjectStoreConfig `yaml:"objectStore"`

	// PreloadedModelIDs is a list of model IDs to preload. These models are downloaded locally
	// at the startup time.
	PreloadedModelIDs []string `yaml:"preloadedModelIds"`

	Debug DebugConfig `yaml:"debug"`

	InferenceManagerServerWorkerServiceAddr string `yaml:"inferenceManagerServerWorkerServiceAddr"`
	ModelManagerServerWorkerServiceAddr     string `yaml:"modelManagerServerWorkerServiceAddr"`

	Worker WorkerConfig `yaml:"worker"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
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

	switch c.LLMEngine {
	case llmkind.VLLM:
		if err := c.VLLM.validate(); err != nil {
			return fmt.Errorf("vllm: %s", err)
		}
	case llmkind.Ollama:
		if err := c.Ollama.validate(); err != nil {
			return fmt.Errorf("ollama: %s", err)
		}
	default:
		return fmt.Errorf("unsupported serving engine: %q", c.LLMEngine)
	}

	return nil
}

// Parse parses the configuration file at the given path, returning a new
// Config struct.
func Parse(path string) (Config, error) {
	var config Config

	b, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("config: read: %s", err)
	}

	if err = yaml.Unmarshal(b, &config); err != nil {
		return config, fmt.Errorf("config: unmarshal: %s", err)
	}
	return config, nil
}
