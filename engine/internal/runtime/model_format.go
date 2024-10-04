package runtime

import (
	"fmt"

	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/inference-manager/engine/internal/config"
)

// PreferredModelFormat returns the preferred model format.
func PreferredModelFormat(runtime string, supportedFormats []mv1.ModelFormat) (mv1.ModelFormat, error) {
	// TODO(guangrui): revisit.
	if len(supportedFormats) == 0 {
		return mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE, nil
	}

	switch runtime {
	case config.RuntimeNameOllama:
		// Only support GGUF.
		hasGGUF := false
		for _, f := range supportedFormats {
			if f == mv1.ModelFormat_MODEL_FORMAT_GGUF {
				hasGGUF = true
				break
			}
		}
		if !hasGGUF {
			return mv1.ModelFormat_MODEL_FORMAT_UNSPECIFIED, fmt.Errorf("GGUF format is not supported")
		}
		return mv1.ModelFormat_MODEL_FORMAT_GGUF, nil
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
	default:
		return mv1.ModelFormat_MODEL_FORMAT_UNSPECIFIED, fmt.Errorf("unknown runtime: %s", runtime)
	}
}
