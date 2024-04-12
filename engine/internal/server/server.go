package server

import (
	"fmt"
	"log"
	"net"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
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
