package cache

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sepich/containerd-registry-cache/pkg/model"
)

var _ CachingService = &FileCache{}

type FileCache struct {
	CacheDirectory string
}

func (c *FileCache) GetCache(object *model.ObjectIdentifier) (CachedObject, CacheWriter, error) {
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

	reader := &FileObject{
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

// FileObject implements the CachedObject interface for file-based cache entries
var _ CachedObject = &FileObject{}

type FileObject ObjMeta

func (c *FileObject) GetReader() (io.ReadCloser, error) {
	return os.Open(c.Path)
}
func (c *FileObject) GetMetadata() ObjMeta {
	return ObjMeta(*c)
}

var _ io.Writer = &FileWriter{}
var _ CacheWriter = &FileWriter{}

// FileWriter provides a facility to request a stream to write to the cache
type FileWriter struct {
	cacheDirectory string
	object         model.ObjectIdentifier
	file           *os.File
}

func (c *FileWriter) Write(b []byte) (n int, err error) {
	if c.file == nil {
		file, err := os.CreateTemp(c.cacheDirectory, c.object.Ref)
		if err != nil {
			return 0, err
		}
		c.file = file
	}

	return c.file.Write(b)
}

// Close will (if written to) close the temporary file, generate a cache manifest, and then move it to the cache folder.
func (c *FileWriter) Close(contentType, dockerContentDigest string) error {
	if c.file == nil {
		return nil
	}

	err := c.file.Close()
	if err != nil {
		return err
	}

	cacheName := ObjectToCacheName(&c.object)
	filePath := filepath.Join(c.cacheDirectory, cacheName)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	err = os.Rename(c.file.Name(), filePath)
	if err != nil {
		return err
	}

	manifest := &CacheManifest{
		ObjectIdentifier: c.object,

		ContentType:         contentType,
		DockerContentDigest: dockerContentDigest,
		CacheDate:           time.Now(),
	}
	manifestFilePath := filePath + cacheManifestSuffix

	manifestFile, err := os.Create(manifestFilePath)
	if err != nil {
		return err
	}
	manifestJson, err := json.Marshal(manifest)
	if err != nil {
		return err
	}

	manifestFile.Write(manifestJson)
	return manifestFile.Close()
}

func (c *FileWriter) Cleanup() {
	if c.file != nil {
		_ = c.file.Close()
		_ = os.Remove(c.file.Name())
	}
}
