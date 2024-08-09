package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/llm-operator/common/pkg/certlib/store"
	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/server/internal/config"
	"github.com/llm-operator/inference-manager/server/internal/infprocessor"
	"github.com/llm-operator/rbac-manager/pkg/auth"
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
func NewWorkerServiceServer(infProcessor *infprocessor.P) *WS {
	return &WS{
		infProcessor: infProcessor,
	}
}

// WS is a server for worker services.
type WS struct {
	v1.UnimplementedInferenceWorkerServiceServer

	srv *grpc.Server

	infProcessor *infprocessor.P

	enableAuth bool
}

// Run runs the worker service server.
func (ws *WS) Run(ctx context.Context, port int, authConfig config.AuthConfig, tlsConfig *config.TLS) error {
	log.Printf("Starting worker service server on port %d", port)

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
		c, err := buildTLSConfig(ctx, tlsConfig)
		if err != nil {
			return err
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(c)))
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
				log.Printf("processTasks error: %s\n", err)
			}
			return err
		}
		if !registered && engineID != "" {
			defer func() {
				ws.infProcessor.RemoveEngine(engineID, clusterInfo)
				log.Printf("Unregistered engine: %s\n", engineID)
			}()
			registered = true
		}
	}
}

func (ws *WS) processMessagesFromEngine(
	srv v1.InferenceWorkerService_ProcessTasksServer,
	clusterInfo *auth.ClusterInfo,
) (string, error) {
	req, err := srv.Recv()
	if err != nil {
		return "", err
	}

	var engineID string
	switch msg := req.Message.(type) {
	case *v1.ProcessTasksRequest_EngineStatus:
		log.Printf("Received engine status: engineID=%s\n", msg.EngineStatus.EngineId)
		ws.infProcessor.AddOrUpdateEngineStatus(srv, msg.EngineStatus, clusterInfo)
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

func buildTLSConfig(ctx context.Context, tlsConfig *config.TLS) (*tls.Config, error) {
	st, err := store.NewReloadingFileStore(store.ReloadingFileStoreOpts{
		KeyPath:  tlsConfig.Key,
		CertPath: tlsConfig.Cert,
	})
	if err != nil {
		return nil, err
	}

	go func() {
		log.Printf("Starting reloading certificate store.\n")
		if err := st.Run(ctx); err != nil {
			// Ensure we fail fast if the cert store can not be reloaded.
			log.Fatalf("Server run: run certificate store reloader: %s\n", err)
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
