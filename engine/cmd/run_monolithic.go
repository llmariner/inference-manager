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
	"github.com/llm-operator/inference-manager/engine/internal/modelsyncer"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/processor"
	"github.com/llm-operator/inference-manager/engine/internal/s3"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	flagConfig = "config"

	modelPreloadConcurrency = 2
)

// runCmd creates a command for running the inference manager and models in a monolithic fashion.
func runMonoCmd() *cobra.Command {
	var logLevel int

	cmd := &cobra.Command{
		Use:   "run_monolithic",
		Short: "run_monolithic",
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

			if err := runMono(cmd.Context(), &c, logLevel); err != nil {
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

func runMono(ctx context.Context, c *config.Config, lv int) error {
	stdr.SetVerbosity(lv)
	logger := stdr.New(log.Default())

	s3Client, err := s3.NewClient(ctx, c.ObjectStore.S3)
	if err != nil {
		return err
	}

	llmAddr := fmt.Sprintf("0.0.0.0:%d", c.LLMPort)
	if err := os.Setenv("OLLAMA_HOST", llmAddr); err != nil {
		return err
	}
	if err := ollama.SetEnvVarsFromConfig(c.Ollama); err != nil {
		return err
	}

	m := ollama.New(c.FormattedModelContextLengths(), s3Client)

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
	if c.Debug.Standalone {
		syncer = processor.NewFakeModelSyncer()
	} else {
		conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(c))
		if err != nil {
			return err
		}
		mc := mv1.NewModelsWorkerServiceClient(conn)
		syncer, err = modelsyncer.New(m, s3Client, mc)
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
		syncer,
		logger,
		&processor.NoopMetricsCollector{},
	)

	healthHandler.AddProbe(p)

	go func() {
		errCh <- p.Start(ctx)
	}()

	if ids := c.FormattedPreloadedModelIDs(); len(ids) > 0 {
		go func() {
			log.Printf("Preloading %d model(s)", len(ids))
			ctx := auth.AppendWorkerAuthorization(ctx)
			ctx = ctrl.LoggerInto(ctx, logger)
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
