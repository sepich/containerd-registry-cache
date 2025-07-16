package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/sepich/containerd-registry-cache/pkg/model"
)

var _ CachingService = &S3Cache{}

type S3Cache struct {
	bucket         string
	client         *s3.Client
	cacheDirectory string // TODO: streaming upload?
}

func NewS3Cache(bucket, cacheDir string) (*S3Cache, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("Failed to load AWS config: %v", err)
	}
	// check access on startup
	client := s3.NewFromConfig(cfg)
	_, err = client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket:  &bucket,
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return nil, fmt.Errorf("Failed to access S3 bucket `%s`: %v", bucket, err)
	}

	return &S3Cache{
		bucket:         bucket,
		client:         client,
		cacheDirectory: cacheDir,
	}, nil
}

func (c *S3Cache) GetCache(object *model.ObjectIdentifier) (CachedObject, CacheWriter, error) {
	key := ObjectToCacheName(object)
	writer := &S3Writer{
		object:         *object,
		client:         c.client,
		bucket:         c.bucket,
		key:            key,
		cacheDirectory: c.cacheDirectory,
	}
	obj, err := c.client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	if err != nil {
		var notFoundError *types.NotFound
		if errors.As(err, &notFoundError) {
			return nil, writer, nil
		}
		return nil, nil, err
	}

	reader := &S3Object{
		ObjMeta: ObjMeta{
			CacheManifest: CacheManifest{
				ObjectIdentifier:    *object,
				ContentType:         obj.Metadata["content-type"],
				DockerContentDigest: obj.Metadata["docker-content-digest"],
				CacheDate:           *obj.LastModified,
			},
			Path:      key,
			SizeBytes: *obj.ContentLength,
		},
		client: c.client,
		bucket: c.bucket,
	}
	return reader, writer, nil
}

var _ CachedObject = &S3Object{}

type S3Object struct {
	ObjMeta
	client *s3.Client
	bucket string
}

func (o *S3Object) GetReader() (io.ReadCloser, error) {
	obj, err := o.client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: &o.bucket,
		Key:    &o.Path,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get object from S3: %v", err)
	}
	return obj.Body, nil
}

func (o *S3Object) GetMetadata() ObjMeta {
	return o.ObjMeta
}

// S3Writer implements the CacheWriter interface for S3
var _ io.Writer = &S3Writer{}
var _ CacheWriter = &S3Writer{}

type S3Writer struct {
	object         model.ObjectIdentifier
	client         *s3.Client
	bucket         string
	key            string
	cacheDirectory string
	file           *os.File
}

func (w *S3Writer) Write(b []byte) (n int, err error) {
	if w.file == nil {
		file, err := os.CreateTemp(w.cacheDirectory, w.object.Ref)
		if err != nil {
			return 0, err
		}
		w.file = file
	}

	return w.file.Write(b)
}

func (w *S3Writer) Close(contentType, dockerContentDigest string) error {
	if w.file == nil {
		return nil
	}

	info, err := w.file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat cache file: %v", err)
	}

	_, err = w.client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:        aws.String(w.bucket),
		Key:           aws.String(w.key),
		Body:          w.file,
		ContentLength: aws.Int64(info.Size()),
		Metadata: map[string]string{
			"content-type":          contentType,
			"docker-content-digest": dockerContentDigest,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to upload object: %v", err)
	}

	return w.file.Close()
}

func (w *S3Writer) Cleanup() {
	if w.file != nil {
		_ = w.file.Close()
		_ = os.Remove(w.file.Name())
	}
}
