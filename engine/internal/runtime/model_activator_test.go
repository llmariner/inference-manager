package runtime

import (
	"context"
	"testing"

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

	a := NewModelActivator([]string{"model3"}, mmanager, fakeModelLister)
	err := a.reconcileModelActivation(context.Background())
	assert.NoError(t, err)

	assert.ElementsMatch(t, []string{"model1"}, mmanager.pulled)
	assert.ElementsMatch(t, []string{"model2"}, mmanager.deleted)
}

type fakeModelManager struct {
	pulled  []string
	deleted []string
}

func (f *fakeModelManager) PullModelUnblocked(ctx context.Context, modelID string) error {
	f.pulled = append(f.pulled, modelID)
	return nil
}

func (f *fakeModelManager) DeleteModel(ctx context.Context, modelID string) error {
	f.deleted = append(f.deleted, modelID)
	return nil
}

type fakeModelLister struct {
	resp *mv1.ListModelsResponse
}

func (f *fakeModelLister) ListModels(ctx context.Context, req *mv1.ListModelsRequest, opts ...grpc.CallOption) (*mv1.ListModelsResponse, error) {
	return f.resp, nil
}
