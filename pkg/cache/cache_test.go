package cache

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/sepich/containerd-registry-cache/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestReadWriteFromCache(t *testing.T) {
	var contentType = "application/vnd.docker.distribution.manifest.list.v2+json"
	var digest = "sha256:65f65e75f5eed0e6ce330028a88f1d62475ea0c4a3d8dc038bde7866aeedf76d"

	headers := http.Header{}
	headers.Add(model.HeaderContentType, contentType)
	headers.Add(model.HeaderDockerContentDigest, digest)

	testCases := []struct {
		object   model.ObjectIdentifier
		name     string
		contents []byte
		manifest []byte
	}{
		{
			object: model.ObjectIdentifier{
				Registry:   "docker.io",
				Repository: "user/repository",
				Ref:        "v1.2.3",
				Type:       model.ObjectTypeManifest,
			},
			name:     "docker.io/user/repository/v1.2.3",
			contents: []byte(`6bytes`),
			manifest: []byte(`{
				"Registry": "docker.io",
				"ContentType": "application/vnd.docker.distribution.manifest.list.v2+json",
				"DockerContentDigest": "sha256:65f65e75f5eed0e6ce330028a88f1d62475ea0c4a3d8dc038bde7866aeedf76d",
				"Repository": "user/repository",
				"Ref": "v1.2.3",
				"Type": "manifest"
			}`),
		},
		{
			object: model.ObjectIdentifier{
				Registry:   "docker.io",
				Repository: "user/repository",
				Ref:        "sha256:65f65e75f5eed0e6ce330028a88f1d62475ea0c4a3d8dc038bde7866aeedf76d",
				Type:       model.ObjectTypeBlob,
			},
			name:     "blobs/65/65f65e75f5eed0e6ce330028a88f1d62475ea0c4a3d8dc038bde7866aeedf76d",
			contents: []byte(`6bytes`),
			manifest: []byte(`{
				"Registry": "docker.io",
				"ContentType": "application/vnd.docker.distribution.manifest.list.v2+json",
				"DockerContentDigest": "sha256:65f65e75f5eed0e6ce330028a88f1d62475ea0c4a3d8dc038bde7866aeedf76d",
				"Repository": "user/repository",
				"Ref": "sha256:65f65e75f5eed0e6ce330028a88f1d62475ea0c4a3d8dc038bde7866aeedf76d",
				"Type": "blob"
			}`),
		},
	}

	// Reading
	for _, tC := range testCases {
		t.Run("read: "+tC.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			p := filepath.Join(tmpDir, tC.name)
			os.MkdirAll(filepath.Dir(p), os.ModePerm)
			os.WriteFile(p, tC.contents, os.ModePerm)
			os.WriteFile(p+".json", tC.manifest, os.ModePerm)

			cacheService := &FileCache{
				CacheDirectory: tmpDir,
			}

			cachedObject, writer, err := cacheService.GetCache(&tC.object)
			assert.Nil(t, err)
			assert.NotNil(t, writer)
			assert.NotNil(t, cachedObject)

			meta := cachedObject.GetMetadata()
			assert.Equal(t, int64(6), meta.SizeBytes)
			assert.Equal(t, contentType, meta.ContentType)
			assert.Equal(t, digest, meta.DockerContentDigest)

			reader, err := cachedObject.GetReader()
			assert.Nil(t, err)
			defer reader.Close()
			contents, err := io.ReadAll(reader)
			assert.Nil(t, err)
			assert.Equal(t, tC.contents, contents)

		})
	}

	// Writing
	for _, tC := range testCases {
		t.Run("write: "+tC.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			cacheService := &FileCache{
				CacheDirectory: tmpDir,
			}

			cachedObject, writer, err := cacheService.GetCache(&tC.object)
			assert.Nil(t, err)
			assert.NotNil(t, writer)
			assert.Nil(t, cachedObject)

			n, err := writer.Write(tC.contents)
			assert.Nil(t, err)
			assert.Equal(t, 6, n)

			err = writer.Close(headers.Get(model.HeaderContentType), headers.Get(model.HeaderDockerContentDigest))
			assert.Nil(t, err)

			writtenContents, err := os.ReadFile(filepath.Join(tmpDir, tC.name))
			assert.Nil(t, err)
			assert.Equal(t, tC.contents, writtenContents)

			writtenManifestBytes, err := os.ReadFile(filepath.Join(tmpDir, tC.name+".json"))
			assert.Nil(t, err)
			writtenManifest := CacheManifest{}
			err = json.Unmarshal(writtenManifestBytes, &writtenManifest)
			assert.Nil(t, err)

			assert.Equal(t, tC.object, writtenManifest.ObjectIdentifier)
			assert.Equal(t, contentType, writtenManifest.ContentType)
			assert.Equal(t, digest, writtenManifest.DockerContentDigest)
		})
	}
}
