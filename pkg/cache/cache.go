package cache

import (
	"github.com/sepich/containerd-registry-cache/pkg/model"
)

type CachingService interface {
	GetCache(object *model.ObjectIdentifier) (*CachedObject, CacheWriter, error)
}

type CacheWriter interface {
	Write(p []byte) (n int, err error)
	Close(contentType, dockerContentDigest string) error
}
