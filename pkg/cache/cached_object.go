package cache

import (
	"io"
	"os"
	"time"

	"github.com/jamesorlakin/cacheyd/pkg/model"
)

type CachedObject struct {
	Object    model.ObjectIdentifier
	Path      string
	SizeBytes int64
	CacheDate time.Time
}

func (c *CachedObject) GetReader() (io.ReadCloser, error) {
	return os.Open(c.Path)
}
