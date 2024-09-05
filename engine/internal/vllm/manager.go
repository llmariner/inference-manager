package vllm

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/llm-operator/inference-manager/engine/internal/huggingface"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	mv1 "github.com/llm-operator/model-manager/api/v1"
)

type s3Client interface {
	Download(ctx context.Context, f io.WriterAt, path string) error
}

// New returns a new Manager.
func New(modelDir string, s3Client s3Client) *Manager {
	return &Manager{
		modelDir: modelDir,
		s3Client: s3Client,
	}
}

// Manager manages the Ollama service.
//
// TODO(kenji): Refactor this class once we completely switch to the one-odel-per-pod implementation where
// inference-manager-engine doesn't directly run vLLM or Ollama.
type Manager struct {
	modelDir string

	s3Client s3Client
}

// CreateNewModelOfGGUF creates a new model with the given name and spec that uses a GGUF model file.
func (m *Manager) CreateNewModelOfGGUF(modelName string, spec *ollama.ModelSpec) error {
	return fmt.Errorf("createNewModelOfGGUF is not implemented")
}

// DownloadAndCreateNewModel downloads the model from the given path and creates a new model.
func (m *Manager) DownloadAndCreateNewModel(ctx context.Context, modelName string, resp *mv1.GetBaseModelPathResponse) error {
	format, err := PreferredModelFormat(resp)
	if err != nil {
		return nil
	}

	destPath, err := ModelFilePath(m.modelDir, modelName, format)
	if err != nil {
		return err
	}

	// Check if the completion indication file exists. If so, download should have been completed with a previous run. Do not download again.
	completionIndicationFile := filepath.Join(m.modelDir, "completed.txt")

	if _, err := os.Stat(completionIndicationFile); err == nil {
		log.Printf("The model has already been downloaded. Skipping the download.\n")
		return nil
	}

	switch format {
	case mv1.ModelFormat_MODEL_FORMAT_GGUF:
		log.Printf("Downloading the GGUF model from %q\n", resp.GgufModelPath)
		f, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if err := m.s3Client.Download(ctx, f, resp.GgufModelPath); err != nil {
			return fmt.Errorf("download: %s", err)
		}
		log.Printf("Downloaded the model to %q\n", f.Name())
		if err := f.Close(); err != nil {
			return err
		}
	case mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE:
		log.Printf("Downloading the Hugging Face model from %q\n", resp.Path)
		if err := os.MkdirAll(destPath, 0755); err != nil {
			return fmt.Errorf("create directory: %s", err)
		}
		if err := huggingface.DownloadModelFiles(ctx, m.s3Client, resp.Path, destPath); err != nil {
			return fmt.Errorf("download: %s", err)
		}
		log.Printf("Downloaded the model to %q\n", destPath)
	default:
		return fmt.Errorf("unsupported model format: %s", format)
	}

	// Create a file that indicates the completion of model download.
	f, err := os.Create(completionIndicationFile)
	if err != nil {
		return nil
	}
	if err := f.Close(); err != nil {
		return err
	}

	return nil
}

// UpdateModelTemplateToLatest updates the model template to the latest.
func (m *Manager) UpdateModelTemplateToLatest(modelName string) error {
	log.Printf("UpdateModelTemplateToLatest is not implemented\n")
	return nil
}
