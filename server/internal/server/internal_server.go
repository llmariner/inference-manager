package server

import (
	"context"
	"fmt"
	"net"

	"github.com/go-logr/logr"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// NewInternalServer creates a new internal server.
func NewInternalServer(logger logr.Logger) *IS {
	return &IS{
		logger: logger.WithName("internal"),
	}
}

// IS is a server for internal services.
type IS struct {
	v1.UnimplementedInferenceInternalServiceServer

	logger logr.Logger

	srv *grpc.Server
}

// Run runs the internal service server.
func (is *IS) Run(ctx context.Context, port int) error {
	is.logger.Info("Starting IS server...", "port", port)

	srv := grpc.NewServer()
	v1.RegisterInferenceInternalServiceServer(srv, is)
	reflection.Register(srv)

	is.srv = srv

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %s", err)
	}
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

// ProcessTasks processes tasks.
func (is *IS) ProcessTasks(srv v1.InferenceInternalService_ProcessTasksInternalServer) error {
	// TODO(kenji): Implement.

	return nil
}
