package vllm

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/manager"
)

// New returns a new Manager.
func New(c *config.Config) *Manager {
	return &Manager{
		host:    "0.0.0.0",
		port:    c.LLMPort,
		model:   c.VLLM.Model,
		numGPUs: c.VLLM.NumGPUs,
	}
}

// Manager manages the Ollama service.
type Manager struct {
	host    string
	port    int
	model   string
	numGPUs int
}

// Run starts the vLLM service.
func (m *Manager) Run() error {
	log.Printf("Starting vllm service.\n")
	return m.runCommand([]string{
		"-m", "vllm.entrypoints.openai.api_server",
		"--tensor-parallel-size", fmt.Sprintf("%d", m.numGPUs),
		"--worker-use-ray",
		"--host", m.host,
		"--port", fmt.Sprintf("%d", m.port),
		"--model", m.model,
		"--served-model-name", m.model,
	})
}

// CreateNewModel creates a new model with the given name and spec.
func (m *Manager) CreateNewModel(modelName string, spec *manager.ModelSpec) error {
	log.Printf("Create Model is not implemented\n")
	return nil
}

// WaitForReady waits for the vllm service to be ready.
func (m *Manager) WaitForReady() error {
	log.Printf("vLLM is ready\n")
	return nil
}

func (m *Manager) runCommand(args []string) error {
	log.Printf("Running vllm command: %v", args)
	cmd := exec.Command("python3", args...)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to run %v: %s", args, errb.String())
		return err
	}

	return nil
}

// UpdateModelTemplateToLatest updates the model template to the latest.
func (m *Manager) UpdateModelTemplateToLatest(modelName string) error {
	log.Printf("UpdateModelTemplateToLatest is not implemented\n")
	return nil
}

// IsReady returns true if the processor is ready. If not,
// it returns a message describing why it is not ready.
func (m *Manager) IsReady() (bool, string) {
	// TODO(kenji): Implement this.
	return true, ""
}
