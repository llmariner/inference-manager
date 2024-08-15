package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/llm-operator/common/pkg/id"
	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/health"
	"github.com/llm-operator/inference-manager/engine/internal/manager"
	"github.com/llm-operator/inference-manager/engine/internal/modelsyncer"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/processor"
	"github.com/llm-operator/inference-manager/engine/internal/s3"
	"github.com/llm-operator/inference-manager/engine/internal/vllm"
	"github.com/llm-operator/inference-manager/pkg/llmkind"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
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
	llmAddr := fmt.Sprintf("0.0.0.0:%d", c.LLMPort)
	var m manager.M
	switch c.LLMEngine {
	case llmkind.Ollama:
		if err := os.Setenv("OLLAMA_HOST", llmAddr); err != nil {
			return err
		}
		if c.Ollama.ForceSpreading {
			log.Printf("Ollama Force spreading is enabled\n")
			if err := os.Setenv("OLLAMA_SCHED_SPREAD", "true"); err != nil {
				return err
			}
		}
		if c.Ollama.Debug {
			log.Printf("Ollama Debug is enabled\n")
			if err := os.Setenv("OLLAMA_DEBUG", "true"); err != nil {
				return err
			}
		}
		m = ollama.New(llmAddr)
	case llmkind.VLLM:
		m = vllm.New(c)
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
		llmAddr,
		c.LLMEngine,
		syncer,
	)

	healthHandler.AddProbe(p)

	go func() {
		errCh <- p.Run(ctx)
	}()

	if ids := c.PreloadedModelIDs; len(ids) > 0 {
		log.Printf("Preloading %d model(s)", len(ids))
		ctx := auth.AppendWorkerAuthorization(ctx)
		for _, id := range ids {
			log.Printf("Preloading model %q", id)
			if err := syncer.PullModel(ctx, id); err != nil {
				return err
			}
		}
		log.Printf("Completed the preloading")
	}

	return <-errCh
}

func grpcOption(c *config.Config) grpc.DialOption {
	if c.Worker.TLS.Enable {
		return grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	}
	return grpc.WithTransportCredentials(insecure.NewCredentials())
}

func init() {
	runCmd.Flags().StringP(flagConfig, "c", "", "Configuration file path")
	_ = runCmd.MarkFlagRequired(flagConfig)
}
