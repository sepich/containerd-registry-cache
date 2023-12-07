package cache

import (
	"fmt"
	"strings"
	"time"

	"github.com/jamesorlakin/cacheyd/pkg/model"
)

var cacheManifestSuffix = ".json"

type CacheManifest struct {
	model.ObjectIdentifier
	CacheDate time.Time
}

// ObjectToCacheName returns a filename for the relevant object
func ObjectToCacheName(object *model.ObjectIdentifier) string {
	// if it's a blob we spread it to the whole registry
	var keyDirty string
	if object.Type == model.ObjectTypeBlob {
		keyDirty = fmt.Sprintf("%s-b-%s", object.Registry, object.Ref)
	} else {
		keyDirty = fmt.Sprintf("%s-m-%s-%s", object.Registry, object.Repository, object.Ref)
	}
	// TODO: Actually make this clean up each section
	key := strings.ReplaceAll(keyDirty, "/", "_")
	return key
}
