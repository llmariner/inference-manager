package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ModelSyncer syncs models.
type ModelSyncer interface {
	PullModel(ctx context.Context, modelID string) error
	ListSyncedModelIDs(ctx context.Context) []string
	DeleteModel(ctx context.Context, modelID string) error
}

// NewFakeModelSyncer returns a FakeModelSyncer.
func NewFakeModelSyncer() *FakeModelSyncer {
	return &FakeModelSyncer{
		modelIDs: map[string]struct{}{},
	}
}

// FakeModelSyncer is a fake implementation of model syncer.
type FakeModelSyncer struct {
	modelIDs map[string]struct{}
	mu       sync.Mutex
}

// PullModel downloads and registers a model from model manager.
func (s *FakeModelSyncer) PullModel(ctx context.Context, modelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelIDs[modelID] = struct{}{}
	return nil
}

// ListSyncedModelIDs lists all models that have been synced.
func (s *FakeModelSyncer) ListSyncedModelIDs(ctx context.Context) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id := range s.modelIDs {
		ids = append(ids, id)
	}
	return ids
}

// DeleteModel deletes a model.
func (s *FakeModelSyncer) DeleteModel(ctx context.Context, modelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.modelIDs, modelID)
	return nil
}

// New creates a server.
func New(syncer ModelSyncer) *S {
	return &S{
		syncer: syncer,
	}
}

// S is a server.
type S struct {
	v1.UnimplementedInferenceEngineInternalServiceServer

	syncer ModelSyncer

	srv *grpc.Server
}

// Run starts the gRPC server.
func (s *S) Run(port int) error {
	log.Printf("Starting internal gRPC server on port %d\n", port)

	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(newLoggingInterceptor()),
	}
	grpcServer := grpc.NewServer(opts...)
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
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	ctx = auth.AppendWorkerAuthorization(ctx)
	if err := s.syncer.PullModel(ctx, req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to sync the model: %s", err)
	}
	return nil, nil
}

// DeleteModel removes a model from the engine.
func (s *S) DeleteModel(ctx context.Context, req *v1.DeleteModelRequest) (*emptypb.Empty, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	ctx = auth.AppendWorkerAuthorization(ctx)
	if err := s.syncer.DeleteModel(ctx, req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to delete the model: %s", err)
	}

	return nil, nil
}

// ListModels lists all downloaded models in the engine.
func (s *S) ListModels(ctx context.Context, req *v1.ListModelsRequest) (*v1.ListModelsResponse, error) {
	ctx = auth.AppendWorkerAuthorization(ctx)
	ids := s.syncer.ListSyncedModelIDs(ctx)
	var models []*v1.Model
	for _, id := range ids {
		models = append(models, &v1.Model{
			Id: id,
		})
	}
	return &v1.ListModelsResponse{
		Models: models,
	}, nil
}
