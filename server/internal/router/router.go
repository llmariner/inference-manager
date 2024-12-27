package router

import (
	"context"
	"fmt"
	"sync"
)

type model struct {
	id string
}

// R manages the routing table of models to engines.
type R struct {
	enableDynamicModelLoading bool

	mapsByTenantID map[string]*routeMap
	mu             sync.Mutex
}

// New creates a new router.
func New(enableDynamicModelLoading bool) *R {
	return &R{
		enableDynamicModelLoading: enableDynamicModelLoading,
		mapsByTenantID:            make(map[string]*routeMap),
	}
}

// GetEnginesForModel returns the engine IDs for the given model.
func (r *R) GetEnginesForModel(ctx context.Context, modelID, tenantID string, ignores map[string]bool) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.mapsByTenantID[tenantID]
	if !ok {
		return nil, fmt.Errorf("no route found")
	}

	routes := m.getRoute(model{id: modelID})
	if len(routes) != 0 {
		return routes, nil
	}

	// There is no route to the model right now.

	// If dynamic model loading is disabled, return an error.
	if !r.enableDynamicModelLoading {
		return nil, fmt.Errorf("no route found")
	}

	engine, err := m.findLeastLoadedEngine(ignores)
	if err != nil {
		return nil, err
	}
	return []string{engine}, nil
}

// AddOrUpdateEngine adds or updates the engine with the given model IDs.
func (r *R) AddOrUpdateEngine(engineID, tenantID string, modelIDs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.mapsByTenantID[tenantID]
	if !ok {
		m = newRouteMap()
		r.mapsByTenantID[tenantID] = m
	}

	m.addOrUpdateEngine(engineID, modelIDs)
}

// DeleteEngine deletes the engine.
func (r *R) DeleteEngine(engineID, tenantID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.mapsByTenantID[tenantID]
	if !ok {
		return
	}
	m.deleteEngine(engineID)
}
