package service

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindCreds(t *testing.T) {
	credsA := RegistryCreds{Username: "userA", Password: "passA"}
	credsB := RegistryCreds{Username: "userB", Password: "passB"}
	credsC := RegistryCreds{Username: "userC", Password: "passC"}

	s := &CacheService{
		DefaultCreds: map[string]RegistryCreds{
			"docker.io":                             credsA,
			"europe-west3-docker.pkg.dev":           credsB,
			"europe-west3-docker.pkg.dev/project-a": credsC,
		},
	}

	testCases := []struct {
		name      string
		url       string
		wantCreds RegistryCreds
		wantKey   string
		wantOk    bool
	}{
		{"hostname match", "https://docker.io/v2/library/alpine/manifests/latest", credsA, "docker.io", true},
		{"hostname match with path", "https://europe-west3-docker.pkg.dev/v2/other-project/repo/manifests/v1", credsB, "europe-west3-docker.pkg.dev", true},
		{"prefix match wins over hostname", "https://europe-west3-docker.pkg.dev/v2/project-a/repo/manifests/v1", credsC, "europe-west3-docker.pkg.dev/project-a", true},
		{"no partial segment match", "https://europe-west3-docker.pkg.dev/v2/project-abc/repo/manifests/v1", credsB, "europe-west3-docker.pkg.dev", true},
		{"no match", "https://ghcr.io/v2/org/repo/manifests/v1", RegistryCreds{}, "", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.url)
			assert.NoError(t, err)
			creds, key, ok := s.findCreds(u)
			assert.Equal(t, tc.wantOk, ok)
			assert.Equal(t, tc.wantKey, key)
			assert.Equal(t, tc.wantCreds, creds)
		})
	}
}

func TestFindCredsTrailingSlash(t *testing.T) {
	creds := RegistryCreds{Username: "user", Password: "pass"}
	s := &CacheService{
		DefaultCreds: map[string]RegistryCreds{
			"registry.example.com/project/": creds,
		},
	}

	u, _ := url.Parse("https://registry.example.com/v2/project/repo/blobs/sha256:abc")
	got, key, ok := s.findCreds(u)
	assert.True(t, ok)
	assert.Equal(t, "registry.example.com/project/", key)
	assert.Equal(t, creds, got)
}
