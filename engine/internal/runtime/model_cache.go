package runtime

import (
	"context"
	"sync"
	"time"

	mv1 "github.com/llmariner/model-manager/api/v1"
	"google.golang.org/grpc"
)

const defaultCacheInvalidationPeriod = 5 * time.Second

// NewModelCache creates a new model cache
func NewModelCache(modelGetter modelGetter) *ModelCache {
	return &ModelCache{
		modelGetter:             modelGetter,
		modelsByID:              make(map[string]*cacheEntry),
		cacheInvalidationPeriod: defaultCacheInvalidationPeriod,
	}
}

type cacheEntry struct {
	model *mv1.Model
	// lastUpdated is the time when the model was last updated.
	lastUpdated time.Time
}

// ModelCache is a cache for models.
type ModelCache struct {
	modelGetter modelGetter

	modelsByID map[string]*cacheEntry
	mu         sync.Mutex

	cacheInvalidationPeriod time.Duration
}

// GetModel gets the model.
func (c *ModelCache) GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error) {
	// Lock the entire function so that at most one goroutine will make an RPC call when cache miss happens.
	c.mu.Lock()
	defer c.mu.Unlock()

	if m, ok := c.getModelFromCache(in.Id); ok {
		return m, nil
	}

	m, err := c.modelGetter.GetModel(ctx, in, opts...)
	if err != nil {
		return nil, err
	}

	c.modelsByID[in.Id] = &cacheEntry{
		model:       m,
		lastUpdated: time.Now(),
	}

	return m, nil
}

func (c *ModelCache) getModelFromCache(modelID string) (*mv1.Model, bool) {
	entry, ok := c.modelsByID[modelID]
	if !ok {
		return nil, false
	}

	if time.Since(entry.lastUpdated) < c.cacheInvalidationPeriod {
		return entry.model, true
	}

	// Stale cache. Remove and return false.
	delete(c.modelsByID, modelID)
	return nil, false
}
