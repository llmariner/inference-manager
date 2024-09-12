package ollama

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModelSpec is the specification for a new model.
type ModelSpec struct {
	From        string
	AdapterPath string
}

// ModelfilePath returns the model file path.
func ModelfilePath(modelDir string, modelID string) string {
	return filepath.Join(modelDir, modelID, "modelfile")
}

// CreateModelfile creates a new model file.
func CreateModelfile(
	filePath string,
	modelID string,
	spec *ModelSpec,
	contextLength int,
) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	if err := WriteModelfile(modelID, spec, contextLength, file); err != nil {
		return err
	}
	return nil
}

// WriteModelfile writes the model file.
func WriteModelfile(
	modelID string,
	spec *ModelSpec,
	contextLength int,
	file *os.File,
) error {
	s := fmt.Sprintf("FROM %s\n", spec.From)
	if p := spec.AdapterPath; p != "" {
		s += fmt.Sprintf("Adapter %s\n", p)
	} else {
		modelFile, err := ollamaBaseModelFile(modelID)
		if err != nil {
			return err
		}
		s += modelFile
		if contextLength == 0 {
			if l, useNonDefault, err := contextLengthOfModel(modelID); err != nil {
				return err
			} else if useNonDefault {
				contextLength = l
			}
		}
		if contextLength > 0 {
			s += fmt.Sprintf("PARAMETER num_ctx %d\n", contextLength)
		}
	}
	if _, err := file.Write([]byte(s)); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return nil
}

// contextLengthOfModel returns the context length for the given model name if it is set to a non-default value.
// If it is set to the default value, the function returns false.
func contextLengthOfModel(modelID string) (int, bool, error) {
	switch {
	case strings.HasPrefix(modelID, "google-gemma-"):
		return 0, false, nil
	case strings.HasPrefix(modelID, "meta-llama-Meta-Llama-3-8B-Instruct"):
		return 0, false, nil
	case strings.HasPrefix(modelID, "mistralai-Mistral-7B-Instruct"):
		return 0, false, nil
	case strings.HasPrefix(modelID, "mistralai-Mixtral-8x22B-Instruct"):
		return 0, false, nil
	case strings.HasPrefix(modelID, "meta-llama-Meta-Llama-3.1-"):
		// The publicly announced max context length is 128K, but we limit the context length
		// to 64K here to make this work smoothly in g5.48xlarge.
		return 65536, true, nil
	case strings.HasPrefix(modelID, "deepseek-ai-deepseek-coder-6.7b-base"):
		return 16384, true, nil
	case strings.HasPrefix(modelID, "sentence-transformers-all-MiniLM-L6-v2"):
		return 256, true, nil
	default:
		return 0, false, fmt.Errorf("unsupported base model in Ollama modelfile: %q", modelID)
	}
}

