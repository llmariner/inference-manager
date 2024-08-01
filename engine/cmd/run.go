package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"

	"github.com/llm-operator/common/pkg/id"
	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/manager"
	"github.com/llm-operator/inference-manager/engine/internal/modelsyncer"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/processor"
	"github.com/llm-operator/inference-manager/engine/internal/s3"
	"github.com/llm-operator/inference-manager/engine/internal/vllm"
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
	var llmAddr string
	var m manager.M
	switch c.LLMEngine {
	case "ollama":
		llmAddr = fmt.Sprintf("0.0.0.0:%d", c.Ollama.Port)
		if err := os.Setenv("OLLAMA_HOST", llmAddr); err != nil {
			return err
		}
		m = ollama.New(llmAddr)

	case "vllm":
		llmAddr = fmt.Sprintf("0.0.0.0:%d", c.VLLM.Port)
		m = vllm.New(c)
	default:
		return fmt.Errorf("unsupported llm engine: %q", c.LLMEngine)
	}

	errCh := make(chan error)

	go func() {
		errCh <- m.Run()
	}()

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
		syncer = modelsyncer.New(m, sc, mc)
	}

	if err := m.WaitForReady(); err != nil {
		return err
	}

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

	go func() {
		errCh <- p.Run(ctx)
	}()

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
