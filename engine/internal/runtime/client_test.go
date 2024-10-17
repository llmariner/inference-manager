package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
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
