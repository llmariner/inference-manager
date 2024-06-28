package server

import (
	"context"
	"fmt"
	"log"
	"net"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/server/internal/config"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// NewWorkerServiceServer creates a new worker service server.
func NewWorkerServiceServer() *WS {
	return &WS{}
}

// WS is a server for worker services.
type WS struct {
	v1.UnimplementedInferenceWorkerServiceServer

	srv *grpc.Server

	enableAuth bool
}

// Run runs the worker service server.
func (ws *WS) Run(ctx context.Context, port int, authConfig config.AuthConfig) error {
	log.Printf("Starting worker service server on port %d", port)

	var opts []grpc.ServerOption
	if authConfig.Enable {
		ai, err := auth.NewWorkerInterceptor(ctx, auth.WorkerConfig{
			RBACServerAddr: authConfig.RBACInternalServerAddr,
		})
		if err != nil {
			return err
		}
		opts = append(opts, grpc.ChainUnaryInterceptor(ai.Unary()))
		ws.enableAuth = true
	}

	srv := grpc.NewServer(opts...)
	v1.RegisterInferenceWorkerServiceServer(srv, ws)
	reflection.Register(srv)

	ws.srv = srv

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if err := srv.Serve(l); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// Stop stops the worker service server.
func (ws *WS) Stop() {
	ws.srv.Stop()
}
