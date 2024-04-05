package main

import (
	"context"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/server"
	"github.com/spf13/cobra"
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

	om := ollama.NewManager()
	go func() {
		errCh <- om.Run(c.OllamaPort)
	}()

	go func() {
		s := server.New(om)
		errCh <- s.Run(c.InternalGRPCPort)
	}()

	return <-errCh
}

func init() {
	runCmd.Flags().StringP(flagConfig, "c", "", "Configuration file path")
	_ = runCmd.MarkFlagRequired(flagConfig)
}
