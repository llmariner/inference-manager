package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/api-usage/pkg/sender"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/server/internal/config"
	"github.com/llmariner/rbac-manager/pkg/auth"
	vsv1 "github.com/llmariner/vector-store-manager/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

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

// VectorStoreClient is an interface for a vector store client.
type VectorStoreClient interface {
	GetVectorStoreByName(ctx context.Context, req *vsv1.GetVectorStoreByNameRequest, opts ...grpc.CallOption) (*vsv1.VectorStore, error)
}

// NoopVectorStoreClient is a no-op vector store client.
type NoopVectorStoreClient struct {
}

// GetVectorStoreByName is a no-op implementation of GetVectorStoreByName.
func (c *NoopVectorStoreClient) GetVectorStoreByName(ctx context.Context, req *vsv1.GetVectorStoreByNameRequest, opts ...grpc.CallOption) (*vsv1.VectorStore, error) {
	return &vsv1.VectorStore{}, nil
}

type reqIntercepter interface {
	InterceptHTTPRequest(req *http.Request) (int, auth.UserInfo, error)
}

type noopReqIntercepter struct {
}

// InterceptHTTPRequest is a no-op implementation of InterceptHTTPRequest.
func (n noopReqIntercepter) InterceptHTTPRequest(req *http.Request) (int, auth.UserInfo, error) {
	return http.StatusOK, auth.UserInfo{
		OrganizationID: "default",
		ProjectID:      defaultProjectID,
		AssignedKubernetesEnvs: []auth.AssignedKubernetesEnv{
			{
				ClusterID: defaultClusterID,
				Namespace: "default",
			},
		},
		TenantID: defaultTenantID,
	}, nil
}

// Rewriter is an interface for rag.
type Rewriter interface {
	ProcessMessages(
		ctx context.Context,
		vstore *vsv1.VectorStore,
		messages []*v1.CreateChatCompletionRequest_Message,
	) ([]*v1.CreateChatCompletionRequest_Message, error)
}

// NoopRewriter is a no-op rewriter.
type NoopRewriter struct {
}

// ProcessMessages is a no-op implementation of ProcessMessages.
func (r *NoopRewriter) ProcessMessages(
	ctx context.Context,
	vstore *vsv1.VectorStore,
	messages []*v1.CreateChatCompletionRequest_Message,
) ([]*v1.CreateChatCompletionRequest_Message, error) {
	return messages, nil
}

// metricsMonitoring is an interface for monitoring metrics.
type metricsMonitoring interface {
	ObserveCompletionLatency(modelID string, latency time.Duration)
	UpdateCompletionRequest(modelID string, c int)
	ObserveEmbeddingLatency(modelID string, latency time.Duration)
	UpdateEmbeddingRequest(modelID string, c int)
}

type taskSender interface {
	SendChatCompletionTask(ctx context.Context, tenantID string, req *v1.CreateChatCompletionRequest, header http.Header) (*http.Response, error)
	SendEmbeddingTask(ctx context.Context, tenantID string, req *v1.CreateEmbeddingRequest, header http.Header) (*http.Response, error)
}

// New creates a server.
func New(
	m metricsMonitoring,
	usage sender.UsageSetter,
	modelClient ModelClient,
	vsClient VectorStoreClient,
	r Rewriter,
	taskSender taskSender,
	logger logr.Logger,
) *S {
	return &S{
		metricsMonitor: m,
		usageSetter:    usage,
		modelClient:    modelClient,
		vsClient:       vsClient,
		reqIntercepter: noopReqIntercepter{},
		taskSender:     taskSender,
		rewriter:       r,
		logger:         logger.WithName("grpc"),
	}
}

// S is a server.
type S struct {
	v1.UnimplementedChatServiceServer

	enableAuth bool

	metricsMonitor metricsMonitoring
	usageSetter    sender.UsageSetter

	modelClient ModelClient

	vsClient VectorStoreClient

	rewriter Rewriter

	taskSender taskSender

	reqIntercepter reqIntercepter

	srv *grpc.Server

	logger logr.Logger
}

// Run starts the gRPC server.
func (s *S) Run(ctx context.Context, port int, authConfig config.AuthConfig) error {
	s.logger.Info("Starting gRPC server...", "port", port)

	var opts []grpc.ServerOption
	if authConfig.Enable {
		ai, err := auth.NewInterceptor(ctx, auth.Config{
			RBACServerAddr: authConfig.RBACInternalServerAddr,
			AccessResource: "api.model",
		})
		if err != nil {
			return err
		}
		authFn := ai.Unary()
		healthSkip := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
			if info.FullMethod == "/grpc.health.v1.Health/Check" {
				// Skip authentication for health check
				return handler(ctx, req)
			}
			return authFn(ctx, req, info, handler)
		}
		opts = append(opts, grpc.ChainUnaryInterceptor(healthSkip))

		s.reqIntercepter = ai
		s.enableAuth = true
	}

	grpcServer := grpc.NewServer(opts...)
	v1.RegisterChatServiceServer(grpcServer, s)
	reflection.Register(grpcServer)

	healthCheck := health.NewServer()
	healthCheck.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthCheck)

	s.srv = grpcServer

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %s", err)
	}
	if err := grpcServer.Serve(l); err != nil {
		return fmt.Errorf("serve: %s", err)
	}
	s.logger.Info("Stopped gRPC server")
	return nil
}

// Stop stops the gRPC server.
func (s *S) Stop() {
	s.srv.Stop()
}
