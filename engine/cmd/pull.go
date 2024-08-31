package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/modelsyncer"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/runtime"
	"github.com/llm-operator/inference-manager/engine/internal/s3"
	"github.com/llm-operator/inference-manager/engine/internal/vllm"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

// pullCmd creates a new pull command.
// pull command pulls a specified model from the s3 and registers it to the runtime.
// If the model is already registered, this command does nothing.
func pullCmd() *cobra.Command {
	var o opts
	var path string
	var forcePull bool
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "pull",
		RunE: func(cmd *cobra.Command, args []string) error {
			if o.index != 0 && !forcePull {
				log.Printf("Skip initializing (INDEX: %d)", o.index)
				return nil
			}

			c, err := config.Parse(path)
			if err != nil {
				return err
			}

			return pull(cmd.Context(), o, c)
		},
	}
	cmd.Flags().IntVar(&o.index, "index", 0, "Index of the pod")
	cmd.Flags().StringVar(&o.runtime, "runtime", "", "Runtime name for the model")
	cmd.Flags().StringVar(&o.modelID, "model-id", "", "Model ID to be registered")
	cmd.Flags().StringVar(&path, "config", "", "Path to the config file")
	cmd.Flags().BoolVar(&forcePull, "force-pull", false, "Pull the model even if its index is not 0")
	_ = cmd.MarkFlagRequired("index")
	_ = cmd.MarkFlagRequired("runtime")
	_ = cmd.MarkFlagRequired("model-id")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

type opts struct {
	index   int
	runtime string
	modelID string
}

func pull(ctx context.Context, o opts, c config.Config) error {
	s3Client := s3.NewClient(c.ObjectStore.S3)

	var mgr modelsyncer.ModelManager

	done := make(chan error)
	switch o.runtime {
	case runtime.RuntimeNameOllama:
		if isRegistered, err := isModelRegistered(o.modelID); err != nil {
			return fmt.Errorf("check model registration: %s", err)
		} else if isRegistered {
			log.Printf("Model %s is already registered", o.modelID)
			return nil
		}
		omgr := ollama.New(c.FormattedModelContextLengths(), s3Client)
		go func() { done <- omgr.Run() }()
		mgr = omgr
	case runtime.RuntimeNameVLLM:
		// TODO(kenji): Check if a model already exists.
		mgr = vllm.New(&c, runtime.ModelDir(), s3Client)
	default:
		return fmt.Errorf("invalid runtime: %s", o.runtime)
	}

	conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(&c))
	if err != nil {
		return err
	}

	syncer, err := modelsyncer.New(
		mgr,
		s3Client,
		mv1.NewModelsWorkerServiceClient(conn))
	if err != nil {
		return fmt.Errorf("create model syncer: %s", err)
	}

	go func() {
		ctx = auth.AppendWorkerAuthorization(ctx)
		done <- syncer.PullModel(ctx, o.modelID)
	}()
	return <-done
}

func isModelRegistered(modelID string) (bool, error) {
	modelDir := filepath.Join(runtime.ModelDir(), "manifests/registry.ollama.ai/library/")
	if _, err := os.Stat(filepath.Join(modelDir, modelID)); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("stat file: %s", err)
	}
	return true, nil
}
