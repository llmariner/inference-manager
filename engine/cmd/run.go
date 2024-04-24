package main

import (
	"context"
	"log"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/modelsyncer"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/s3"
	"github.com/llm-operator/inference-manager/engine/internal/server"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
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
	errCh := make(chan error)

	om := ollama.NewManager(c.OllamaPort)
	go func() {
		errCh <- om.Run()
	}()

	go func() {
		s := server.New(om)
		errCh <- s.Run(c.InternalGRPCPort)
	}()

	if err := om.WaitForReady(); err != nil {
		return err
	}

	for _, b := range c.BaseModels {
		ob, err := ollama.ConvertHuggingFaceModelNameToOllama(b)
		if err != nil {
			return err
		}
		if err := om.PullBaseModel(ob); err != nil {
			return err
		}
	}
	log.Printf("Finished pulling base models\n")

	if !c.Debug.Standalone {
		sc := s3.NewClient(c.ObjectStore.S3)

		conn, err := grpc.Dial(c.ModelManagerServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return err
		}
		mc := mv1.NewModelsServiceClient(conn)

		conn, err = grpc.Dial(c.ModelManagerInternalServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return err
		}
		mic := mv1.NewModelsInternalServiceClient(conn)

		s := modelsyncer.New(om, sc, mc, mic)
		go func() {
			errCh <- s.Run(ctx)
		}()
	}

	return <-errCh
}

func init() {
	runCmd.Flags().StringP(flagConfig, "c", "", "Configuration file path")
	_ = runCmd.MarkFlagRequired(flagConfig)
}
