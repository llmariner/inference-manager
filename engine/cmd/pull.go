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
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

func pullCmd() *cobra.Command {
	var o opts
	var path string
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "pull",
		RunE: func(cmd *cobra.Command, args []string) error {
			if o.index != 0 {
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
	if o.runtime != runtime.RuntimeNameOllama {
		return fmt.Errorf("unsupported runtime: %s", o.runtime)
	}

	if isRegistered, err := isModelRegistered(o.modelID); err != nil {
		return fmt.Errorf("check model registration: %s", err)
	} else if isRegistered {
		log.Printf("Model %s is already registered", o.modelID)
		return nil
	}

	conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(&c))
	if err != nil {
		return err
	}
	mgr := ollama.New("dummy", c.ModelContextLengths)
	syncer, err := modelsyncer.New(
		mgr,
		s3.NewClient(c.ObjectStore.S3),
		mv1.NewModelsWorkerServiceClient(conn))
	if err != nil {
		return fmt.Errorf("create model syncer: %s", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		if err := mgr.Run(); err != nil {
			log.Printf("Error: Run model syncer: %s", err)
		} else {
			log.Printf("Model manager has been stopped")
		}
		cancel()
	}()

	ctx = auth.AppendWorkerAuthorization(ctx)
	return syncer.PullModel(ctx, o.modelID)
}

func isModelRegistered(modelID string) (bool, error) {
	const modelDir = "/models/manifests/registry.ollama.ai/library/"
	if _, err := os.Stat(filepath.Join(modelDir, modelID)); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("stat file: %s", err)
	}
	return true, nil
}
