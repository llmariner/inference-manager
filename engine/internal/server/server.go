package server

import (
	"context"
	"fmt"
	"log"
	"net"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// New creates a server.
func New(om *ollama.Manager) *S {
	return &S{
		om: om,
	}
}

// S is a server.
type S struct {
	v1.UnimplementedInferenceEngineInternalServiceServer

	om *ollama.Manager

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

// RegisterModel registers a new model.
func (s *S) RegisterModel(
	ctx context.Context,
	req *v1.RegisterModelRequest,
) (*v1.RegisterModelResponse, error) {
	if req.ModelName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "model name is required")
	}
	if req.BaseModel == "" {
		return nil, status.Errorf(codes.InvalidArgument, "base model is required")
	}
	if req.AdapterPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "adapter path is required")
	}

	ms := &ollama.ModelSpec{
		BaseModel:   req.BaseModel,
		AdapterPath: req.AdapterPath,
	}

	if err := s.om.CreateNewModel(req.ModelName, ms); err != nil {
		return nil, status.Errorf(codes.Internal, "register model: %s", err)
	}

	return &v1.RegisterModelResponse{}, nil
}
