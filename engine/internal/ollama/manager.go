package ollama

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
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

	os.Setenv("OLLAMA_HOST", fmt.Sprintf("0.0.0.0:%d", m.port))
	_, err := exec.Command("ollama", "serve").Output()
	return err
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
	defer os.Remove(file.Name())

	s := fmt.Sprintf("FROM %s\nAdapter %s\n", spec.BaseModel, spec.AdapterPath)
	if _, err := file.Write([]byte(s)); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	os.Setenv("OLLAMA_HOST", fmt.Sprintf("0.0.0.0:%d", m.port))
	cmd := exec.Command("ollama", "create", modelName, "-f", file.Name())
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to create model: %s", errb.String())
		return err
	}

	return nil
}