// ollamaBaseModelFile returns the base model file for the given model ID.
// This is based on the output of "ollama show <model> --modelfile".
func ollamaBaseModelFile(modelID string) (string, error) {
	switch {
	case strings.HasPrefix(modelID, "google-gemma-"):
		// Output of "ollama show gemma:2b --modelfile".
		return `
TEMPLATE """<start_of_turn>user
{{ if .System }}{{ .System }} {{ end }}{{ .Prompt }}<end_of_turn>
<start_of_turn>model
{{ .Response }}<end_of_turn>
"""
PARAMETER repeat_penalty 1
PARAMETER stop "<start_of_turn>"
PARAMETER stop "<end_of_turn>"`, nil

	case strings.HasPrefix(modelID, "meta-llama-Meta-Llama-3-8B-Instruct"):
		// Output of "ollama show llama3 --modelfile".
		return `
TEMPLATE "{{ if .System }}<|start_header_id|>system<|end_header_id|>

{{ .System }}<|eot_id|>{{ end }}{{ if .Prompt }}<|start_header_id|>user<|end_header_id|>

{{ .Prompt }}<|eot_id|>{{ end }}<|start_header_id|>assistant<|end_header_id|>

{{ .Response }}<|eot_id|>"
PARAMETER stop <|start_header_id|>
PARAMETER stop <|end_header_id|>
PARAMETER stop <|eot_id|>
PARAMETER num_keep 24`, nil

	case strings.HasPrefix(modelID, "mistralai-Mistral-7B-Instruct"):
		// Output of "ollama show mistral --modelfile".
		return `
TEMPLATE """[INST] {{ .System }} {{ .Prompt }} [/INST]"""
PARAMETER stop "[INST]"
PARAMETER stop "[/INST]"`, nil

	case strings.HasPrefix(modelID, "mistralai-Mixtral-8x22B-Instruct"):
		// Output of "ollama show mixtral --modelfile".
		return `
TEMPLATE """[INST] {{ if .System }}{{ .System }} {{ end }}{{ .Prompt }} [/INST]"""
PARAMETER stop "[INST]"
PARAMETER stop "[/INST]"`, nil

	case strings.HasPrefix(modelID, "meta-llama-Meta-Llama-3.1-"),
		strings.HasPrefix(modelID, "mattshumer-Reflection-Llama-3.1-70B"):
		// Output of "ollama show llama3.1 --modelfile" except the context length parameter.
		// The publicly announced max context length is 128K, but we limit the context length
		// to 64K here to make this work smoothly in g5.48xlarge.
		//
		// TODO(kenji): Might need to update the template once
		// https://github.com/ollama/ollama/issues/6060 is fixed.
		return `
TEMPLATE """{{ if .Messages }}
{{- if or .System .Tools }}<|start_header_id|>system<|end_header_id|>
{{- if .System }}

{{ .System }}
{{- end }}
{{- if .Tools }}

You are a helpful assistant with tool calling capabilities. When you receive a tool call response, use the output to format an answer to the orginal use question.
{{- end }}<|eot_id|>
{{- end }}
{{- range $i, $_ := .Messages }}
{{- $last := eq (len (slice $.Messages $i)) 1 }}
{{- if eq .Role "user" }}<|start_header_id|>user<|end_header_id|>
{{- if and $.Tools $last }}

Given the following functions, please respond with a JSON for a function call with its proper arguments that best answers the given prompt.

Respond in the format {"name": function name, "parameters": dictionary of argument name and its value}. Do not use variables.

{{ $.Tools }}
{{- end }}

{{ .Content }}<|eot_id|>{{ if $last }}<|start_header_id|>assistant<|end_header_id|>

{{ end }}
{{- else if eq .Role "assistant" }}<|start_header_id|>assistant<|end_header_id|>
{{- if .ToolCalls }}

{{- range .ToolCalls }}{"name": "{{ .Function.Name }}", "parameters": {{ .Function.Arguments }}}{{ end }}
{{- else }}

{{ .Content }}{{ if not $last }}<|eot_id|>{{ end }}
{{- end }}
{{- else if eq .Role "tool" }}<|start_header_id|>ipython<|end_header_id|>

{{ .Content }}<|eot_id|>{{ if $last }}<|start_header_id|>assistant<|end_header_id|>

{{ end }}
{{- end }}
{{- end }}
{{- else }}
{{- if .System }}<|start_header_id|>system<|end_header_id|>

{{ .System }}<|eot_id|>{{ end }}{{ if .Prompt }}<|start_header_id|>user<|end_header_id|>

{{ .Prompt }}<|eot_id|>{{ end }}<|start_header_id|>assistant<|end_header_id|>

{{ end }}{{ .Response }}{{ if .Response }}<|eot_id|>{{ end }}"""
PARAMETER stop <|start_header_id|>
PARAMETER stop <|end_header_id|>
PARAMETER stop <|eot_id|>
`, nil

	case strings.HasPrefix(modelID, "deepseek-ai-deepseek-coder-6.7b-base"),
		strings.HasPrefix(modelID, "deepseek-ai-DeepSeek-Coder-V2-Lite-Base"):
		// This is different from the output of "ollama show deepseek-coder --modelfile".
		// Instead, this is tailored for auto completion for continue.dev.
		return `
TEMPLATE {{ .Prompt }}
PARAMETER stop <｜end▁of▁sentence｜>
`, nil
	case strings.HasPrefix(modelID, "sentence-transformers-all-MiniLM-L6-v2"):
		// This model is for embedding.
		return `
TEMPLATE {{ .Prompt }}
`, nil
	default:
		return "", fmt.Errorf("unsupported base model in Ollama modelfile: %q", modelID)
	}
}

// ModelName returns the Ollama model name from the model ID used in LLM Operator.
//
// Ollama does not accept more than two ":" while the model ID of the fine-tuning jobs is be "ft:<base-model>:<suffix>".
func ModelName(modelID string) string {
	if !strings.HasPrefix(modelID, "ft:") {
		return modelID
	}
	return modelID[3:]
}
