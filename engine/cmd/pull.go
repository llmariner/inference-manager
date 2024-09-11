package main

import (
	"context"
	"log"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/modeldownloader"
	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/llm-operator/inference-manager/engine/internal/runtime"
	"github.com/llm-operator/inference-manager/engine/internal/s3"
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
	s3Client, err := s3.NewClient(ctx, c.ObjectStore.S3)
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(&c))
	if err != nil {
		return err
	}
	mClient := mv1.NewModelsWorkerServiceClient(conn)

	// TODO(kenji): Support non-base model.
	ctx = auth.AppendWorkerAuthorization(ctx)
	resp, err := mClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: o.modelID,
	})
	if err != nil {
		return err
	}

	d := modeldownloader.New(runtime.ModelDir(), s3Client)

	format, err := runtime.PreferredModelFormat(o.runtime, resp.Formats)
	if err != nil {
		return err
	}
	if err := d.Download(ctx, o.modelID, resp, format); err != nil {
		return err
	}

	if o.runtime == config.RuntimeNameOllama {
		filePath := ollama.ModelfilePath(runtime.ModelDir(), o.modelID)
		log.Printf("Creating an Ollama modelfile at %q\n", filePath)
		modelPath, err := modeldownloader.ModelFilePath(runtime.ModelDir(), o.modelID, format)
		if err != nil {
			return err
		}
		spec := &ollama.ModelSpec{
			From: modelPath,
		}
		if err := ollama.CreateModelfile(filePath, o.modelID, spec, c.ModelContextLengths); err != nil {
			return err
		}
		log.Printf("Successfully created the Ollama modelfile\n")
	}

	return nil
}
