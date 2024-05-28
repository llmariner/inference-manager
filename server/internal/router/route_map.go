package router

import (
	"fmt"
	"log"
	"sync"
)

type routeMap struct {
	// m maps a model to a list of engine IPs.
	m map[model][]string
	// engines is the collections of engine IPs.
	engines map[string]struct{}

	// mu protects m and engines.
	mu sync.Mutex
}

func newRouteMap() *routeMap {
	return &routeMap{
		m:       make(map[model][]string),
		engines: make(map[string]struct{}),
	}
}

func (r *routeMap) getRoute(model model) []string {
	var rs []string

	r.mu.Lock()
	defer r.mu.Unlock()

	found := r.m[model]
	rs = append(rs, found...)
	return rs
}

func (r *routeMap) addRoute(model model, ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	found, ok := r.m[model]
	if !ok {
		r.m[model] = []string{ip}
		return
	}
	for _, v := range found {
		if v == ip {
			return
		}
	}
	r.m[model] = append(found, ip)
}

func (r *routeMap) deleteRoute(model model) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for k := range r.m {
		if k.id == model.id {
			delete(r.m, k)
			return nil
		}
	}
	return fmt.Errorf("model %s not found", model)
}

func (r *routeMap) addServer(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.engines[ip] = struct{}{}
}

func (r *routeMap) deleteServer(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for k, ips := range r.m {
		for i, v := range ips {
			if v == ip {
				r.m[k] = append(ips[:i], ips[i+1:]...)
				continue
			}
		}
	}

	delete(r.engines, ip)
}

func (r *routeMap) replace(nr *routeMap) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.m = nr.m
	r.engines = nr.engines
}

// findLeastLoadedEngine finds the engine with the least number of loaded models.
func (r *routeMap) findLeastLoadedEngine() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.engines) == 0 {
		return "", fmt.Errorf("no engine available")
	}

	// modelsIP is a list of models keyed by engine IPs. This includes engines that don't have any models
	modelsByIP := make(map[string][]model)
	for ip := range r.engines {
		modelsByIP[ip] = []model{}
	}

	for k, v := range r.m {
		for _, ip := range v {
			modelsByIP[ip] = append(modelsByIP[ip], k)
		}
	}

	// Find the engine that does not have the least number of models pulled.
	min := 0
	ip := ""
	for k, v := range modelsByIP {
		if len(v) < min || ip == "" {
			min = len(v)
			ip = k
		}
	}
	return ip, nil
}

func (r *routeMap) printRoute() {
	r.mu.Lock()
	defer r.mu.Unlock()

	log.Printf("Engines: %+v\n", r.engines)
	log.Printf("Route: %+v\n", r.m)
}
