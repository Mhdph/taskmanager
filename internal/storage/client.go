package storage

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Client struct {
	client *minio.Client
	signer *minio.Client

	bucket string
}

func NewClient(
	endpoint string,
	accessKey string,
	secretKey string,
	bucket string,
	useSSL bool,
) (*Client, error) {
	client, err := minio.New(
		endpoint,
		&minio.Options{
			Creds: credentials.NewStaticV4(
				accessKey,
				secretKey,
				"",
			),

			Secure: useSSL,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create minio client: %w",
			err,
		)
	}

	return &Client{
		client: client,
		signer: client,
		bucket: bucket,
	}, nil
}

// Public download URLs may use a different host or TLS scheme than the internal
// storage connection; signing still uses the same bucket and credentials.
func (c *Client) SetPublicURL(publicURL, accessKey, secretKey string) error {
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("invalid MINIO_PUBLIC_URL")
	}
	c.signer, err = minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: u.Scheme == "https", Region: "us-east-1"})
	return err
}

func (c *Client) CheckHealth(ctx context.Context) error {
	exists, err := c.client.BucketExists(ctx, c.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("storage bucket unavailable")
	}
	return nil
}

func (c *Client) EnsureBucket(
	ctx context.Context,
) error {
	exists, err :=
		c.client.BucketExists(
			ctx,
			c.bucket,
		)

	if err != nil {
		return fmt.Errorf(
			"check storage bucket: %w",
			err,
		)
	}

	if exists {
		return nil
	}

	err = c.client.MakeBucket(
		ctx,
		c.bucket,
		minio.MakeBucketOptions{},
	)
	if err != nil {
		return fmt.Errorf(
			"create storage bucket: %w",
			err,
		)
	}

	return nil
}
func (c *Client) Upload(
	ctx context.Context,
	input UploadInput,
) error {
	_, err := c.client.PutObject(
		ctx,
		c.bucket,
		input.ObjectKey,
		input.Reader,
		input.Size,
		minio.PutObjectOptions{
			ContentType: input.ContentType,
		},
	)

	if err != nil {
		return fmt.Errorf(
			"upload object: %w",
			err,
		)
	}

	return nil
}

func (c *Client) Delete(
	ctx context.Context,
	objectKey string,
) error {
	err := c.client.RemoveObject(
		ctx,
		c.bucket,
		objectKey,
		minio.RemoveObjectOptions{},
	)

	if err != nil {
		return fmt.Errorf(
			"delete object: %w",
			err,
		)
	}

	return nil
}
func (c *Client) PresignedDownloadURL(
	ctx context.Context,
	objectKey string,
	filename string,
	expiry time.Duration,
) (string, error) {
	params := make(
		url.Values,
	)

	params.Set(
		"response-content-disposition",
		fmt.Sprintf(
			`attachment; filename="%s"`,
			filename,
		),
	)

	signedURL, err :=
		c.signer.PresignedGetObject(
			ctx,
			c.bucket,
			objectKey,
			expiry,
			params,
		)

	if err != nil {
		return "", fmt.Errorf(
			"create presigned download url: %w",
			err,
		)
	}

	return signedURL.String(), nil
}
