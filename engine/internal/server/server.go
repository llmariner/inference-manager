package server

import (
	"context"
	"fmt"
	"log"
	"net"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/engine/internal/modelsyncer"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// New creates a server.
func New(om *ollama.Manager, syncer *modelsyncer.S) *S {
	return &S{
		om:     om,
		syncer: syncer,
	}
}

// S is a server.
type S struct {
	v1.UnimplementedInferenceEngineInternalServiceServer

	om     *ollama.Manager
	syncer *modelsyncer.S

	srv *grpc.Server
}

// Run starts the gRPC server.
func (s *S) Run(port int) error {
	log.Printf("Starting internal gRPC server on port %d\n", port)

	grpcServer := grpc.NewServer()
	v1.RegisterInferenceEngineInternalServiceServer(grpcServer, s)
	reflection.Register(grpcServer)

	s.srv = grpcServer

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %s", err)
	}
	if err := grpcServer.Serve(l); err != nil {
		return fmt.Errorf("serve: %s", err)
	}
	return nil
}

// Stop stops the gRPC server.
func (s *S) Stop() {
	s.srv.Stop()
}

// PullModel downloads and registers a model with the engine.
func (s *S) PullModel(ctx context.Context, req *v1.PullModelRequest) (*emptypb.Empty, error) {
	log.Printf("Received a PullModel request: %+v\n", req)

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := s.syncer.PullModel(ctx, req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to sync the model: %s", err)
	}
	return nil, nil
}

// DeleteModel removes a model from the engine.
func (s *S) DeleteModel(ctx context.Context, req *v1.DeleteModelRequest) (*emptypb.Empty, error) {
	log.Printf("Received a DeleteModel request: %+v\n", req)

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := s.om.DeleteModel(ctx, req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to delete the model: %s", err)
	}

	return nil, nil
}

// ListModels lists all downloaded models in the engine.
func (s *S) ListModels(ctx context.Context, req *v1.ListModelsRequest) (*v1.ListModelsResponse, error) {
	log.Printf("Received a ListModels request: %+v\n", req)

	ms, err := s.om.ListModels(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to list models: %s", err)
	}
	return &v1.ListModelsResponse{
		Models: ms,
	}, nil
}
