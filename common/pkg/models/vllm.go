package models

import "fmt"

// VLLMModelName returns the vllm model name from the model ID used in LLM Operator.
func VLLMModelName(modelID string) (string, error) {
	m := map[string]string{
		"google-gemma-2b": "google/gemma-2b",
		"google-gemma-2b-it": "google/gemma-2b-it",
	}
	model, ok := m[modelID]
	if !ok {
		return "", fmt.Errorf("model ID %s not found", modelID)
	}
	return model, nil
}

