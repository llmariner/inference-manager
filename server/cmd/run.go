package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-logr/stdr"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/llm-operator/inference-manager/server/internal/admin"
	"github.com/llm-operator/inference-manager/server/internal/config"
	"github.com/llm-operator/inference-manager/server/internal/infprocessor"
	"github.com/llm-operator/inference-manager/server/internal/monitoring"
	"github.com/llm-operator/inference-manager/server/internal/rag"
	"github.com/llm-operator/inference-manager/server/internal/router"
	"github.com/llm-operator/inference-manager/server/internal/server"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
	vsv1 "github.com/llm-operator/vector-store-manager/api/v1"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/encoding/protojson"
)

const monitoringRunnerInterval = 10 * time.Second

func runCmd() *cobra.Command {
	var path string
	var logLevel int
	cmd := &cobra.Command{
		Use:   "run",
		Short: "run",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Parse(path)
			if err != nil {
				return err
			}
			if err := c.Validate(); err != nil {
				return err
			}

			if err := run(cmd.Context(), &c, logLevel); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "config", "", "Path to the config file")
	cmd.Flags().IntVar(&logLevel, "v", 0, "Log level")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func run(ctx context.Context, c *config.Config, lv int) error {
	stdr.SetVerbosity(lv)
	logger := stdr.New(log.Default())

	errCh := make(chan error)

	options := grpc.WithTransportCredentials(insecure.NewCredentials())
	conn, err := grpc.NewClient(fmt.Sprintf(":%d", c.GRPCPort), options)
	if err != nil {
		return err
	}
	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			// Do not use the camel case for JSON fields to follow OpenAI API.
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:     true,
				EmitDefaultValues: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		}),
		runtime.WithIncomingHeaderMatcher(auth.HeaderMatcher),
		runtime.WithHealthzEndpoint(grpc_health_v1.NewHealthClient(conn)),
	)
	// TODO(kenji): Call v1.RegisterChatServiceHandlerFromEndpoint once the gRPC method is defined
	// with gRPC gateway.

	var mclient server.ModelClient
	var vsClient server.VectorStoreClient
	var rwt server.Rewriter
	if c.Debug.UseNoopClient {
		mclient = &server.NoopModelClient{}
		vsClient = &server.NoopVectorStoreClient{}
		rwt = &server.NoopRewriter{}
	} else {
		conn, err := grpc.NewClient(c.ModelManagerServerAddr, options)
		if err != nil {
			return err
		}
		mclient = mv1.NewModelsServiceClient(conn)

		conn, err = grpc.NewClient(c.VectorStoreManagerServerAddr, options)
		if err != nil {
			return err
		}
		vsClient = vsv1.NewVectorStoreServiceClient(conn)

		conn, err = grpc.NewClient(c.VectorStoreManagerInternalServerAddr, options)
		if err != nil {
			return err
		}
		vsInternalClient := vsv1.NewVectorStoreInternalServiceClient(conn)
		rwt = rag.NewR(c.AuthConfig.Enable, vsInternalClient, logger)
	}

	r := router.New()
	infProcessor := infprocessor.NewP(r, logger)
	go func() {
		errCh <- infProcessor.Run(ctx)
	}()

	m := monitoring.NewMetricsMonitor(infProcessor, logger)
	go func() {
		errCh <- m.Run(ctx, monitoringRunnerInterval)
	}()

	defer m.UnregisterAllCollectors()

	grpcSrv := server.New(m, mclient, vsClient, rwt, infProcessor, logger)

	pat := runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1, 2, 2},
			[]string{"v1", "chat", "completions"},
			"",
		))
	mux.Handle("POST", pat, grpcSrv.CreateChatCompletion)

	pat = runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1},
			[]string{"v1", "completions"},
			"",
		))
	mux.Handle("POST", pat, grpcSrv.CreateCompletion)

	pat = runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1},
			[]string{"v1", "embeddings"},
			"",
		))
	mux.Handle("POST", pat, grpcSrv.CreateEmbedding)

	go func() {
		log := logger.WithName("http")
		log.Info("Starting HTTP server...", "port", c.HTTPPort)
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", c.HTTPPort), mux)
		log.Info("Stopped HTTP server")
	}()

	go func() {
		log := logger.WithName("metrics")
		log.Info("Starting metrics server...", "port", c.MonitoringPort)
		monitorMux := http.NewServeMux()
		monitorMux.Handle("/metrics", promhttp.Handler())
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", c.MonitoringPort), monitorMux)
		log.Info("Stopped metrics server")
	}()

	go func() {
		errCh <- grpcSrv.Run(ctx, c.GRPCPort, c.AuthConfig)
	}()

	go func() {
		wsSrv := server.NewWorkerServiceServer(infProcessor, logger)
		errCh <- wsSrv.Run(ctx, c.WorkerServiceGRPCPort, c.AuthConfig, c.WorkerServiceTLS)
	}()

	go func() {
		adminSrv := admin.NewHandler(infProcessor, logger)
		errCh <- adminSrv.Run(c.AdminPort)
	}()

	return <-errCh
}
