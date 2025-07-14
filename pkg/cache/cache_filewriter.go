package cache

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sepich/containerd-registry-cache/pkg/model"
)

// FileWriter provides a facility to request a stream to write to the cache
type FileWriter struct {
	cacheDirectory string
	object         model.ObjectIdentifier
	file           *os.File
}

var _ io.Writer = &FileWriter{}
var _ CacheWriter = &FileWriter{}

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
