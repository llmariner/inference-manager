package router

import (
	"fmt"
	"log"
	"sync"
)

type routeMap struct {
	// m maps a model to engine IDs.
	m map[model]map[string]struct{}

	// engines is the collections of engine IDs.
	engines map[string]struct{}

	// mu protects m and engines.
	mu sync.Mutex
}

func newRouteMap() *routeMap {
	return &routeMap{
		m:       make(map[model]map[string]struct{}),
		engines: make(map[string]struct{}),
	}
}

func (r *routeMap) getRoute(model model) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	engines := r.m[model]
	var engineIDs []string
	for e := range engines {
		engineIDs = append(engineIDs, e)
	}
	return engineIDs
}

func (r *routeMap) addRoute(model model, engineID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	engines, ok := r.m[model]
	if !ok {
		engines = make(map[string]struct{})
		r.m[model] = engines
	}
	engines[engineID] = struct{}{}

	r.engines[engineID] = struct{}{}
}

func (r *routeMap) deleteRoute(model model, engineID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	engines, ok := r.m[model]
	if !ok {
		return
	}
	delete(engines, engineID)
}

func (r *routeMap) addOrUpdateEngine(engineID string, modelIDs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// First delete the engine from the model map.
	for _, engines := range r.m {
		delete(engines, engineID)
	}

	// Add the engine back to the model map.
	for _, id := range modelIDs {
		model := model{id: id}
		engines, ok := r.m[model]
		if !ok {
			engines = make(map[string]struct{})
			r.m[model] = engines
		}
		engines[engineID] = struct{}{}
	}

	r.engines[engineID] = struct{}{}
}

func (r *routeMap) deleteEngine(engineID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, engines := range r.m {
		delete(engines, engineID)
	}
	delete(r.engines, engineID)
}

// findLeastLoadedEngine finds the engine with the least number of loaded models.
func (r *routeMap) findLeastLoadedEngine() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.engines) == 0 {
		return "", fmt.Errorf("no engine available")
	}

	// modelsByID is a list of models keyed by engine IDs. This includes engines that don't have any models
	modelsByID := make(map[string][]model)
	for id := range r.engines {
		modelsByID[id] = []model{}
	}

	for model, engines := range r.m {
		for id := range engines {
			modelsByID[id] = append(modelsByID[id], model)
		}
	}

	// Find the engine that does not have the least number of models pulled.
	min := 0
	id := ""
	for k, v := range modelsByID {
		if len(v) < min || id == "" {
			min = len(v)
			id = k
		}
	}
	return id, nil
}

func (r *routeMap) printRoute() {
	r.mu.Lock()
	defer r.mu.Unlock()

	log.Printf("Dumping the current route map:\n")
	log.Printf("- engines: %+v\n", r.engines)
	log.Printf("- route: %+v\n", r.m)
}
