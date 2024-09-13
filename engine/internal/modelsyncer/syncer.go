package modelsyncer

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"sync"

	models "github.com/llm-operator/inference-manager/engine/internal/models"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"google.golang.org/grpc"
)

const (
	modelDir = "/.ollama/models/manifests/registry.ollama.ai/library/"
)

// ModelManager is an interface for managing models.
type ModelManager interface {
	CreateNewModelOfGGUF(modelID string, spec *ollama.ModelSpec) error
	DownloadAndCreateNewModel(ctx context.Context, modelID string, resp *mv1.GetBaseModelPathResponse) error
	UpdateModelTemplateToLatest(modelname string) error
}

type s3Client interface {
	Download(ctx context.Context, f io.WriterAt, path string) error
}

type modelClient interface {
	GetModelPath(ctx context.Context, in *mv1.GetModelPathRequest, opts ...grpc.CallOption) (*mv1.GetModelPathResponse, error)
	GetBaseModelPath(ctx context.Context, in *mv1.GetBaseModelPathRequest, opts ...grpc.CallOption) (*mv1.GetBaseModelPathResponse, error)
	GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error)
}

// New creates a syncer..
func New(
	mm ModelManager,
	s3Client s3Client,
	miClient modelClient,
) (*S, error) {
	registered, err := registeredModels(mm, modelDir)
	if err != nil {
		return nil, fmt.Errorf("registered models: %s", err)
	}
	return &S{
		mm:               mm,
		s3Client:         s3Client,
		miClient:         miClient,
		registeredModels: registered,
		inProgressModels: map[string]chan struct{}{},
	}, nil
}

func registeredModels(mm ModelManager, dir string) (map[string]bool, error) {
	registeredModels := map[string]bool{}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return registeredModels, nil
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %s", err)
	}
	log.Printf("Found %d model file(s) under %s", len(files), dir)
	for _, file := range files {
		mname := file.Name()
		// Update the model tempalte to the latest as it might have been updated.
		log.Printf("Registering model %q", mname)
		if err := mm.UpdateModelTemplateToLatest(mname); err != nil {
			// Gracefully handle the error and continue as the file might be corrupted for some reason.
			log.Printf("Failed to update model template for %q: %s. Ignoring", mname, err)
			if s, err := os.Stat(path.Join(dir, mname)); err != nil {
				log.Printf("Failed to get the file info for %q: %s. Ignoring", mname, err)
			} else {
				log.Printf("File info: %+v\n", s)
			}
			continue
		}
		log.Printf("Registered model %q", mname)
		registeredModels[mname] = true
	}
	return registeredModels, nil
}

// S is a syncer.
type S struct {
	mm       ModelManager
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

	isBase, err := models.IsBaseModel(ctx, s.miClient, modelID)
	if err != nil {
		return err
	}
	if isBase {
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

	if err := s.mm.DownloadAndCreateNewModel(ctx, modelID, resp); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.registeredModels[modelID] = true

	log.Printf("Registered the base model successfully\n")

	return nil
}

func (s *S) registerModel(ctx context.Context, modelID string) error {
	log.Printf("Registering model %q\n", modelID)
	baseModel, err := models.ExtractBaseModel(modelID)
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

	if err := s.s3Client.Download(ctx, f, resp.Path); err != nil {
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
	if err := s.mm.CreateNewModelOfGGUF(ollama.ModelName(modelID), ms); err != nil {
		return fmt.Errorf("create new model: %s", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.registeredModels[modelID] = true

	log.Printf("Registered %s successfully\n", modelID)

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
		log.Printf("Waiting for the completion of the in-progress registration of %s\n", modelID)
		<-ch
		log.Printf("The in-progress registration of %s has completed\n", modelID)

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

			log.Printf("Notifying the completion of the registration of %s\n", modelID)
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

// ListInProgressModels lists all models that are in progress of registration.x
func (s *S) ListInProgressModels() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ms []string
	for m := range s.inProgressModels {
		ms = append(ms, m)
	}
	return ms
}
