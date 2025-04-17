package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelConfigItem(t *testing.T) {
	tcs := []struct {
		name    string
		c       *ProcessedModelConfig
		modelID string
		want    ModelConfigItem
	}{
		{
			name: "override in the model config",
			c: &ProcessedModelConfig{
				runtime: &RuntimeConfig{},
				model: &ModelConfig{
					Default: ModelConfigItem{
						RuntimeName: RuntimeNameOllama,
						Resources: Resources{
							Requests: map[string]string{
								"nvidia.com/gpu": "1",
							},
						},
					},
					Overrides: map[string]ModelConfigItem{
						"google/gemma-2b-it-q4": {
							RuntimeName: RuntimeNameVLLM,
							Preloaded:   true,
						},
					},
				},
				items: map[string]ModelConfigItem{},
			},
			modelID: "google/gemma-2b-it-q4",
			want: ModelConfigItem{
				RuntimeName: RuntimeNameVLLM,
				Preloaded:   true,
				Resources: Resources{
					Requests: map[string]string{
						"nvidia.com/gpu": "1",
					},
				},
			},
		},
		{
			name: "fine-tuned model",
			c: &ProcessedModelConfig{
				model: &ModelConfig{
					Default: ModelConfigItem{
						Resources: Resources{
							Requests: map[string]string{},
						},
					},
				},
				items: map[string]ModelConfigItem{
					"google-gemma-2b-it": {
						Resources: Resources{
							Requests: map[string]string{
								"nvidia.com/gpu": "1",
							},
						},
					},
				},
			},
			modelID: "ft:google-gemma-2b-it:fine-tuning-BN2TAF-WGA",
			want: ModelConfigItem{
				Resources: Resources{
					Requests: map[string]string{
						"nvidia.com/gpu": "1",
					},
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.ModelConfigItem(tc.modelID)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestExtractBaseModel(t *testing.T) {
	tcs := []struct {
		modelID string
		want    string
		wantErr bool
	}{
		{
			modelID: "ft:google-gemma-2b:fine-tuning-wpsd9kb5nl",
			want:    "google-gemma-2b",
		},
		{
			modelID: "bogus",
			wantErr: true,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.modelID, func(t *testing.T) {
			got, err := extractBaseModel(tc.modelID)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
