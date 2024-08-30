package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-logr/stdr"
	"github.com/llm-operator/common/pkg/id"
	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/health"
	"github.com/llm-operator/inference-manager/engine/internal/manager"
	"github.com/llm-operator/inference-manager/engine/internal/metrics"
	"github.com/llm-operator/inference-manager/engine/internal/modelsyncer"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/processor"
	"github.com/llm-operator/inference-manager/engine/internal/s3"
	"github.com/llm-operator/inference-manager/engine/internal/vllm"
	"github.com/llm-operator/inference-manager/pkg/llmkind"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

const (
	flagConfig = "config"

	modelPreloadConcurrency = 2
)

func runCmd() *cobra.Command {
	var logLevel int

	cmd := &cobra.Command{
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

			if err := run(cmd.Context(), &c, logLevel); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().String(flagConfig, "", "Configuration file path")
	cmd.Flags().IntVar(&logLevel, "v", 0, "Log level")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func run(ctx context.Context, c *config.Config, lv int) error {
	stdr.SetVerbosity(lv)
	logger := stdr.New(log.Default())

	llmAddr := fmt.Sprintf("0.0.0.0:%d", c.LLMPort)
	var m manager.M
	switch c.LLMEngine {
	case llmkind.Ollama:
		if err := os.Setenv("OLLAMA_HOST", llmAddr); err != nil {
			return err
		}
		if err := ollama.SetEnvVarsFromConfig(c.Ollama); err != nil {
			return err
		}

		// TODO(kenji): Remove this once we fix existing deployments.
		if err := ollama.DeleteOrphanedRunnersDir(c.Ollama); err != nil {
			return err
		}

		m = ollama.New(c.FormattedModelContextLengths())
	case llmkind.VLLM:
		m = vllm.New(c, "")
	default:
		return fmt.Errorf("unsupported llm engine: %q", c.LLMEngine)
	}
	errCh := make(chan error)

	go func() {
		errCh <- m.Run()
	}()

	healthHandler := health.NewProbeHandler()
	healthHandler.AddProbe(m)
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/ready", healthHandler.ProbeHandler)
	srv := http.Server{
		Addr:    fmt.Sprintf(":%d", c.HealthPort),
		Handler: healthMux,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()

	if err := m.WaitForReady(); err != nil {
		return err
	}

	var syncer processor.ModelSyncer
	if c.Debug.Standalone || c.LLMEngine == "vllm" {
		syncer = processor.NewFakeModelSyncer()
	} else {
		sc := s3.NewClient(c.ObjectStore.S3)

		conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(c))
		if err != nil {
			return err
		}
		mc := mv1.NewModelsWorkerServiceClient(conn)
		syncer, err = modelsyncer.New(m, sc, mc)
		if err != nil {
			return err
		}
	}

	engineID, err := id.GenerateID("engine_", 24)
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(c.InferenceManagerServerWorkerServiceAddr, grpcOption(c))
	if err != nil {
		return err
	}
	p := processor.NewP(
		engineID,
		v1.NewInferenceWorkerServiceClient(conn),
		processor.NewFixedAddressGetter(llmAddr),
		c.LLMEngine,
		syncer,
		logger,
		metrics.NewClient(),
	)

	healthHandler.AddProbe(p)

	go func() {
		errCh <- p.Run(ctx)
	}()

	if ids := c.FormattedPreloadedModelIDs(); len(ids) > 0 {
		go func() {
			log.Printf("Preloading %d model(s)", len(ids))
			ctx := auth.AppendWorkerAuthorization(ctx)
			g, ctx := errgroup.WithContext(ctx)
			g.SetLimit(modelPreloadConcurrency)
			for _, id := range ids {
				g.Go(func() error {
					log.Printf("Preloading model %q", id)
					if err := syncer.PullModel(ctx, id); err != nil {
						return err
					}
					log.Printf("Completed the preloading of %q", id)
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				errCh <- err
			}
		}()
	}

	return <-errCh
}

func grpcOption(c *config.Config) grpc.DialOption {
	if c.Worker.TLS.Enable {
		return grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	}
	return grpc.WithTransportCredentials(insecure.NewCredentials())
}
