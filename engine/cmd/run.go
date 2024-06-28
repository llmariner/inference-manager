package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/modelsyncer"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/s3"
	"github.com/llm-operator/inference-manager/engine/internal/server"
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
	addr := fmt.Sprintf("0.0.0.0:%d", c.OllamaPort)
	if err := os.Setenv("OLLAMA_HOST", addr); err != nil {
		return err
	}
	om := ollama.NewManager(addr)

	errCh := make(chan error)

	go func() {
		errCh <- om.Run()
	}()

	var syncer server.ModelSyncer
	if c.Debug.Standalone {
		syncer = server.NewFakeModelSyncer()
	} else {

		sc := s3.NewClient(c.ObjectStore.S3)

		conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(c))
		if err != nil {
			return err
		}
		mc := mv1.NewModelsWorkerServiceClient(conn)
		syncer = modelsyncer.New(om, sc, mc)
	}

	go func() {
		s := server.New(syncer)
		errCh <- s.Run(c.InternalGRPCPort)
	}()

	if err := om.WaitForReady(); err != nil {
		return err
	}

	if c.Debug.Standalone {
		for _, b := range c.Debug.BaseModels {
			ob, err := ollama.ConvertHuggingFaceModelNameToOllama(b)
			if err != nil {
				return err
			}
			if err := om.PullBaseModel(ob); err != nil {
				return err
			}
		}
		log.Printf("Finished pulling base models\n")
	}

	conn, err := grpc.NewClient(c.InferenceManagerServerWorkerServiceAddr, grpcOption(c))
	if err != nil {
		return err
	}
	// TODO(kenji): Use the client
	_ = v1.NewInferenceWorkerServiceClient(conn)

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
