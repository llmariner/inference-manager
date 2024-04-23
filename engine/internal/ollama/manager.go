package ollama

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

// NewManager returns a new Manager.
func NewManager(port int) *Manager {
	return &Manager{port: port}
}

// Manager manages the Ollama service.
type Manager struct {
	port int
}

// Run starts the Ollama service on the given port.
func (m *Manager) Run() error {
	log.Printf("Starting Ollama on port %d\n", m.port)

	return m.runCommand([]string{"serve"})
}

// ModelSpec is the specification for a new model.
type ModelSpec struct {
	BaseModel   string
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

	s := fmt.Sprintf("FROM %s\nAdapter %s\n", spec.BaseModel, spec.AdapterPath)
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
	if err := os.Setenv("OLLAMA_HOST", fmt.Sprintf("0.0.0.0:%d", m.port)); err != nil {
		return err
	}
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
