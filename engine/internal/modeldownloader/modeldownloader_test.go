package modeldownloader

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
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
	err = d.Download(ctx, "model0", "", mv1.ModelFormat_MODEL_FORMAT_GGUF)
	assert.NoError(t, err)
	assert.Equal(t, 1, s3Client.numDownload)

	// Run again.
	err = d.Download(ctx, "model0", "", mv1.ModelFormat_MODEL_FORMAT_GGUF)
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

func (c *fakeS3Client) ListObjectsPages(
	ctx context.Context,
	prefix string,
	f func(page *s3.ListObjectsV2Output, lastPage bool) bool) error {
	return nil
}
