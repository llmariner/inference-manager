package s3

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	laws "github.com/llmariner/common/pkg/aws"
	"github.com/llmariner/inference-manager/engine/internal/config"
)

// NewClient returns a new S3 client.
func NewClient(ctx context.Context, c config.S3Config) (*Client, error) {
	opts := laws.NewS3ClientOptions{
		EndpointURL: c.EndpointURL,
		Region:      c.Region,
	}
	if ar := c.AssumeRole; ar != nil {
		opts.AssumeRole = &laws.AssumeRole{
			RoleARN:    ar.RoleARN,
			ExternalID: ar.ExternalID,
		}
	}
	svc, err := laws.NewS3Client(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Client{
		svc:    svc,
		bucket: c.Bucket,
	}, nil
}

// Client is a client for S3.
type Client struct {
	svc    *s3.Client
	bucket string
}

// Download uses a download manager to download an object from a bucket.
// The download manager gets the data in parts and writes them to a buffer until all of
// the data has been downloaded.
func (c *Client) Download(ctx context.Context, w io.WriterAt, key string) error {
	const partMiBs int64 = 128
	downloader := manager.NewDownloader(c.svc, func(d *manager.Downloader) {
		d.PartSize = partMiBs * 1024 * 1024
	})
	_, err := downloader.Download(ctx, w, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}
	return nil
}

// ListObjectsPages returns S3 objects with pagination.
func (c *Client) ListObjectsPages(
	ctx context.Context,
	prefix string,
	f func(page *s3.ListObjectsV2Output, lastPage bool) bool,
) error {
	p := s3.NewListObjectsV2Paginator(
		c.svc,
		&s3.ListObjectsV2Input{
			Bucket: aws.String(c.bucket),
			Prefix: aws.String(prefix),
		},
		func(o *s3.ListObjectsV2PaginatorOptions) {},
	)
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return err
		}
		f(page, !p.HasMorePages())
	}
	return nil
}
