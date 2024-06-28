package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the configuration.
type Config struct {
	GRPCPort              int `yaml:"grpcPort"`
	HTTPPort              int `yaml:"httpPort"`
	WorkerServiceGRPCPort int `yaml:"workerServiceGrpcPort"`
	MonitoringPort        int `yaml:"monitoringPort"`

	ModelManagerServerAddr string `yaml:"modelManagerServerAddr"`

	AuthConfig                   AuthConfig                   `yaml:"auth"`
	InferenceManagerEngineConfig InferenceManagerEngineConfig `yaml:"inferenceManagerEngine"`

	Debug DebugConfig `yaml:"debug"`
}

// AuthConfig is the authentication configuration.
type AuthConfig struct {
	Enable                 bool   `yaml:"enable"`
	RBACInternalServerAddr string `yaml:"rbacInternalServerAddr"`
}

// Validate validates the configuration.
func (c *AuthConfig) validate() error {
	if !c.Enable {
		return nil
	}
	if c.RBACInternalServerAddr == "" {
		return fmt.Errorf("rbacInternalServerAddr must be set")
	}
	return nil
}

// DebugConfig is the debug configuration.
type DebugConfig struct {
	UseNoopModelClient      bool   `yaml:"useNoopModelClient"`
	UseFakeKubernetesClient bool   `yaml:"useFakeKubernetesClient"`
	EnginePodIP             string `yaml:"enginePodIP"`
}

// InferenceManagerEngineConfig is the inference manager engine configuration.
type InferenceManagerEngineConfig struct {
	OllamaPort       int    `yaml:"ollamaPort"`
	InternalGRPCPort int    `yaml:"internalGrpcPort"`
	Namespace        string `yaml:"namespace"`
	LabelKey         string `yaml:"labelKey"`
	LabelValue       string `yaml:"labelValue"`
}

func (c *InferenceManagerEngineConfig) validate() error {
	if c.OllamaPort <= 0 {
		return fmt.Errorf("ollamaPort must be greater than 0")
	}
	if c.InternalGRPCPort <= 0 {
		return fmt.Errorf("inferenceManagerEngineInternalGRPCPort must be greater than 0")
	}
	if c.Namespace == "" {
		return fmt.Errorf("namespace must be set")
	}
	if c.LabelKey == "" {
		return fmt.Errorf("labelKey must be set")
	}
	if c.LabelValue == "" {
		return fmt.Errorf("labelValue must be set")
	}
	return nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.GRPCPort <= 0 {
		return fmt.Errorf("grpcPort must be greater than 0")
	}
	if c.HTTPPort <= 0 {
		return fmt.Errorf("httpPort must be greater than 0")
	}
	if c.WorkerServiceGRPCPort <= 0 {
		return fmt.Errorf("workerServiceGrpcPort must be greater than 0")
	}
	if c.MonitoringPort <= 0 {
		return fmt.Errorf("monitoringPort must be greater than 0")
	}

	if err := c.AuthConfig.validate(); err != nil {
		return err
	}

	if err := c.InferenceManagerEngineConfig.validate(); err != nil {
		return err
	}

	if c.Debug.UseFakeKubernetesClient && c.Debug.EnginePodIP == "" {
		return fmt.Errorf("enginePodIP must be set")
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
