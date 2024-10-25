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
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	statusReportInterval = 30 * time.Second
	retryInterval        = 10 * time.Second
)

func newTaskReceiver(
	infProcessor *infprocessor.P,
	localPodName string,
	serverAddr string,
	gracefulShutdownTimeout time.Duration,
	cancelF context.CancelFunc,
	logger logr.Logger,
) *taskReceiver {
	return &taskReceiver{
		infProcessor:    infProcessor,
		localPodName:    localPodName,
		serverAddr:      serverAddr,
		taskGracePeriod: gracefulShutdownTimeout - 3*time.Second,
		cancelF:         cancelF,
		logger:          logger,
	}
}

type taskReceiver struct {
	infProcessor    *infprocessor.P
	localPodName    string
	serverAddr      string
	taskGracePeriod time.Duration
	cancelF         context.CancelFunc
	logger          logr.Logger
}

func (r *taskReceiver) run(ctx context.Context) error {
	log := r.logger
	log.Info("Starting taskReceiver")
	ctx = ctrl.LoggerInto(ctx, log)

	conn, err := grpc.NewClient(r.serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	client := v1.NewInferenceInternalServiceClient(conn)

	runInternal := func() error {
		streamCtx, streamCancel := context.WithCancel(context.Background())
		streamCtx = ctrl.LoggerInto(streamCtx, ctrl.LoggerFrom(ctx))
		defer streamCancel()
		// Use separate context for the stream to gracefully handle the task requests.
		stream, err := client.ProcessTasksInternal(streamCtx)
		if err != nil {
			return err
		}
		defer func() { _ = stream.CloseSend() }()

		eg, ctx := errgroup.WithContext(ctx)
		eg.Go(func() error { return r.sendServerStatusPeriodically(ctx, stream) })
		eg.Go(func() error { return r.processTasks(ctx, stream) })
		return eg.Wait()
	}

	for {
		if err := runInternal(); err != nil {
			log.Error(err, "TaskReceiver error")
		}
		select {
		case <-ctx.Done():
			log.Info("Stopped taskReceiver", "ctx", ctx.Err())
			return nil
		case <-time.After(retryInterval):
			log.Info("Retrying taskReceiver", "retry-interval", retryInterval)
		}
	}
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
			return nil
		case <-time.After(statusReportInterval):
		}
	}
}

type senderSrv interface {
	Context() context.Context
	Send(*v1.ProcessTasksInternalRequest) error
}

func (r *taskReceiver) sendServerStatus(stream senderSrv, ready bool) error {
	// Report the status of the local engines. We don't report the status of remote
	// engines since that will cause a loop.
	enginesByTenantID := r.infProcessor.LocalEngines()

	var statuses []*v1.ServerStatus_EngineStatusWithTenantID
	for tenantID, es := range enginesByTenantID {
		for _, e := range es {
			// Overwrite the ready status based on the status of the server.
			e.Ready = ready
			statuses = append(statuses, &v1.ServerStatus_EngineStatusWithTenantID{
				EngineStatus: e,
				TenantId:     tenantID,
			})
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
			log.Info("Stopping and waiting for all tasks to complete", "grace-period", r.taskGracePeriod)
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

func isConnClosedErr(err error) bool {
	return err == io.EOF ||
		// connection error type is defined in the gRPC internal transpot package.
		strings.Contains(err.Error(), "error reading from server: EOF")
}
