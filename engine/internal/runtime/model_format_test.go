package runtime

import (
	"testing"

	"github.com/llmariner/inference-manager/engine/internal/config"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestPreferredModelFormat(t *testing.T) {
	tcs := []struct {
		name             string
		runtime          string
		supportedFormats []mv1.ModelFormat
		want             mv1.ModelFormat
		wantErr          bool
	}{
		{
			name:    "ollama",
			runtime: config.RuntimeNameOllama,
			supportedFormats: []mv1.ModelFormat{
				mv1.ModelFormat_MODEL_FORMAT_GGUF,
			},
			want: mv1.ModelFormat_MODEL_FORMAT_GGUF,
		},
		{
			name:    "ollama, no gguf",
			runtime: config.RuntimeNameOllama,
			supportedFormats: []mv1.ModelFormat{
				mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE,
			},
			wantErr: true,
		},
		{
			name:    "vllm",
			runtime: config.RuntimeNameVLLM,
			supportedFormats: []mv1.ModelFormat{
				mv1.ModelFormat_MODEL_FORMAT_GGUF,
				mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE,
			},
			want: mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE,
		},
		{
			name:    "vllm, gguf only",
			runtime: config.RuntimeNameVLLM,
			supportedFormats: []mv1.ModelFormat{
				mv1.ModelFormat_MODEL_FORMAT_GGUF,
			},
			want: mv1.ModelFormat_MODEL_FORMAT_GGUF,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PreferredModelFormat(tc.runtime, tc.supportedFormats)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
