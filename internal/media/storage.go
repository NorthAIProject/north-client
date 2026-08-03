// Package media owns uploaded files and the analyses run over them.
package media

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Storage is object storage. An interface so the media service does not depend
// on S3 specifically, and so tests do not need a bucket.
type Storage interface {
	Put(ctx context.Context, key, contentType string, body io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error

	// SignedURL is a time-limited link the browser can use directly, so video
	// bytes never pass through the application.
	SignedURL(ctx context.Context, key string, expires time.Duration) (string, error)
}

// S3Storage talks to any S3-compatible service. One implementation covers MinIO
// in development, S3, and Cloudflare R2 — they differ by endpoint and
// addressing style, not by protocol.
type S3Storage struct {
	client   *s3.Client
	uploader *transfermanager.Client
	presign  *s3.PresignClient
	bucket   string
}

type S3Options struct {
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
}

func NewS3Storage(ctx context.Context, opts S3Options) (*S3Storage, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(opts.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(opts.AccessKey, opts.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, apperr.Wrap(err, "load aws config")
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if opts.Endpoint != "" {
			o.BaseEndpoint = aws.String(opts.Endpoint)
		}
		// MinIO addresses buckets by path; S3 and R2 use the hostname.
		o.UsePathStyle = opts.UsePathStyle

		// The SDK defaults to calculating a checksum on every upload, which it
		// can only do over a plain HTTP connection if the body is seekable. An
		// upload arrives as a stream, and buffering a 200 MB video into memory
		// to satisfy that is not a trade worth making. Requiring it only where
		// S3 itself requires it keeps streaming uploads working against MinIO
		// on http:// and against S3 and R2 on https://, where the SDK uses a
		// trailing checksum instead.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})

	return &S3Storage{
		client:   client,
		uploader: transfermanager.New(client),
		presign:  s3.NewPresignClient(client),
		bucket:   opts.Bucket,
	}, nil
}

// Put streams an object into the bucket.
//
// It goes through the transfer manager rather than calling PutObject directly.
// PutObject signs the whole payload up front, which needs a seekable body; an
// upload arrives as a stream, and buffering a 200 MB video into memory to make
// it seekable is not a trade worth making. The transfer manager reads the
// stream in parts and uses a multipart upload, so memory stays bounded no
// matter how long the clip is.
func (s *S3Storage) Put(ctx context.Context, key, contentType string, body io.Reader) error {
	_, err := s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	return apperr.Wrap(err, "store object %s", key)
}

func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "fetch object %s", key)
	}
	return out.Body, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	return apperr.Wrap(err, "delete object %s", key)
}

func (s *S3Storage) SignedURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", apperr.Wrap(err, "sign url for %s", key)
	}
	return req.URL, nil
}

// StorageKey builds the object key for an upload.
//
// Namespaced by user and dated, so a bucket listing is navigable and one user's
// objects are trivially separable from another's.
func StorageKey(userID fmt.Stringer, mediaID fmt.Stringer, extension string) string {
	now := time.Now().UTC()
	return fmt.Sprintf("users/%s/%d/%02d/%s%s", userID, now.Year(), now.Month(), mediaID, extension)
}
