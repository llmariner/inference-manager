package taskexchanger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	statusReportInterval = 10 * time.Second
	retryInterval        = 10 * time.Second
)

func newTaskReceiver(
	infProcessor *infprocessor.P,
	localPodName string,
	serverAddr string,
	cancelF context.CancelFunc,
	logger logr.Logger,
) *taskReceiver {
	return &taskReceiver{
		infProcessor:   infProcessor,
		localPodName:   localPodName,
		serverAddr:     serverAddr,
		cancelF:        cancelF,
		logger:         logger,
		engineStatuses: make(map[string]map[string]*v1.EngineStatus),
	}
}

type taskReceiver struct {
	infProcessor *infprocessor.P
	localPodName string
	serverAddr   string
	cancelF      context.CancelFunc
	logger       logr.Logger

	// engineStatuses is mapped by tenant ID and engine ID.
	engineStatuses map[string]map[string]*v1.EngineStatus
	// isShutdown is true if the task exchanger is shutting down.
	isShutdown bool
	mu         sync.Mutex
}

func (r *taskReceiver) run(ctx context.Context) error {
	log := r.logger
	log.Info("Starting taskReceiver")
	ctx = ctrl.LoggerInto(ctx, log)

	for {
		if err := r.runInternal(ctx); err != nil {
			log.Error(err, "TaskReceiver error")
		}
		select {
		case <-ctx.Done():
			log.Info("Stopped taskReceiver", "ctx", ctx.Err())
			return ctx.Err()
		case <-time.After(retryInterval):
			log.Info("Retrying taskReceiver", "retry-interval", retryInterval)
		}
	}
}

func (r *taskReceiver) runInternal(ctx context.Context) error {
	// Use separate context for the stream to gracefully handle the task requests.
	streamCtx, streamCancel := context.WithCancel(context.Background())
	streamCtx = ctrl.LoggerInto(streamCtx, ctrl.LoggerFrom(ctx))
	defer streamCancel()

	conn, err := grpc.NewClient(r.serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}

	defer func() {
		_ = conn.Close()
	}()

	client := v1.NewInferenceInternalServiceClient(conn)
	stream, err := client.ProcessTasksInternal(streamCtx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.CloseSend() }()

	ctx, cancel := context.WithCancel(ctx)

	errCh := make(chan error)
	go func() {
		errCh <- r.sendServerStatusPeriodically(ctx, stream)
	}()
	go func() {
		errCh <- r.processTasks(ctx, stream)
	}()

	// Wait for the first error from either sendEngineStatusPeriodically or processTasks.
	// Then cancel the context to stop both goroutines.
	err = <-errCh
	cancel()
	<-errCh
	return err
}

func (r *taskReceiver) stop() {
	r.cancelF()
}

func (r *taskReceiver) sendServerStatusPeriodically(
	ctx context.Context,
	stream v1.InferenceInternalService_ProcessTasksInternalClient,
) error {
	log := ctrl.LoggerFrom(ctx).WithName("status")
	ctx = ctrl.LoggerInto(ctx, log)
	defer log.Info("Stopped status reporter")

	isFirst := true
	for {
		if err := r.sendServerStatus(stream, true); err != nil {
			return err
		}

		if isFirst {
			isFirst = false
			log.Info("Successfully registered taskReceiver")
		}

		select {
		case <-stream.Context().Done():
			return nil
		case <-ctx.Done():
			if err := r.sendServerStatus(stream, false); err != nil {
				return err
			}
			return ctx.Err()
		case <-time.After(statusReportInterval):
		}
	}
}

type senderSrv interface {
	Context() context.Context
	Send(*v1.ProcessTasksInternalRequest) error
}

