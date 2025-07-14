package cache

import (
	"fmt"
	"strings"
	"time"

	"github.com/sepich/containerd-registry-cache/pkg/model"
)

var cacheManifestSuffix = ".json"

type CacheManifest struct {
	model.ObjectIdentifier

	ContentType         string
	DockerContentDigest string // Only relevant for manifests
	CacheDate           time.Time
}

// ObjectToCacheName returns a filename for the relevant object
func ObjectToCacheName(object *model.ObjectIdentifier) string {
	// if it's a blob we spread it to the whole registry
	var key string
	id := strings.ReplaceAll(strings.Replace(object.Ref, "sha256:", "", 1), "/", "")
	if object.Type == model.ObjectTypeBlob {
		key = fmt.Sprintf("blobs/%s/%s", id[0:2], id)
	} else {
		key = fmt.Sprintf("%s/%s/%s", object.Registry, object.Repository, object.Ref)
	}
	return key
}
