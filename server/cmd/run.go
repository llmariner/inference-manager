package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-logr/stdr"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/llmariner/api-usage/pkg/sender"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/server/internal/admin"
	"github.com/llmariner/inference-manager/server/internal/config"
	"github.com/llmariner/inference-manager/server/internal/heartbeater"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
	"github.com/llmariner/inference-manager/server/internal/monitoring"
	"github.com/llmariner/inference-manager/server/internal/rag"
	"github.com/llmariner/inference-manager/server/internal/rate"
	"github.com/llmariner/inference-manager/server/internal/router"
	"github.com/llmariner/inference-manager/server/internal/server"
	"github.com/llmariner/inference-manager/server/internal/taskexchanger"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
	vsv1 "github.com/llmariner/vector-store-manager/api/v1"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/encoding/protojson"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	monitoringRunnerInterval                = 10 * time.Second
	taskExchangerServerStatusUpdateInterval = 10 * time.Second
)

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

			podName := os.Getenv("POD_NAME")
			if podName == "" {
				return fmt.Errorf("missing POD_NAME")
			}

			ns, ok := os.LookupEnv("NAMESPACE")
			if !ok {
				return fmt.Errorf("missing NAMESPACE")
			}

			if err := run(cmd.Context(), &c, podName, ns, logLevel); err != nil {
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

func run(ctx context.Context, c *config.Config, podName, ns string, lv int) error {
	stdr.SetVerbosity(lv)
	logger := stdr.New(log.Default())
	log := logger.WithName("boot")
	ctx = ctrl.LoggerInto(ctx, log)
	ctrl.SetLogger(logger)

	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		LeaderElection:   c.KubernetesManager.EnableLeaderElection,
		LeaderElectionID: c.KubernetesManager.LeaderElectionID,
		Metrics: metricsserver.Options{
			BindAddress: c.KubernetesManager.MetricsBindAddress,
		},
		HealthProbeBindAddress: c.KubernetesManager.HealthBindAddress,
		PprofBindAddress:       c.KubernetesManager.PprofBindAddress,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{ns: {}},
		},
		GracefulShutdownTimeout: ptr.To(c.GracefulShutdownTimeout),
	})
	if err != nil {
		return err
	}
	if c.KubernetesManager.HealthBindAddress != "" {
		if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
			return err
		}
	}

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

	infProcessor := infprocessor.NewP(router.New(c.RequestRouting.EnableDynamicModelLoading), logger)
	go func() {
		errCh <- infProcessor.Run(ctx)
	}()

	if c.EngineHeartbeat.Enable {
		log.Info("Starting engine heartbeat")
		heartbeater := heartbeater.New(infProcessor, c.EngineHeartbeat, logger)
		go func() {
			errCh <- heartbeater.Run(ctx)
		}()
	}

	m := monitoring.NewMetricsMonitor(infProcessor, logger)
	go func() {
		errCh <- m.Run(ctx, monitoringRunnerInterval)
	}()
	infProcessor.SetMetricsMonitor(m)

	defer m.UnregisterAllCollectors()

	var usageSetter sender.UsageSetter
	if c.UsageSender.Enable {
		usage, err := sender.New(ctx, c.UsageSender, options, logger)
		if err != nil {
			return err
		}
		go func() { usage.Run(ctx) }()
		usageSetter = usage
	} else {
		usageSetter = sender.NoopUsageSetter{}
	}

	ratelimiter := rate.NewLimiter(c.RateLimit, logger)

	grpcSrv := server.New(m, usageSetter, ratelimiter, mclient, vsClient, rwt, infProcessor, c.NIMModels, logger)

	registerHTTPHandlers(grpcSrv, mux)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

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

	wsSrv := server.NewWorkerServiceServer(infProcessor, logger)
	go func() {
		errCh <- wsSrv.Run(ctx, c.WorkerServiceGRPCPort, c.AuthConfig, c.WorkerServiceTLS)
	}()

	te := taskexchanger.NewE(
		infProcessor,
		mgr.GetClient(),
		c.InternalGRPCPort,
		podName,
		c.ServerPodLabelKey,
		c.ServerPodLabelValue,
		taskExchangerServerStatusUpdateInterval,
		logger,
	)
	if err := te.SetupWithManager(mgr); err != nil {
		return err
	}

	go func() {
		s := server.NewInternalServer(infProcessor, te, logger)
		errCh <- s.Run(ctx, c.InternalGRPCPort)
	}()

	go func() {
		adminSrv := admin.NewHandler(infProcessor, logger)
		errCh <- adminSrv.Run(c.AdminPort)
	}()

	go func() {
		log.Info("Starting manager")
		// Do not set a singal handler for the manager so that
		// we can stop the task exchanger after gracefully shutting down task
		// processing.
		errCh <- mgr.Start(ctx)
	}()

	imux := runtime.NewServeMux(
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
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := v1.RegisterInferenceServiceHandlerFromEndpoint(ctx, imux, fmt.Sprintf("localhost:%d", c.ManagementGRPCPort), opts); err != nil {
		return err
	}

	ims := server.NewInferenceManagementServer(infProcessor, logger)
	go func() {
		log := logger.WithName("inference management server")
		log.Info("Starting inference management server...", "port", c.ManagementPort)
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", c.ManagementPort), imux)
		log.Info("Stopped inference management server")
	}()
	go func() {
		errCh <- ims.Run(ctx, c.AuthConfig, c.ManagementGRPCPort)
	}()

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		// Wait until the endpoints are updated. Once the endpoints are updated,
		// engines will not reconnect to this server. Also new tasks will not be
		// directly coming to this server. (Tasks will still come from other servers via task exchanger.)
		log.Info("Got signal. Waiting for graceful shutdown", "signal", sig, "delay", c.GracefulShutdownDelay)
		time.Sleep(c.GracefulShutdownDelay)

		// Make local engines connect to other serves. Engines continue to accept new tasks and
		// send task results until all inflight tasks complete.
		log.Info("Sending GoAway task to local engines", "inProgressTaskCount", infProcessor.NumInProgressTasks())
		cctx, cancel := context.WithTimeout(ctx, c.GracefulShutdownDelay)
		defer cancel()
		if err := infProcessor.SendGoAwayTaskToLocalEngines(cctx); err != nil {
			log.Error(err, "Failed to send go away task to local engines")
		}
		// Wait until the engine successfully reconnects to the other servers.
		log.Info("Waiting for graceful shutdown", "delay", c.GracefulShutdownDelay, "inProgressTaskCount", infProcessor.NumInProgressTasks())
		time.Sleep(c.GracefulShutdownDelay)

		// Make the task exchanger stop accepting new requests.
		log.Info("Starting graceful shutdown of task exchanger", "inProgressTaskCount", infProcessor.NumInProgressTasks())
		te.StartGracefulShutdown()

		log.Info("Waiting for graceful shutdown", "delay", c.GracefulShutdownDelay, "inProgressTaskCount", infProcessor.NumInProgressTasks())
		time.Sleep(c.GracefulShutdownDelay)

		// Wait until all in-flight tasks complete.
		for {
			n := infProcessor.NumInProgressTasks()
			if n == 0 {
				log.Info("No in-flight task")
				break
			}
			log.Info("Waiting for all in-flight tasks to complete", "inProgressTaskCount", n)
			time.Sleep(100 * time.Millisecond)
		}

		log.Info("Starting graceful shutdown of gRPC servers", "inProgressTaskCount", infProcessor.NumInProgressTasks())
		grpcSrv.GracefulStop()
		wsSrv.GracefulStop()

		return nil
	}
}

func registerHTTPHandlers(s *server.S, mux *runtime.ServeMux) {
	pat := runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1, 2, 2},
			[]string{"v1", "chat", "completions"},
			"",
		))
	mux.Handle("POST", pat, s.CreateChatCompletion)

	pat = runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1},
			[]string{"v1", "completions"},
			"",
		))
	mux.Handle("POST", pat, s.CreateCompletion)

	pat = runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1},
			[]string{"v1", "embeddings"},
			"",
		))
	mux.Handle("POST", pat, s.CreateEmbedding)

	pat = runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1, 2, 2},
			[]string{"v1", "audio", "transcriptions"},
			"",
		))
	mux.Handle("POST", pat, s.CreateAudioTranscription)

	pat = runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1},
			[]string{"v1", "responses"},
			"",
		))
	mux.Handle("POST", pat, s.CreateModelResponse)

	pat = runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1},
			[]string{"v1", "tokenize"},
			"",
		))
	mux.Handle("POST", pat, s.Tokenize)
}
