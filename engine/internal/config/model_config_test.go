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
			name: "runtime in the legacy field",
			c: &ProcessedModelConfig{
				runtime: &RuntimeConfig{
					Name: RuntimeNameVLLM,
				},
				model: &ModelConfig{
					Default: ModelConfigItem{
						RuntimeName: RuntimeNameOllama,
					},
				},
				items: map[string]ModelConfigItem{},
			},
			modelID: "google/gemma-2b-it-q4",
			want: ModelConfigItem{
				RuntimeName: RuntimeNameVLLM,
			},
		},
		{
			name: "context length in the legacy field",
			c: &ProcessedModelConfig{
				runtime: &RuntimeConfig{},
				model: &ModelConfig{
					Default: ModelConfigItem{
						ContextLength: 1024,
					},
				},
				modelContextLengths: map[string]int{
					"google/gemma-2b-it-q4": 2048,
				},
				items: map[string]ModelConfigItem{},
			},
			modelID: "google/gemma-2b-it-q4",
			want: ModelConfigItem{
				ContextLength: 2048,
			},
		},
		{
			name: "resources in the legacy field",
			c: &ProcessedModelConfig{
				model: &ModelConfig{
					Default: ModelConfigItem{},
				},
				runtime: &RuntimeConfig{
					DefaultResources: Resources{
						Requests: map[string]string{
							"nvidia.com/gpu": "1",
						},
					},
				},
				items: map[string]ModelConfigItem{},
			},
			modelID: "google/gemma-2b-it-q4",
			want: ModelConfigItem{
				Resources: Resources{
					Requests: map[string]string{
						"nvidia.com/gpu": "1",
					},
				},
			},
		},
		{
			name: "fine-tined model",
			c: &ProcessedModelConfig{
				model: &ModelConfig{
					Default: ModelConfigItem{},
				},
				runtime: &RuntimeConfig{
					DefaultResources: Resources{
						Requests: map[string]string{},
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
