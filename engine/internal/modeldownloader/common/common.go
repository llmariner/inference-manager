package common

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type s3Client interface {
	Download(ctx context.Context, f io.WriterAt, path string) error
	ListObjectsPages(ctx context.Context, prefix string, f func(page *s3.ListObjectsV2Output, lastPage bool) bool) error
}

// DownloadAllModelFiles downloads all model files from the specified S3 path to the destination directory.
func DownloadAllModelFiles(ctx context.Context, s3Client s3Client, srcS3Path string, destDir string) error {
	keys, err := listFiles(ctx, s3Client, srcS3Path)
	if err != nil {
		return err
	}
	if err := downloadAllModelFiles(ctx, s3Client, keys, destDir); err != nil {
		return err
	}
	return nil
}

func listFiles(ctx context.Context, s3Client s3Client, srcS3Path string) ([]string, error) {
	var keys []string
	f := func(page *s3.ListObjectsV2Output, lastPage bool) bool {
		for _, obj := range page.Contents {
			keys = append(keys, *obj.Key)
		}
		return lastPage
	}
	// We need to append "/". Otherwise, we will download all objects with the same prefix
	// (e.g., "google/gemma-2b" will download "google/gemma-2b" and "google/gemma-2b-it").
	if err := s3Client.ListObjectsPages(ctx, srcS3Path+"/", f); err != nil {
		// Ignore access denied error, since some user maybe do not have access to the bucket.
		// For such case, we can only download the model files that do not require list bucket
		// access to the bucket.
		if isAccessDenied(err) {
			return nil, nil
		}
		return nil, err
	}
	return keys, nil
}

func isAccessDenied(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "AccessDenied"
}

func downloadAllModelFiles(ctx context.Context, s3Client s3Client, keys []string, destDir string) error {
	for _, key := range keys {
		names := strings.Split(key, "/")
		fname := names[len(names)-1]
		if strings.HasPrefix(fname, ".") {
			log.Printf("Skip downloading hidden file: %q", key)
			continue
		}
		log.Printf("Downloading %q to %q\n", key, destDir)
		df, err := os.Create(filepath.Join(destDir, fname))
		if err != nil {
			return fmt.Errorf("create file %s: %s", key, err)
		}
		if err := s3Client.Download(ctx, df, key); err != nil {
			return fmt.Errorf("download %q: %s", key, err)
		}
		log.Printf("Downloaded %q\n", key)
	}

	return nil
}
