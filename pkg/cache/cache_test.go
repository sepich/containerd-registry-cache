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
	var digest = "sha256:41891b95aca23018ba65b320ff3ce10a98ee3cb39261f02fd74867c68414e814"

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
			name:     "docker.io-m-user_repository-v1.2.3",
			contents: []byte(`6bytes`),
			manifest: []byte(`{
				"Registry": "docker.io",
				"ContentType": "application/vnd.docker.distribution.manifest.list.v2+json",
				"DockerContentDigest": "sha256:41891b95aca23018ba65b320ff3ce10a98ee3cb39261f02fd74867c68414e814",
				"Repository": "user/repository",
				"Ref": "v1.2.3",
				"Type": "manifest"
			}`),
		},
		{
			object: model.ObjectIdentifier{
				Registry:   "docker.io",
				Repository: "user/repository",
				Ref:        "sha256:41891b95aca23018ba65b320ff3ce10a98ee3cb39261f02fd74867c68414e814",
				Type:       model.ObjectTypeBlob,
			},
			name:     "docker.io-b-sha256:41891b95aca23018ba65b320ff3ce10a98ee3cb39261f02fd74867c68414e814",
			contents: []byte(`6bytes`),
			manifest: []byte(`{
				"Registry": "docker.io",
				"ContentType": "application/vnd.docker.distribution.manifest.list.v2+json",
				"DockerContentDigest": "sha256:41891b95aca23018ba65b320ff3ce10a98ee3cb39261f02fd74867c68414e814",
				"Repository": "user/repository",
				"Ref": "sha256:41891b95aca23018ba65b320ff3ce10a98ee3cb39261f02fd74867c68414e814",
				"Type": "blob"
			}`),
		},
	}

	// Reading
	for _, tC := range testCases {
		t.Run("read: "+tC.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			os.WriteFile(filepath.Join(tmpDir, tC.name), tC.contents, os.ModePerm)
			os.WriteFile(filepath.Join(tmpDir, tC.name+".json"), tC.manifest, os.ModePerm)

			cacheService := &FileCache{
				CacheDirectory: tmpDir,
			}

			cachedObject, writer, err := cacheService.GetCache(&tC.object)
			assert.Nil(t, err)
			assert.NotNil(t, writer)
			assert.NotNil(t, cachedObject)

			assert.Equal(t, int64(6), cachedObject.SizeBytes)
			assert.Equal(t, contentType, cachedObject.ContentType)
			assert.Equal(t, digest, cachedObject.DockerContentDigest)

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
