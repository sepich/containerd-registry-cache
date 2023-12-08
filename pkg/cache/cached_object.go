package cache

import (
	"io"
	"os"
)

type CachedObject struct {
	CacheManifest
	Path      string
	SizeBytes int64
}

func (c *CachedObject) GetReader() (io.ReadCloser, error) {
	return os.Open(c.Path)
}
