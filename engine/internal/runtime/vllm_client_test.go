package runtime

import (
	"testing"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNumGPUs(t *testing.T) {
	v := &vllmClient{
		commonClient: &commonClient{
			RuntimeConfig: config.RuntimeConfig{
				ModelResources: map[string]config.Resources{
					"model0": {
						Limits: map[string]string{
							nvidiaGPUResource: "2",
							"cpu":             "4",
						},
					},
					"model1": {
						Limits: map[string]string{
							"cpu": "8",
						},
					},
				},
			},
		},
	}

	tcs := []struct {
		name    string
		modelID string
		want    int
	}{
		{
			name:    "model0",
			modelID: "model0",
			want:    2,
		},
		{
			name:    "model1",
			modelID: "model1",
			want:    0,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := v.numGPUs(tc.modelID)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
