package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
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
	var daemonMode bool
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

			if daemonMode {
				if c.Ollama.PullerPort == 0 {
					return fmt.Errorf("socket path must be set on the daemon mode")
				}
				if o.runtime != config.RuntimeNameOllama {
					return fmt.Errorf("daemon mode is only available for the ollama")
				}
			} else {
				// Check if the model ID is set on the non daemon mode.
				// In the daemon mode, the model is optional and pre-pulled
				// only if the model ID is set.
				if o.modelID == "" {
					return fmt.Errorf("model ID must be set on non daemon mode")
				}
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGTERM)
			defer cancel()
			if !daemonMode {
				return pull(ctx, o, c)
			}
			return runServer(ctx, c, c.Ollama.PullerPort, o.modelID)
		},
	}
	cmd.Flags().IntVar(&o.index, "index", 0, "Index of the pod")
	cmd.Flags().StringVar(&o.runtime, "runtime", "", "Runtime name for the model")
	cmd.Flags().StringVar(&o.modelID, "model-id", "", "Model ID to be registered")
	cmd.Flags().StringVar(&path, "config", "", "Path to the config file")
	cmd.Flags().BoolVar(&forcePull, "force-pull", false, "Pull the model even if its index is not 0")
	cmd.Flags().BoolVar(&daemonMode, "daemon-mode", false, "Run the server in the daemon mode (only available for the ollama model)")
	_ = cmd.MarkFlagRequired("index")
	_ = cmd.MarkFlagRequired("runtime")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

type pullModelRequest struct {
	ModelID string `json:"modelID"`
}

func runServer(ctx context.Context, c *config.Config, port int, modelID string) error {
	const queueLengths = 5
	pullCh := make(chan string, queueLengths)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case modelID := <-pullCh:
				if err := pull(ctx, opts{
					modelID: modelID,
					runtime: config.RuntimeNameOllama,
				}, c); err != nil {
					log.Printf("Failed to pull the model: %v\n", err)
				}
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/pull", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Unsupported method", http.StatusMethodNotAllowed)
			return
		}
		var req pullModelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Failed to decode the request: %v\n", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.ModelID == "" {
			http.Error(w, "Model ID must be set", http.StatusBadRequest)
			return
		}
		select {
		case pullCh <- req.ModelID:
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusTooManyRequests)
		}
	})

	srv := http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		log.Printf("Starting HTTP server at %q\n", srv.Addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("serve: %v\n", err)
		}
	}()

	if modelID != "" {
		pullCh <- modelID
	}

	<-ctx.Done()
	log.Printf("Shutting down HTTP server...\n")
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("http server shutdown: %s", err)
	}
	log.Printf("Shutdown has finished\n")
	return nil
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

	model, err := mClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: o.modelID,
	})
	if err != nil {
		return err
	}

	if model.IsBaseModel {
		return pullBaseModel(ctx, o, c, mClient, s3Client)
	}

	if err := pullFineTunedModel(ctx, o, c, mClient, s3Client); err != nil {
		return err
	}
	return nil
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

	log.Printf("Successfully pulled the model %q\n", o.modelID)

	if o.runtime != config.RuntimeNameOllama || format == mv1.ModelFormat_MODEL_FORMAT_OLLAMA {
		// No need to create a model file.
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

func pullFineTunedModel(
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

	if o.runtime != config.RuntimeNameOllama {
		return nil
	}

	filePath := ollama.ModelfilePath(runtime.ModelDir(), o.modelID)
	log.Printf("Creating an Ollama modelfile at %q\n", filePath)

	adapterPath, err := modeldownloader.ModelFilePath(runtime.ModelDir(), o.modelID, format)
	if err != nil {
		return err
	}
	spec := &ollama.ModelSpec{
		From:        attr.BaseModel,
		AdapterPath: adapterPath,
	}
	mci := config.NewProcessedModelConfig(c).ModelConfigItem(o.modelID)
	if err := ollama.CreateModelfile(filePath, o.modelID, spec, mci.ContextLength); err != nil {
		return err
	}
	log.Printf("Successfully created the Ollama modelfile\n")

	return nil
}
