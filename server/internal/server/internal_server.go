package server

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/go-logr/logr"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
	"github.com/llmariner/inference-manager/server/internal/taskexchanger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// NewInternalServer creates a new internal server.
func NewInternalServer(
	infProcessor *infprocessor.P,
	taskExchanger *taskexchanger.E,
	logger logr.Logger,
) *IS {
	return &IS{
		infProcessor:  infProcessor,
		taskExchanger: taskExchanger,
		logger:        logger.WithName("internal"),
	}
}

// IS is a server for internal services.
type IS struct {
	v1.UnimplementedInferenceInternalServiceServer

	infProcessor  *infprocessor.P
	taskExchanger *taskexchanger.E

	logger logr.Logger

	srv *grpc.Server
}

// Run runs the internal service server.
func (is *IS) Run(ctx context.Context, port int) error {
	is.logger.Info("Starting IS server...", "port", port)

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %s", err)
	}
	return is.RunWithListener(ctx, l)
}

// RunWithListener runs the server with a given listener.
func (is *IS) RunWithListener(ctx context.Context, l net.Listener) error {
	srv := grpc.NewServer()
	v1.RegisterInferenceInternalServiceServer(srv, is)
	reflection.Register(srv)

	is.srv = srv

	if err := srv.Serve(l); err != nil {
		return fmt.Errorf("serve: %s", err)
	}

	is.logger.Info("Stopped IS server")
	return nil
}

// Stop stops the internal service server.
func (is *IS) Stop() {
	is.srv.Stop()
}

// ProcessTasksInternal processes tasks.
func (is *IS) ProcessTasksInternal(srv v1.InferenceInternalService_ProcessTasksInternalServer) error {
	is.logger.Info("Processing tasks from engine...")
	var registered bool
	for {
		// Check if the context is done with a non-blocking select.
		ctx := srv.Context()
		select {
		case <-ctx.Done():
			is.logger.Info("Context done, stopping processing tasks from engine", "error", ctx.Err())
			return ctx.Err()
		default:
		}

		var serverPodName string

		req, err := srv.Recv()
		if err != nil {
			if err != io.EOF {
				is.logger.Error(err, "processMessagesFromEngine error", "serverPodName", serverPodName)
			}
			return err
		}

		switch msg := req.Message.(type) {
		case *v1.ProcessTasksInternalRequest_ServerStatus:
			is.logger.Info("Received server status", "serverPodName", msg.ServerStatus.PodName)
			is.taskExchanger.AddOrUpdateServerStatus(srv, msg.ServerStatus)
			serverPodName = msg.ServerStatus.PodName
		case *v1.ProcessTasksInternalRequest_TaskResult:
			is.logger.V(1).Info("Received task result", "taskID", msg.TaskResult.TaskId)
			is.infProcessor.ProcessTaskResult(msg.TaskResult)
		default:
			return fmt.Errorf("unknown message type: %T", msg)
		}

		if !registered && serverPodName != "" {
			defer func() {
				is.taskExchanger.RemoveServer(serverPodName)
				is.logger.Info("Unregistered server", "serverPodName", serverPodName)
				// TODO(kenji): Wait until all sends complete before closing.
			}()
			registered = true
		}
	}
}
