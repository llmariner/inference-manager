package huggingface

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const siFilename = "model.safetensors.index.json"

type s3Client interface {
	Download(ctx context.Context, f io.WriterAt, path string) error
}

// DownloadModelFiles downloads model files from S3.
func DownloadModelFiles(ctx context.Context, s3Client s3Client, srcS3Path string, destDir string) error {
	// Check if "model.safetensors.index.json" exists.
	// if exists, download the file and unmarshal so that we can extract the safetensors file names.
	// Otherwise download "model.safetensors" as a safetensors file.
	f, err := os.Create(filepath.Join(destDir, siFilename))
	if err != nil {
		return fmt.Errorf("create file %q: %s", siFilename, err)
	}
	var safetensorFiles []string
	if err := s3Client.Download(ctx, f, filepath.Join(srcS3Path, siFilename)); err != nil {
		_ = f.Close()
		if err := os.Remove(f.Name()); err != nil {
			return fmt.Errorf("remove file %q: %s", f.Name(), err)
		}

		// TODO(kenji): Only ignore a not-found error.
		log.Printf("Downloading %q failed: %s. Using 'model.safetensors' as a safetensors file\n", siFilename, err)
		safetensorFiles = append(safetensorFiles, "model.safetensors")
	} else {
		b, err := os.ReadFile(filepath.Join(destDir, siFilename))
		if err != nil {
			return fmt.Errorf("read file %q: %s", siFilename, err)
		}
		si, err := unmarshalSafetensorsIndex(b)
		if err != nil {
			return fmt.Errorf("unmarshal %q: %s", siFilename, err)
		}

		sfs := map[string]struct{}{}
		for _, fn := range si.WeightMap {
			sfs[fn] = struct{}{}
		}
		for fn := range sfs {
			safetensorFiles = append(safetensorFiles, fn)
		}
	}

	filenames := []string{
		"config.json",
		"generation_config.json",
		"special_tokens_map.json",
		"tokenizer.json",
		"tokenizer_config.json",
	}
	filenames = append(filenames, safetensorFiles...)
	for _, fn := range filenames {
		log.Printf("Downloading %q\n", fn)
		f, err := os.Create(filepath.Join(destDir, fn))
		if err != nil {
			return fmt.Errorf("create file %s: %s", fn, err)
		}
		if err := s3Client.Download(ctx, f, filepath.Join(srcS3Path, fn)); err != nil {
			return fmt.Errorf("download %s: %s", fn, err)
		}
		log.Printf("Downloaded %q\n", fn)
	}

	return nil
}
