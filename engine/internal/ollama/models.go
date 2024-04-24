package ollama

import "fmt"

// ConvertHuggingFaceModelNameToOllama converts a HuggingFace model name to an Ollama model name.
func ConvertHuggingFaceModelNameToOllama(hfModelName string) (string, error) {
	fromHuggingFacetoOllama := map[string]string{
		"google/gemma-2b": "gemma:2b",
		// This format is for a fine-tuned model.
		"google-gemma-2b": "gemma:2b",
	}
	v, ok := fromHuggingFacetoOllama[hfModelName]
	if !ok {
		return "", fmt.Errorf("unsupported base model: %q", hfModelName)
	}
	return v, nil
}
