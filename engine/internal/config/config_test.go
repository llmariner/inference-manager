package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// Test data based on the provided example
const testConfigYAML = `
runtime:
  pullerImage: llmariner/llmariner-inference-manager-engine:1.32.0
  tritonProxyImage: public.ecr.aws/cloudnatix/llmariner/inference-manager-triton-proxy:1.32.0
  runtimeImages:
    ollama: mirror.gcr.io/ollama/ollama:0.6.3-rc0
    triton: nvcr.io/nvidia/tritonserver:24.09-trtllm-python-py3
    vllm: mirror.gcr.io/vllm/vllm-openai:v0.7.3
  pullerImagePullPolicy: IfNotPresent
  tritonProxyImagePullPolicy: IfNotPresent
  runtimeImagePullPolicy: IfNotPresent
  configMapName: inference-manager-engine
  awsSecretName: aws-s3-credentials
  awsKeyIdEnvKey: aws_access_key_id
  awsAccessKeyEnvKey: aws_secret_access_key
  llmoWorkerSecretName: default-cluster-registration-key
  llmoKeyEnvKey: key
  serviceAccountName: inference-manager-engine
  pullerPort: 8080
  envFrom:
    - configMapRef:
        name: proxy-config
ollama:
  keepAlive: 96h
  numParallel: 0
  forceSpreading: false
  debug: false
  runnersDir: /tmp/ollama-runners
  dynamicModelLoading: false
vllm:
  dynamicLoRALoading: false
  loggingLevel: ERROR
nim:
  ngcApiKey: ""
model:
  default:
    runtimeName: ollama
    resources:
      requests:
        cpu: 1000m
        memory: 500Mi
      limits:
        nvidia.com/gpu: "1"
    replicas: 1
    preloaded: false
    contextLength: 0
    schedulerName: ""
    containerRuntimeClassName: ""
  overrides:
    meta-llama-Meta-Llama-3.1-8B-Instruct:
      containerRuntimeClassName: nvidia
      nodeSelector:
        nvidia.com/mig-2g.20gb.product: NVIDIA-H100-PCIe-MIG-2g.20gb
      resources:
        limits:
          cpu: 1000m
          memory: 500Mi
        requests:
          cpu: 1000m
          memory: 500Mi
      schedulerName: ""
healthPort: 8081
metricsPort: 8084
leaderElection:
  id: inference-manager-engine
autoscaler:
  enable: false
  type: builtin
  builtin:
    initialDelay: 12s
    syncPeriod: 2s
    scaleToZeroGracePeriod: 5m
    metricsWindow: 60s
    defaultScaler:
      maxReplicas: 10
      maxScaleDownRate: 0.5
      maxScaleUpRate: 3
      minReplicas: 1
      targetValue: 100
  keda:
    maxReplicaCount: 10
    promServerAddress: http://prometheus-server.monitoring
    promTriggers:
      - query: avg(llmariner_active_inference_request_count{model="{{.}}"})
        threshold: 30
inferenceManagerServerWorkerServiceAddr: inference-manager-server-worker-service-grpc:8082
modelManagerServerWorkerServiceAddr: model-manager-server-worker-service-grpc:8082
componentStatusSender:
  enable: true
  name: inference-manager-engine
  initialDelay: 1m
  interval: 15m
  clusterManagerServerWorkerServiceAddr: cluster-manager-server-worker-service-grpc:8082
worker:
  tls:
    enable: false
objectStore:
  s3:
    endpointUrl: ""
    region: us-east-1
    insecureSkipVerify: false
    bucket: clgpu-llmariner
`

