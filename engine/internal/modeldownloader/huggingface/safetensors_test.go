package huggingface

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnmarshalSafetensorIndex(t *testing.T) {
	b := []byte(`{
    "metadata": {
        "total_size": 39767785472
    },
    "weight_map": {
        "model.embed_tokens.weight": "model-00001-of-00009.safetensors",
        "model.layers.0.self_attn.q_proj.qweight": "model-00001-of-00009.safetensors",
        "model.layers.79.post_attention_layernorm.weight": "model-00008-of-00009.safetensors",
        "model.norm.weight": "model-00008-of-00009.safetensors",
        "lm_head.weight": "model-00009-of-00009.safetensors"
    }
}`)

	si, err := unmarshalSafetensorsIndex(b)
	assert.NoError(t, err)
	want := &safetensorsIndex{
		WeightMap: map[string]string{
			"model.embed_tokens.weight":                       "model-00001-of-00009.safetensors",
			"model.layers.0.self_attn.q_proj.qweight":         "model-00001-of-00009.safetensors",
			"model.layers.79.post_attention_layernorm.weight": "model-00008-of-00009.safetensors",
			"model.norm.weight":                               "model-00008-of-00009.safetensors",
			"lm_head.weight":                                  "model-00009-of-00009.safetensors",
		},
	}
	assert.Equal(t, want, si)
}
