package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/llm-operator/inference-manager/server/internal/config"
	"github.com/llm-operator/inference-manager/server/internal/infprocessor"
	"github.com/llm-operator/inference-manager/server/internal/monitoring"
	"github.com/llm-operator/inference-manager/server/internal/rag"
	"github.com/llm-operator/inference-manager/server/internal/router"
	"github.com/llm-operator/inference-manager/server/internal/server"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	vsv1 "github.com/llm-operator/vector-store-manager/api/v1"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
)

const flagConfig = "config"

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "run",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := cmd.Flags().GetString(flagConfig)
		if err != nil {
			return err
		}

		c, err := config.Parse(path)
		if err != nil {
			return err
		}

		if err := c.Validate(); err != nil {
			return err
		}

		if err := run(cmd.Context(), &c); err != nil {
			return err
		}
		return nil
	},
}

func run(ctx context.Context, c *config.Config) error {
	errCh := make(chan error)

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
		options := grpc.WithTransportCredentials(insecure.NewCredentials())
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
		rwt = rag.NewR(c.AuthConfig.Enable, vsInternalClient)
	}

	m := monitoring.NewMetricsMonitor()
	defer m.UnregisterAllCollectors()

	queue := infprocessor.NewTaskQueue()
	s := server.New(m, mclient, vsClient, rwt, queue)

	createFile := runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1, 2, 2},
			[]string{"v1", "chat", "completions"},
			"",
		))
	mux.Handle("POST", createFile, s.CreateChatCompletion)

	go func() {
		log.Printf("Starting HTTP server on port %d", c.HTTPPort)
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", c.HTTPPort), mux)
	}()

	go func() {
		log.Printf("Starting metrics server on port %d", c.MonitoringPort)
		monitorMux := http.NewServeMux()
		monitorMux.Handle("/metrics", promhttp.Handler())
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", c.MonitoringPort), monitorMux)

	}()

	go func() {
		errCh <- s.Run(ctx, c.GRPCPort, c.AuthConfig)
	}()

	r := router.New()
	infProcessor := infprocessor.NewP(queue, r)
	go func() {
		errCh <- infProcessor.Run(ctx)
	}()

	go func() {
		s := server.NewWorkerServiceServer(infProcessor)
		errCh <- s.Run(ctx, c.WorkerServiceGRPCPort, c.AuthConfig)
	}()

	return <-errCh
}

func init() {
	runCmd.Flags().StringP(flagConfig, "c", "", "Configuration file path")
	_ = runCmd.MarkFlagRequired(flagConfig)
}
