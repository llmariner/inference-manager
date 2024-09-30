package modeldownloader

import (
	"context"
	"io"
	"os"
	"testing"

	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestDownload(t *testing.T) {
	// Create a temp dir
	modelDir, err := os.MkdirTemp("", "model")
	assert.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(modelDir)
	}()

	ctx := context.Background()
	s3Client := &fakeS3Client{}
	d := New(modelDir, s3Client)
	resp := &mv1.GetBaseModelPathResponse{
		Formats: []mv1.ModelFormat{
			mv1.ModelFormat_MODEL_FORMAT_GGUF,
		},
	}
	err = d.Download(ctx, "model0", resp, mv1.ModelFormat_MODEL_FORMAT_GGUF)
	assert.NoError(t, err)
	assert.Equal(t, 1, s3Client.numDownload)

	// Run again.
	err = d.Download(ctx, "model0", resp, mv1.ModelFormat_MODEL_FORMAT_GGUF)
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
