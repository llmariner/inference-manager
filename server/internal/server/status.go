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

// NewInferenceStatusServer creates a new inference status server.
func NewInferenceStatusServer(
	infProcessor *infprocessor.P,
	logger logr.Logger,
) *ISS {
	return &ISS{
		infProcessor: infProcessor,
		logger:       logger.WithName("inference status server"),
	}
}

// ISS is a server for inference status services.
type ISS struct {
	v1.UnimplementedInferenceServiceServer

	// keyed by tenant ID and cluster ID.
	tenantStatuses map[string]map[string][]*v1.EngineStatus
	mu             sync.RWMutex

	infProcessor *infprocessor.P
	logger       logr.Logger

	srv *grpc.Server
}

// Run runs the inference status server.
func (s *ISS) Run(ctx context.Context, authConfig config.AuthConfig, port int) error {
	s.logger.Info("Starting infernce server...", "port", port)

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %s", err)
	}
	return s.RunWithListener(ctx, authConfig, l)
}

// RunWithListener runs the server with a given listener.
func (s *ISS) RunWithListener(ctx context.Context, authConfig config.AuthConfig, l net.Listener) error {
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
func (s *ISS) Stop() {
	s.srv.Stop()
}

// Refresh refreshes the inference status.
func (s *ISS) Refresh(ctx context.Context, interval time.Duration) error {
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

func (s *ISS) refresh() {
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
				Ready:     e.Ready,
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
func (s *ISS) GetInferenceStatus(ctx context.Context, req *v1.GetInferenceStatusRequest) (*v1.InferenceStatus, error) {
	userInfo, ok := auth.ExtractUserInfoFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user info not found")
	}

	s.mu.Lock()
	ts := s.tenantStatuses[userInfo.TenantID]
	s.mu.Unlock()

	var css []*v1.ClusterStatus
	tasks := make(map[string]int32)
	for _, env := range userInfo.AssignedKubernetesEnvs {
		es, ok := ts[env.ClusterID]
		if !ok {
			continue
		}
		for _, e := range es {
			for _, m := range e.Models {
				tasks[m.Id] += m.InProgressTaskCount
			}
		}
		css = append(css, &v1.ClusterStatus{
			Id:             env.ClusterID,
			Name:           env.ClusterName,
			EngineStatuses: es,
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
