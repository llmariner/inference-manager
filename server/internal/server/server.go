package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/server/internal/config"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type reqIntercepter interface {
	InterceptHTTPRequest(req *http.Request) (int, error)
}

type noopReqIntercepter struct {
}

func (n noopReqIntercepter) InterceptHTTPRequest(req *http.Request) (int, error) {
	return http.StatusOK, nil
}

// New creates a server.
func New(ollamaServerAddr string) *S {
	return &S{
		ollamaServerAddr: ollamaServerAddr,
		reqIntercepter:   noopReqIntercepter{},
	}
}

// S is a server.
type S struct {
	v1.UnimplementedChatServiceServer

	ollamaServerAddr string

	reqIntercepter reqIntercepter

	srv *grpc.Server
}

// Run starts the gRPC server.
func (s *S) Run(ctx context.Context, port int, authConfig config.AuthConfig) error {
	log.Printf("Starting server on port %d\n", port)

	var opts []grpc.ServerOption
	if authConfig.Enable {
		ai, err := auth.NewInterceptor(ctx, authConfig.RBACInternalServerAddr, "api.model")
		if err != nil {
			return err
		}
		opts = append(opts, grpc.ChainUnaryInterceptor(ai.Unary()))

		s.reqIntercepter = ai
	}

	grpcServer := grpc.NewServer(opts...)
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