func TestParse(t *testing.T) {
	// Create temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(testConfigYAML), 0644)
	require.NoError(t, err)

	// Test successful parsing
	config, err := Parse(configPath)
	require.NoError(t, err)
	assert.NotNil(t, config)

	// Validate parsed values
	assert.Equal(t, "llmariner/llmariner-inference-manager-engine:1.32.0", config.Runtime.PullerImage)
	assert.Equal(t, "inference-manager-engine", config.Runtime.ConfigMapName)
	assert.Equal(t, 96*time.Hour, config.Ollama.KeepAlive)
	assert.Equal(t, "ERROR", config.VLLM.LoggingLevel)
	assert.Equal(t, 8081, config.HealthPort)
	assert.Equal(t, 8084, config.MetricsPort)
	assert.Equal(t, "ollama", config.Model.Default.RuntimeName)
	assert.Equal(t, 1, config.Model.Default.Replicas)
	assert.Equal(t, "clgpu-llmariner", config.ObjectStore.S3.Bucket)
	assert.Equal(t, "us-east-1", config.ObjectStore.S3.Region)

	// Test envFrom parsing
	require.Len(t, config.Runtime.EnvFrom, 1)
	assert.Equal(t, "proxy-config", config.Runtime.EnvFrom[0].ConfigMapRef.Name)
}

func TestParseNonExistentFile(t *testing.T) {
	_, err := Parse("/non/existent/file.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config: read:")
}

func TestParseInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0644)
	require.NoError(t, err)

	_, err = Parse(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config: unmarshal:")
}

func TestRuntimeConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  RuntimeConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: RuntimeConfig{
				PullerImage:                "test-puller:latest",
				TritonProxyImage:           "test-triton:latest",
				RuntimeImages:              map[string]string{"ollama": "ollama:latest"},
				PullerImagePullPolicy:      "IfNotPresent",
				TritonProxyImagePullPolicy: "IfNotPresent",
				RuntimeImagePullPolicy:     "IfNotPresent",
				ConfigMapName:              "test-config",
				LLMOWorkerSecretName:       "test-secret",
				LLMOKeyEnvKey:              "key",
			},
			wantErr: false,
		},
		{
			name: "missing puller image",
			config: RuntimeConfig{
				TritonProxyImage: "test-triton:latest",
			},
			wantErr: true,
			errMsg:  "pullerImage must be set",
		},
		{
			name: "missing triton proxy image",
			config: RuntimeConfig{
				PullerImage: "test-puller:latest",
			},
			wantErr: true,
			errMsg:  "tritonProxyImage must be set",
		},
		{
			name: "empty runtime images",
			config: RuntimeConfig{
				PullerImage:      "test-puller:latest",
				TritonProxyImage: "test-triton:latest",
				RuntimeImages:    map[string]string{},
			},
			wantErr: true,
			errMsg:  "runtimeImages must be set",
		},
		{
			name: "invalid puller image pull policy",
			config: RuntimeConfig{
				PullerImage:           "test-puller:latest",
				TritonProxyImage:      "test-triton:latest",
				RuntimeImages:         map[string]string{"ollama": "ollama:latest"},
				PullerImagePullPolicy: "InvalidPolicy",
			},
			wantErr: true,
			errMsg:  "pullerImagePullPolicy",
		},
		{
			name: "missing config map name",
			config: RuntimeConfig{
				PullerImage:                "test-puller:latest",
				TritonProxyImage:           "test-triton:latest",
				RuntimeImages:              map[string]string{"ollama": "ollama:latest"},
				PullerImagePullPolicy:      "IfNotPresent",
				TritonProxyImagePullPolicy: "IfNotPresent",
				RuntimeImagePullPolicy:     "IfNotPresent",
			},
			wantErr: true,
			errMsg:  "configMapName must be set",
		},
		{
			name: "AWS secret without key ID env key",
			config: RuntimeConfig{
				PullerImage:                "test-puller:latest",
				TritonProxyImage:           "test-triton:latest",
				RuntimeImages:              map[string]string{"ollama": "ollama:latest"},
				PullerImagePullPolicy:      "IfNotPresent",
				TritonProxyImagePullPolicy: "IfNotPresent",
				RuntimeImagePullPolicy:     "IfNotPresent",
				ConfigMapName:              "test-config",
				AWSSecretName:              "aws-secret",
				LLMOWorkerSecretName:       "test-secret",
				LLMOKeyEnvKey:              "key",
			},
			wantErr: true,
			errMsg:  "awsKeyIdEnvKey must be set",
		},
		{
			name: "env variable with empty name",
			config: RuntimeConfig{
				PullerImage:                "test-puller:latest",
				TritonProxyImage:           "test-triton:latest",
				RuntimeImages:              map[string]string{"ollama": "ollama:latest"},
				PullerImagePullPolicy:      "IfNotPresent",
				TritonProxyImagePullPolicy: "IfNotPresent",
				RuntimeImagePullPolicy:     "IfNotPresent",
				ConfigMapName:              "test-config",
				LLMOWorkerSecretName:       "test-secret",
				LLMOKeyEnvKey:              "key",
				Env: []corev1.EnvVar{
					{Name: "", Value: "test"},
				},
			},
			wantErr: true,
			errMsg:  "runtime.env[0].name must be set",
		},
		{
			name: "env variable with reserved name",
			config: RuntimeConfig{
				PullerImage:                "test-puller:latest",
				TritonProxyImage:           "test-triton:latest",
				RuntimeImages:              map[string]string{"ollama": "ollama:latest"},
				PullerImagePullPolicy:      "IfNotPresent",
				TritonProxyImagePullPolicy: "IfNotPresent",
				RuntimeImagePullPolicy:     "IfNotPresent",
				ConfigMapName:              "test-config",
				LLMOWorkerSecretName:       "test-secret",
				LLMOKeyEnvKey:              "key",
				Env: []corev1.EnvVar{
					{Name: "INDEX", Value: "test"},
				},
			},
			wantErr: true,
			errMsg:  "name 'INDEX' is reserved and cannot be overridden",
		},
		{
			name: "envFrom without configMapRef or secretRef",
			config: RuntimeConfig{
				PullerImage:                "test-puller:latest",
				TritonProxyImage:           "test-triton:latest",
				RuntimeImages:              map[string]string{"ollama": "ollama:latest"},
				PullerImagePullPolicy:      "IfNotPresent",
				TritonProxyImagePullPolicy: "IfNotPresent",
				RuntimeImagePullPolicy:     "IfNotPresent",
				ConfigMapName:              "test-config",
				LLMOWorkerSecretName:       "test-secret",
				LLMOKeyEnvKey:              "key",
				EnvFrom: []corev1.EnvFromSource{
					{},
				},
			},
			wantErr: true,
			errMsg:  "runtime.envFrom[0] must specify either configMapRef or secretRef",
		},
		{
			name: "envFrom with both configMapRef and secretRef",
			config: RuntimeConfig{
				PullerImage:                "test-puller:latest",
				TritonProxyImage:           "test-triton:latest",
				RuntimeImages:              map[string]string{"ollama": "ollama:latest"},
				PullerImagePullPolicy:      "IfNotPresent",
				TritonProxyImagePullPolicy: "IfNotPresent",
				RuntimeImagePullPolicy:     "IfNotPresent",
				ConfigMapName:              "test-config",
				LLMOWorkerSecretName:       "test-secret",
				LLMOKeyEnvKey:              "key",
				EnvFrom: []corev1.EnvFromSource{
					{
						ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "config"}},
						SecretRef:    &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "secret"}},
					},
				},
			},
			wantErr: true,
			errMsg:  "runtime.envFrom[0] cannot specify both configMapRef and secretRef",
		},
		{
			name: "envFrom configMapRef with empty name",
			config: RuntimeConfig{
				PullerImage:                "test-puller:latest",
				TritonProxyImage:           "test-triton:latest",
				RuntimeImages:              map[string]string{"ollama": "ollama:latest"},
				PullerImagePullPolicy:      "IfNotPresent",
				TritonProxyImagePullPolicy: "IfNotPresent",
				RuntimeImagePullPolicy:     "IfNotPresent",
				ConfigMapName:              "test-config",
				LLMOWorkerSecretName:       "test-secret",
				LLMOKeyEnvKey:              "key",
				EnvFrom: []corev1.EnvFromSource{
					{
						ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: ""}},
					},
				},
			},
			wantErr: true,
			errMsg:  "runtime.envFrom[0].configMapRef.name must be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOllamaConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  OllamaConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: OllamaConfig{
				KeepAlive:   time.Hour,
				NumParallel: 2,
				RunnersDir:  "/tmp/runners",
			},
			wantErr: false,
		},
		{
			name: "zero keep alive",
			config: OllamaConfig{
				KeepAlive:  0,
				RunnersDir: "/tmp/runners",
			},
			wantErr: true,
			errMsg:  "keepAlive must be greater than 0",
		},
		{
			name: "negative num parallel",
			config: OllamaConfig{
				KeepAlive:   time.Hour,
				NumParallel: -1,
				RunnersDir:  "/tmp/runners",
			},
			wantErr: true,
			errMsg:  "numParallel must be non-negative",
		},
		{
			name: "empty runners dir",
			config: OllamaConfig{
				KeepAlive:   time.Hour,
				NumParallel: 2,
				RunnersDir:  "",
			},
			wantErr: true,
			errMsg:  "runnerDir must be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVLLMConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  VLLMConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid DEBUG level",
			config: VLLMConfig{
				LoggingLevel: "DEBUG",
			},
			wantErr: false,
		},
		{
			name: "valid ERROR level",
			config: VLLMConfig{
				LoggingLevel: "ERROR",
			},
			wantErr: false,
		},
		{
			name: "invalid logging level",
			config: VLLMConfig{
				LoggingLevel: "INVALID",
			},
			wantErr: true,
			errMsg:  "invalid loggingLevel: \"INVALID\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateImagePullPolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		wantErr bool
	}{
		{"Always", "Always", false},
		{"IfNotPresent", "IfNotPresent", false},
		{"Never", "Never", false},
		{"Invalid", "Invalid", true},
		{"Empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateImagePullPolicy(tt.policy)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestModelConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  ModelConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: ModelConfig{
				Default: ModelConfigItem{
					RuntimeName: RuntimeNameOllama,
					Replicas:    1,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid default config",
			config: ModelConfig{
				Default: ModelConfigItem{
					RuntimeName: "invalid",
					Replicas:    1,
				},
			},
			wantErr: true,
			errMsg:  "invalid runtimeName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestModelConfigItemValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  ModelConfigItem
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid ollama config",
			config: ModelConfigItem{
				RuntimeName: RuntimeNameOllama,
				Replicas:    1,
			},
			wantErr: false,
		},
		{
			name: "valid vllm config",
			config: ModelConfigItem{
				RuntimeName: RuntimeNameVLLM,
				Replicas:    2,
			},
			wantErr: false,
		},
		{
			name: "invalid runtime name",
			config: ModelConfigItem{
				RuntimeName: "invalid",
				Replicas:    1,
			},
			wantErr: true,
			errMsg:  "invalid runtimeName",
		},
		{
			name: "zero replicas (valid)",
			config: ModelConfigItem{
				RuntimeName: RuntimeNameOllama,
				Replicas:    0,
			},
			wantErr: false,
		},
		{
			name: "negative replicas",
			config: ModelConfigItem{
				RuntimeName: RuntimeNameOllama,
				Replicas:    -1,
			},
			wantErr: true,
			errMsg:  "replicas must be non-negative",
		},
		{
			name: "negative context length",
			config: ModelConfigItem{
				RuntimeName:   RuntimeNameOllama,
				Replicas:      1,
				ContextLength: -1,
			},
			wantErr: true,
			errMsg:  "contextLength must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsValidRuntimeName(t *testing.T) {
	tests := []struct {
		name     string
		runtime  string
		expected bool
	}{
		{"ollama", RuntimeNameOllama, true},
		{"vllm", RuntimeNameVLLM, true},
		{"triton", RuntimeNameTriton, true},
		{"invalid", "invalid", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidRuntimeName(tt.runtime)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseWithEnvironmentVariable(t *testing.T) {
	// Test preloaded model IDs environment variable override
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(testConfigYAML), 0644)
	require.NoError(t, err)

	// Set environment variable
	original := os.Getenv("PRELOADED_MODEL_IDS")
	defer func() {
		if original == "" {
			err := os.Unsetenv("PRELOADED_MODEL_IDS")
			assert.NoError(t, err)
		} else {
			err := os.Setenv("PRELOADED_MODEL_IDS", original)
			assert.NoError(t, err)
		}
	}()

	err = os.Setenv("PRELOADED_MODEL_IDS", "model1,model2,model3")
	assert.NoError(t, err)

	config, err := Parse(configPath)
	require.NoError(t, err)

	expected := []string{"model1", "model2", "model3"}
	assert.Equal(t, expected, config.PreloadedModelIDs)
}
