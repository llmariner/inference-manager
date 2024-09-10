package modeldownloader

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/llm-operator/inference-manager/engine/internal/huggingface"
	mv1 "github.com/llm-operator/model-manager/api/v1"
)

type s3Client interface {
	Download(ctx context.Context, f io.WriterAt, path string) error
}

// New returns a new Manager.
func New(modelDir string, s3Client s3Client) *D {
	return &D{
		modelDir: modelDir,
		s3Client: s3Client,
	}
}

// D is a downloader.
type D struct {
	modelDir string

	s3Client s3Client
}

// Download downloads the model.
func (d *D) Download(
	ctx context.Context,
	modelName string,
	resp *mv1.GetBaseModelPathResponse,
	format mv1.ModelFormat,
) error {
	// Check if the completion indication file exists. If so, download should have been completed with a previous run. Do not download again.
	completionDir := filepath.Join(d.modelDir, modelName)
	if err := os.MkdirAll(completionDir, 0755); err != nil {
		return err
	}
	completionIndicationFile := filepath.Join(completionDir, "completed.txt")

	if _, err := os.Stat(completionIndicationFile); err == nil {
		log.Printf("The model has already been downloaded. Skipping the download.\n")
		return nil
	}

	destPath, err := ModelFilePath(d.modelDir, modelName, format)
	if err != nil {
		return err
	}

	switch format {
	case mv1.ModelFormat_MODEL_FORMAT_GGUF:
		log.Printf("Downloading the GGUF model from %q\n", resp.GgufModelPath)
		f, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if err := d.s3Client.Download(ctx, f, resp.GgufModelPath); err != nil {
			return fmt.Errorf("download: %s", err)
		}
		log.Printf("Downloaded the model to %q\n", f.Name())
		if err := f.Close(); err != nil {
			return err
		}
	case mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE:
		log.Printf("Downloading the Hugging Face model from %q\n", resp.Path)
		if err := os.MkdirAll(destPath, 0755); err != nil {
			return fmt.Errorf("create directory: %s", err)
		}
		if err := huggingface.DownloadModelFiles(ctx, d.s3Client, resp.Path, destPath); err != nil {
			return fmt.Errorf("download: %s", err)
		}
		log.Printf("Downloaded the model to %q\n", destPath)
	default:
		return fmt.Errorf("unsupported model format: %s", format)
	}

	// Create a file that indicates the completion of model download.
	f, err := os.Create(completionIndicationFile)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	return nil
}

// ModelFilePath returns the file path of the model.
func ModelFilePath(modelDir, modelName string, format mv1.ModelFormat) (string, error) {
	switch format {
	case mv1.ModelFormat_MODEL_FORMAT_GGUF:
		return filepath.Join(modelDir, modelName, "model.gguf"), nil
	case mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE:
		return filepath.Join(modelDir, modelName), nil
	default:
		return "", fmt.Errorf("unsupported model format: %s", format)
	}
}
