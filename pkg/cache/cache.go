package cache

import (
	"io"

	"github.com/sepich/containerd-registry-cache/pkg/model"
)

type CachingService interface {
	GetCache(object *model.ObjectIdentifier) (CachedObject, CacheWriter, error)
}

type CachedObject interface {
	GetReader() (io.ReadCloser, error)
	GetMetadata() ObjMeta
}
type ObjMeta struct {
	CacheManifest
	Path      string
	SizeBytes int64
}

type CacheWriter interface {
	Write(p []byte) (n int, err error)
	Close(contentType, dockerContentDigest string) error
	Cleanup() // allows the writer to clean up any temporary files or resources
}
