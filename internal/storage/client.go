package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type StorageConfig struct {
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	Folder          string `json:"folder,omitempty"`
}

type Client struct {
	s3     *s3.Client
	bucket string
	folder string
}

func NewClient(ctx context.Context, cfg StorageConfig) (*Client, error) {
	if cfg.Bucket == "" || cfg.Region == "" || cfg.Endpoint == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("storage: incomplete s3 config")
	}

	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...any) (aws.Endpoint, error) {
		return aws.Endpoint{URL: cfg.Endpoint, HostnameImmutable: true}, nil
	})

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithEndpointResolverWithOptions(resolver),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: aws config: %w", err)
	}

	return &Client{
		s3:     s3.NewFromConfig(awsCfg),
		bucket: cfg.Bucket,
		folder: cfg.Folder,
	}, nil
}

func (c *Client) objectKey(key string) string {
	if c.folder != "" {
		return c.folder + "/" + key
	}
	return key
}

func (c *Client) Upload(ctx context.Context, key string, body io.Reader, mimeType string) error {
	objKey := c.objectKey(key)
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         &objKey,
		Body:        body,
		ContentType: &mimeType,
	})
	if err != nil {
		return fmt.Errorf("storage: upload %s: %w", objKey, err)
	}
	return nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	objKey := c.objectKey(key)
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &c.bucket,
		Key:    &objKey,
	})
	if err != nil {
		return fmt.Errorf("storage: delete %s: %w", objKey, err)
	}
	return nil
}

func (c *Client) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	objKey := c.objectKey(key)
	ps := s3.NewPresignClient(c.s3)
	req, err := ps.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.bucket,
		Key:    &objKey,
	}, func(opts *s3.PresignOptions) {
		opts.Expires = ttl
	})
	if err != nil {
		return "", fmt.Errorf("storage: signed url %s: %w", objKey, err)
	}
	return req.URL, nil
}
