package modelsyncer

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/llm-operator/inference-manager/common/pkg/models"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"google.golang.org/grpc"
)

const (
	systemOwner = "system"
)

type ollamaManager interface {
	CreateNewModel(modelName string, spec *ollama.ModelSpec) error
	PullBaseModel(modelName string) error
	DeleteModel(ctx context.Context, modelName string) error
}

type s3Client interface {
	Download(f io.WriterAt, path string) error
}

type modelClient interface {
	GetModelPath(ctx context.Context, in *mv1.GetModelPathRequest, opts ...grpc.CallOption) (*mv1.GetModelPathResponse, error)
	GetBaseModelPath(ctx context.Context, in *mv1.GetBaseModelPathRequest, opts ...grpc.CallOption) (*mv1.GetBaseModelPathResponse, error)
	GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error)
}

// New creates a syncer..
func New(
	om ollamaManager,
	s3Client s3Client,
	miClient modelClient,
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

	miClient modelClient

	registeredModels map[string]bool
	// mu protects registeredModels.
	mu sync.Mutex
}

// PullModel downloads and registers a model from model manager.
func (s *S) PullModel(ctx context.Context, modelID string) error {
	s.mu.Lock()
	registerd := s.registeredModels[modelID]
	s.mu.Unlock()
	if registerd {
		return nil
	}

	// TODO(kenji): Currently we call this RPC to check if the model is a base model or not.
	// Consider changing Model Manager gRPC interface to simplify the interaction (e.g.,
	// add an RPC method that returns a model path for both base model and fine-tuning model).
	ctx = auth.AppendWorkerAuthorization(ctx)
	model, err := s.miClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: modelID,
	})
	if err != nil {
		return err
	}
	if model.OwnedBy == systemOwner {
		return s.registerBaseModel(ctx, modelID)
	}

	return s.registerModel(ctx, modelID)
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

	s.mu.Lock()
	defer s.mu.Unlock()
	s.registeredModels[modelID] = true

	log.Printf("Registered the base model successfully\n")

	return nil
}

func (s *S) registerModel(ctx context.Context, modelID string) error {
	log.Printf("Registering model %q\n", modelID)
	baseModel, err := extractBaseModel(modelID)
	if err != nil {
		return err
	}

	// Registesr the base model if it has not yet.
	s.mu.Lock()
	registerd := s.registeredModels[baseModel]
	s.mu.Unlock()
	if !registerd {
		if err := s.registerBaseModel(ctx, baseModel); err != nil {
			return err
		}
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

	s.mu.Lock()
	defer s.mu.Unlock()
	s.registeredModels[modelID] = true

	log.Printf("Registered the model successfully\n")

	return nil
}

// ListSyncedModelIDs lists all models that have been synced.
func (s *S) ListSyncedModelIDs(ctx context.Context) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ms []string
	for m := range s.registeredModels {
		ms = append(ms, m)
	}
	return ms
}

// DeleteModel deletes a model.
func (s *S) DeleteModel(ctx context.Context, modelID string) error {
	s.mu.Lock()
	ok := s.registeredModels[modelID]
	s.mu.Unlock()
	if !ok {
		// Do nothing.
		return nil
	}

	if err := s.om.DeleteModel(ctx, models.OllamaModelName(modelID)); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.registeredModels, modelID)
	return nil
}

func extractBaseModel(modelID string) (string, error) {
	l := strings.Split(modelID, ":")
	if len(l) <= 2 {
		return "", fmt.Errorf("invalid model ID: %q", modelID)
	}
	return strings.Join(l[1:len(l)-1], ":"), nil
}
