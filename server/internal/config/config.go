package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the configuration.
type Config struct {
	GRPCPort       int `yaml:"grpcPort"`
	HTTPPort       int `yaml:"httpPort"`
	MonitoringPort int `yaml:"monitoringPort"`

	ModelManagerServerAddr string `yaml:"modelManagerServerAddr"`
	OllamaServerAddr       string `yaml:"ollamaServerAddr"`

	AuthConfig AuthConfig `yaml:"auth"`

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
	Standalone bool `yaml:"standalone"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.GRPCPort <= 0 {
		return fmt.Errorf("grpcPort must be greater than 0")
	}
	if c.HTTPPort <= 0 {
		return fmt.Errorf("httpPort must be greater than 0")
	}
	if c.MonitoringPort <= 0 {
		return fmt.Errorf("monitoringPort must be greater than 0")
	}
	if c.OllamaServerAddr == "" {
		return fmt.Errorf("ollamaServerAddr must be set")
	}
	if err := c.AuthConfig.validate(); err != nil {
		return err
	}

	if !c.Debug.Standalone {
		if c.ModelManagerServerAddr == "" {
			return fmt.Errorf("modelManagerServerAddr must be set")
		}
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
