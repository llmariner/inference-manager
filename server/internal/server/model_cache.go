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
		models:                  make(map[string]*cacheEntry),
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

	models map[string]*cacheEntry
	mu     sync.Mutex

	cacheInvalidationPeriod time.Duration
}

func (c *modelCache) getModelFromCache(id string) (*mv1.Model, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.models[id]
	if !ok {
		return nil, false
	}

	if time.Since(entry.lastUpdated) < c.cacheInvalidationPeriod {
		return entry.model, true
	}

	delete(c.models, id)
	return nil, false
}

func (c *modelCache) addModelToCache(model *mv1.Model) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.models[model.Id] = &cacheEntry{
		model:       model,
		lastUpdated: time.Now(),
	}
}

func (c *modelCache) removeModelFromCache(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.models, id)
}

func (c *modelCache) GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error) {
	m, ok := c.getModelFromCache(in.Id)
	if ok {
		return m, nil
	}

	m, err := c.modelClient.GetModel(ctx, in, opts...)
	if err != nil {
		return nil, err
	}

	c.addModelToCache(m)

	return m, nil
}

func (c *modelCache) ActivateModel(ctx context.Context, in *mv1.ActivateModelRequest, opts ...grpc.CallOption) (*mv1.ActivateModelResponse, error) {
	resp, err := c.modelClient.ActivateModel(ctx, in, opts...)
	if err != nil {
		return nil, err
	}

	// Invalidate the cache.
	// TODO(kenji): This should invalidate the cache of other server instances.
	c.removeModelFromCache(in.Id)

	return resp, nil
}
