package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/go-logr/logr"
	"github.com/llmariner/common/pkg/certlib/store"
	v1 "github.com/llmariner/inference-manager/api/v1"
	v1legacy "github.com/llmariner/inference-manager/api/v1/legacy"
	"github.com/llmariner/inference-manager/server/internal/config"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
	"github.com/llmariner/rbac-manager/pkg/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const (
	defaultClusterID = "default"
	defaultProjectID = "default"
	defaultTenantID  = "default-tenant-id"
)

// NewWorkerServiceServer creates a new worker service server.
func NewWorkerServiceServer(infProcessor *infprocessor.P, logger logr.Logger) *WS {
	return &WS{
		logger:       logger.WithName("worker"),
		infProcessor: infProcessor,
	}
}

type legacyService struct {
	v1legacy.UnimplementedInferenceWorkerServiceServer

	ws *WS
}

// ProcessTasks processes tasks.
func (ls *legacyService) ProcessTasks(srv v1legacy.InferenceWorkerService_ProcessTasksServer) error {
	return ls.ws.ProcessTasks(srv)
}

// WS is a server for worker services.
type WS struct {
	v1.UnimplementedInferenceWorkerServiceServer

	srv *grpc.Server

	logger       logr.Logger
	infProcessor *infprocessor.P

	enableAuth bool

	legacyService legacyService
}

// Run runs the worker service server.
func (ws *WS) Run(ctx context.Context, port int, authConfig config.AuthConfig, tlsConfig *config.TLS) error {
	ws.logger.Info("Starting WS server...", "port", port)

	var opts []grpc.ServerOption
	if authConfig.Enable {
		ai, err := auth.NewWorkerInterceptor(ctx, auth.WorkerConfig{
			RBACServerAddr: authConfig.RBACInternalServerAddr,
		})
		if err != nil {
			return err
		}
		opts = append(opts, grpc.StreamInterceptor(ai.Stream()))
		ws.enableAuth = true
	}

	// Create a reloading TLS certificate store to pick up any updates to the
	// TLS certificates.
	//
	// We only need this for the worker service as the rest of the endpoints are under an ingress controller that terminates TLS
	// before requests hit the endpoints.
	//
	// The worker service endpoint is different from the rest of the endpoints as they can be directly hit by clients (as
	// an ingress controller might not support gRPC streaming).
	if tlsConfig != nil {
		c, err := ws.buildTLSConfig(ctx, tlsConfig)
		if err != nil {
			return err
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(c)))
	}

	srv := grpc.NewServer(opts...)
	v1.RegisterInferenceWorkerServiceServer(srv, ws)
	reflection.Register(srv)

	ws.srv = srv

	ws.legacyService.ws = ws
	v1legacy.RegisterInferenceWorkerServiceServer(srv, &ws.legacyService)

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if err := srv.Serve(l); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	ws.logger.Info("Stopped WS server")
	return nil
}

// Stop stops the worker service server.
func (ws *WS) Stop() {
	ws.srv.Stop()
}

func (ws *WS) extractClusterInfoFromContext(ctx context.Context) (*auth.ClusterInfo, error) {
	if !ws.enableAuth {
		return &auth.ClusterInfo{
			ClusterID: defaultClusterID,
			TenantID:  defaultTenantID,
		}, nil
	}
	clusterInfo, ok := auth.ExtractClusterInfoFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "cluster info not found")
	}
	return clusterInfo, nil
}

// ProcessTasks processes tasks.
func (ws *WS) ProcessTasks(srv v1.InferenceWorkerService_ProcessTasksServer) error {
	return ws.processTasks(srv)
}

type serverInterface interface {
	Context() context.Context
	Send(*v1.ProcessTasksResponse) error
	Recv() (*v1.ProcessTasksRequest, error)
}

func (ws *WS) processTasks(srv serverInterface) error {
	clusterInfo, err := ws.extractClusterInfoFromContext(srv.Context())
	if err != nil {
		return err
	}

	var registered bool
	for {
		// Check if the context is done with a non-blocking select.
		ctx := srv.Context()
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		engineID, err := ws.processMessagesFromEngine(srv, clusterInfo)
		if err != nil {
			if err != io.EOF {
				ws.logger.Error(err, "processMessagesFromEngine error")
			}
			return err
		}
		if !registered && engineID != "" {
			defer func() {
				if err := ws.infProcessor.RemoveEngine(engineID, clusterInfo); err != nil {
					ws.logger.Error(err, "RemoveEngine error", "engineID", engineID)
				}
				ws.logger.Info("Unregistered engine", "engineID", engineID)
			}()
			registered = true
		}
	}
}

func (ws *WS) processMessagesFromEngine(
	srv serverInterface,
	clusterInfo *auth.ClusterInfo,
) (string, error) {
	req, err := srv.Recv()
	if err != nil {
		return "", err
	}

	var engineID string
	switch msg := req.Message.(type) {
	case *v1.ProcessTasksRequest_EngineStatus:
		ws.logger.Info("Received engine status", "engineID", msg.EngineStatus.EngineId)
		if err := ws.infProcessor.AddOrUpdateEngineStatus(srv, msg.EngineStatus, clusterInfo); err != nil {
			return "", err
		}
		engineID = msg.EngineStatus.EngineId
	case *v1.ProcessTasksRequest_TaskResult:
		if err := ws.infProcessor.ProcessTaskResult(msg.TaskResult, clusterInfo); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unknown message type: %T", msg)
	}

	return engineID, nil
}

func (ws *WS) buildTLSConfig(ctx context.Context, tlsConfig *config.TLS) (*tls.Config, error) {
	st, err := store.NewReloadingFileStore(store.ReloadingFileStoreOpts{
		KeyPath:  tlsConfig.Key,
		CertPath: tlsConfig.Cert,
	})
	if err != nil {
		return nil, err
	}

	go func() {
		ws.logger.Info("Starting reloading certificate store")
		if err := st.Run(ctx); err != nil {
			ws.logger.Error(err, "run certificate store reloader")
			// Ensure we fail fast if the cert store can not be reloaded.
			os.Exit(1)
		}
	}()

	var cipherSuites []uint16
	// CipherSuites returns only secure ciphers.
	for _, c := range tls.CipherSuites() {
		cipherSuites = append(cipherSuites, c.ID)
	}

	return &tls.Config{
		GetCertificate: st.GetCertificateFunc(),
		// Support v1.2 as at least intruder.io needs v1.2 to run its scan.
		MinVersion: tls.VersionTLS12,
		// Exclude insecure ciphers.
		CipherSuites: cipherSuites,
	}, nil
}
