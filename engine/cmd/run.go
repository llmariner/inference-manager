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
	"github.com/llm-operator/inference-manager/engine/internal/modelsyncer"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/processor"
	"github.com/llm-operator/inference-manager/engine/internal/s3"
	mv1 "github.com/llm-operator/model-manager/api/v1"
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
	ollamaAddr := fmt.Sprintf("0.0.0.0:%d", c.Ollama.Port)
	if err := os.Setenv("OLLAMA_HOST", ollamaAddr); err != nil {
		return err
	}
	if err := os.Setenv("OLLAMA_KEEP_ALIVE", c.Ollama.KeepAlive.String()); err != nil {
		return err
	}

	om := ollama.NewManager(ollamaAddr)

	errCh := make(chan error)

	go func() {
		errCh <- om.Run()
	}()

	var syncer processor.ModelSyncer
	if c.Debug.Standalone {
		syncer = processor.NewFakeModelSyncer()
	} else {

		sc := s3.NewClient(c.ObjectStore.S3)

		conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(c))
		if err != nil {
			return err
		}
		mc := mv1.NewModelsWorkerServiceClient(conn)
		syncer = modelsyncer.New(om, sc, mc)
	}

	if err := om.WaitForReady(); err != nil {
		return err
	}

	for _, id := range c.PreloadedModelIDs {
		log.Printf("Preloading model %q", id)
		if err := syncer.PullModel(ctx, id); err != nil {
			return err
		}
		log.Printf("Completed preloading model %q", id)
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
		ollamaAddr,
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
