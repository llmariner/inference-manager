package infprocessor

import (
	"fmt"
	"sync"
	"time"

	"github.com/llmariner/inference-manager/server/internal/store"
	"google.golang.org/protobuf/proto"

	v1 "github.com/llmariner/inference-manager/api/v1"
)

const (
	// cacheStalePeriod is the duration after which the cache is considered stale.
	cacheStalePeriod = 30 * time.Second

	// engineActivePeriod is the duration after which the engine is considered inactive if there is no status update.
	engineActivePeriod = 1 * time.Minute
)

// NewEngineTracker creates a new EngineTracker.
func NewEngineTracker(store *store.S, localPodName, localPodIP string) *EngineTracker {
	return &EngineTracker{
		store:        store,
		localPodName: localPodName,
		localPodIP:   localPodIP,

		tenantStatuses: make(map[string]*tenantStatus),
	}
}

type tenantStatus struct {
	// enginesByModelID is a map from model ID to a slice of engines. It is used to cache DB query results.
	enginesByModelID map[string][]*store.Engine

	// allEngines is a slice of all engines for the tenant.
	allEngines []*store.Engine

	lastUpdatedAt time.Time

	mu sync.Mutex
}

// EngineTracker trackes the status of the engines across multiple server pods.
type EngineTracker struct {
	store *store.S

	localPodName string
	localPodIP   string

	// tenantStatuses is a map from tenant IDs to cached tenantStatuses.
	tenantStatuses map[string]*tenantStatus
	// mu protects tenantStatuses.
	mu sync.Mutex
}

func (t *EngineTracker) addOrUpdateEngine(status *v1.EngineStatus, tenantID string) error {
	b, err := proto.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal engine status: %s", err)
	}

	e := &store.Engine{
		EngineID:            status.EngineId,
		TenantID:            tenantID,
		PodName:             t.localPodName,
		PodIP:               t.localPodIP,
		Status:              b,
		IsReady:             status.Ready,
		LastStatusUpdatedAt: time.Now().UnixNano(),
	}
	if err := t.store.CreateOrUpdateEngine(e); err != nil {
		return fmt.Errorf("add or update engine: %s", err)
	}

	t.invalidateTenantStatus(tenantID)

	return nil
}

func (t *EngineTracker) deleteEngine(engineID, tenantID string) error {
	if err := t.store.DeleteEngine(engineID); err != nil {
		return fmt.Errorf("delete engine: %s", err)
	}

	t.invalidateTenantStatus(tenantID)

	return nil
}

// getEngineIDsForModel returns a list of engine IDs for the given model ID and tenant ID.
func (t *EngineTracker) getEngineIDsForModel(modelID, tenantID string) ([]string, error) {
	ts, err := t.getTenantStatus(tenantID)
	if err != nil {
		return nil, fmt.Errorf("get tenant status: %s", err)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	engines := ts.enginesByModelID[modelID]
	if len(engines) == 0 {
		// No engine is available for the model. Just return all engines.
		engines = ts.allEngines
		// Return an error if no engine is available.
		if len(engines) == 0 {
			return nil, fmt.Errorf("no engine is available")
		}
	}

	var ids []string
	for _, e := range engines {
		ids = append(ids, e.EngineID)
	}
	return ids, nil
}

func (t *EngineTracker) getTenantStatus(tenantID string) (*tenantStatus, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Return the cached tenant status if it is not stale.
	ts, ok := t.tenantStatuses[tenantID]
	if ok && time.Since(ts.lastUpdatedAt) < cacheStalePeriod {
		return ts, nil
	}

	// Reconstrct the tenant status.

	engines, err := t.store.ListEnginesByTenantID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("list engines by tenant ID: %s", err)
	}
	ts, err = buildTenantStatus(engines, time.Now())
	if err != nil {
		return nil, fmt.Errorf("build tenant status: %s", err)
	}
	t.tenantStatuses[tenantID] = ts
	return ts, nil
}

func (t *EngineTracker) invalidateTenantStatus(tenantID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.tenantStatuses, tenantID)
}

func buildTenantStatus(engines []*store.Engine, now time.Time) (*tenantStatus, error) {
	ts := &tenantStatus{
		enginesByModelID: map[string][]*store.Engine{},
		lastUpdatedAt:    now,
	}

	for _, e := range engines {
		if !e.IsReady {
			continue
		}

		if e.LastStatusUpdatedAt < now.Add(-engineActivePeriod).UnixNano() {
			// Skip inactive engines.
			continue
		}

		ts.allEngines = append(ts.allEngines, e)

		status := &v1.EngineStatus{}
		if err := proto.Unmarshal(e.Status, status); err != nil {
			return nil, fmt.Errorf("unmarshal engine status: %s", err)
		}
		for _, modelID := range status.ModelIds {
			ts.enginesByModelID[modelID] = append(ts.enginesByModelID[modelID], e)
		}
	}

	return ts, nil
}
