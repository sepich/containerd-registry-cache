package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/sepich/containerd-registry-cache/pkg/model"
)

type FileCache struct {
	CacheDirectory string
}

var _ CachingService = &FileCache{}

func (c *FileCache) GetCache(object *model.ObjectIdentifier) (*CachedObject, CacheWriter, error) {
	writer := &FileWriter{
		object:         *object,
		cacheDirectory: c.CacheDirectory,
	}

	key := filepath.Join(c.CacheDirectory, ObjectToCacheName(object))
	manifest, size, err := c.getManifestOrNilOnMiss(key)
	if err != nil {
		return nil, nil, err
	}
	if manifest == nil {
		return nil, writer, nil
	}

	reader := &CachedObject{
		CacheManifest: *manifest,
		Path:          key,
		SizeBytes:     size,
	}

	return reader, writer, nil
}

func (c *FileCache) getManifestOrNilOnMiss(cacheFilePath string) (*CacheManifest, int64, error) {
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
