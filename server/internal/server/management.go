package server

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/server/internal/config"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const (
	defaultOrganizationID = "default"
	defaultNamespace      = "default"
)

// NewInferenceManagementServer creates a new inference management server.
func NewInferenceManagementServer(
	infProcessor *infprocessor.P,
	modelClient ModelClient,
	logger logr.Logger,
) *IMS {
	return &IMS{
		infProcessor: infProcessor,
		modelClient:  modelClient,
		logger:       logger.WithName("inference status server"),
	}
}

// IMS is a server for inference management services.
type IMS struct {
	v1.UnimplementedInferenceServiceServer

	// keyed by tenant ID and cluster ID.
	tenantStatuses map[string]map[string][]*v1.EngineStatus
	mu             sync.RWMutex

	infProcessor *infprocessor.P
	modelClient  ModelClient
	logger       logr.Logger

	srv *grpc.Server
}

// Run runs the inference status server.
func (s *IMS) Run(ctx context.Context, authConfig config.AuthConfig, port int) error {
	s.logger.Info("Starting infernce server...", "port", port)

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %s", err)
	}
	return s.RunWithListener(ctx, authConfig, l)
}

// RunWithListener runs the server with a given listener.
func (s *IMS) RunWithListener(ctx context.Context, authConfig config.AuthConfig, l net.Listener) error {
	var opt grpc.ServerOption
	if authConfig.Enable {
		ai, err := auth.NewInterceptor(ctx, auth.Config{
			RBACServerAddr: authConfig.RBACInternalServerAddr,
			// TODO(guangrui): Consider to create a resource in rbac manager, e.g. "api.inference.status"
			AccessResource: "api.model",
		})
		if err != nil {
			return err
		}
		opt = grpc.ChainUnaryInterceptor(ai.Unary())
	} else {
		opt = grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
			return handler(fakeAuthInto(ctx), req)
		})
	}

	srv := grpc.NewServer(opt)
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
func (s *IMS) Stop() {
	s.srv.Stop()
}

// Refresh refreshes the inference status.
func (s *IMS) Refresh(ctx context.Context, interval time.Duration) error {
	s.refresh()

	ticker := time.NewTicker(interval)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.refresh()
		}
	}
}

func (s *IMS) refresh() {
	tss := make(map[string]map[string][]*v1.EngineStatus)
	for tid, ts := range s.infProcessor.DumpStatus().Tenants {
		tss[tid] = make(map[string][]*v1.EngineStatus)
		for eid, e := range ts.Engines {
			es, ok := tss[tid][e.ClusterID]
			if !ok {
				es = make([]*v1.EngineStatus, 0)
			}
			es = append(es, &v1.EngineStatus{
				EngineId:  eid,
				ClusterId: e.ClusterID,
				Models:    e.Models,
				// Set to true as the engine reported from infProcessor is already ready.
				Ready: true,
			})
			tss[tid][e.ClusterID] = es
		}

		for _, es := range tss[tid] {
			sort.Slice(es, func(i, j int) bool {
				return es[i].EngineId < es[j].EngineId
			})
		}
	}

	s.logger.V(10).Info("refreshing status...", "tss", tss)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tenantStatuses = tss
}

// GetInferenceStatus returns the inference status.
func (s *IMS) GetInferenceStatus(ctx context.Context, req *v1.GetInferenceStatusRequest) (*v1.InferenceStatus, error) {
	userInfo, ok := auth.ExtractUserInfoFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user info not found")
	}
	clusterNamesByID := map[string]string{}
	// Construct a map to avoid duplicated clusters in the env.
	for _, env := range userInfo.AssignedKubernetesEnvs {
		clusterNamesByID[env.ClusterID] = env.ClusterName
	}

	s.mu.Lock()
	ts := s.tenantStatuses[userInfo.TenantID]
	s.mu.Unlock()

	var css []*v1.ClusterStatus
	tasks := make(map[string]int32)
	for cid, cname := range clusterNamesByID {
		es, ok := ts[cid]
		if !ok {
			continue
		}
		var tc int32
		modelsByID := map[string]*v1.EngineStatus_Model{}
		for _, e := range es {
			for _, m := range e.Models {
				tasks[m.Id] += m.InProgressTaskCount
				tc += m.InProgressTaskCount
				// Simply overrride if there is an existing value as every engine in the same cluster
				// should have the same info.
				modelsByID[m.Id] = m
			}
		}
		var ga int32
		for _, m := range modelsByID {
			ga += m.GpuAllocated
		}

		css = append(css, &v1.ClusterStatus{
			Id:                  cid,
			Name:                cname,
			EngineStatuses:      es,
			ModelCount:          int32(len(modelsByID)),
			InProgressTaskCount: tc,
			GpuAllocated:        ga,
		})
	}

	sort.Slice(css, func(i, j int) bool {
		return css[i].Id < css[j].Id
	})

	return &v1.InferenceStatus{
		ClusterStatuses: css,
		TaskStatus: &v1.TaskStatus{
			InProgressTaskCounts: tasks,
		},
	}, nil
}

// ActivateModel activates a model.
func (s *IMS) ActivateModel(ctx context.Context, req *v1.ActivateModelRequest) (*v1.ActivateModelResponse, error) {
	userInfo, ok := auth.ExtractUserInfoFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user info not found")
	}

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is empty")
	}

	if code, err := s.checkModelAvailability(ctx, req.Id); err != nil {
		return nil, status.Errorf(code, "%s", err)
	}

	resp, err := s.infProcessor.SendModelActivationTask(ctx, userInfo.TenantID, req)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to send model activation task: %s", err))
	}
	_ = resp.Body.Close()

	return &v1.ActivateModelResponse{}, nil
}

// DeactivateModel deactivates a model.
func (s *IMS) DeactivateModel(ctx context.Context, req *v1.DeactivateModelRequest) (*v1.DeactivateModelResponse, error) {
	userInfo, ok := auth.ExtractUserInfoFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user info not found")
	}

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is empty")
	}

	if code, err := s.checkModelAvailability(ctx, req.Id); err != nil {
		return nil, status.Errorf(code, "%s", err)
	}

	resp, err := s.infProcessor.SendModelDeactivationTask(ctx, userInfo.TenantID, req)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to send model deactivation task: %s", err))
	}
	_ = resp.Body.Close()

	return &v1.DeactivateModelResponse{}, nil
}

func (s *IMS) checkModelAvailability(ctx context.Context, modelID string) (codes.Code, error) {
	ctx = auth.CarryMetadata(ctx)
	if _, err := s.modelClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: modelID,
	}); err != nil {
		if status.Code(err) == codes.NotFound {
			return codes.InvalidArgument, fmt.Errorf("model not found: %s", modelID)
		}
		return codes.Internal, fmt.Errorf("failed to get model: %s", err)
	}
	return codes.OK, nil
}

// fakeAuthInto sets dummy user info and token into the context.
func fakeAuthInto(ctx context.Context) context.Context {
	// Set dummy user info and token
	ctx = auth.AppendUserInfoToContext(ctx, auth.UserInfo{
		OrganizationID: defaultOrganizationID,
		ProjectID:      defaultProjectID,
		AssignedKubernetesEnvs: []auth.AssignedKubernetesEnv{
			{
				ClusterID: defaultClusterID,
				Namespace: defaultNamespace,
			},
		},
		TenantID: defaultTenantID,
	})
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer token"))
	return ctx
}
