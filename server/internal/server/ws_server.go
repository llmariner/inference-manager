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
	defaultAPIKeyID  = "default-key-id"
)

// NewWorkerServiceServer creates a new worker service server.
func NewWorkerServiceServer(infProcessor *infprocessor.P, logger logr.Logger) *WS {
	return &WS{
		logger:       logger.WithName("worker"),
		infProcessor: infProcessor,
	}
}

// WS is a server for worker services.
type WS struct {
	v1.UnimplementedInferenceWorkerServiceServer

	srv *grpc.Server

	logger       logr.Logger
	infProcessor *infprocessor.P

	enableAuth bool
}

// Run runs the worker service server.
func (ws *WS) Run(ctx context.Context, port int, authConfig config.AuthConfig, tlsConfig *config.TLS) error {
	ws.logger.Info("Starting WS server...", "port", port)

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	return ws.RunWithListener(ctx, authConfig, tlsConfig, l)
}

// RunWithListener runs the server with a given listener.
func (ws *WS) RunWithListener(ctx context.Context, authConfig config.AuthConfig, tlsConfig *config.TLS, l net.Listener) error {
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

// GracefulStop gracefully stops the worker service server.
func (ws *WS) GracefulStop() {
	ws.srv.GracefulStop()
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

func (ws *WS) processTasks(srv v1.InferenceWorkerService_ProcessTasksServer) error {
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

		var engineID string

		req, err := srv.Recv()
		if err != nil {
			if err != io.EOF {
				ws.logger.Error(err, "processMessagesFromEngine error", "engineID", engineID)
			}
			return err
		}

		switch msg := req.Message.(type) {
		case *v1.ProcessTasksRequest_EngineStatus:
			ws.logger.Info("Received engine status", "engineID", msg.EngineStatus.EngineId)
			msg.EngineStatus.ClusterId = clusterInfo.ClusterID
			ws.infProcessor.AddOrUpdateEngineStatus(srv, msg.EngineStatus, clusterInfo.TenantID, true /* isLocal */)
			engineID = msg.EngineStatus.EngineId
		case *v1.ProcessTasksRequest_TaskResult:
			ws.infProcessor.ProcessTaskResult(msg.TaskResult)
		default:
			return fmt.Errorf("unknown message type: %T", msg)
		}

		if !registered && engineID != "" {
			defer func() {
				ws.infProcessor.RemoveEngine(engineID, clusterInfo.TenantID)
				ws.logger.Info("Unregistered engine", "engineID", engineID)
				// TODO(kenji): Wait until all sends complete before closing.
			}()
			registered = true
		}
	}
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
