package huggingface

import (
	"context"
	"io"
	"os"

	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDownloadModelFiles(t *testing.T) {
	tcs := []struct {
		name            string
		s3Client        *fakeS3Client
		downloadedFiles []string
	}{
		{
			name:     "no safe tensors index",
			s3Client: &fakeS3Client{},
			downloadedFiles: []string{
				"/src/model.safetensors.index.json",
				"/src/config.json",
				"/src/generation_config.json",
				"/src/special_tokens_map.json",
				"/src/tokenizer.json",
				"/src/tokenizer_config.json",
				"/src/model.safetensors",
			},
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
			},
			downloadedFiles: []string{
				"/src/model.safetensors.index.json",
				"/src/config.json",
				"/src/generation_config.json",
				"/src/special_tokens_map.json",
				"/src/tokenizer.json",
				"/src/tokenizer_config.json",
				"/src/model-00001-of-00002.safetensors",
				"/src/model-00002-of-00002.safetensors",
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "dest")
			assert.NoError(t, err)

			defer func() {
				_ = os.RemoveAll(dir)
			}()

			err = DownloadModelFiles(context.Background(), tc.s3Client, "/src", dir)
			assert.NoError(t, err)
			assert.ElementsMatch(t, tc.s3Client.downloadedFiles, tc.downloadedFiles)
		})
	}
}

type fakeS3Client struct {
	safetensorsIndex *safetensorsIndex

	downloadedFiles []string
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
