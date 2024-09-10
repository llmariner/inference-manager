package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-logr/stdr"
	"github.com/llm-operator/common/pkg/id"
	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/engine/internal/autoscaler"
	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/metrics"
	"github.com/llm-operator/inference-manager/engine/internal/processor"
	"github.com/llm-operator/inference-manager/engine/internal/runtime"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/types"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
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

			ns, ok := os.LookupEnv("NAMESPACE")
			if !ok {
				return fmt.Errorf("missing NAMESPACE")
			}

			return run(cmd.Context(), &c, ns, logLevel)
		},
	}
	cmd.Flags().StringVar(&path, "config", "", "Path to the config file")
	cmd.Flags().IntVar(&logLevel, "v", 0, "Log level")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func run(ctx context.Context, c *config.Config, ns string, lv int) error {
	stdr.SetVerbosity(lv)
	logger := stdr.New(log.Default())
	ctx = ctrl.LoggerInto(ctx, logger)
	bootLog := logger.WithName("boot")

	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Logger:                 logger,
		HealthProbeBindAddress: fmt.Sprintf(":%d", c.HealthPort),
		ReadinessEndpointName:  "/ready",
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{ns: {}},
		},
	})
	if err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("manager", healthz.Ping); err != nil {
		return err
	}

	mClient := metrics.NewClient(c.Autoscaler.MetricsWindow)
	var scaler runtime.ScalerRegisterer
	if c.Autoscaler.Enable {
		mas := autoscaler.NewMultiAutoscaler(mgr.GetClient(), mClient, c.Autoscaler)
		if err := mas.SetupWithManager(mgr); err != nil {
			return err
		}
		scaler = mas
	} else {
		scaler = &noopScaler{}
	}

	conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(c))
	if err != nil {
		return err
	}

	var rtClient runtime.Client
	switch c.Runtime.Name {
	case runtime.RuntimeNameOllama:
		rtClient = runtime.NewOllamaClient(mgr.GetClient(), ns, c.Runtime, c.Ollama)
	case runtime.RuntimeNameVLLM:
		rtClient = runtime.NewVLLMClient(
			mgr.GetClient(),
			ns,
			c.Runtime,
			c.VLLM,
			c.FormattedModelContextLengths(),
			mv1.NewModelsWorkerServiceClient(conn),
		)
	default:
		return fmt.Errorf("invalid llm engine: %q", c.LLMEngine)
	}

	rtManager := runtime.NewManager(mgr.GetClient(), rtClient, scaler)
	if err := rtManager.Initialize(ctx, mgr.GetAPIReader(), ns); err != nil {
		return err
	}
	if err := rtManager.SetupWithManager(mgr); err != nil {
		return err
	}

	engineID, err := id.GenerateID("engine_", 24)
	if err != nil {
		return err
	}

	conn, err = grpc.NewClient(c.InferenceManagerServerWorkerServiceAddr, grpcOption(c))
	if err != nil {
		return err
	}
	wsClient := v1.NewInferenceWorkerServiceClient(conn)

	p := processor.NewP(
		engineID,
		wsClient,
		rtManager,
		c.LLMEngine,
		rtManager,
		logger,
		mClient,
	)

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		bootLog.Info("Starting manager")
		return mgr.Start(ctx)
	})
	g.Go(func() error {
		bootLog.Info("Starting processor")
		return p.Run(ctx)
	})

	if ids := c.FormattedPreloadedModelIDs(); len(ids) > 0 {
		bootLog.Info("Preloading models", "count", len(ids))
		if err := preloadModels(ctx, rtManager, ids); err != nil {
			return err
		}
	}

	return g.Wait()
}

func preloadModels(ctx context.Context, rtManager *runtime.Manager, ids []string) error {
	ctx = auth.AppendWorkerAuthorization(ctx)
	const preloadingParallelism = 3

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(preloadingParallelism)
	for _, id := range ids {
		g.Go(func() error { return rtManager.PullModel(ctx, id) })
	}
	return g.Wait()
}

type noopScaler struct{}

func (n *noopScaler) Register(modelID string, target types.NamespacedName) {
}

func (n *noopScaler) Unregister(target types.NamespacedName) {
}
