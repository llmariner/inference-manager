package server

import (
	"fmt"
	"log"
	"net"

	v1 "github.com/llm-operator/inference-server/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// New creates a server.
func New() *S {
	return &S{}
}

// S is a server.
type S struct {
	v1.UnimplementedChatServiceServer

	srv *grpc.Server
}

// Run starts the gRPC server.
func (s *S) Run(port int) error {
	log.Printf("Starting server on port %d\n", port)

	grpcServer := grpc.NewServer()
	v1.RegisterChatServiceServer(grpcServer, s)
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
