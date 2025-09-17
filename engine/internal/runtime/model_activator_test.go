package runtime

import (
	"context"
	"sync"
	"testing"

	iv1 "github.com/llmariner/inference-manager/api/v1"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

func TestReconcileModelActivation(t *testing.T) {
	mmanager := &fakeModelManager{}

	fakeModelLister := &fakeModelLister{
		resp: &mv1.ListModelsResponse{
			Data: []*mv1.Model{
				{
					Id:               "model1",
					ActivationStatus: mv1.ActivationStatus_ACTIVATION_STATUS_ACTIVE,
				},
				{
					Id:               "model2",
					ActivationStatus: mv1.ActivationStatus_ACTIVATION_STATUS_INACTIVE,
				},
				{
					Id:               "model3",
					ActivationStatus: mv1.ActivationStatus_ACTIVATION_STATUS_INACTIVE,
				},
				{
					Id:               "model4",
					ActivationStatus: mv1.ActivationStatus_ACTIVATION_STATUS_UNSPECIFIED,
				},
			},
		},
	}

	a := NewModelActivator([]string{"model3"}, mmanager, fakeModelLister, false)
	err := a.reconcileModelActivation(context.Background())
	assert.NoError(t, err)

	assert.ElementsMatch(t, []string{"model1", "model3"}, mmanager.pulled)
	assert.ElementsMatch(t, []string{"model2"}, mmanager.deleted)
}

func TestReconcileModelActivation_DynamicLoRALoading(t *testing.T) {
	mmanager := &fakeModelManager{}

	fakeModelLister := &fakeModelLister{
		resp: &mv1.ListModelsResponse{
			Data: []*mv1.Model{
				{
					Id:               "bm0",
					ActivationStatus: mv1.ActivationStatus_ACTIVATION_STATUS_INACTIVE,
					IsBaseModel:      true,
				},
				{
					Id:               "bm1",
					ActivationStatus: mv1.ActivationStatus_ACTIVATION_STATUS_INACTIVE,
					IsBaseModel:      true,
				},
				{
					Id:               "fm0",
					ActivationStatus: mv1.ActivationStatus_ACTIVATION_STATUS_ACTIVE,
					IsBaseModel:      false,
					BaseModelId:      "bm0",
				},
			},
		},
	}

	a := NewModelActivator(nil, mmanager, fakeModelLister, true)
	err := a.reconcileModelActivation(context.Background())
	assert.NoError(t, err)

	assert.ElementsMatch(t, []string{"fm0"}, mmanager.pulled)
	assert.ElementsMatch(t, []string{"bm1"}, mmanager.deleted)
}

func TestReconcileModelActivation_DeleteNonExistingModel(t *testing.T) {
	mmanager := &fakeModelManager{
		models: []*iv1.EngineStatus_Model{
			// "bm0" is in the model manager but not in the model lister.
			{
				Id: "bm0",
			},
			{
				Id: "bm1",
			},
		},
	}

	fakeModelLister := &fakeModelLister{
		resp: &mv1.ListModelsResponse{
			Data: []*mv1.Model{
				{
					Id:               "bm1",
					ActivationStatus: mv1.ActivationStatus_ACTIVATION_STATUS_ACTIVE,
					IsBaseModel:      true,
				},
			},
		},
	}

	a := NewModelActivator(nil, mmanager, fakeModelLister, true)
	err := a.reconcileModelActivation(context.Background())
	assert.NoError(t, err)

	assert.ElementsMatch(t, []string{"bm1"}, mmanager.pulled)
	assert.ElementsMatch(t, []string{"bm0"}, mmanager.deleted)
}

type fakeModelManager struct {
	pulled  []string
	deleted []string
	models  []*iv1.EngineStatus_Model
	mu      sync.Mutex
}

func (f *fakeModelManager) PullModelUnblocked(ctx context.Context, modelID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.pulled = append(f.pulled, modelID)
	return nil
}

func (f *fakeModelManager) DeleteModel(ctx context.Context, modelID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.deleted = append(f.deleted, modelID)
	return nil
}

func (f *fakeModelManager) UpdateModel(ctx context.Context, modelID string) error {
	return nil
}

func (f *fakeModelManager) ListModels() []*iv1.EngineStatus_Model {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.models
}

type fakeModelLister struct {
	resp *mv1.ListModelsResponse
}

func (f *fakeModelLister) ListModels(ctx context.Context, req *mv1.ListModelsRequest, opts ...grpc.CallOption) (*mv1.ListModelsResponse, error) {
	return f.resp, nil
}
