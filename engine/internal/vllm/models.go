package vllm

import (
	"fmt"
	"path/filepath"
	"strings"

	mv1 "github.com/llm-operator/model-manager/api/v1"
)

// IsAWQQuantizedModel returns true if the model name is an AWQ quantized model.
func IsAWQQuantizedModel(modelName string) bool {
	return strings.HasSuffix(modelName, "-awq")
}

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

// ChatTemplate returns the chat template for the given model.
func ChatTemplate(modelName string) (string, error) {
	switch {
	case strings.HasPrefix(modelName, "meta-llama-Meta-Llama-3.1-"),
		strings.HasPrefix(modelName, "TinyLlama-TinyLlama-1.1B-Chat-v1.0"),
		strings.HasPrefix(modelName, "mattshumer-Reflection-Llama-3.1-70B"):
		// This is a simplified template that does not support functions etc.
		// Please see https://llama.meta.com/docs/model-cards-and-prompt-formats/llama3_1/ for the spec.
		return `
<|begin_of_text|>
{% for message in messages %}
{{'<|start_header_id|>' + message['role'] + '<|end_header_id|>\n' + message['content'] + '\n<|eot_id|>\n'}}
{% endfor %}
`, nil
	case strings.HasPrefix(modelName, "deepseek-ai-deepseek-coder-6.7b-base"),
		strings.HasPrefix(modelName, "deepseek-ai-DeepSeek-Coder-V2-Lite-Base"):
		// This is a simplified template that works for auto code completion.
		// See https://huggingface.co/deepseek-ai/deepseek-coder-6.7b-instruct/blob/main/tokenizer_config.json#L34.
		return `
{% for message in messages %}
{{message['content']}}
{% endfor %}
`, nil
	default:
		return "", fmt.Errorf("unsupported model: %q", modelName)
	}
}
