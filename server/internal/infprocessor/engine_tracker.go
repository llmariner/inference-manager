package infprocessor

import (
	"fmt"
	"time"

	"github.com/llmariner/inference-manager/server/internal/store"
	"google.golang.org/protobuf/proto"

	v1 "github.com/llmariner/inference-manager/api/v1"
)

// NewEngineTracker creates a new EngineTracker.
func NewEngineTracker(store *store.S, localPodName, localPodIP string) *EngineTracker {
	return &EngineTracker{
		store:        store,
		localPodName: localPodName,
		localPodIP:   localPodIP,
	}
}

// EngineTracker trackes the status of the engines across multiple server pods.
type EngineTracker struct {
	store *store.S

	localPodName string
	localPodIP   string
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
	return nil
}

func (t *EngineTracker) deleteEngine(engineID string) error {
	if err := t.store.DeleteEngine(engineID); err != nil {
		return fmt.Errorf("delete engine: %s", err)
	}
	return nil
}
