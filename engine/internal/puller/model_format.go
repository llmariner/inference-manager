package puller

import (
	"fmt"

	"github.com/llmariner/inference-manager/engine/internal/config"
	mv1 "github.com/llmariner/model-manager/api/v1"
)

// PreferredModelFormat returns the preferred model format.
func PreferredModelFormat(runtime string, supportedFormats []mv1.ModelFormat) (mv1.ModelFormat, error) {
	// TODO(guangrui): revisit.
	if len(supportedFormats) == 0 {
		return mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE, nil
	}

	switch runtime {
	case config.RuntimeNameOllama:
		if isSupportedFormat(supportedFormats, mv1.ModelFormat_MODEL_FORMAT_OLLAMA) {
			return mv1.ModelFormat_MODEL_FORMAT_OLLAMA, nil
		}
		if isSupportedFormat(supportedFormats, mv1.ModelFormat_MODEL_FORMAT_GGUF) {
			return mv1.ModelFormat_MODEL_FORMAT_GGUF, nil
		}
		if isSupportedFormat(supportedFormats, mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE) {
			return mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE, nil
		}
		return mv1.ModelFormat_MODEL_FORMAT_UNSPECIFIED, fmt.Errorf("unsupported model format for Ollama runtime: %v", supportedFormats)
	case config.RuntimeNameVLLM:
		var preferredFormat mv1.ModelFormat
		for _, f := range supportedFormats {
			if f == mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE {
				// Prefer the HuggingFace format to GGUF.
				preferredFormat = f
				break
			}
			preferredFormat = f
		}
		return preferredFormat, nil
	case config.RuntimeNameTriton:
		// Only support the Triton model format.
		if !isSupportedFormat(supportedFormats, mv1.ModelFormat_MODEL_FORMAT_NVIDIA_TRITON) {
			return mv1.ModelFormat_MODEL_FORMAT_UNSPECIFIED, fmt.Errorf("Nvidia Triton format is not included in the supported formats")
		}
		return mv1.ModelFormat_MODEL_FORMAT_NVIDIA_TRITON, nil
	default:
		return mv1.ModelFormat_MODEL_FORMAT_UNSPECIFIED, fmt.Errorf("unknown runtime: %s", runtime)
	}
}

func isSupportedFormat(formats []mv1.ModelFormat, format mv1.ModelFormat) bool {
	for _, f := range formats {
		if f == format {
			return true
		}
	}
	return false
}
