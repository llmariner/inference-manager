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
	const (
		tenantID = "tenant1"
		modelID  = "model1"
	)

	modelClient := &fakeModelClient{
		models: map[string]*mv1.Model{},
	}

	c := newModelCache(modelClient)

	ctx := context.Background()
	_, err := c.GetModel(ctx, tenantID, &mv1.GetModelRequest{Id: modelID})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	// No cache.
	assert.Empty(t, c.modelsByTenantID[tenantID])

	modelClient.models[modelID] = &mv1.Model{
		Id: modelID,
	}

	_, err = c.GetModel(ctx, tenantID, &mv1.GetModelRequest{Id: modelID})
	assert.NoError(t, err)
	// Cached.
	models := c.modelsByTenantID[tenantID]
	assert.Contains(t, models, modelID)

	_, err = c.ActivateModel(ctx, tenantID, &mv1.ActivateModelRequest{Id: modelID})
	assert.NoError(t, err)
	// Cache will be invalidated.
	assert.Empty(t, models)

	// Cache again.
	_, err = c.GetModel(ctx, tenantID, &mv1.GetModelRequest{Id: modelID})
	assert.NoError(t, err)

	models[modelID].lastUpdated = time.Now().Add(-2 * c.cacheInvalidationPeriod)

	now := time.Now()
	_, err = c.GetModel(ctx, tenantID, &mv1.GetModelRequest{Id: modelID})
	assert.NoError(t, err)
	// Cache was updated.
	assert.Greater(t, models[modelID].lastUpdated, now)
}
