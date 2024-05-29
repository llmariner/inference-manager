package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/server/internal/config"
	"github.com/llm-operator/inference-manager/server/internal/monitoring"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type engineGetter interface {
	GetEngineForModel(ctx context.Context, modelID string) (string, error)
}

// ModelClient is an interface for a model client.
type ModelClient interface {
	GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error)
}

// NoopModelClient is a no-op model client.
type NoopModelClient struct {
}

// GetModel is a no-op implementation of GetModel.
func (c *NoopModelClient) GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error) {
	return &mv1.Model{}, nil
}

type reqIntercepter interface {
	InterceptHTTPRequest(req *http.Request) (int, auth.UserInfo, error)
}

type noopReqIntercepter struct {
}

// InterceptHTTPRequest is a no-op implementation of InterceptHTTPRequest.
func (n noopReqIntercepter) InterceptHTTPRequest(req *http.Request) (int, auth.UserInfo, error) {
	return http.StatusOK, auth.UserInfo{}, nil
}

// New creates a server.
func New(
	engineGetter engineGetter,
	m monitoring.MetricsMonitoring,
	modelClient ModelClient,
) *S {
	return &S{
		engineGetter:   engineGetter,
		metricsMonitor: m,
		modelClient:    modelClient,
		reqIntercepter: noopReqIntercepter{},
	}
}

// S is a server.
type S struct {
	v1.UnimplementedChatServiceServer

	engineGetter engineGetter

	metricsMonitor monitoring.MetricsMonitoring

	modelClient ModelClient

	reqIntercepter reqIntercepter

	srv *grpc.Server
}

// Run starts the gRPC server.
func (s *S) Run(ctx context.Context, port int, authConfig config.AuthConfig) error {
	log.Printf("Starting server on port %d\n", port)

	var opts []grpc.ServerOption
	if authConfig.Enable {
		ai, err := auth.NewInterceptor(ctx, auth.Config{
			RBACServerAddr: authConfig.RBACInternalServerAddr,
			AccessResource: "api.model",
		})
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
