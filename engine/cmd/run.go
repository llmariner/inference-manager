package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"time"

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
	"github.com/llmariner/rbac-manager/pkg/auth"
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
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const loraReconciliationInterval = 30 * time.Second

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

	if err := auth.ValidateClusterRegistrationKey(); err != nil {
		return err
	}

	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Logger:                 logger,
		HealthProbeBindAddress: fmt.Sprintf(":%d", c.HealthPort),
		Metrics:                metricsserver.Options{BindAddress: fmt.Sprintf(":%d", c.MetricsPort)},
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

	var leaderElection bool
	var collector metrics.Collector
	var scaler autoscaler.Registerer
	if c.Autoscaler.Enable {
		var as autoscaler.Autoscaler
		switch c.Autoscaler.Type {
		case autoscaler.TypeBuiltin:
			leaderElection = true
			mClient := metrics.NewClient(c.Autoscaler.Builtin.MetricsWindow)
			collector = mClient
			as = autoscaler.NewBuiltinScaler(c.Autoscaler.Builtin, mClient)
		case autoscaler.TypeKeda:
			collector = &metrics.PromMetricsCollector{}
			as = autoscaler.NewKedaScaler(c.Autoscaler.Keda, ns)
		default:
			return fmt.Errorf("unknown autoscaler type: %q", c.Autoscaler.Type)
		}
		if err := as.SetupWithManager(mgr); err != nil {
			return err
		}
		scaler = as
	} else {
		collector = &metrics.NoopCollector{}
		scaler = &autoscaler.NoopRegisterer{}
	}

	if c.ComponentStatusSender.Enable {
		label := fmt.Sprintf("app.kubernetes.io/name=%s", "runtime")
		pss, err := status.NewPodStatusSender(c.ComponentStatusSender, ns, label, grpcOption(c), logger)
		if err != nil {
			return err
		}
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
	ollamaClient := runtime.NewOllamaClient(
		mgr.GetClient(),
		ns,
		owner,
		&c.Runtime,
		processedConfig,
		c.Ollama,
		modelClient,
	)

	var (
		addrGetter  processor.AddressGetter
		modelSyncer processor.ModelSyncer
		modelPuller runtime.ModelPuller
	)

	errCh := make(chan error)
	if c.Ollama.DynamicModelLoading {
		pullerAddr := fmt.Sprintf("%s:%d", ollamaClient.GetName(""), c.Ollama.PullerPort)
		ollamaManager := runtime.NewOllamaManager(mgr.GetClient(), ollamaClient, scaler, pullerAddr)
		if err := ollamaManager.SetupWithManager(mgr, leaderElection); err != nil {
			return err
		}
		addrGetter = ollamaManager
		modelSyncer = ollamaManager
		modelPuller = ollamaManager

	} else {
		rtClientFactory := &clientFactory{
			config: c,
			clients: map[string]runtime.Client{
				config.RuntimeNameOllama: ollamaClient,
				config.RuntimeNameVLLM: runtime.NewVLLMClient(
					mgr.GetClient(),
					ns,
					owner,
					&c.Runtime,
					processedConfig,
					modelClient,
					&c.VLLM,
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

		rtManager := runtime.NewManager(
			mgr.GetClient(),
			rtClientFactory,
			scaler,
			modelClient,
			c.VLLM,
		)
		if err := rtManager.SetupWithManager(mgr, leaderElection); err != nil {
			return err
		}
		addrGetter = rtManager
		modelSyncer = rtManager
		modelPuller = rtManager

		updater := runtime.NewUpdater(ns, rtClientFactory)
		if err := updater.SetupWithManager(mgr); err != nil {
			return err
		}

		if c.VLLM.DynamicLoRALoading {
			r := runtime.NewLoRAReconciler(
				mgr.GetClient(),
				rtManager,
				&runtime.LoRAAdapterStatusGetter{},
			)
			if err := r.SetupWithManager(mgr); err != nil {
				return err
			}

			go func() {
				errCh <- r.Run(ctx, loraReconciliationInterval)
			}()
		}
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
		addrGetter,
		modelSyncer,
		logger,
		collector,
		c.GracefulShutdownTimeout,
	)
	if err := p.SetupWithManager(mgr, leaderElection); err != nil {
		return err
	}

	preloader := runtime.NewPreloader(modelPuller, processedConfig.PreloadedModelIDs(), modelClient)
	if err := preloader.SetupWithManager(mgr); err != nil {
		return err
	}

	bootLog.Info("Starting manager")
	go func() {
		err := mgr.Start(signals.SetupSignalHandler())
		if err != nil {
			logger.Error(err, "Manager exited non-zero")
		}
		errCh <- err
	}()

	return <-errCh
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
