package ollama

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	mv1 "github.com/llm-operator/model-manager/api/v1"
)

type cmdRunnter interface {
	Run(*exec.Cmd) error
}

type cmdRunnerImpl struct {
}

type s3Client interface {
	Download(ctx context.Context, f io.WriterAt, path string) error
}

func (c *cmdRunnerImpl) Run(cmd *exec.Cmd) error {
	return cmd.Run()
}

// New returns a new Manager.
func New(contextLengthsByModelID map[string]int, s3Client s3Client) *Manager {
	return &Manager{
		contextLengthsByModelID: contextLengthsByModelID,
		s3Client:                s3Client,
		cmdRunner:               &cmdRunnerImpl{},
	}
}

// Manager manages the Ollama service.
//
// TODO(kenji): Refactor this class once we completely switch to the one-odel-per-pod implementation where
// inference-manager-engine doesn't directly run vLLM or Ollama.
type Manager struct {
	contextLengthsByModelID map[string]int

	s3Client s3Client

	cmdRunner cmdRunnter

	isReady bool
	mu      sync.Mutex
}

// Run starts the Ollama service.
func (m *Manager) Run() error {
	log.Printf("Starting Ollama service.\n")

	return m.runCommand([]string{"serve"})
}

// CreateNewModelOfGGUF creates a new model with the given name and spec that uses a GGUF model file.
func (m *Manager) CreateNewModelOfGGUF(modelID string, spec *ollama.ModelSpec) error {
	file, err := os.CreateTemp("/tmp", "model")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil {
			log.Printf("Failed to remove %q: %s", file.Name(), err)
		}
	}()

	if err := ollama.WriteModelfile(modelID, spec, m.contextLengthsByModelID, file); err != nil {
		return err
	}

	return m.runCommand([]string{"create", modelID, "-f", file.Name()})
}

// DownloadAndCreateNewModel downloads the model from the given path and creates a new model.
func (m *Manager) DownloadAndCreateNewModel(ctx context.Context, modelID string, resp *mv1.GetBaseModelPathResponse) error {
	var hasGGUF bool
	for _, f := range resp.Formats {
		if f == mv1.ModelFormat_MODEL_FORMAT_GGUF {
			hasGGUF = true
			break
		}
	}
	if !hasGGUF {
		return fmt.Errorf("supported model formats: %s", resp.Formats)
	}

	log.Printf("Downloading the GGUF model from %q\n", resp.GgufModelPath)
	f, err := os.CreateTemp("/tmp", "model")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(f.Name()); err != nil {
			log.Printf("Failed to remove %q: %s", f.Name(), err)
		}
	}()

	if err := m.s3Client.Download(ctx, f, resp.GgufModelPath); err != nil {
		return fmt.Errorf("download: %s", err)
	}
	log.Printf("Downloaded the model to %q\n", f.Name())
	if err := f.Close(); err != nil {
		return err
	}

	ms := &ollama.ModelSpec{
		From: f.Name(),
	}

	if err := m.CreateNewModelOfGGUF(modelID, ms); err != nil {
		return fmt.Errorf("create new model: %s", err)
	}

	return nil
}

// PullBaseModel pulls the base model from the given path.
func (m *Manager) PullBaseModel(modelID string) error {
	return m.runCommand([]string{"pull", modelID})
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
				// Ollama is ready.
				m.mu.Lock()
				defer m.mu.Unlock()
				m.isReady = true
				return nil
			}
		case <-time.After(timeout):
			return fmt.Errorf("timeout waiting for Ollama to be ready")
		}
	}
}

// UpdateModelTemplateToLatest updates the model template to the latest.
func (m *Manager) UpdateModelTemplateToLatest(modelID string) error {
	// TODO(kenji): Update only when there is actual delta. It requires a
	// non-trival amount of work as Ollama makes modification to the original modelfile content
	// (e.g., add comments).
	log.Printf("Recreating model %q with the updated template.\n", modelID)
	ms := &ollama.ModelSpec{
		From: modelID,
	}
	if err := m.CreateNewModelOfGGUF(modelID, ms); err != nil {
		return fmt.Errorf("create new model: %s", err)
	}
	return nil
}

func (m *Manager) runCommand(args []string) error {
	log.Printf("Running Ollama command: %v", args)
	cmd := exec.Command("ollama", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := m.cmdRunner.Run(cmd); err != nil {
		return fmt.Errorf("run %v: %s", args, err)
	}

	return nil
}

// IsReady returns true if the processor is ready. If not,
// it returns a message describing why it is not ready.
func (m *Manager) IsReady() (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isReady {
		return true, ""
	}
	return false, "Ollama is not ready"
}
