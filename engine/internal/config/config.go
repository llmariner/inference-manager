package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the configuration.
type Config struct {
	InternalGRPCPort int `yaml:"internalGrpcPort"`
	OllamaPort       int `yaml:"ollamaPort"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.InternalGRPCPort <= 0 {
		return fmt.Errorf("internalGrpcPort must be greater than 0")
	}
	if c.OllamaPort <= 0 {
		return fmt.Errorf("ollamaPort must be greater than 0")
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
