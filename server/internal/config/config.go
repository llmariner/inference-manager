package config

import (
	"fmt"
	"os"
	"time"

	"github.com/llmariner/api-usage/pkg/sender"
	"github.com/llmariner/inference-manager/server/internal/rate"
	"gopkg.in/yaml.v3"
)

// Config is the configuration.
type Config struct {
	GRPCPort              int `yaml:"grpcPort"`
	HTTPPort              int `yaml:"httpPort"`
	WorkerServiceGRPCPort int `yaml:"workerServiceGrpcPort"`
	MonitoringPort        int `yaml:"monitoringPort"`
	AdminPort             int `yaml:"adminPort"`
	InternalGRPCPort      int `yaml:"internalGrpcPort"`
	ManagementPort        int `yaml:"managementPort"`
	ManagementGRPCPort    int `yaml:"managementGrpcPort"`

	// GracefulShutdownDelay is the delay before shutting down the server.
	GracefulShutdownDelay time.Duration `yaml:"gracefulShutdownDelay"`

	ModelManagerServerAddr               string `yaml:"modelManagerServerAddr"`
	VectorStoreManagerServerAddr         string `yaml:"vectorStoreManagerServerAddr"`
	VectorStoreManagerInternalServerAddr string `yaml:"vectorStoreManagerInternalServerAddr"`

	AuthConfig AuthConfig `yaml:"auth"`

	WorkerServiceTLS *TLS `yaml:"workerServiceTls"`

	UsageSender sender.Config `yaml:"usageSender"`

	RateLimit rate.Config `yaml:"rateLimit"`

	RequestRouting RequestRoutingConfig `yaml:"requestRouting"`

	KubernetesManager KubernetesManagerConfig `yaml:"kubernetesManager"`

	// GracefulShutdownTimeout is the duration given to runnable to stop
	// before the manager actually returns on stop.
	GracefulShutdownTimeout time.Duration `yaml:"gracefulShutdownTimeout"`

	EngineHeartbeat EngineHeartbeatConfig `yaml:"engineHeartbeat"`

	// ServerPodLabelKey is the key of the label that the server pod has.
	ServerPodLabelKey string `yaml:"serverPodLabelKey"`
	// ServerPodLabelKey is the value of the label that the server pod has for ServerPodLabelKey.
	ServerPodLabelValue string `yaml:"serverPodLabelValue"`

	Debug DebugConfig `yaml:"debug"`

	NIMModels []string `yaml:"nimModels"`
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

// RequestRoutingConfig is the request routing configuration.
type RequestRoutingConfig struct {
	// EnableDynamicModelLoading specifies whether dynamic on-demand model loading is enabled.
	EnableDynamicModelLoading bool `yaml:"enableDynamicModelLoading"`
}

// KubernetesManagerConfig is the Kubernetes manager configuration.
type KubernetesManagerConfig struct {
	EnableLeaderElection bool   `yaml:"enableLeaderElection"`
	LeaderElectionID     string `yaml:"leaderElectionID"`

	MetricsBindAddress string `yaml:"metricsBindAddress"`
	HealthBindAddress  string `yaml:"healthBindAddress"`
	PprofBindAddress   string `yaml:"pprofBindAddress"`
}

func (c *KubernetesManagerConfig) validate() error {
	if c.EnableLeaderElection && c.LeaderElectionID == "" {
		return fmt.Errorf("leader election ID must be set")
	}
	return nil
}

// DebugConfig is the debug configuration.
type DebugConfig struct {
	UseNoopClient bool `yaml:"useNoopClient"`
}

// EngineHeartbeatConfig is the engine heartbeat configuration.
type EngineHeartbeatConfig struct {
	Enable   bool          `yaml:"enable"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
}

// validate validates the configuration.
func (c *EngineHeartbeatConfig) validate() error {
	if c.Interval <= 0 {
		return fmt.Errorf("interval must be greater than 0")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0")
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
	if c.InternalGRPCPort <= 0 {
		return fmt.Errorf("internalGrpcPort must be greater than 0")
	}
	if c.MonitoringPort <= 0 {
		return fmt.Errorf("monitoringPort must be greater than 0")
	}
	if c.AdminPort <= 0 {
		return fmt.Errorf("adminPort must be greater than 0")
	}
	if c.ManagementPort <= 0 {
		return fmt.Errorf("managementPort must be greater than 0")
	}
	if c.ManagementGRPCPort <= 0 {
		return fmt.Errorf("managementGrpcPort must be greater than 0")
	}
	if c.GracefulShutdownDelay < 0 {
		return fmt.Errorf("gracefulShutdownDelay must be greater than or equal to 0")
	}

	if err := c.EngineHeartbeat.validate(); err != nil {
		return fmt.Errorf("engineHeartbeat: %s", err)
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
	if err := c.RateLimit.Validate(); err != nil {
		return err
	}

	if err := c.KubernetesManager.validate(); err != nil {
		return fmt.Errorf("kubernetesManager: %s", err)
	}

	if c.ServerPodLabelKey == "" {
		return fmt.Errorf("serverPodLabelKey must be set")
	}
	if c.ServerPodLabelValue == "" {
		return fmt.Errorf("serverPodLabelValue must be set")
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
