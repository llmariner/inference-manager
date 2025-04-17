package puller

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

func TestPull_BaseModel(t *testing.T) {
	const (
		modelID           = "google-gemma-2b"
		fakeModelFileName = "fake_model.gguf"
		runtime           = config.RuntimeNameOllama
	)

	modelDir, err := os.MkdirTemp("", "model")
	assert.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(modelDir)
	}()

	mconfig := config.NewProcessedModelConfig(&config.Config{})
	fakeModelClient := &fakeModelClient{
		model: mv1.Model{
			Id:          modelID,
			IsBaseModel: true,
		},
	}
	fakeModelDownloader := &fakeModelDownloader{
		modelDir:     modelDir,
		fakeFileName: fakeModelFileName,
	}

	p := New(
		mconfig,
		runtime,
		fakeModelClient,
		fakeModelDownloader,
	)

	err = p.Pull(context.Background(), modelID)
	assert.NoError(t, err)

	// Ckeck if the Ollama model file is created.
	ollamaModelFilePath := ollama.ModelfilePath(modelDir, modelID)
	_, err = os.Stat(ollamaModelFilePath)
	assert.NoError(t, err)

	// Check if the model file is created.
	modelFilePath := filepath.Join(modelDir, modelID, fakeModelFileName)
	_, err = os.Stat(modelFilePath)
	assert.NoError(t, err)
}

func TestPull_FineTunedModel(t *testing.T) {
	const (
		modelID           = "ft:google-gemma-2b:test"
		baseModelID       = "google-gemma-2b"
		fakeModelFileName = "fake_model.gguf"
		runtime           = config.RuntimeNameOllama
	)

	modelDir, err := os.MkdirTemp("", "model")
	assert.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(modelDir)
	}()

	mconfig := config.NewProcessedModelConfig(&config.Config{})
	fakeModelClient := &fakeModelClient{
		model: mv1.Model{
			Id:          modelID,
			IsBaseModel: false,
			BaseModelId: baseModelID,
		},
		attrs: mv1.ModelAttributes{
			BaseModel: baseModelID,
			Path:      "path/to/model",
			Adapter:   mv1.AdapterType_ADAPTER_TYPE_LORA,
		},
	}
	fakeModelDownloader := &fakeModelDownloader{
		modelDir:     modelDir,
		fakeFileName: fakeModelFileName,
	}

	p := New(
		mconfig,
		runtime,
		fakeModelClient,
		fakeModelDownloader,
	)

	err = p.Pull(context.Background(), modelID)
	assert.NoError(t, err)

	// Ckeck if the Ollama model file is created.
	ollamaModelFilePath := ollama.ModelfilePath(modelDir, modelID)
	_, err = os.Stat(ollamaModelFilePath)
	assert.NoError(t, err)

	// Check if the model file is created for both the base model and the fine-tuned model.
	modelFilePath := filepath.Join(modelDir, modelID, fakeModelFileName)
	_, err = os.Stat(modelFilePath)
	assert.NoError(t, err)

	modelFilePath = filepath.Join(modelDir, baseModelID, fakeModelFileName)
	_, err = os.Stat(modelFilePath)
	assert.NoError(t, err)
}

type fakeModelClient struct {
	model mv1.Model
	attrs mv1.ModelAttributes
}

func (c *fakeModelClient) GetBaseModelPath(ctx context.Context, in *mv1.GetBaseModelPathRequest, opts ...grpc.CallOption) (*mv1.GetBaseModelPathResponse, error) {
	return &mv1.GetBaseModelPathResponse{}, nil
}

func (c *fakeModelClient) GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error) {
	return &c.model, nil
}

func (c *fakeModelClient) GetModelAttributes(ctx context.Context, in *mv1.GetModelAttributesRequest, opts ...grpc.CallOption) (*mv1.ModelAttributes, error) {
	return &c.attrs, nil
}

type fakeModelDownloader struct {
	modelDir     string
	fakeFileName string
}

func (d *fakeModelDownloader) ModelDir() string {
	return d.modelDir
}

func (d *fakeModelDownloader) ModelFilePath(modelID string, format mv1.ModelFormat) (string, error) {
	return filepath.Join(d.modelDir, modelID), nil
}

func (d *fakeModelDownloader) Download(ctx context.Context, modelID string, srcPath string, format mv1.ModelFormat, adapterType mv1.AdapterType) error {
	err := os.Mkdir(filepath.Join(d.modelDir, modelID), 0755)
	if err != nil {
		return err
	}

	// Create a fake file.
	f, err := os.Create(filepath.Join(d.modelDir, modelID, d.fakeFileName))
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()
	if _, err := f.WriteString("fake model data"); err != nil {
		return err
	}

	return nil
}
