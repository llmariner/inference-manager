package main

import (
	"context"
	"fmt"
	"log"

	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"github.com/llmariner/inference-manager/engine/internal/models"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	"github.com/llmariner/inference-manager/engine/internal/runtime"
	"github.com/llmariner/inference-manager/engine/internal/s3"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
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

func pull(ctx context.Context, o opts, c *config.Config) error {
	s3Client, err := s3.NewClient(ctx, c.ObjectStore.S3)
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(c.ModelManagerServerWorkerServiceAddr, grpcOption(c))
	if err != nil {
		return err
	}
	mClient := mv1.NewModelsWorkerServiceClient(conn)

	ctx = auth.AppendWorkerAuthorization(ctx)

	isBase, err := models.IsBaseModel(ctx, mClient, o.modelID)
	if err != nil {
		return err
	}

	if isBase {
		return pullBaseModel(ctx, o, c, mClient, s3Client)
	}

	switch o.runtime {
	case config.RuntimeNameOllama:
		return pullFineTunedModelForOllama(ctx, o, c, mClient, s3Client)
	case config.RuntimeNameVLLM:
		return pullFineTunedModelForVLLM(ctx, o, c, mClient, s3Client)
	default:
		return fmt.Errorf("unsupported runtime: %s", o.runtime)
	}
}

func pullBaseModel(
	ctx context.Context,
	o opts,
	c *config.Config,
	mClient mv1.ModelsWorkerServiceClient,
	s3Client *s3.Client,
) error {
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

	var srcPath string
	switch format {
	case mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE,
		mv1.ModelFormat_MODEL_FORMAT_OLLAMA,
		mv1.ModelFormat_MODEL_FORMAT_NVIDIA_TRITON:
		srcPath = resp.Path
	case mv1.ModelFormat_MODEL_FORMAT_GGUF:
		srcPath = resp.GgufModelPath
	default:
		return fmt.Errorf("unsupported format: %v", format)
	}

	if err := d.Download(ctx, o.modelID, srcPath, format, mv1.AdapterType_ADAPTER_TYPE_UNSPECIFIED); err != nil {
		return err
	}

	if !(o.runtime == config.RuntimeNameOllama && format == mv1.ModelFormat_MODEL_FORMAT_GGUF) {
		return nil
	}

	// Create a modelfile for Ollama.

	filePath := ollama.ModelfilePath(runtime.ModelDir(), o.modelID)
	log.Printf("Creating an Ollama modelfile at %q\n", filePath)
	modelPath, err := modeldownloader.ModelFilePath(runtime.ModelDir(), o.modelID, format)
	if err != nil {
		return err
	}
	spec := &ollama.ModelSpec{
		From: modelPath,
	}

	mci := config.NewProcessedModelConfig(c).ModelConfigItem(o.modelID)
	if err := ollama.CreateModelfile(filePath, o.modelID, spec, mci.ContextLength); err != nil {
		return err
	}
	log.Printf("Successfully created the Ollama modelfile\n")

	return nil
}

func pullFineTunedModelForOllama(
	ctx context.Context,
	o opts,
	c *config.Config,
	mClient mv1.ModelsWorkerServiceClient,
	s3Client *s3.Client,
) error {
	baseModelID, err := models.ExtractBaseModel(o.modelID)
	if err != nil {
		return err
	}

	if err := pullBaseModel(ctx, opts{modelID: baseModelID, runtime: o.runtime}, c, mClient, s3Client); err != nil {
		return err
	}

	d := modeldownloader.New(runtime.ModelDir(), s3Client)

	mresp, err := mClient.GetModelPath(ctx, &mv1.GetModelPathRequest{
		Id: o.modelID,
	})
	if err != nil {
		return err
	}

	if err := d.DownloadAdapterOfGGUF(ctx, o.modelID, mresp); err != nil {
		return err
	}

	filePath := ollama.ModelfilePath(runtime.ModelDir(), o.modelID)
	log.Printf("Creating an Ollama modelfile at %q\n", filePath)

	adapterPath, err := modeldownloader.AdapterFilePath(runtime.ModelDir(), o.modelID)
	if err != nil {
		return err
	}
	spec := &ollama.ModelSpec{
		From:        baseModelID,
		AdapterPath: adapterPath,
	}
	mci := config.NewProcessedModelConfig(c).ModelConfigItem(o.modelID)
	if err := ollama.CreateModelfile(filePath, o.modelID, spec, mci.ContextLength); err != nil {
		return err
	}
	log.Printf("Successfully created the Ollama modelfile\n")

	return nil
}

func pullFineTunedModelForVLLM(
	ctx context.Context,
	o opts,
	c *config.Config,
	mClient mv1.ModelsWorkerServiceClient,
	s3Client *s3.Client,
) error {
	attr, err := mClient.GetModelAttributes(ctx, &mv1.GetModelAttributesRequest{
		Id: o.modelID,
	})
	if err != nil {
		return err
	}

	if attr.BaseModel == "" {
		return fmt.Errorf("base model ID is not set for %q", o.modelID)
	}
	if err := pullBaseModel(ctx, opts{modelID: attr.BaseModel, runtime: o.runtime}, c, mClient, s3Client); err != nil {
		return err
	}

	resp, err := mClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: attr.BaseModel,
	})
	if err != nil {
		return err
	}

	format, err := runtime.PreferredModelFormat(o.runtime, resp.Formats)
	if err != nil {
		return err
	}

	d := modeldownloader.New(runtime.ModelDir(), s3Client)

	if err := d.Download(ctx, o.modelID, attr.Path, format, attr.Adapter); err != nil {
		return err
	}
	log.Printf("Successfully pulled the fine-tuning adapter\n")

	return nil
}
