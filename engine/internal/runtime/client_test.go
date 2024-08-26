package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatefulSetName(t *testing.T) {
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
	}
	for _, tc := range tcs {
		t.Run(tc.modelID, func(t *testing.T) {
			got := statefulSetName(tc.runtime, tc.modelID)
			assert.Equal(t, tc.want, got)
		})
	}
}
