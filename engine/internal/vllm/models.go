package vllm

import (
	"fmt"
	"path/filepath"
	"strings"
)

// IsAWQQuantizedModel returns true if the model name is an AWQ quantized model.
func IsAWQQuantizedModel(modelName string) bool {
	return strings.HasSuffix(modelName, "-awq")
}

// ModelFilePath returns the file path of the model.
func ModelFilePath(modelDir, modelName string) string {
	if IsAWQQuantizedModel(modelName) {
		// vLLM requires the entire directory with the HuggingFace file structure.
		return modelDir
	}

	// TODO(kenji): Remove this. This is for testing.
	if modelName == "deepseek-ai-deepseek-coder-6.7b-base" {
		return modelDir
	}

	// If the model is not an AWQ quantized model, we assume it is a GGUF file.
	return filepath.Join(modelDir, modelName+".gguf")
}

// ChatTemplate returns the chat template for the given model.
func ChatTemplate(modelName string) (string, error) {
	switch {
	case strings.HasPrefix(modelName, "meta-llama-Meta-Llama-3.1-"),
		strings.HasPrefix(modelName, "TinyLlama-TinyLlama-1.1B-Chat-v1.0"):
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
