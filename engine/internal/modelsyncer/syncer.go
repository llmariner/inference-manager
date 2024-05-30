package modelsyncer

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/llm-operator/inference-manager/common/pkg/models"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"google.golang.org/grpc"
)

const (
	fakeTenantID = "fake-tenant-id"

	systemOwner = "system"
)

type ollamaManager interface {
	CreateNewModel(modelName string, spec *ollama.ModelSpec) error
	PullBaseModel(modelName string) error
}

type s3Client interface {
	Download(f io.WriterAt, path string) error
}

type modelInternalClient interface {
	GetModelPath(ctx context.Context, in *mv1.GetModelPathRequest, opts ...grpc.CallOption) (*mv1.GetModelPathResponse, error)
	GetBaseModelPath(ctx context.Context, in *mv1.GetBaseModelPathRequest, opts ...grpc.CallOption) (*mv1.GetBaseModelPathResponse, error)
	ListModels(ctx context.Context, in *mv1.ListModelsRequest, opts ...grpc.CallOption) (*mv1.ListModelsResponse, error)
}

// New creates a syncer..
func New(
	om ollamaManager,
	s3Client s3Client,
	miClient modelInternalClient,
) *S {
	return &S{
		om:               om,
		s3Client:         s3Client,
		miClient:         miClient,
		registeredModels: map[string]bool{},
	}
}

// S is a syncer.
type S struct {
	om       ollamaManager
	s3Client s3Client

	miClient modelInternalClient

	registeredModels map[string]bool
}

// PullModel downloads and registers a model from model manager.
func (s *S) PullModel(ctx context.Context, modelID string) error {
	// list all models for the fake tenant.
	resp, err := s.miClient.ListModels(ctx, &mv1.ListModelsRequest{})
	if err != nil {
		return err
	}

	found := false
	for _, model := range resp.Data {
		if modelID != "" && modelID != model.Id {
			continue
		}
		found = true

		if s.registeredModels[model.Id] {
			break
		}

		if model.OwnedBy == systemOwner {
			if err := s.registerBaseModel(ctx, model.Id); err != nil {
				return err
			}
		} else {
			if err := s.registerModel(ctx, model.Id); err != nil {
				return err
			}
		}

		s.registeredModels[model.Id] = true
		break
	}

	if !found {
		return fmt.Errorf("model %q not found", modelID)
	}
	return nil
}

func (s *S) registerBaseModel(ctx context.Context, modelID string) error {
	log.Printf("Registering base model %q\n", modelID)

	resp, err := s.miClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: modelID,
	})
	if err != nil {
		return err
	}

	log.Printf("Downloading the model from %q\n", resp.GgufModelPath)
	f, err := os.CreateTemp("/tmp", "model")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(f.Name()); err != nil {
			log.Printf("Failed to remove %q: %s", f.Name(), err)
		}
	}()

	if err := s.s3Client.Download(f, resp.GgufModelPath); err != nil {
		return fmt.Errorf("download: %s", err)
	}
	log.Printf("Downloaded the model to %q\n", f.Name())
	if err := f.Close(); err != nil {
		return err
	}

	ms := &ollama.ModelSpec{
		From: f.Name(),
	}

	if err := s.om.CreateNewModel(modelID, ms); err != nil {
		return fmt.Errorf("create new model: %s", err)
	}
	log.Printf("Registered the base model successfully\n")

	return nil
}

func (s *S) registerModel(ctx context.Context, modelID string) error {
	log.Printf("Registering model %q\n", modelID)
	baseModel, err := extractBaseModel(modelID)
	if err != nil {
		return err
	}

	resp, err := s.miClient.GetModelPath(ctx, &mv1.GetModelPathRequest{
		Id: modelID,
	})
	if err != nil {
		return err
	}

	log.Printf("Downloading the model from %q\n", resp.Path)
	f, err := os.CreateTemp("/tmp", "model")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(f.Name()); err != nil {
			log.Printf("Failed to remove %q: %s", f.Name(), err)
		}
	}()

	if err := s.s3Client.Download(f, resp.Path); err != nil {
		return fmt.Errorf("download: %s", err)
	}
	log.Printf("Downloaded the model to %q\n", f.Name())
	if err := f.Close(); err != nil {
		return err
	}

	ms := &ollama.ModelSpec{
		From:        baseModel,
		AdapterPath: f.Name(),
	}
	if err := s.om.CreateNewModel(models.OllamaModelName(modelID), ms); err != nil {
		return fmt.Errorf("create new model: %s", err)
	}
	log.Printf("Registered the model successfully\n")

	return nil
}

func extractBaseModel(modelID string) (string, error) {
	l := strings.Split(modelID, ":")
	if len(l) <= 2 {
		return "", fmt.Errorf("invalid model ID: %q", modelID)
	}
	return strings.Join(l[1:len(l)-1], ":"), nil
}
