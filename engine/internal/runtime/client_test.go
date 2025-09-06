package runtime

import (
	"reflect"
	"testing"

	"github.com/llmariner/inference-manager/engine/internal/config"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"
)

func TestResourceName(t *testing.T) {
	tcs := []struct {
		runtime string
		modelID string
		want    string
	}{
		{
			runtime: "ollama",
			modelID: "meta-llama-Meta-Llama-3.1-70B-Instruct-q2_k",
			want:    "ollama-meta-llama-meta-llama-3-1-70b-instruct-q2-k",
		},
		{
			runtime: "ollama",
			modelID: "ft:meta-llama-Meta-Llama-3.1-70B-Instruct-q2_k:fine-tuning-MSV0AI_NYQ",
			want:    "ollama--meta-llama-3-1-70b-instruct-q2-k--msv0ai-nyq",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.modelID, func(t *testing.T) {
			got := resourceName(tc.runtime, tc.modelID)
			assert.Equal(t, tc.want, got)

			// Verify the length of the resource name is not too long for "controller-revision-hash".
			hash := got + "-67b77fd448"
			assert.LessOrEqual(t, len(hash), 63)
		})
	}
}

func TestBuildAffinityApplyConfig(t *testing.T) {
	var tests = []struct {
		name     string
		affinity *corev1.Affinity
		want     *corev1apply.AffinityApplyConfiguration
	}{
		{
			name: "node affinity",
			affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "test",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"val1", "val2"},
									},
								},
							},
						},
					},
				},
			},
			want: corev1apply.Affinity().
				WithNodeAffinity(corev1apply.NodeAffinity().
					WithRequiredDuringSchedulingIgnoredDuringExecution(corev1apply.NodeSelector().
						WithNodeSelectorTerms(corev1apply.NodeSelectorTerm().
							WithMatchExpressions(corev1apply.NodeSelectorRequirement().
								WithKey("test").
								WithOperator(corev1.NodeSelectorOperator(corev1.NodeSelectorOpIn)).
								WithValues("val1", "val2"))))),
		},
		{
			name: "pod affinity",
			affinity: &corev1.Affinity{
				PodAffinity: &corev1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "test"},
							},
						},
					},
				},
			},
			want: corev1apply.Affinity().
				WithPodAffinity(corev1apply.PodAffinity().
					WithRequiredDuringSchedulingIgnoredDuringExecution(corev1apply.PodAffinityTerm().
						WithLabelSelector(metav1apply.LabelSelector().
							WithMatchLabels(map[string]string{"app": "test"})))),
		},
		{
			name: "pod anti-affinity",
			affinity: &corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							TopologyKey:    "zone",
							MatchLabelKeys: []string{"foo", "bar"},
						},
					},
				},
			},
			want: corev1apply.Affinity().
				WithPodAntiAffinity(corev1apply.PodAntiAffinity().
					WithRequiredDuringSchedulingIgnoredDuringExecution(corev1apply.PodAffinityTerm().
						WithTopologyKey("zone").
						WithMatchLabelKeys("foo", "bar"))),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildAffinityApplyConfig(test.affinity)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestConvertEnvVarsToApplyConfig(t *testing.T) {
	tests := []struct {
		name    string
		envVars []corev1.EnvVar
		want    []*corev1apply.EnvVarApplyConfiguration
	}{
		{
			name:    "empty slice",
			envVars: []corev1.EnvVar{},
			want:    nil,
		},
		{
			name: "simple env var with value",
			envVars: []corev1.EnvVar{
				{Name: "TEST_VAR", Value: "test_value"},
			},
			want: []*corev1apply.EnvVarApplyConfiguration{
				corev1apply.EnvVar().WithName("TEST_VAR").WithValue("test_value"),
			},
		},
		{
			name: "env var with configmap key ref",
			envVars: []corev1.EnvVar{
				{
					Name: "CONFIG_VAR",
					ValueFrom: &corev1.EnvVarSource{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "test-config"},
							Key:                  "config-key",
							Optional:             ptr.To(true),
						},
					},
				},
			},
			want: []*corev1apply.EnvVarApplyConfiguration{
				corev1apply.EnvVar().WithName("CONFIG_VAR").WithValueFrom(
					corev1apply.EnvVarSource().WithConfigMapKeyRef(
						corev1apply.ConfigMapKeySelector().
							WithName("test-config").
							WithKey("config-key").
							WithOptional(true),
					),
				),
			},
		},
		{
			name: "env var with secret key ref",
			envVars: []corev1.EnvVar{
				{
					Name: "SECRET_VAR",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
							Key:                  "secret-key",
						},
					},
				},
			},
			want: []*corev1apply.EnvVarApplyConfiguration{
				corev1apply.EnvVar().WithName("SECRET_VAR").WithValueFrom(
					corev1apply.EnvVarSource().WithSecretKeyRef(
						corev1apply.SecretKeySelector().
							WithName("test-secret").
							WithKey("secret-key"),
					),
				),
			},
		},
		{
			name: "env var with field ref",
			envVars: []corev1.EnvVar{
				{
					Name: "FIELD_VAR",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							APIVersion: "v1",
							FieldPath:  "metadata.name",
						},
					},
				},
			},
			want: []*corev1apply.EnvVarApplyConfiguration{
				corev1apply.EnvVar().WithName("FIELD_VAR").WithValueFrom(
					corev1apply.EnvVarSource().WithFieldRef(
						corev1apply.ObjectFieldSelector().
							WithAPIVersion("v1").
							WithFieldPath("metadata.name"),
					),
				),
			},
		},
		{
			name: "env var with resource field ref",
			envVars: []corev1.EnvVar{
				{
					Name: "RESOURCE_VAR",
					ValueFrom: &corev1.EnvVarSource{
						ResourceFieldRef: &corev1.ResourceFieldSelector{
							ContainerName: "test-container",
							Resource:      "limits.memory",
							Divisor:       resource.MustParse("1Mi"),
						},
					},
				},
			},
			want: []*corev1apply.EnvVarApplyConfiguration{
				corev1apply.EnvVar().WithName("RESOURCE_VAR").WithValueFrom(
					corev1apply.EnvVarSource().WithResourceFieldRef(
						corev1apply.ResourceFieldSelector().
							WithContainerName("test-container").
							WithResource("limits.memory").
							WithDivisor(resource.MustParse("1Mi")),
					),
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertEnvToApplyConfig(tt.envVars)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestConvertEnvFromToApplyConfig(t *testing.T) {
	tests := []struct {
		name           string
		envFromSources []corev1.EnvFromSource
		want           []*corev1apply.EnvFromSourceApplyConfiguration
	}{
		{
			name:           "empty slice",
			envFromSources: []corev1.EnvFromSource{},
			want:           nil,
		},
		{
			name: "configmap ref without prefix",
			envFromSources: []corev1.EnvFromSource{
				{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "test-config"},
					},
				},
			},
			want: []*corev1apply.EnvFromSourceApplyConfiguration{
				corev1apply.EnvFromSource().WithPrefix("").WithConfigMapRef(
					corev1apply.ConfigMapEnvSource().WithName("test-config"),
				),
			},
		},
		{
			name: "configmap ref with prefix and optional",
			envFromSources: []corev1.EnvFromSource{
				{
					Prefix: "CONFIG_",
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "test-config"},
						Optional:             ptr.To(true),
					},
				},
			},
			want: []*corev1apply.EnvFromSourceApplyConfiguration{
				corev1apply.EnvFromSource().WithPrefix("CONFIG_").WithConfigMapRef(
					corev1apply.ConfigMapEnvSource().WithName("test-config").WithOptional(true),
				),
			},
		},
		{
			name: "secret ref without prefix",
			envFromSources: []corev1.EnvFromSource{
				{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
					},
				},
			},
			want: []*corev1apply.EnvFromSourceApplyConfiguration{
				corev1apply.EnvFromSource().WithPrefix("").WithSecretRef(
					corev1apply.SecretEnvSource().WithName("test-secret"),
				),
			},
		},
		{
			name: "secret ref with prefix and optional",
			envFromSources: []corev1.EnvFromSource{
				{
					Prefix: "SECRET_",
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
						Optional:             ptr.To(false),
					},
				},
			},
			want: []*corev1apply.EnvFromSourceApplyConfiguration{
				corev1apply.EnvFromSource().WithPrefix("SECRET_").WithSecretRef(
					corev1apply.SecretEnvSource().WithName("test-secret").WithOptional(false),
				),
			},
		},
		{
			name: "multiple sources",
			envFromSources: []corev1.EnvFromSource{
				{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
					},
				},
				{
					Prefix: "APP_",
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "secret1"},
					},
				},
			},
			want: []*corev1apply.EnvFromSourceApplyConfiguration{
				corev1apply.EnvFromSource().WithPrefix("").WithConfigMapRef(
					corev1apply.ConfigMapEnvSource().WithName("config1"),
				),
				corev1apply.EnvFromSource().WithPrefix("APP_").WithSecretRef(
					corev1apply.SecretEnvSource().WithName("secret1"),
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertEnvFromToApplyConfig(tt.envFromSources)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestUpdateResourceConfWithModelConfig(t *testing.T) {
	tcs := []struct {
		name          string
		origResConf   *config.Resources
		runtimeConfig *mv1.ModelConfig_RuntimeConfig
		want          *config.Resources
	}{
		{
			name: "gpu not in model config",
			origResConf: &config.Resources{
				Limits: map[string]string{
					"cpu":             "1",
					nvidiaGPUResource: "2",
				},
			},
			runtimeConfig: &mv1.ModelConfig_RuntimeConfig{},
			want: &config.Resources{
				Limits: map[string]string{
					"cpu":             "1",
					nvidiaGPUResource: "2",
				},
			},
		},
		{
			name: "gpu in model config, gpu not in resource conf",
			origResConf: &config.Resources{
				Limits: map[string]string{
					"cpu": "1",
				},
			},
			runtimeConfig: &mv1.ModelConfig_RuntimeConfig{
				Resources: &mv1.ModelConfig_RuntimeConfig_Resources{
					Gpu: 3,
				},
			},
			want: &config.Resources{
				Limits: map[string]string{
					"cpu":             "1",
					nvidiaGPUResource: "3",
				},
				Requests: map[string]string{
					nvidiaGPUResource: "3",
				},
			},
		},
		{
			name: "gpu in model config, gpu in resource conf",
			origResConf: &config.Resources{
				Limits: map[string]string{
					"cpu":             "1",
					nvidiaGPUResource: "2",
				},
			},
			runtimeConfig: &mv1.ModelConfig_RuntimeConfig{
				Resources: &mv1.ModelConfig_RuntimeConfig_Resources{
					Gpu: 3,
				},
			},
			want: &config.Resources{
				Limits: map[string]string{
					"cpu":             "1",
					nvidiaGPUResource: "3",
				},
				Requests: map[string]string{
					nvidiaGPUResource: "3",
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			updateResourceConfWithModelConfig(tc.origResConf, tc.runtimeConfig)
			assert.True(t, reflect.DeepEqual(tc.want, tc.origResConf), "got: %+v, want: %+v", tc.origResConf, tc.want)
		})
	}

}
