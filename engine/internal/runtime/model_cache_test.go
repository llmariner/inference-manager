package runtime

import (
	"context"
	"testing"

	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestModelCache(t *testing.T) {
	const modelID = "model1"

	modelClient := &fakeModelClient{
		models: map[string]*mv1.Model{},
	}

	c := NewModelCache(modelClient)

	ctx := context.Background()
	_, err := c.GetModel(ctx, &mv1.GetModelRequest{Id: modelID})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	// No cache.
	assert.Empty(t, c.modelsByID)

	modelClient.models[modelID] = &mv1.Model{Id: modelID}

	_, err = c.GetModel(ctx, &mv1.GetModelRequest{Id: modelID})
	assert.NoError(t, err)
	// Cached.
	assert.Contains(t, c.modelsByID, modelID)
}
