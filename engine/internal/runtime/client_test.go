package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
