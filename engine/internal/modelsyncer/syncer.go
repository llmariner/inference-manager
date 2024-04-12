package modelsyncer

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/s3"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	v1 "github.com/llm-operator/model-manager/api/v1"
)

const (
	fakeTenantID = "fake-tenant-id"
)

// New creates a syncer..
func New(
	om *ollama.Manager,
	s3Client *s3.Client,
	mClient mv1.ModelsServiceClient,
	miClient mv1.ModelsInternalServiceClient,
) *S {
	return &S{
		om:               om,
		s3Client:         s3Client,
		mClient:          mClient,
		miClient:         miClient,
		registeredModels: map[string]bool{},
	}
}

// S is a syncer.
type S struct {
	om       *ollama.Manager
	s3Client *s3.Client

	mClient  mv1.ModelsServiceClient
	miClient mv1.ModelsInternalServiceClient

	registeredModels map[string]bool
}

// Run starts the syncer.
func (s *S) Run(ctx context.Context) error {
	// TODO(kenji): Make this configurable.
	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.syncModels(ctx); err != nil {
				return err
			}
		}
	}
}

func (s *S) syncModels(ctx context.Context) error {
	// list all models for the fake tenant.
	resp, err := s.mClient.ListModels(ctx, &v1.ListModelsRequest{})
	if err != nil {
		return err
	}

	for _, model := range resp.Data {
		if s.registeredModels[model.Id] {
			continue
		}
		if err := s.registerModel(ctx, model.Id); err != nil {
			return err
		}
	}
	return nil
}

func (s *S) registerModel(ctx context.Context, modelID string) error {
	log.Printf("Registering model %q\n", modelID)
	baseModel, err := extractBaseModel(modelID)
	if err != nil {
		return err
	}

	resp, err := s.miClient.GetModelPath(ctx, &mv1.GetModelPathRequest{
		Id:       modelID,
		TenantId: fakeTenantID,
	})
	if err != nil {
		return err
	}

	log.Printf("Downloading the model from %q\n", resp.Path)
	f, err := os.CreateTemp("/tmp", "model")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())

	if err := s.s3Client.Download(f, resp.Path); err != nil {
		return fmt.Errorf("download: %s", err)
	}
	log.Printf("Downloaded the model to %q\n", f.Name())
	if err := f.Close(); err != nil {
		return err
	}

	ms := &ollama.ModelSpec{
		BaseModel:   baseModel,
		AdapterPath: f.Name(),
	}
	// Ollama does not allow extra ':'s in the tag.

	if err := s.om.CreateNewModel(ollamaModelName(modelID), ms); err != nil {
		return fmt.Errorf("create new model: %s", err)
	}
	log.Printf("Registered the model successfully\n")

	return nil
}

func extractBaseModel(modelID string) (string, error) {
	l := strings.Split(modelID, ":")
	if len(l) != 5 {
		return "", fmt.Errorf("invalid model ID: %q", modelID)
	}
	return strings.Join(l[1:len(l)-2], ":"), nil
}

func ollamaModelName(modelID string) string {
	// Remove the "ft:" suffix. Also replace ":" with "-" as Ollama does not allow ":" in the tag.
	l := strings.Split(modelID, ":")
	return fmt.Sprintf("%s:%s", l[1], strings.Join(l[2:], "-"))
}
