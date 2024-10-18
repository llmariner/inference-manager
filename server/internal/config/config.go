package config

import (
	"fmt"
	"os"

	"github.com/llmariner/api-usage/pkg/sender"
	"gopkg.in/yaml.v3"
)

// Config is the configuration.
type Config struct {
	GRPCPort              int `yaml:"grpcPort"`
	HTTPPort              int `yaml:"httpPort"`
	WorkerServiceGRPCPort int `yaml:"workerServiceGrpcPort"`
	MonitoringPort        int `yaml:"monitoringPort"`
	AdminPort             int `yaml:"adminPort"`

	ModelManagerServerAddr               string `yaml:"modelManagerServerAddr"`
	VectorStoreManagerServerAddr         string `yaml:"vectorStoreManagerServerAddr"`
	VectorStoreManagerInternalServerAddr string `yaml:"vectorStoreManagerInternalServerAddr"`

	AuthConfig AuthConfig `yaml:"auth"`

	WorkerServiceTLS *TLS `yaml:"workerServiceTls"`

	UsageSender sender.Config `yaml:"usageSender"`

	Debug DebugConfig `yaml:"debug"`

	// EnableEngineReadinessCheck enables the engine readiness check. This is set to false
	// when the server still connects to an old version of engines that don't set the 'ready'
	// field in the status.
	EnableEngineReadinessCheck bool `yaml:"enableEngineReadinessCheck"`
}

// AuthConfig is the authentication configuration.
type AuthConfig struct {
	Enable                 bool   `yaml:"enable"`
	RBACInternalServerAddr string `yaml:"rbacInternalServerAddr"`
}

// validate validates the configuration.
func (c *AuthConfig) validate() error {
	if !c.Enable {
		return nil
	}
	if c.RBACInternalServerAddr == "" {
		return fmt.Errorf("rbacInternalServerAddr must be set")
	}
	return nil
}

// TLS is the TLS configuration for the proxy.
type TLS struct {
	Key  string `yaml:"key"`
	Cert string `yaml:"cert"`
}

// validate validates the configuration.
func (c *TLS) validate() error {
	if c.Key == "" {
		return fmt.Errorf("key must be set")
	}
	if c.Cert == "" {
		return fmt.Errorf("cert must be set")
	}
	return nil
}

// DebugConfig is the debug configuration.
type DebugConfig struct {
	UseNoopClient bool `yaml:"useNoopClient"`
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
	if c.AdminPort <= 0 {
		return fmt.Errorf("adminPort must be greater than 0")
	}

	if err := c.AuthConfig.validate(); err != nil {
		return err
	}

	if t := c.WorkerServiceTLS; t != nil {
		if err := t.validate(); err != nil {
			return err
		}
	}
	if err := c.UsageSender.Validate(); err != nil {
		return err
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
