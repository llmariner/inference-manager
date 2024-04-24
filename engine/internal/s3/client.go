package s3

import (
	"io"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/llm-operator/inference-manager/engine/internal/config"
)

// NewClient returns a new S3 client.
func NewClient(c config.S3Config) *Client {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	conf := &aws.Config{
		Endpoint: aws.String(c.EndpointURL),
		Region:   aws.String(c.Region),
		// This is needed as the minio server does not support the virtual host style.
		S3ForcePathStyle: aws.Bool(true),
	}
	return &Client{
		svc:    s3.New(sess, conf),
		bucket: c.Bucket,
	}
}

// Client is a client for S3.
type Client struct {
	svc    *s3.S3
	bucket string
}

// Download downloads an object from S3.
func (c *Client) Download(w io.WriterAt, key string) error {
	downloader := s3manager.NewDownloaderWithClient(c.svc)
	_, err := downloader.Download(w, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}
	return nil
}
