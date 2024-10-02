package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"

	"github.com/go-logr/stdr"
	"github.com/llmariner/common/pkg/id"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/engine/internal/autoscaler"
	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/metrics"
	"github.com/llmariner/inference-manager/engine/internal/processor"
	"github.com/llmariner/inference-manager/engine/internal/runtime"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/apimachinery/pkg/types"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
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

			return run(c, ns, logLevel)
		},
	}
	cmd.Flags().StringVar(&path, "config", "", "Path to the config file")
	cmd.Flags().IntVar(&logLevel, "v", 0, "Log level")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func run(c *config.Config, ns string, lv int) error {
	stdr.SetVerbosity(lv)
	logger := stdr.New(log.Default())
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
		GracefulShutdownTimeout:       ptr.To(c.GracefulShutdownTimeout),
		LeaderElection:                true,
		LeaderElectionID:              c.LeaderElection.ID,
		LeaderElectionReleaseOnCancel: true,
		LeaseDuration:                 c.LeaderElection.LeaseDuration,
		RenewDeadline:                 c.LeaderElection.RenewDeadline,
		RetryPeriod:                   c.LeaderElection.RetryPeriod,
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

	processedConfig := config.NewProcessedModelConfig(c)
	modelClient := mv1.NewModelsWorkerServiceClient(conn)
	rtClientFactory := &clientFactory{
		config: c,
		ollamaClient: runtime.NewOllamaClient(
			mgr.GetClient(),
			ns,
			&c.Runtime,
			processedConfig,
			c.Ollama,
			modelClient,
		),
		vllmClient: runtime.NewVLLMClient(
			mgr.GetClient(),
			ns,
			&c.Runtime,
			processedConfig,
			modelClient,
		),
	}

	rtManager := runtime.NewManager(mgr.GetClient(), rtClientFactory, scaler)
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
		rtManager,
		logger,
		mClient,
		c.GracefulShutdownTimeout,
	)
	if err := p.SetupWithManager(mgr, c.Autoscaler.Enable); err != nil {
		return err
	}

	preloader := runtime.NewPreloader(rtManager, processedConfig.PreloadedModelIDs(), modelClient)
	if err := preloader.SetupWithManager(mgr); err != nil {
		return err
	}

	updater := runtime.NewUpdater(ns, rtClientFactory)
	if err := updater.SetupWithManager(mgr); err != nil {
		return err
	}

	bootLog.Info("Starting manager")
	return mgr.Start(signals.SetupSignalHandler())
}

type noopScaler struct{}

func (n *noopScaler) Register(modelID string, target types.NamespacedName) {
}

func (n *noopScaler) Unregister(target types.NamespacedName) {
}

type clientFactory struct {
	config       *config.Config
	ollamaClient runtime.Client
	vllmClient   runtime.Client
}

func (f *clientFactory) New(modelID string) (runtime.Client, error) {
	mci := config.NewProcessedModelConfig(f.config).ModelConfigItem(modelID)
	switch mci.RuntimeName {
	case config.RuntimeNameOllama:
		return f.ollamaClient, nil
	case config.RuntimeNameVLLM:
		return f.vllmClient, nil
	}
	return nil, fmt.Errorf("unknown runtime name: %q", mci.RuntimeName)
}

func grpcOption(c *config.Config) grpc.DialOption {
	if c.Worker.TLS.Enable {
		return grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	}
	return grpc.WithTransportCredentials(insecure.NewCredentials())
}
