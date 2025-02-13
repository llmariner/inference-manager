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

// NewInferenceStatusServer creates a new inference status server.
func NewInferenceStatusServer(logger logr.Logger) *ISS {
	return &ISS{
		logger: logger.WithName("inference status server"),
	}
}

// ISS is a server for inference status services.
type ISS struct {
	v1.UnimplementedInferenceServiceServer

	logger logr.Logger

	srv *grpc.Server
}

// Run runs the inference status server.
func (s *ISS) Run(ctx context.Context, port int) error {
	s.logger.Info("Starting infernce server...", "port", port)

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %s", err)
	}
	return s.RunWithListener(ctx, l)
}

// RunWithListener runs the server with a given listener.
func (s *ISS) RunWithListener(ctx context.Context, l net.Listener) error {
	srv := grpc.NewServer()
	v1.RegisterInferenceServiceServer(srv, s)
	reflection.Register(srv)

	s.srv = srv

	if err := srv.Serve(l); err != nil {
		return fmt.Errorf("serve: %s", err)
	}

	s.logger.Info("Stopped inference status server")
	return nil
}

// Stop stops the inference status server.
func (s *ISS) Stop() {
	s.srv.Stop()
}

// ListInferenceStatus returns the inference status.
func (s *ISS) ListInferenceStatus(ctx context.Context, req *v1.ListInferenceStatusRequest) (*v1.InferenceStatus, error) {
	return &v1.InferenceStatus{}, nil
}
