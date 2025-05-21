package server

import (
	"context"
	"sync"
	"time"

	mv1 "github.com/llmariner/model-manager/api/v1"
	"google.golang.org/grpc"
)

const defaultCacheInvalidationPeriod = 10 * time.Minute

func newModelCache(
	modelClient ModelClient,
) *modelCache {
	return &modelCache{
		modelClient:             modelClient,
		modelsByTenantID:        make(map[string]map[string]*cacheEntry),
		cacheInvalidationPeriod: defaultCacheInvalidationPeriod,
	}
}

type cacheEntry struct {
	model *mv1.Model
	// lastUpdated is the time when the model was last updated.
	lastUpdated time.Time
}

type modelCache struct {
	modelClient ModelClient

	modelsByTenantID map[string]map[string]*cacheEntry
	mu               sync.Mutex

	cacheInvalidationPeriod time.Duration
}

func (c *modelCache) getModelFromCache(tenantID, modelID string) (*mv1.Model, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	models, ok := c.modelsByTenantID[tenantID]
	if !ok {
		return nil, false
	}

	entry, ok := models[modelID]
	if !ok {
		return nil, false
	}

	if time.Since(entry.lastUpdated) < c.cacheInvalidationPeriod {
		return entry.model, true
	}

	delete(models, modelID)
	return nil, false
}

func (c *modelCache) addModelToCache(tenantID string, model *mv1.Model) {
	c.mu.Lock()
	defer c.mu.Unlock()

	models, ok := c.modelsByTenantID[tenantID]
	if !ok {
		models = make(map[string]*cacheEntry)
		c.modelsByTenantID[tenantID] = models
	}

	models[model.Id] = &cacheEntry{
		model:       model,
		lastUpdated: time.Now(),
	}
}

func (c *modelCache) removeModelFromCache(tenantID, modelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	models, ok := c.modelsByTenantID[tenantID]
	if !ok {
		return
	}
	delete(models, modelID)
}

func (c *modelCache) GetModel(
	ctx context.Context,
	tenantID string,
	in *mv1.GetModelRequest,
	opts ...grpc.CallOption,
) (*mv1.Model, error) {
	m, ok := c.getModelFromCache(tenantID, in.Id)
	if ok {
		return m, nil
	}

	m, err := c.modelClient.GetModel(ctx, in, opts...)
	if err != nil {
		return nil, err
	}

	c.addModelToCache(tenantID, m)

	return m, nil
}

func (c *modelCache) ActivateModel(
	ctx context.Context,
	tenantID string,
	in *mv1.ActivateModelRequest,
	opts ...grpc.CallOption,
) (*mv1.ActivateModelResponse, error) {
	resp, err := c.modelClient.ActivateModel(ctx, in, opts...)
	if err != nil {
		return nil, err
	}

	// Invalidate the cache.
	// TODO(kenji): This should invalidate the cache of other server instances.
	c.removeModelFromCache(tenantID, in.Id)

	return resp, nil
}
