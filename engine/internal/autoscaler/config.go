package autoscaler

import (
	"fmt"
	"html/template"
	"time"
)

const (
	// TypeBuiltin is the builtin autoscaler.
	TypeBuiltin = "builtin"
	// TypeKeda is the KEDA autoscaler.
	TypeKeda = "keda"
)

// Config is the configuration for the autoscaler
type Config struct {
	Enable bool   `yaml:"enable"`
	Type   string `yaml:"type"`

	Builtin BuiltinConfig `yaml:"builtin"`
	Keda    KedaConfig    `yaml:"keda"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if !c.Enable {
		return nil
	}
	switch c.Type {
	case TypeBuiltin:
		if err := c.Builtin.validate(); err != nil {
			return fmt.Errorf("builtin: %s", err)
		}
	case TypeKeda:
		if err := c.Keda.validate(); err != nil {
			return fmt.Errorf("keda: %s", err)
		}
	default:
		return fmt.Errorf("type must be either %q or %q", TypeBuiltin, TypeKeda)
	}
	return nil
}

// BuiltinConfig is the configuration for the builtin autoscaler.
type BuiltinConfig struct {
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

func (c *BuiltinConfig) validate() error {
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

// KedaConfig is the configuration for KEDA.
type KedaConfig struct {
	// https://keda.sh/docs/2.16/reference/scaledobject-spec/#pollinginterval
	PollingInterval *int32 `yaml:"pollingInterval"`
	// https://keda.sh/docs/2.16/reference/scaledobject-spec/#cooldownperiod
	CooldownPeriod *int32 `yaml:"cooldownPeriod"`
	// https://keda.sh/docs/2.16/reference/scaledobject-spec/#idlereplicacount
	IdleReplicaCount *int32 `yaml:"idleReplicaCount"`
	// https://keda.sh/docs/2.16/reference/scaledobject-spec/#minreplicacount
	MinReplicaCount *int32 `yaml:"minReplicaCount"`
	// https://keda.sh/docs/2.16/reference/scaledobject-spec/#maxreplicacount
	MaxReplicaCount *int32 `yaml:"maxReplicaCount"`

	// Address of Prometheus server.
	PromServerAddress string `yaml:"promServerAddress"`
	// DefaultPromTrigger is the default Prometheus trigger.
	PromTriggers []KedaPromTrigger `yaml:"promTriggers"`
	// TODO: support prom authentication Parameters
}

func (c *KedaConfig) validate() error {
	if c.PromServerAddress == "" {
		return fmt.Errorf("promServerAddress must not be empty")
	}

	if len(c.PromTriggers) == 0 {
		return fmt.Errorf("PromTriggers must set at least one trigger")
	}
	for i := range c.PromTriggers {
		if err := c.PromTriggers[i].validate(); err != nil {
			return fmt.Errorf("promTriggers[%d]: %s", i, err)
		}
	}
	return nil
}

// KedaPromTrigger is the Prometheus trigger configuration.
type KedaPromTrigger struct {
	// Query to run. You can use `{{.}}` as a placeholder for the model name.
	// e.g., avg(rate(vllm:num_requests_running{model="{{.}}"}[10m])) is converted
	// to avg(rate(vllm:num_requests_running{model="<MODEL_NAME>"}[10m]))
	Query         string             `yaml:"query"`
	queryTemplate *template.Template `yaml:"-"`
	// Value to start scaling for.
	Threshold float64 `yaml:"threshold"`
	// Target value for activating the scaler. (Default: 0)
	ActivationThreshold float64 `yaml:"activationThreshold"`
}

func (c *KedaPromTrigger) validate() error {
	if c.Query == "" {
		return fmt.Errorf("query is required")
	}

	tmpl, err := template.New("query").Parse(c.Query)
	if err != nil {
		return fmt.Errorf("failed to parse query: %s", err)
	}
	c.queryTemplate = tmpl

	if c.Threshold == 0 {
		return fmt.Errorf("threshold is required")
	}
	return nil
}
