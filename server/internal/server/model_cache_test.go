package server

import (
	"context"
	"testing"
	"time"

	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestModelCache(t *testing.T) {
	modelClient := &fakeModelClient{
		models: map[string]*mv1.Model{},
	}

	c := newModelCache(modelClient)

	ctx := context.Background()
	_, err := c.GetModel(ctx, &mv1.GetModelRequest{
		Id: "model1",
	})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	// No cache.
	assert.Empty(t, c.models)

	modelClient.models["model1"] = &mv1.Model{
		Id: "model1",
	}

	_, err = c.GetModel(ctx, &mv1.GetModelRequest{
		Id: "model1",
	})
	assert.NoError(t, err)
	// Cached.
	assert.Contains(t, c.models, "model1")

	_, err = c.ActivateModel(ctx, &mv1.ActivateModelRequest{
		Id: "model1",
	})
	assert.NoError(t, err)
	// Cache will be invalidated.
	assert.Empty(t, c.models)

	// Cache again.
	_, err = c.GetModel(ctx, &mv1.GetModelRequest{
		Id: "model1",
	})
	assert.NoError(t, err)

	c.models["model1"].lastUpdated = time.Now().Add(-2 * c.cacheInvalidationPeriod)

	now := time.Now()
	_, err = c.GetModel(ctx, &mv1.GetModelRequest{
		Id: "model1",
	})
	assert.NoError(t, err)
	// Cache was updated.
	assert.Greater(t, c.models["model1"].lastUpdated, now)
}
