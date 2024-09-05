package vllm

import (
	"context"
	"io"
	"os"
	"testing"

	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestDownloadAndCreateNewModel(t *testing.T) {
	// Create a temp dir
	modelDir, err := os.MkdirTemp("", "model")
	assert.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(modelDir)
	}()

	ctx := context.Background()
	s3Client := &fakeS3Client{}
	m := New(modelDir, s3Client)
	resp := &mv1.GetBaseModelPathResponse{
		Formats: []mv1.ModelFormat{
			mv1.ModelFormat_MODEL_FORMAT_GGUF,
		},
	}
	err = m.DownloadAndCreateNewModel(ctx, "model0", resp)
	assert.NoError(t, err)
	assert.Equal(t, 1, s3Client.numDownload)

	// Run again.
	err = m.DownloadAndCreateNewModel(ctx, "model0", resp)
	assert.NoError(t, err)
	assert.Equal(t, 1, s3Client.numDownload)
}

type fakeS3Client struct {
	numDownload int
}

func (c *fakeS3Client) Download(ctx context.Context, f io.WriterAt, path string) error {
	c.numDownload++
	return nil
}
