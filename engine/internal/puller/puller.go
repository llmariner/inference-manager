package puller

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
	"google.golang.org/grpc"
)

type s3Client interface {
	Download(ctx context.Context, f io.WriterAt, path string) error
	ListObjectsPages(ctx context.Context, prefix string, f func(page *s3.ListObjectsV2Output, lastPage bool) bool) error
}

type modelClient interface {
	GetBaseModelPath(ctx context.Context, in *mv1.GetBaseModelPathRequest, opts ...grpc.CallOption) (*mv1.GetBaseModelPathResponse, error)
	GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error)
	GetModelAttributes(ctx context.Context, in *mv1.GetModelAttributesRequest, opts ...grpc.CallOption) (*mv1.ModelAttributes, error)
}

// New creates a new puller.
func New(
	mconfig *config.ProcessedModelConfig,
	runtimeName string,
	mClient modelClient,
	s3Client s3Client,
) *P {
	return &P{
		mconfig:     mconfig,
		runtimeName: runtimeName,
		mClient:     mClient,
		s3Client:    s3Client,
	}
}

// P is a puller.
type P struct {
	mconfig     *config.ProcessedModelConfig
	runtimeName string
	mClient     modelClient
	s3Client    s3Client
}

// Pull pulls the model from the Model Manager Server.
func (p *P) Pull(ctx context.Context, modelID string) error {
	ctx = auth.AppendWorkerAuthorization(ctx)

	model, err := p.mClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: modelID,
	})
	if err != nil {
		return err
	}

	if model.IsBaseModel {
		return p.pullBaseModel(ctx, modelID)
	}

	if err := p.pullFineTunedModel(ctx, modelID); err != nil {
		return err
	}
	return nil
}

func (p *P) pullBaseModel(ctx context.Context, modelID string) error {
	resp, err := p.mClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: modelID,
	})
	if err != nil {
		return err
	}

	d := modeldownloader.New(ModelDir(), p.s3Client)

	format, err := PreferredModelFormat(p.runtimeName, resp.Formats)
	if err != nil {
		return err
	}

	var srcPath string
	switch format {
	case mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE,
		mv1.ModelFormat_MODEL_FORMAT_OLLAMA,
		mv1.ModelFormat_MODEL_FORMAT_NVIDIA_TRITON:
		srcPath = resp.Path
	case mv1.ModelFormat_MODEL_FORMAT_GGUF:
		srcPath = resp.GgufModelPath
	default:
		return fmt.Errorf("unsupported format: %v", format)
	}

	if err := d.Download(ctx, modelID, srcPath, format, mv1.AdapterType_ADAPTER_TYPE_UNSPECIFIED); err != nil {
		return err
	}

	log.Printf("Successfully pulled the model %q\n", modelID)

	if p.runtimeName != config.RuntimeNameOllama || format == mv1.ModelFormat_MODEL_FORMAT_OLLAMA {
		// No need to create a model file.
		return nil
	}

	// Create a modelfile for Ollama.

	filePath := ollama.ModelfilePath(ModelDir(), modelID)
	log.Printf("Creating an Ollama modelfile at %q\n", filePath)
	modelPath, err := modeldownloader.ModelFilePath(ModelDir(), modelID, format)
	if err != nil {
		return err
	}
	spec := &ollama.ModelSpec{
		From: modelPath,
	}

	mci := p.mconfig.ModelConfigItem(modelID)
	if err := ollama.CreateModelfile(filePath, modelID, spec, mci.ContextLength); err != nil {
		return err
	}
	log.Printf("Successfully created the Ollama modelfile\n")

	return nil
}

func (p *P) pullFineTunedModel(ctx context.Context, modelID string) error {
	attr, err := p.mClient.GetModelAttributes(ctx, &mv1.GetModelAttributesRequest{
		Id: modelID,
	})
	if err != nil {
		return err
	}

	if attr.BaseModel == "" {
		return fmt.Errorf("base model ID is not set for %q", modelID)
	}
	if err := p.pullBaseModel(ctx, attr.BaseModel); err != nil {
		return err
	}

	resp, err := p.mClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: attr.BaseModel,
	})
	if err != nil {
		return err
	}

	format, err := PreferredModelFormat(p.runtimeName, resp.Formats)
	if err != nil {
		return err
	}

	d := modeldownloader.New(ModelDir(), p.s3Client)

	if err := d.Download(ctx, modelID, attr.Path, format, attr.Adapter); err != nil {
		return err
	}

	log.Printf("Successfully pulled the fine-tuning adapter\n")

	if p.runtimeName != config.RuntimeNameOllama {
		return nil
	}

	filePath := ollama.ModelfilePath(ModelDir(), modelID)
	log.Printf("Creating an Ollama modelfile at %q\n", filePath)

	adapterPath, err := modeldownloader.ModelFilePath(ModelDir(), modelID, format)
	if err != nil {
		return err
	}
	spec := &ollama.ModelSpec{
		From:        attr.BaseModel,
		AdapterPath: adapterPath,
	}
	mci := p.mconfig.ModelConfigItem(modelID)
	if err := ollama.CreateModelfile(filePath, modelID, spec, mci.ContextLength); err != nil {
		return err
	}
	log.Printf("Successfully created the Ollama modelfile\n")

	return nil
}
