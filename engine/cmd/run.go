package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"

	"github.com/go-logr/logr"
	"github.com/go-logr/stdr"
	"github.com/llmariner/cluster-manager/pkg/status"
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
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

			return run(cmd.Context(), c, ns, logLevel)
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

	label := fmt.Sprintf("app.kubernetes.io/name=%s", "runtime")
	pss, err := status.NewPodStatusSender(c.ComponentStatusSender, ns, label, grpcOption(c), logger)
	if err != nil {
		return err
	}
	if c.ComponentStatusSender.Enable {
		go func() {
			pss.Run(logr.NewContext(ctx, logger))
		}()
	}

	conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(c))
	if err != nil {
		return err
	}

	owner, err := getOwnerReference(context.Background(), mgr.GetAPIReader(), ns)
	if err != nil {
		return fmt.Errorf("get owner reference: %s", err)
	}

	processedConfig := config.NewProcessedModelConfig(c)
	modelClient := mv1.NewModelsWorkerServiceClient(conn)
	rtClientFactory := &clientFactory{
		config: c,
		clients: map[string]runtime.Client{
			config.RuntimeNameOllama: runtime.NewOllamaClient(
				mgr.GetClient(),
				ns,
				owner,
				&c.Runtime,
				processedConfig,
				c.Ollama,
				modelClient,
			),
			config.RuntimeNameVLLM: runtime.NewVLLMClient(
				mgr.GetClient(),
				ns,
				owner,
				&c.Runtime,
				processedConfig,
				modelClient,
			),
			config.RuntimeNameTriton: runtime.NewTritonClient(
				mgr.GetClient(),
				ns,
				owner,
				&c.Runtime,
				processedConfig,
			),
		},
	}

	rtManager := runtime.NewManager(mgr.GetClient(), rtClientFactory, scaler)
	if err := rtManager.SetupWithManager(mgr, c.Autoscaler.Enable); err != nil {
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
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		logger.Error(err, "Manager exited non-zero")
		return err
	}
	return nil
}

type noopScaler struct{}

func (n *noopScaler) Register(modelID string, target types.NamespacedName) {
}

func (n *noopScaler) Unregister(target types.NamespacedName) {
}

type clientFactory struct {
	config  *config.Config
	clients map[string]runtime.Client
}

func (f *clientFactory) New(modelID string) (runtime.Client, error) {
	mci := config.NewProcessedModelConfig(f.config).ModelConfigItem(modelID)
	c, ok := f.clients[mci.RuntimeName]
	if !ok {
		return nil, fmt.Errorf("unknown runtime name: %q", mci.RuntimeName)
	}
	return c, nil

}

func grpcOption(c *config.Config) grpc.DialOption {
	if c.Worker.TLS.Enable {
		return grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	}
	return grpc.WithTransportCredentials(insecure.NewCredentials())
}

// getOwnerReference returns this inference engine as an OwnerReference.
func getOwnerReference(ctx context.Context, c client.Reader, ns string) (*metav1apply.OwnerReferenceApplyConfiguration, error) {
	appName, ok := os.LookupEnv("APP_NAME")
	if !ok {
		return nil, fmt.Errorf("missing APP_NAME")
	}
	var app appsv1.Deployment
	if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: ns}, &app); err != nil {
		return nil, err
	}
	return metav1apply.OwnerReference().
		WithAPIVersion("apps/v1").
		WithKind("Deployment").
		WithName(app.GetName()).
		WithUID(app.GetUID()).
		WithBlockOwnerDeletion(true).
		WithController(true), nil
}
