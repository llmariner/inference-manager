package taskexchanger

import (
	"github.com/go-logr/logr"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
)

func newTaskSender(
	taskSenderSrv taskSenderSrv,
	infProcessor *infprocessor.P,
	logger logr.Logger,
) *taskSender {
	return &taskSender{
		taskSenderSrv: taskSenderSrv,
		infProcessor:  infProcessor,
		logger:        logger,
		activeEngines: map[string]*activeEngineStatus{},
	}
}

type activeEngineStatus struct {
	engineID string
	tenantID string
}

type taskSenderSrv interface {
	Send(*v1.ProcessTasksInternalResponse) error
}

// taskSender sends tasks to a remote server. It also updates the statuses of the engines that connect
// to the remote server so that infProcessor.P can route tasks.
type taskSender struct {
	taskSenderSrv taskSenderSrv
	infProcessor  *infprocessor.P
	logger        logr.Logger

	// activeEngines tracks the active engines reported from the server.
	activeEngines map[string]*activeEngineStatus
}

func (s *taskSender) addOrUpdateEngines(statuses []*v1.ServerStatus_EngineStatusWithTenantID) {
	engines := map[string]*activeEngineStatus{}

	for _, status := range statuses {
		sender := &taskSenderForTenant{
			taskSenderSrv: s.taskSenderSrv,
			tenantID:      status.TenantId,
		}
		s.infProcessor.AddOrUpdateEngineStatus(sender, status.EngineStatus, status.TenantId, false /* isLocal */)

		engines[status.EngineStatus.EngineId] = &activeEngineStatus{
			engineID: status.EngineStatus.EngineId,
			tenantID: status.TenantId,
		}
	}

	// Remove engines that are not in the statuses.
	var toDelete []*activeEngineStatus
	for id, status := range s.activeEngines {
		if _, ok := engines[id]; !ok {
			toDelete = append(toDelete, status)
		}
	}

	for _, status := range toDelete {
		s.infProcessor.RemoveEngine(status.engineID, status.tenantID)
	}

	s.activeEngines = engines
}

func (s *taskSender) removeAllEngines() {
	// Pass a nil to delete all engines.
	s.addOrUpdateEngines(nil)
}

type taskSenderForTenant struct {
	taskSenderSrv taskSenderSrv
	tenantID      string
}

// Send forwards the received task to a remote server.
func (s *taskSenderForTenant) Send(resp *v1.ProcessTasksResponse) error {
	return s.taskSenderSrv.Send(&v1.ProcessTasksInternalResponse{
		NewTask:  resp.NewTask,
		TenantId: s.tenantID,
	})
}
