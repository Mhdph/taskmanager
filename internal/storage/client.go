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
		bucket: bucket,
	}, nil
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
		c.client.PresignedGetObject(
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
