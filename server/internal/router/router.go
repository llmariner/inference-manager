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
	mapsByTenantID map[string]*routeMap
	mu             sync.Mutex
}

// New creates a new router.
func New() *R {
	return &R{
		mapsByTenantID: make(map[string]*routeMap),
	}
}

// GetEngineForModel returns the engine ID for the given model.
func (r *R) GetEngineForModel(ctx context.Context, modelID, tenantID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.mapsByTenantID[tenantID]
	if !ok {
		return "", fmt.Errorf("tenant %q not found", tenantID)
	}

	routes := m.getRoute(model{id: modelID})
	if len(routes) != 0 {
		// TODO(guangrui): handle load balance.
		return routes[0], nil
	}

	engine, err := m.findLeastLoadedEngine()
	if err != nil {
		return "", err
	}
	m.addRoute(model{id: modelID}, engine)
	return engine, nil
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
