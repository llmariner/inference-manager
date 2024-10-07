package modeldownloader

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/llmariner/inference-manager/engine/internal/modeldownloader/huggingface"
	mv1 "github.com/llmariner/model-manager/api/v1"
)

type s3Client interface {
	Download(ctx context.Context, f io.WriterAt, path string) error
	ListObjectsPages(ctx context.Context, prefix string, f func(page *s3.ListObjectsV2Output, lastPage bool) bool) error
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
	modelID string,
	srcPath string,
	format mv1.ModelFormat,
	adapterType mv1.AdapterType,
) error {
	destPath, err := ModelFilePath(d.modelDir, modelID, format)
	if err != nil {
		return err
	}
	return d.download(ctx, modelID, format, adapterType, srcPath, destPath)
}

// DownloadAdapterOfGGUF downloads the adapter.
func (d *D) DownloadAdapterOfGGUF(
	ctx context.Context,
	modelID string,
	resp *mv1.GetModelPathResponse,
) error {
	destPath, err := AdapterFilePath(d.modelDir, modelID)
	if err != nil {
		return err
	}

	// TODO(kenji): Revisit the adapater type. Currently this is no-op as we don't use the HuggingFace downloader.
	return d.download(ctx, modelID, mv1.ModelFormat_MODEL_FORMAT_GGUF, mv1.AdapterType_ADAPTER_TYPE_QLORA, resp.Path, destPath)
}

func (d *D) download(
	ctx context.Context,
	modelID string,
	format mv1.ModelFormat,
	adapter mv1.AdapterType,
	srcPath string,
	destPath string,
) error {
	// Check if the completion indication file exists. If so, download should have been completed with a previous run. Do not download again.
	completionDir := filepath.Join(d.modelDir, modelID)
	if err := os.MkdirAll(completionDir, 0755); err != nil {
		return err
	}
	completionIndicationFile := filepath.Join(completionDir, "completed.txt")

	if _, err := os.Stat(completionIndicationFile); err == nil {
		log.Printf("The model %s has already been downloaded at %s. Skipping the download.\n", modelID, completionDir)
		return nil
	}

	switch format {
	case mv1.ModelFormat_MODEL_FORMAT_GGUF:
		log.Printf("Downloading the GGUF model from %q\n", srcPath)
		f, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if err := d.s3Client.Download(ctx, f, srcPath); err != nil {
			return fmt.Errorf("download: %s", err)
		}
		log.Printf("Downloaded the model to %q\n", f.Name())
		if err := f.Close(); err != nil {
			return err
		}
	case mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE:
		log.Printf("Downloading the Hugging Face model from %q\n", srcPath)
		if err := os.MkdirAll(destPath, 0755); err != nil {
			return fmt.Errorf("create directory: %s", err)
		}
		if err := huggingface.DownloadModelFiles(ctx, d.s3Client, adapter, srcPath, destPath); err != nil {
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
func ModelFilePath(modelDir, modelID string, format mv1.ModelFormat) (string, error) {
	switch format {
	case mv1.ModelFormat_MODEL_FORMAT_GGUF:
		return filepath.Join(modelDir, modelID, "model.gguf"), nil
	case mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE:
		return filepath.Join(modelDir, modelID), nil
	default:
		return "", fmt.Errorf("unsupported model format: %s", format)
	}
}

// AdapterFilePath returns the file path of the adapter.
func AdapterFilePath(modelDir, modelID string) (string, error) {
	return filepath.Join(modelDir, modelID, "adapter.gguf"), nil
}
