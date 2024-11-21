package rate

import (
	"fmt"
	"os"
	"time"
)

const (
	storeTypeMemory = "memory"
	storeTypeRedis  = "redis"
)

// Config is the configuration for rate limiter.
type Config struct {
	Enable bool `yaml:"enable"`

	StoreType string `yaml:"storeType"`

	Redis *RedisStoreConfig `yaml:"redis"`

	// Rate is allowed tokens for the period.
	Rate int `yaml:"rate"`
	// Period is the time period for the rate.
	Period time.Duration `yaml:"period"`
	// Burst is the maximum burst capacity.
	Burst int `yaml:"burst"`
}

// intervalSec returns the token emission intervalSec in seconds.
func (c *Config) intervalSec() float64 {
	return c.Period.Seconds() / float64(c.Rate)
}

// burstOffset returns max allowed burst time.
func (c *Config) burstOffset() float64 {
	return float64(c.Burst) * c.intervalSec()
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if !c.Enable {
		return nil
	}

	switch c.StoreType {
	case storeTypeRedis:
		if err := c.Redis.validate(); err != nil {
			return err
		}
	case storeTypeMemory:
	default:
		return fmt.Errorf("unknown store type: %s", c.StoreType)
	}

	if c.Rate <= 0 {
		return fmt.Errorf("rate must be greater than 0")
	}
	if c.Period <= 0 {
		return fmt.Errorf("period must be greater than 0")
	}
	if c.Burst <= 0 {
		return fmt.Errorf("burst must be greater than 0")
	}
	return nil
}

// RedisStoreConfig is the configuration for RedisStore.
type RedisStoreConfig struct {
	// host:port address.
	Address string `yaml:"address"`

	Username string `yaml:"username"`
	Password string `yaml:"-"`
	Database int    `yaml:"database"`

	// TODO(aya): support TLS
}

func (c *RedisStoreConfig) validate() error {
	if c.Address == "" {
		return fmt.Errorf("address is required")
	}

	c.Password = os.Getenv("REDIS_PASSWORD")
	return nil
}
