package main

import (
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"github.com/llmariner/inference-manager/engine/internal/puller"
	"github.com/llmariner/inference-manager/engine/internal/s3"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

// pullCmd creates a new pull command.
// pull command pulls a specified model from the s3 and registers it to the runtime.
// If the model is already registered, this command does nothing.
func pullCmd() *cobra.Command {
	var (
		index      int
		modelID    string
		runtime    string
		path       string
		forcePull  bool
		daemonMode bool
	)
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "pull",
		RunE: func(cmd *cobra.Command, args []string) error {
			if index != 0 && !forcePull {
				log.Printf("Skip initializing (INDEX: %d)", index)
				return nil
			}

			c, err := config.Parse(path)
			if err != nil {
				return err
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGTERM)
			defer cancel()

			s3Client, err := s3.NewClient(ctx, c.ObjectStore.S3)
			if err != nil {
				return err
			}
			conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(c))
			if err != nil {
				return err
			}
			mClient := mv1.NewModelsWorkerServiceClient(conn)

			p := puller.New(
				config.NewProcessedModelConfig(c),
				runtime,
				mClient,
				modeldownloader.New(puller.ModelDir(), s3Client),
			)

			if !daemonMode {
				// Check if the model ID is set on the non daemon mode.
				// In the daemon mode, the model is optional and pre-pulled
				// only if the model ID is set.
				if modelID == "" {
					return fmt.Errorf("model ID must be set on non daemon mode")
				}

				return p.Pull(ctx, modelID)
			}

			var pullerPort int
			switch runtime {
			case config.RuntimeNameOllama:
				pullerPort = c.Ollama.PullerPort
			case config.RuntimeNameVLLM:
				pullerPort = c.VLLM.PullerPort
			default:
				return fmt.Errorf("daemonmode unsupported runtime: %q", runtime)
			}
			if pullerPort <= 0 {
				return fmt.Errorf("puller port must be set on the daemon mode")
			}

			srv := puller.NewServer(p)

			errCh := make(chan error)
			go func() {
				errCh <- srv.Start(pullerPort)
			}()

			go func() {
				errCh <- srv.ProcessPullRequests(ctx)
			}()

			if modelID != "" {
				srv.QueuePullRequest(modelID)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-errCh:
				return err
			}
		},
	}
	cmd.Flags().IntVar(&index, "index", 0, "Index of the pod")
	cmd.Flags().StringVar(&runtime, "runtime", "", "Runtime name for the model")
	cmd.Flags().StringVar(&modelID, "model-id", "", "Model ID to be registered")
	cmd.Flags().StringVar(&path, "config", "", "Path to the config file")
	cmd.Flags().BoolVar(&forcePull, "force-pull", false, "Pull the model even if its index is not 0")
	cmd.Flags().BoolVar(&daemonMode, "daemon-mode", false, "Run the server in the daemon mode (only available for the ollama model)")
	_ = cmd.MarkFlagRequired("index")
	_ = cmd.MarkFlagRequired("runtime")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}
