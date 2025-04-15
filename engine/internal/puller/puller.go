package puller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	"github.com/llmariner/inference-manager/engine/internal/s3"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
)

// New creates a new puller.
func New(
	c *config.Config,
	mClient mv1.ModelsWorkerServiceClient,
	s3Client *s3.Client,
) *P {
	return &P{
		c:        c,
		mClient:  mClient,
		s3Client: s3Client,
	}
}

// P is a puller.
type P struct {
	c        *config.Config
	mClient  mv1.ModelsWorkerServiceClient
	s3Client *s3.Client
}

// PullOpts contains options for pulling a model.
type PullOpts struct {
	Runtime string
	ModelID string
}

// Pull pulls the model from the Model Manager Server.
func (p *P) Pull(
	ctx context.Context,
	o PullOpts,
) error {
	ctx = auth.AppendWorkerAuthorization(ctx)

	model, err := p.mClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: o.ModelID,
	})
	if err != nil {
		return err
	}

	if model.IsBaseModel {
		return p.pullBaseModel(ctx, o)
	}

	if err := p.pullFineTunedModel(ctx, o); err != nil {
		return err
	}
	return nil
}

// RunServer starts an HTTP server that listens for pull requests.
func (p *P) RunServer(
	ctx context.Context,
	c *config.Config,
	runtimeName string,
	port int,
	modelID string,
) error {
	const queueLengths = 5
	pullCh := make(chan string, queueLengths)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case modelID := <-pullCh:
				if err := p.Pull(ctx, PullOpts{
					ModelID: modelID,
					Runtime: runtimeName,
				}); err != nil {
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

func (p *P) pullBaseModel(
	ctx context.Context,
	o PullOpts,
) error {
	resp, err := p.mClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: o.ModelID,
	})
	if err != nil {
		return err
	}

	d := modeldownloader.New(ModelDir(), p.s3Client)

	format, err := PreferredModelFormat(o.Runtime, resp.Formats)
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

	if err := d.Download(ctx, o.ModelID, srcPath, format, mv1.AdapterType_ADAPTER_TYPE_UNSPECIFIED); err != nil {
		return err
	}

	log.Printf("Successfully pulled the model %q\n", o.ModelID)

	if o.Runtime != config.RuntimeNameOllama || format == mv1.ModelFormat_MODEL_FORMAT_OLLAMA {
		// No need to create a model file.
		return nil
	}

	// Create a modelfile for Ollama.

	filePath := ollama.ModelfilePath(ModelDir(), o.ModelID)
	log.Printf("Creating an Ollama modelfile at %q\n", filePath)
	modelPath, err := modeldownloader.ModelFilePath(ModelDir(), o.ModelID, format)
	if err != nil {
		return err
	}
	spec := &ollama.ModelSpec{
		From: modelPath,
	}

	mci := config.NewProcessedModelConfig(p.c).ModelConfigItem(o.ModelID)
	if err := ollama.CreateModelfile(filePath, o.ModelID, spec, mci.ContextLength); err != nil {
		return err
	}
	log.Printf("Successfully created the Ollama modelfile\n")

	return nil
}

func (p *P) pullFineTunedModel(
	ctx context.Context,
	o PullOpts,
) error {
	attr, err := p.mClient.GetModelAttributes(ctx, &mv1.GetModelAttributesRequest{
		Id: o.ModelID,
	})
	if err != nil {
		return err
	}

	if attr.BaseModel == "" {
		return fmt.Errorf("base model ID is not set for %q", o.ModelID)
	}
	if err := p.pullBaseModel(ctx, PullOpts{ModelID: attr.BaseModel, Runtime: o.Runtime}); err != nil {
		return err
	}

	resp, err := p.mClient.GetBaseModelPath(ctx, &mv1.GetBaseModelPathRequest{
		Id: attr.BaseModel,
	})
	if err != nil {
		return err
	}

	format, err := PreferredModelFormat(o.Runtime, resp.Formats)
	if err != nil {
		return err
	}

	d := modeldownloader.New(ModelDir(), p.s3Client)

	if err := d.Download(ctx, o.ModelID, attr.Path, format, attr.Adapter); err != nil {
		return err
	}

	log.Printf("Successfully pulled the fine-tuning adapter\n")

	if o.Runtime != config.RuntimeNameOllama {
		return nil
	}

	filePath := ollama.ModelfilePath(ModelDir(), o.ModelID)
	log.Printf("Creating an Ollama modelfile at %q\n", filePath)

	adapterPath, err := modeldownloader.ModelFilePath(ModelDir(), o.ModelID, format)
	if err != nil {
		return err
	}
	spec := &ollama.ModelSpec{
		From:        attr.BaseModel,
		AdapterPath: adapterPath,
	}
	mci := config.NewProcessedModelConfig(p.c).ModelConfigItem(o.ModelID)
	if err := ollama.CreateModelfile(filePath, o.ModelID, spec, mci.ContextLength); err != nil {
		return err
	}
	log.Printf("Successfully created the Ollama modelfile\n")

	return nil
}
