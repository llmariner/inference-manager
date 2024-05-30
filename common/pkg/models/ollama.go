package models

import "strings"

// OllamaModelName returns the Ollama model name from the model ID used in LLM Operator.
//
// Ollama does not accept more than two ":" while the model ID of the fine-tuning jobs is be "ft:<base-model>:<suffix>".
func OllamaModelName(modelID string) string {
	if !strings.HasPrefix(modelID, "ft:") {
		return modelID
	}
	return modelID[3:]
}
