package vllm

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ModelFilePath returns the file path of the model.
func ModelFilePath(modelDir, modelName string) string {
	return filepath.Join(modelDir, modelName+".gguf")
}

// ChatTemplate returns the chat template for the given model.
func ChatTemplate(modelName string) (string, error) {
	switch {
	case strings.HasPrefix(modelName, "meta-llama-Meta-Llama-3.1-"):
		// This is a simplified template that does not support functions etc.
		// Please see https://llama.meta.com/docs/model-cards-and-prompt-formats/llama3_1/ for the spec.
		return `
<|begin_of_text|>
{% for message in messages %}
{{'<|start_header_id|>' + message['role'] + '<|end_header_id|>\n' + message['content'] + '\n<|eot_id|>\n'}}
{% endfor %}
`, nil
	case strings.HasPrefix(modelName, "deepseek-ai-deepseek-coder-6.7b-base"):
		return "", nil
	default:
		return "", fmt.Errorf("unsupported model: %q", modelName)
	}
}
