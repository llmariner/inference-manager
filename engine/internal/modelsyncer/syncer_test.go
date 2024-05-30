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

func TestPullModel(t *testing.T) {
	tcs := []struct {
		name                 string
		modelID              string
		models               []*mv1.Model
		wantCreated          []string
		wantRegisteredModels []string
	}{
		{
			name:    "system model",
			modelID: "google-gemma-2b",
			models: []*mv1.Model{
				{
					Id:      "google-gemma-2b",
					OwnedBy: systemOwner,
				},
			},
			wantCreated: []string{
				"google-gemma-2b",
			},
			wantRegisteredModels: []string{
				"google-gemma-2b",
			},
		},
		{
			name:    "non-system model",
			modelID: "ft:google-gemma-2b:fine-tuning-wpsd9kb5nl",
			models: []*mv1.Model{
				{
					Id:      "ft:google-gemma-2b:fine-tuning-wpsd9kb5nl",
					OwnedBy: "fake-tenant-id",
				},
			},
			wantCreated: []string{
				"google-gemma-2b:fine-tuning-wpsd9kb5nl",
			},
			wantRegisteredModels: []string{
				"ft:google-gemma-2b:fine-tuning-wpsd9kb5nl",
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			fom := &fakeOllamaManager{}
			om := New(
				fom,
				&noopS3Client{},
				&fakeModelInternalClient{models: tc.models},
			)
			err := om.PullModel(context.Background(), tc.modelID)
			assert.NoError(t, err)

			assert.ElementsMatch(t, tc.wantCreated, fom.created)

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
			want:    "google-gemma-2b",
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

type fakeOllamaManager struct {
	created []string
}

func (n *fakeOllamaManager) CreateNewModel(modelName string, spec *ollama.ModelSpec) error {
	n.created = append(n.created, modelName)
	return nil
}

func (n *fakeOllamaManager) PullBaseModel(modelName string) error {
	return nil
}

func (n *fakeOllamaManager) DeleteModel(ctx context.Context, modelName string) error {
	return nil
}

type noopS3Client struct {
}

func (n *noopS3Client) Download(f io.WriterAt, path string) error {
	return nil
}

type fakeModelInternalClient struct {
	models []*mv1.Model
}

func (n *fakeModelInternalClient) ListModels(ctx context.Context, in *mv1.ListModelsRequest, opts ...grpc.CallOption) (*mv1.ListModelsResponse, error) {
	return &mv1.ListModelsResponse{
		Data: n.models,
	}, nil
}

func (n *fakeModelInternalClient) GetBaseModelPath(ctx context.Context, in *mv1.GetBaseModelPathRequest, opts ...grpc.CallOption) (*mv1.GetBaseModelPathResponse, error) {
	return &mv1.GetBaseModelPathResponse{
		Path:          "fake-path",
		GgufModelPath: "fake-gguf-path",
	}, nil
}

func (n *fakeModelInternalClient) GetModelPath(ctx context.Context, in *mv1.GetModelPathRequest, opts ...grpc.CallOption) (*mv1.GetModelPathResponse, error) {
	return &mv1.GetModelPathResponse{
		Path: "fake-path",
	}, nil
}
