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
	"github.com/llm-operator/inference-manager/engine/internal/manager"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"google.golang.org/grpc"
)

const (
	systemOwner = "system"
)

type modelManager interface {
	CreateNewModel(modelName string, spec *manager.ModelSpec) error
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
	mm modelManager,
	s3Client s3Client,
	miClient modelClient,
) *S {
	return &S{
		mm:               mm,
		s3Client:         s3Client,
		miClient:         miClient,
		registeredModels: map[string]bool{},
		inProgressModels: map[string]chan struct{}{},
	}
}

// S is a syncer.
type S struct {
	mm       modelManager
	s3Client s3Client

	miClient modelClient

	registeredModels map[string]bool

	// inProgressModels is a map from model ID to a channel that is closed when the model is registered.
	inProgressModels map[string]chan struct{}

	// mu protects registeredModels and inProgressModels.
	mu sync.Mutex
}

// PullModel downloads and registers a model from model manager.
func (s *S) PullModel(ctx context.Context, modelID string) error {
	s.mu.Lock()
	registered := s.registeredModels[modelID]
	s.mu.Unlock()
	if registered {
		return nil
	}

	// TODO(kenji): Currently we call this RPC to check if the model is a base model or not.
	// Consider changing Model Manager gRPC interface to simplify the interaction (e.g.,
	// add an RPC method that returns a model path for both base model and fine-tuning model).
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

	result, err := s.checkInProgressRegistration(modelID)
	if err != nil {
		return err
	}
	if result.registered {
		return nil
	}
	defer result.notifyCompletion()

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

	ms := &manager.ModelSpec{
		From: f.Name(),
	}

	if err := s.mm.CreateNewModel(modelID, ms); err != nil {
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
	if err := s.registerBaseModel(ctx, baseModel); err != nil {
		return err
	}

	result, err := s.checkInProgressRegistration(modelID)
	if err != nil {
		return err
	}
	if result.registered {
		return nil
	}
	defer result.notifyCompletion()

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

	ms := &manager.ModelSpec{
		From:        baseModel,
		AdapterPath: f.Name(),
	}
	if err := s.mm.CreateNewModel(models.OllamaModelName(modelID), ms); err != nil {
		return fmt.Errorf("create new model: %s", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.registeredModels[modelID] = true

	log.Printf("Registered the model successfully\n")

	// write to the notification channel.

	return nil
}

type checkInProgressRegistrationResult struct {
	registered       bool
	notifyCompletion func()
}

// checkInProgressRegistration checks if the registration is in-progress or completed.
// If the registration has completed, do nothing.
// If the registration is in-progress, wait for its completion.
// If the registration has not started, create a channel that is used to notify the completion.
//
// Note that the channel needs to be created in the same critical section to avoid the race condition.
func (s *S) checkInProgressRegistration(modelID string) (*checkInProgressRegistrationResult, error) {
	s.mu.Lock()
	if s.registeredModels[modelID] {
		s.mu.Unlock()
		return &checkInProgressRegistrationResult{
			registered: true,
		}, nil
	}

	if ch, ok := s.inProgressModels[modelID]; ok {
		s.mu.Unlock()
		log.Printf("Waiting for the completion of the in-progress registration\n")
		<-ch
		log.Printf("The in-progress registration has completed\n")

		s.mu.Lock()
		registered := s.registeredModels[modelID]
		s.mu.Unlock()
		if !registered {
			return nil, fmt.Errorf("register the model: %q", modelID)
		}
		return &checkInProgressRegistrationResult{
			registered: true,
		}, nil
	}

	defer s.mu.Unlock()

	ch := make(chan struct{})
	s.inProgressModels[modelID] = ch
	return &checkInProgressRegistrationResult{
		registered: false,
		notifyCompletion: func() {
			s.mu.Lock()
			delete(s.inProgressModels, modelID)
			s.mu.Unlock()

			log.Printf("Notifying the completion of the registration\n")
			close(ch)
		},
	}, nil
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

	if err := s.mm.DeleteModel(ctx, models.OllamaModelName(modelID)); err != nil {
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
