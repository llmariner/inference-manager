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
