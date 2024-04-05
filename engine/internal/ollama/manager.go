package ollama

import (
	"fmt"
	"log"
	"os"
	"os/exec"
)

// NewManager returns a new Manager.
func NewManager() *Manager {
	return &Manager{}
}

// Manager manages the Ollama service.
type Manager struct {
}

func (m *Manager) Run(port int) error {
	log.Printf("Starting Ollama on port %d\n", port)

	os.Setenv("OLLAMA_HOST", fmt.Sprintf("0.0.0.0:%d", port))
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

	if _, err := exec.Command("ollama", "create", modelName, "-f", file.Name()).Output(); err != nil {
		return err
	}

	return nil
}
