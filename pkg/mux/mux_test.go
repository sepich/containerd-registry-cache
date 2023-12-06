package mux

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type noOpService struct{}

func (s *noOpService) GetManifest(repo string, ref string, registry string, headers *http.Header, w http.ResponseWriter) {
	w.Write([]byte("{}"))
}
func (s *noOpService) GetBlob(repo string, digest string, registry string, headers *http.Header, w http.ResponseWriter) {
}

func TestManifestsPaths(t *testing.T) {
	testCases := []struct {
		url    string
		expect int
	}{
		{
			url:    "/v2/prom/node-exporter/manifests/v1.5.0?ns=docker.io",
			expect: 200,
		},
		{
			url:    "/v2/somebody/prom/node-exporter/manifests/v1.5.0?ns=docker.io",
			expect: 200,
		},
		{
			url:    "/v2/node-exporter/manifests/v1.5.0?ns=docker.io",
			expect: 200,
		},

		// Missing ref or v2
		{
			url:    "/v2/prom/node-exporter/manifests?ns=docker.io",
			expect: 404,
		},
		{
			url:    "/prom/node-exporter/manifests/v1.5.0?ns=docker.io",
			expect: 404,
		},

		// Missing ns
		{
			url:    "/v2/prom/node-exporter/manifests/v1.5.0",
			expect: 400,
		},
	}

	r := NewRouter(&noOpService{})

	for _, tC := range testCases {
		t.Run(strings.ReplaceAll(tC.url, "/", "-"), func(t *testing.T) {
			req, err := http.NewRequest("GET", tC.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			assert.Equal(t, tC.expect, rr.Code)
		})
	}
}

func TestBlobPaths(t *testing.T) {
	testCases := []struct {
		url    string
		expect int
	}{
		{
			url:    "/v2/prom/node-exporter/blobs/sha256:b7b28af77ffec6054d13378df4fdf02725830086c7444d9c278af25312aa39b9?ns=docker.io",
			expect: 200,
		},
		{
			url:    "/v2/somebody/prom/node-exporter/blobs/sha256:b7b28af77ffec6054d13378df4fdf02725830086c7444d9c278af25312aa39b9?ns=docker.io",
			expect: 200,
		},
		{
			url:    "/v2/node-exporter/blobs/sha256:b7b28af77ffec6054d13378df4fdf02725830086c7444d9c278af25312aa39b9?ns=docker.io",
			expect: 200,
		},

		// Missing ref or v2
		{
			url:    "/v2/prom/node-exporter/blobs?ns=docker.io",
			expect: 404,
		},
		{
			url:    "/prom/node-exporter/blobs/sha256:b7b28af77ffec6054d13378df4fdf02725830086c7444d9c278af25312aa39b9?ns=docker.io",
			expect: 404,
		},

		// Missing ns
		{
			url:    "/v2/prom/node-exporter/blobs/sha256:b7b28af77ffec6054d13378df4fdf02725830086c7444d9c278af25312aa39b9",
			expect: 400,
		},
	}

	r := NewRouter(&noOpService{})

	for _, tC := range testCases {
		t.Run(strings.ReplaceAll(tC.url, "/", "-"), func(t *testing.T) {
			req, err := http.NewRequest("GET", tC.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			assert.Equal(t, tC.expect, rr.Code)
		})
	}
}
