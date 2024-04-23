package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// S3Config is the S3 configuration.
type S3Config struct {
	EndpointURL string `yaml:"endpointUrl"`
	Bucket      string `yaml:"bucket"`
}

// ObjectStoreConfig is the object store configuration.
type ObjectStoreConfig struct {
	S3 S3Config `yaml:"s3"`
}

// Validate validates the object store configuration.
func (c *ObjectStoreConfig) Validate() error {
	if c.S3.EndpointURL == "" {
		return fmt.Errorf("s3 endpoint url must be set")
	}
	if c.S3.Bucket == "" {
		return fmt.Errorf("s3 bucket must be set")
	}
	return nil
}

// DebugConfig is the debug configuration.
type DebugConfig struct {
	Standalone bool `yaml:"standalone"`
}

// Config is the configuration.
type Config struct {
	InternalGRPCPort int `yaml:"internalGrpcPort"`
	OllamaPort       int `yaml:"ollamaPort"`

	BaseModels []string `yaml:"baseModels"`

	ObjectStore ObjectStoreConfig `yaml:"objectStore"`

	Debug DebugConfig `yaml:"debug"`

	ModelManagerServerAddr         string `yaml:"modelManagerServerAddr"`
	ModelManagerInternalServerAddr string `yaml:"modelManagerInternalServerAddr"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.InternalGRPCPort <= 0 {
		return fmt.Errorf("internalGrpcPort must be greater than 0")
	}
	if c.OllamaPort <= 0 {
		return fmt.Errorf("ollamaPort must be greater than 0")
	}

	if len(c.BaseModels) == 0 {
		return fmt.Errorf("baseModels must be set")
	}

	if !c.Debug.Standalone {
		if c.ModelManagerServerAddr == "" {
			return fmt.Errorf("model manager address must be set")
		}
		if c.ModelManagerInternalServerAddr == "" {
			return fmt.Errorf("model manager internal address must be set")
		}

		if err := c.ObjectStore.Validate(); err != nil {
			return fmt.Errorf("object store: %s", err)
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
