package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/sepich/containerd-registry-cache/pkg/model"
)

type CachingService interface {
	GetCache(object *model.ObjectIdentifier) (*CachedObject, *CacheWriter, error)
}

type FileCache struct {
	CacheDirectory string
}

var _ CachingService = &FileCache{}

func (c *FileCache) GetCache(object *model.ObjectIdentifier) (*CachedObject, *CacheWriter, error) {
	writer := &CacheWriter{
		Object:         *object,
		cacheDirectory: c.CacheDirectory,
	}

	manifest, size, err := c.getManifestOrNilOnMiss(object)
	if err != nil {
		return nil, nil, err
	}
	if manifest == nil {
		return nil, writer, nil
	}

	reader := &CachedObject{
		CacheManifest: *manifest,
		Path:          filepath.Join(c.CacheDirectory, ObjectToCacheName(object)),
		SizeBytes:     size,
	}

	return reader, writer, nil
}

func (c *FileCache) getManifestOrNilOnMiss(object *model.ObjectIdentifier) (*CacheManifest, int64, error) {
	cacheKey := ObjectToCacheName(object)
	cacheFilePath := filepath.Join(c.CacheDirectory, cacheKey)
	cacheFilePathManifest := cacheFilePath + cacheManifestSuffix

	var sizeBytes int64
	if cacheStat, err := os.Stat(cacheFilePath); errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	} else if err != nil {
		return nil, 0, err
	} else {
		sizeBytes = cacheStat.Size()
	}

	if _, err := os.Stat(cacheFilePathManifest); errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	} else if err != nil {
		return nil, 0, err
	}

	manifest := &CacheManifest{}
	manifestJson, err := os.ReadFile(cacheFilePathManifest)
	if err != nil {
		return nil, 0, err
	}
	err = json.Unmarshal(manifestJson, manifest)
	if err != nil {
		return nil, 0, err
	}

	return manifest, sizeBytes, nil
}
