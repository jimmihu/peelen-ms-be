package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"peelen-ms-be/internal/infrastructure/config"
)

type ObjectStorage interface {
	Upload(ctx context.Context, objectKey, contentType string, data []byte) error
	Delete(ctx context.Context, objectKey string) error
	SignedDownloadURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error)
}

type MinIOObjectStorage struct {
	client *minio.Client
	bucket string
}

func NewMinIOObjectStorage(cfg config.ObjectStorageConfig) (*MinIOObjectStorage, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create MinIO client: %w", err)
	}
	return &MinIOObjectStorage{client: client, bucket: cfg.Bucket}, nil
}

func (s *MinIOObjectStorage) Upload(ctx context.Context, objectKey, contentType string, data []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, objectKey, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("upload object to MinIO: %w", err)
	}
	return nil
}

func (s *MinIOObjectStorage) Delete(ctx context.Context, objectKey string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete object from MinIO: %w", err)
	}
	return nil
}

func (s *MinIOObjectStorage) SignedDownloadURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	value, err := s.client.PresignedGetObject(ctx, s.bucket, objectKey, expiry, url.Values{})
	if err != nil {
		return "", fmt.Errorf("create MinIO download URL: %w", err)
	}
	return value.String(), nil
}