func (r *taskReceiver) sendServerStatus(stream senderSrv, ready bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var needSend bool

	// Report the status of the local engines. We don't report the status of remote
	// engines since that will cause a loop.
	enginesByTenantID := r.infProcessor.LocalEngines()

	var statuses []*v1.ServerStatus_EngineStatusWithTenantID

	// Keep the engine statuses empty if the server is shutting down.
	// This will prevent new tasks from being scheduled to this server.
	if !r.isShutdown {
		for tenantID, es := range enginesByTenantID {
			cachedEngineStatuses, ok := r.engineStatuses[tenantID]
			if !ok {
				needSend = true
			}

			updatedEngineStatuses := make(map[string]*v1.EngineStatus)
			for _, e := range es {
				if !needSend {
					cachedStatus, ok := cachedEngineStatuses[e.EngineId]
					if !ok {
						needSend = true
					} else {
						// Check if the engine status is changed.
						needSend = !sameEngineStatus(cachedStatus, e)
					}
				}
				// Overwrite the ready status based on the status of the server.
				e.Ready = ready
				statuses = append(statuses, &v1.ServerStatus_EngineStatusWithTenantID{
					EngineStatus: e,
					TenantId:     tenantID,
				})
				updatedEngineStatuses[e.EngineId] = e
			}
			r.engineStatuses[tenantID] = updatedEngineStatuses
		}

		if !needSend {
			return nil
		}
	}

	req := &v1.ProcessTasksInternalRequest{
		Message: &v1.ProcessTasksInternalRequest_ServerStatus{
			ServerStatus: &v1.ServerStatus{
				PodName:        r.localPodName,
				EngineStatuses: statuses,
			},
		},
	}
	if err := stream.Send(req); err != nil {
		return err
	}
	return nil
}

func sameEngineStatus(a, b *v1.EngineStatus) bool {
	if a == nil || b == nil {
		return false
	}
	if a.EngineId != b.EngineId || a.ClusterId != b.ClusterId || a.Ready != b.Ready {
		return false
	}
	if len(a.Models) != len(b.Models) {
		return false
	}
	aModels := make(map[string]*v1.EngineStatus_Model)
	for _, m := range a.Models {
		aModels[m.Id] = m
	}
	for _, m := range b.Models {
		am, ok := aModels[m.Id]
		if !ok {
			return false
		}
		if am.IsReady != m.IsReady ||
			am.InProgressTaskCount != m.InProgressTaskCount ||
			am.GpuAllocated != m.GpuAllocated {
			return false
		}
	}
	return true
}

func (r *taskReceiver) processTasks(
	ctx context.Context,
	stream v1.InferenceInternalService_ProcessTasksInternalClient,
) error {
	log := ctrl.LoggerFrom(ctx).WithName("task")
	ctx = ctrl.LoggerInto(ctx, log)
	defer log.Info("Stopped process handler")

	respCh := make(chan *v1.ProcessTasksInternalResponse)
	errCh := make(chan error)
	doneCh := make(chan struct{})
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				if isConnClosedErr(err) {
					err = fmt.Errorf("connection closed")
				} else {
					err = fmt.Errorf("receive task: %s", err)
				}
				errCh <- err
				return
			}
			respCh <- resp

			select {
			case <-doneCh:
				return
			default:
			}
		}
	}()

	var wg sync.WaitGroup
	for {
		select {
		case resp := <-respCh:
			// Create a goroutine to process the task so that we can receive the next task.
			wg.Add(1)
			go func() {
				defer wg.Done()

				log := log.WithValues("taskID", resp.NewTask.Id)
				log.Info("Started processing task")
				if err := r.processTask(ctrl.LoggerInto(ctx, log), stream, resp.NewTask, resp.TenantId); errors.Is(err, context.Canceled) {
					log.Info("Canceled task", "reason", err)
				} else if err != nil {
					log.Error(err, "Failed to process task")
				} else {
					log.Info("Completed task")
				}
			}()
		case err := <-errCh:
			return err
		case <-ctx.Done():
			log.Info("Stopping and waiting for all tasks to complete")
			wg.Wait()
			close(doneCh)
			return nil
		}
	}
}

// processTask processes the task and forwards the task result to the connecting remote server.
func (r *taskReceiver) processTask(
	ctx context.Context,
	stream senderSrv,
	t *v1.Task,
	tenantID string,
) error {
	return r.infProcessor.SendAndProcessTask(ctx, t, tenantID, func(result *v1.TaskResult) error {
		return stream.Send(&v1.ProcessTasksInternalRequest{
			Message: &v1.ProcessTasksInternalRequest_TaskResult{
				TaskResult: result,
			},
		})
	})
}

func (r *taskReceiver) startGracefulShutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isShutdown = true
}

func (r *taskReceiver) shutdownStarted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isShutdown
}

func isConnClosedErr(err error) bool {
	return err == io.EOF ||
		// connection error type is defined in the gRPC internal transpot package.
		strings.Contains(err.Error(), "error reading from server: EOF")
}
