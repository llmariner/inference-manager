package ollama

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ollama/ollama/api"
)

// NewManager returns a new Manager.
func NewManager(addr string) *Manager {
	url := &url.URL{
		Scheme: "http",
		Host:   addr,
	}
	return &Manager{
		client: api.NewClient(url, http.DefaultClient),
	}
}

// Manager manages the Ollama service.
type Manager struct {
	client *api.Client
}

// Run starts the Ollama service.
func (m *Manager) Run() error {
	log.Printf("Starting Ollama service.\n")

	return m.runCommand([]string{"serve"})
}

// ModelSpec is the specification for a new model.
type ModelSpec struct {
	From        string
	AdapterPath string
}

// CreateNewModel creates a new model with the given name and spec.
func (m *Manager) CreateNewModel(modelName string, spec *ModelSpec) error {
	file, err := os.CreateTemp("/tmp", "model")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil {
			log.Printf("Failed to remove %q: %s", file.Name(), err)
		}
	}()

	s := fmt.Sprintf("FROM %s\n", spec.From)
	if p := spec.AdapterPath; p != "" {
		s += fmt.Sprintf("Adapter %s\n", p)
	} else {
		modelFile, err := ollamaBaseModelFile(modelName)
		if err != nil {
			return err
		}
		s += modelFile
	}
	if _, err := file.Write([]byte(s)); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return m.runCommand([]string{"create", modelName, "-f", file.Name()})
}

// PullBaseModel pulls the base model from the given path.
func (m *Manager) PullBaseModel(modelName string) error {
	return m.runCommand([]string{"pull", modelName})
}

// WaitForReady waits for the Ollama service to be ready.
func (m *Manager) WaitForReady() error {
	const (
		timeout = 30 * time.Second
		tick    = 1 * time.Second
	)

	log.Printf("Waiting for Ollama to be ready\n")
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := m.runCommand([]string{"list"}); err == nil {
				return nil
			}
		case <-time.After(timeout):
			return fmt.Errorf("timeout waiting for Ollama to be ready")
		}
	}
}

func (m *Manager) runCommand(args []string) error {
	log.Printf("Running Ollama command: %v", args)
	cmd := exec.Command("ollama", args...)
	var errb bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to run %v: %s", args, errb.String())
		return err
	}

	return nil
}

// ollamaBaseModelFile returns the base model file for the given model name.
// This is based on the output of "ollama show <model> --modelfile".
func ollamaBaseModelFile(name string) (string, error) {
	switch {
	case strings.HasPrefix(name, "google-gemma-"):
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

	case strings.HasPrefix(name, "meta-llama-Meta-Llama-3-8B-Instruct"):
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

	case strings.HasPrefix(name, "mistralai-Mistral-7B-Instruct"):
		// Output of "ollama show mistral --modelfile".
		return `
TEMPLATE """[INST] {{ .System }} {{ .Prompt }} [/INST]"""
PARAMETER stop "[INST]"
PARAMETER stop "[/INST]"`, nil

	case strings.HasPrefix(name, "mistralai-Mixtral-8x22B-Instruct"):
		// Output of "ollama show mixtral --modelfile".
		return `
TEMPLATE """[INST] {{ if .System }}{{ .System }} {{ end }}{{ .Prompt }} [/INST]"""
PARAMETER stop "[INST]"
PARAMETER stop "[/INST]"`, nil

	case strings.HasPrefix(name, "meta-llama-Meta-Llama-3.1-"):
		// Output of "ollama show llama3.1 --modelfile".
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
PARAMETER stop <|eot_id|>`, nil

	case strings.HasPrefix(name, "deepseek-ai-deepseek-coder-6.7b-base"):
		// Output of "ollama show deepseek-coder --modelfile".
		return `
TEMPLATE "{{ .System }}
### Instruction:
{{ .Prompt }}
### Response:
"
SYSTEM You are an AI programming assistant, utilizing the Deepseek Coder model, developed by Deepseek Company, and you only answer questions related to computer science. For politically sensitive questions, security and privacy issues, and other non-computer science questions, you will refuse to answer.
`, nil

	default:
		return "", fmt.Errorf("unsupported base model in Ollama modelfile: %q", name)
	}
}

// DeleteModel deletes the model.
func (m *Manager) DeleteModel(ctx context.Context, modelName string) error {
	return m.client.Delete(ctx, &api.DeleteRequest{
		Model: modelName,
	})
}
