package vllm

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/huggingface"
	"github.com/llm-operator/inference-manager/engine/internal/manager"
	mv1 "github.com/llm-operator/model-manager/api/v1"
)

type s3Client interface {
	Download(f io.WriterAt, path string) error
}

// New returns a new Manager.
func New(c *config.Config, modelDir string, s3Client s3Client) *Manager {
	return &Manager{
		host:    "0.0.0.0",
		port:    c.LLMPort,
		model:   c.VLLM.Model,
		numGPUs: c.VLLM.NumGPUs,

		modelDir: modelDir,

		s3Client: s3Client,
	}
}

// Manager manages the Ollama service.
//
// TODO(kenji): Refactor this class once we completely switch to the one-odel-per-pod implementation where
// inference-manager-engine doesn't directly run vLLM or Ollama.
type Manager struct {
	host    string
	port    int
	model   string
	numGPUs int

	modelDir string

	s3Client s3Client
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

// CreateNewModelOfGGUF creates a new model with the given name and spec that uses a GGUF model file.
func (m *Manager) CreateNewModelOfGGUF(modelName string, spec *manager.ModelSpec) error {
	return fmt.Errorf("createNewModelOfGGUF is not implemented")
}

// DownloadAndCreateNewModel downloads the model from the given path and creates a new model.
func (m *Manager) DownloadAndCreateNewModel(modelName string, resp *mv1.GetBaseModelPathResponse) error {
	switch resp.Format {
	case mv1.ModelFormat_MODEL_FORMAT_GGUF:
		log.Printf("Downloading the GGUF model from %q\n", resp.GgufModelPath)
		destPath := ModelFilePath(m.modelDir, modelName)
		f, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if err := m.s3Client.Download(f, resp.GgufModelPath); err != nil {
			return fmt.Errorf("download: %s", err)
		}
		log.Printf("Downloaded the model to %q\n", f.Name())
		if err := f.Close(); err != nil {
			return err
		}
	case mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE:
		log.Printf("Downloading the Hugging Face model from %q\n", resp.Path)
		destPath := ModelFilePath(m.modelDir, modelName)
		if err := os.MkdirAll(destPath, 0755); err != nil {
			return fmt.Errorf("create directory: %s", err)
		}
		if err := huggingface.DownloadModelFiles(m.s3Client, resp.Path, destPath); err != nil {
			return fmt.Errorf("download: %s", err)
		}
		log.Printf("Downloaded the model to %q\n", destPath)
	default:
		return fmt.Errorf("unsupported model format: %s", resp.Format)
	}

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
