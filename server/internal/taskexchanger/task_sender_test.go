package taskexchanger

import (
	"testing"

	v1 "github.com/llmariner/inference-manager/api/v1"
	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
	"github.com/stretchr/testify/assert"
)

func TestTaskSender(t *testing.T) {
	updater := &fakeEngineStatusUpdater{
		statuses: map[string]map[string]*v1.EngineStatus{},
	}

	sender := newTaskSender(&fakeTaskSenderSrv{}, updater, testutil.NewTestLogger(t))

	statuses := []*v1.ServerStatus_EngineStatusWithTenantID{
		{
			TenantId: "t0",
			EngineStatus: &v1.EngineStatus{
				EngineId: "e0",
			},
		},
		{
			TenantId: "t0",
			EngineStatus: &v1.EngineStatus{
				EngineId: "e1",
			},
		},
		{
			TenantId: "t1",
			EngineStatus: &v1.EngineStatus{
				EngineId: "e2",
			},
		},
	}
	sender.addOrUpdateEngines(statuses)

	assert.Len(t, updater.statuses, 2)
	assert.Len(t, updater.statuses["t0"], 2)
	assert.NotNil(t, updater.statuses["t0"]["e0"])
	assert.NotNil(t, updater.statuses["t0"]["e1"])
	assert.Len(t, updater.statuses["t1"], 1)
	assert.NotNil(t, updater.statuses["t1"]["e2"])

	assert.Len(t, sender.activeEngines, 3)

	// Remove "e1".
	statuses = []*v1.ServerStatus_EngineStatusWithTenantID{
		{
			TenantId: "t0",
			EngineStatus: &v1.EngineStatus{
				EngineId: "e0",
			},
		},
		{
			TenantId: "t1",
			EngineStatus: &v1.EngineStatus{
				EngineId: "e2",
			},
		},
	}
	sender.addOrUpdateEngines(statuses)

	assert.Len(t, updater.statuses, 2)
	assert.Len(t, updater.statuses["t0"], 1)
	assert.NotNil(t, updater.statuses["t0"]["e0"])
	assert.Len(t, updater.statuses["t1"], 1)
	assert.NotNil(t, updater.statuses["t1"]["e2"])

	assert.Len(t, sender.activeEngines, 2)

	// Add "e3" and remove "e2".
	statuses = []*v1.ServerStatus_EngineStatusWithTenantID{
		{
			TenantId: "t0",
			EngineStatus: &v1.EngineStatus{
				EngineId: "e0",
			},
		},
		{
			TenantId: "t2",
			EngineStatus: &v1.EngineStatus{
				EngineId: "e3",
			},
		},
	}
	sender.addOrUpdateEngines(statuses)

	assert.Len(t, updater.statuses, 2)
	assert.Len(t, updater.statuses["t0"], 1)
	assert.NotNil(t, updater.statuses["t0"]["e0"])
	assert.Len(t, updater.statuses["t2"], 1)
	assert.NotNil(t, updater.statuses["t2"]["e3"])

	assert.Len(t, sender.activeEngines, 2)

	sender.removeAllEngines()

	assert.Empty(t, updater.statuses)
	assert.Empty(t, sender.activeEngines)
}

type fakeTaskSenderSrv struct {
}

func (s *fakeTaskSenderSrv) Send(resp *v1.ProcessTasksInternalResponse) error {
	return nil
}

type fakeEngineStatusUpdater struct {
	statuses map[string]map[string]*v1.EngineStatus
}

func (u *fakeEngineStatusUpdater) AddOrUpdateEngineStatus(taskSender infprocessor.TaskSender, engineStatus *v1.EngineStatus, tenantID string, isLocal bool) {
	m, ok := u.statuses[tenantID]
	if !ok {
		m = map[string]*v1.EngineStatus{}
		u.statuses[tenantID] = m
	}
	m[engineStatus.EngineId] = engineStatus
}

func (u *fakeEngineStatusUpdater) RemoveEngine(engineID string, tenantID string) {
	delete(u.statuses[tenantID], engineID)
	if len(u.statuses[tenantID]) == 0 {
		delete(u.statuses, tenantID)
	}
}
