package cache

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jamesorlakin/cacheyd/pkg/model"
)

// Type CacheWriter provides a facility to request a stream to write to the cache
type CacheWriter struct {
	cacheDirectory string
	Object         model.ObjectIdentifier
	file           *os.File
}

var _ io.WriteCloser = &CacheWriter{}

func (c *CacheWriter) Write(b []byte) (n int, err error) {
	if c.file == nil {
		file, err := os.CreateTemp(c.cacheDirectory, c.Object.Ref)
		if err != nil {
			return 0, err
		}
		c.file = file
	}

	return c.file.Write(b)
}

// Close will (if written to) close the temporary file, generate a cache manifest, and then move it to the cache folder.
func (c *CacheWriter) Close() error {
	if c.file == nil {
		return nil
	}

	err := c.file.Close()
	if err != nil {
		return err
	}

	cacheName := ObjectToCacheName(&c.Object)
	filePath := filepath.Join(c.cacheDirectory, cacheName)
	err = os.Rename(c.file.Name(), filePath)
	if err != nil {
		return err
	}

	manifest := &CacheManifest{
		ObjectIdentifier: c.Object,
		CacheDate:        time.Now(),
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
