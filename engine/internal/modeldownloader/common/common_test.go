package common

import (
	"context"
	"path/filepath"

	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
)

func TestDownloadAllModelFiles(t *testing.T) {
	// Create a temp dir
	destDir, err := os.MkdirTemp("", "model")
	assert.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(destDir)
	}()

	s3Client := &fakeS3Client{}
	err = DownloadAllModelFiles(context.Background(), s3Client, "v1/base-models/meta-llama/Meta-Llama-3.1-70B-Instruct-awq-triton", destDir)
	assert.NoError(t, err)

	want := []string{
		"test.txt",
		"repo/llama3/tensorrt_llm/1/rank0.engine",
	}
	for _, fname := range want {
		_, err := os.Stat(filepath.Join(destDir, fname))
		assert.NoError(t, err)
	}

	_, err = os.Stat(filepath.Join(destDir, ".hidden"))
	assert.Error(t, err)
}

type fakeS3Client struct {
}

func (c *fakeS3Client) Download(ctx context.Context, f io.WriterAt, path string) error {
	return nil
}

func (c *fakeS3Client) ListObjectsPages(
	ctx context.Context,
	prefix string,
	f func(page *s3.ListObjectsV2Output, lastPage bool) bool) error {
	page := &s3.ListObjectsV2Output{
		Contents: []types.Object{
			{Key: aws.String("v1/base-models/meta-llama/Meta-Llama-3.1-70B-Instruct-awq-triton/test.txt")},
			{Key: aws.String("v1/base-models/meta-llama/Meta-Llama-3.1-70B-Instruct-awq-triton/repo/llama3/tensorrt_llm/1/rank0.engine")},
		},
	}
	_ = f(page, true)
	return nil
}
