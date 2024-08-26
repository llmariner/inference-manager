package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-logr/stdr"
	"github.com/llm-operator/common/pkg/id"
	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/processor"
	"github.com/llm-operator/inference-manager/engine/internal/runtime"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

func alphaCmd() *cobra.Command {
	var path string
	var logLevel int
	cmd := &cobra.Command{
		Use:   "alpha",
		Short: "alpha",
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

			return alphaRun(cmd.Context(), &c, ns, logLevel)
		},
	}
	cmd.Flags().StringVar(&path, "config", "", "Path to the config file")
	cmd.Flags().IntVar(&logLevel, "v", 0, "Log level")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func alphaRun(ctx context.Context, c *config.Config, ns string, lv int) error {
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

	var rtClient runtime.Client
	switch c.Runtime.Name {
	case runtime.RuntimeNameOllama:
		rtClient = runtime.NewOllamaClient(mgr.GetClient(), ns, c.Runtime, c.Ollama)
	case runtime.RuntimeNameVLLM:
		rtClient = runtime.NewVLLMClient(mgr.GetClient(), ns, c.Runtime, c.VLLM)
	default:
		return fmt.Errorf("invalid llm engine: %q", c.LLMEngine)
	}

	rtManager := runtime.NewManager(mgr.GetClient(), rtClient)
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

	conn, err := grpc.NewClient(c.InferenceManagerServerWorkerServiceAddr, grpcOption(c))
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

	if len(c.PreloadedModelIDs) > 0 {
		bootLog.Info("Preloading models", "count", len(c.PreloadedModelIDs))
		if err := preloadModels(ctx, rtManager, c.PreloadedModelIDs); err != nil {
			return err
		}
	}

	return g.Wait()
}

func preloadModels(ctx context.Context, rtManager *runtime.Manager, ids []string) error {
	const preloadingParallelism = 3
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(preloadingParallelism)
	for _, id := range ids {
		g.Go(func() error { return rtManager.PullModel(ctx, id) })
	}
	return g.Wait()
}
