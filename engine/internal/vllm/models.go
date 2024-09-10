package vllm

import (
	"fmt"
	"path/filepath"

	mv1 "github.com/llm-operator/model-manager/api/v1"
)

// ModelFilePath returns the file path of the model.
func ModelFilePath(modelDir, modelName string, format mv1.ModelFormat) (string, error) {
	switch format {
	case mv1.ModelFormat_MODEL_FORMAT_GGUF:
		return filepath.Join(modelDir, modelName+".gguf"), nil
	case mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE:
		return modelDir, nil
	default:
		return "", fmt.Errorf("unsupported model format: %s", format)
	}
}

// PreferredModelFormat returns the preferred model format.
func PreferredModelFormat(resp *mv1.GetBaseModelPathResponse) (mv1.ModelFormat, error) {
	if len(resp.Formats) == 0 {
		return mv1.ModelFormat_MODEL_FORMAT_UNSPECIFIED, fmt.Errorf("no formats")
	}
	var preferredFormat mv1.ModelFormat
	for _, f := range resp.Formats {
		if f == mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE {
			// Prefer the HuggingFace format to GGUF.
			preferredFormat = f
			break
		}
		preferredFormat = f
	}
	return preferredFormat, nil
}
