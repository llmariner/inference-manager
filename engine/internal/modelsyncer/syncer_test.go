package modelsyncer

import (
	"context"
	"io"
	"testing"

	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

func TestSyncModels(t *testing.T) {
	tcs := []struct {
		name                 string
		models               []*mv1.Model
		wantRegisteredModels []string
	}{
		{
			name:                 "no models",
			models:               []*mv1.Model{},
			wantRegisteredModels: nil,
		},
		{
			name: "system model",
			models: []*mv1.Model{
				{
					Id:      "google/gemma-2b",
					OwnedBy: systemOwner,
				},
			},
			wantRegisteredModels: []string{
				"google/gemma-2b",
			},
		},
		{
			name: "non-system model",
			models: []*mv1.Model{
				{
					Id:      "ft:google-gemma-2b:fine-tuning-wpsd9kb5nl",
					OwnedBy: fakeTenantID,
				},
			},
			wantRegisteredModels: []string{
				"ft:google-gemma-2b:fine-tuning-wpsd9kb5nl",
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			om := New(
				&noopOllamaManager{},
				&noopS3Client{},
				&fakeModelClient{models: tc.models},
				&fakeModelInternalClient{},
			)
			err := om.syncModels(context.Background())
			assert.NoError(t, err)

			var registered []string
			for k := range om.registeredModels {
				registered = append(registered, k)
			}
			assert.ElementsMatch(t, tc.wantRegisteredModels, registered)
		})
	}
}

func TestExtractBaseModel(t *testing.T) {
	tcs := []struct {
		modelID string
		want    string
		wantErr bool
	}{
		{
			modelID: "ft:google-gemma-2b:fine-tuning-wpsd9kb5nl",
			want:    "gemma:2b",
		},
		{
			modelID: "bogus",
			wantErr: true,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.modelID, func(t *testing.T) {
			got, err := extractBaseModel(tc.modelID)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOllamaModelName(t *testing.T) {
	tcs := []struct {
		modelID string
		want    string
		wantErr bool
	}{
		{
			modelID: "ft:gemma:2b-custom-model-name-7p4lURel",
			want:    "gemma:2b-custom-model-name-7p4lURel",
		},
		{
			modelID: "bogus",
			wantErr: true,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.modelID, func(t *testing.T) {
			got, err := ollamaModelName(tc.modelID)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

type noopOllamaManager struct {
}

func (n *noopOllamaManager) CreateNewModel(modelName string, spec *ollama.ModelSpec) error {
	return nil
}

func (n *noopOllamaManager) PullBaseModel(modelName string) error {
	return nil
}

type noopS3Client struct {
}

func (n *noopS3Client) Download(f io.WriterAt, path string) error {
	return nil
}

type fakeModelClient struct {
	models []*mv1.Model
}

func (n *fakeModelClient) ListModels(ctx context.Context, in *mv1.ListModelsRequest, opts ...grpc.CallOption) (*mv1.ListModelsResponse, error) {
	return &mv1.ListModelsResponse{
		Data: n.models,
	}, nil
}

type fakeModelInternalClient struct {
}

func (n *fakeModelInternalClient) GetModelPath(ctx context.Context, in *mv1.GetModelPathRequest, opts ...grpc.CallOption) (*mv1.GetModelPathResponse, error) {
	return &mv1.GetModelPathResponse{
		Path: "fake-path",
	}, nil
}
