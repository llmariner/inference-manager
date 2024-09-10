package vllm

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/llm-operator/inference-manager/engine/internal/modeldownloader"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/runtime"
	mv1 "github.com/llm-operator/model-manager/api/v1"
)

type s3Client interface {
	Download(ctx context.Context, f io.WriterAt, path string) error
}

// New returns a new Manager.
func New(modelDir string, s3Client s3Client) *Manager {
	return &Manager{
		modelDir:        modelDir,
		modelDownloader: modeldownloader.New(modelDir, s3Client),
	}
}

// Manager manages the Ollama service.
//
// TODO(kenji): Refactor this class once we completely switch to the one-odel-per-pod implementation where
// inference-manager-engine doesn't directly run vLLM or Ollama.
type Manager struct {
	modelDir string

	modelDownloader *modeldownloader.D
}

// CreateNewModelOfGGUF creates a new model with the given name and spec that uses a GGUF model file.
func (m *Manager) CreateNewModelOfGGUF(modelName string, spec *ollama.ModelSpec) error {
	return fmt.Errorf("createNewModelOfGGUF is not implemented")
}

// DownloadAndCreateNewModel downloads the model from the given path and creates a new model.
func (m *Manager) DownloadAndCreateNewModel(ctx context.Context, modelName string, resp *mv1.GetBaseModelPathResponse) error {
	format, err := runtime.PreferredModelFormat(runtime.RuntimeNameVLLM, resp.Formats)
	if err != nil {
		return nil
	}

	return m.modelDownloader.Download(ctx, modelName, resp, format)
}

// UpdateModelTemplateToLatest updates the model template to the latest.
func (m *Manager) UpdateModelTemplateToLatest(modelName string) error {
	log.Printf("UpdateModelTemplateToLatest is not implemented\n")
	return nil
}
