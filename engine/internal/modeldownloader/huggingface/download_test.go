package huggingface

import (
	"context"
	"io"
	"os"

	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestDownloadModelFiles(t *testing.T) {
	fs1 := []string{
		"/src/model.safetensors.index.json",
		"/src/config.json",
		"/src/generation_config.json",
		"/src/special_tokens_map.json",
		"/src/tokenizer.json",
		"/src/tokenizer_config.json",
		"/src/preprocessor_config.json",
		"/src/vocab.json",
		"/src/added_tokens.json",
		"/src/merges.txt",
		"/src/normalizer.json",
		"/src/chat_template.jinja",
		"/src/llava_qwen.py",
		"/src/model.safetensors",
	}
	fs2 := []string{
		"/src/model.safetensors.index.json",
		"/src/config.json",
		"/src/generation_config.json",
		"/src/special_tokens_map.json",
		"/src/tokenizer.json",
		"/src/tokenizer_config.json",
		"/src/preprocessor_config.json",
		"/src/vocab.json",
		"/src/added_tokens.json",
		"/src/merges.txt",
		"/src/normalizer.json",
		"/src/chat_template.jinja",
		"/src/llava_qwen.py",
		"/src/model-00001-of-00002.safetensors",
		"/src/model-00002-of-00002.safetensors",
	}
	fs3 := []string{
		"/src/adapter_config.json",
		"/src/generation_config.json",
		"/src/special_tokens_map.json",
		"/src/tokenizer.json",
		"/src/tokenizer_config.json",
		"/src/preprocessor_config.json",
		"/src/vocab.json",
		"/src/added_tokens.json",
		"/src/merges.txt",
		"/src/normalizer.json",
		"/src/model-00001-of-00002.safetensors",
		"/src/model-00002-of-00002.safetensors",
	}

	tcs := []struct {
		name            string
		s3Client        *fakeS3Client
		adapterType     mv1.AdapterType
		downloadedFiles []string
	}{
		{
			name: "no safe tensors index",
			s3Client: &fakeS3Client{
				objs: fs1,
			},
			adapterType:     mv1.AdapterType_ADAPTER_TYPE_UNSPECIFIED,
			downloadedFiles: fs1,
		},
		{
			name: "safe tensors index",
			s3Client: &fakeS3Client{
				safetensorsIndex: &safetensorsIndex{
					WeightMap: map[string]string{
						"model.embed_tokens.weight":               "model-00001-of-00002.safetensors",
						"model.layers.0.self_attn.q_proj.qweight": "model-00001-of-00002.safetensors",
						"model.norm.weight":                       "model-00002-of-00002.safetensors",
					},
				},
				objs: fs2,
			},
			adapterType:     mv1.AdapterType_ADAPTER_TYPE_UNSPECIFIED,
			downloadedFiles: fs2,
		},
		{
			name: "all files",
			s3Client: &fakeS3Client{
				objs: fs3,
			},
			adapterType:     mv1.AdapterType_ADAPTER_TYPE_LORA,
			downloadedFiles: fs3,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "dest")
			assert.NoError(t, err)

			defer func() {
				_ = os.RemoveAll(dir)
			}()

			err = DownloadModelFiles(context.Background(), tc.s3Client, tc.adapterType, "/src", dir)
			assert.NoError(t, err)
			assert.ElementsMatch(t, tc.s3Client.downloadedFiles, tc.downloadedFiles)
		})
	}
}

type fakeS3Client struct {
	safetensorsIndex *safetensorsIndex

	downloadedFiles []string
	objs            []string
}

func (c *fakeS3Client) Download(ctx context.Context, f io.WriterAt, path string) error {
	c.downloadedFiles = append(c.downloadedFiles, path)
	fmt.Println("Downloaded file: ", path)

	if filepath.Base(path) != siFilename {
		return nil
	}

	if c.safetensorsIndex == nil {
		return fmt.Errorf("safetensors index not found")
	}
	b, err := json.Marshal(c.safetensorsIndex)
	if err != nil {
		return err
	}
	_, err = f.WriteAt(b, 0)
	return err

}

func (c *fakeS3Client) ListObjectsPages(
	ctx context.Context,
	prefix string,
	f func(page *s3.ListObjectsV2Output, lastPage bool) bool) error {
	var objs []types.Object
	for _, key := range c.objs {
		objs = append(objs, types.Object{Key: aws.String(key)})
	}
	f(&s3.ListObjectsV2Output{Contents: objs}, true)
	return nil
}
