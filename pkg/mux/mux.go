package mux

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/jamesorlakin/cacheyd/pkg/model"
	"github.com/jamesorlakin/cacheyd/pkg/service"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// Based off the result of remoteName from https://github.com/distribution/distribution's regexp.go
const imageNamePattern = "[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*)*"

var registryOverrides = map[string]string{
	"docker.io": "registry-1.docker.io",
}

func NewRouter(services service.Service) *mux.Router {
	r := mux.NewRouter()

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("cacheyd"))
	})

	r.HandleFunc("/v2/{repo:"+imageNamePattern+"}/manifests/{ref}", func(w http.ResponseWriter, r *http.Request) {
		logger := zap.L()

		vars := mux.Vars(r)
		repo := vars["repo"]
		registry := r.URL.Query().Get("ns")

		if registry == "" {
			w.WriteHeader(400)
			w.Write([]byte("No ns query string given (are you using containerd?): I don't know what registry to ask for " + repo))
			logger.Warn("Request had no ns query string, not sure what registry this is for", zap.String("repo", repo))
			return
		}

		if registryOverride, ok := registryOverrides[registry]; ok {
			logger.Debug("Replacing registry", zap.String("registry", registry), zap.String("registryOverride", registryOverride))
			registry = registryOverride
		}

		isHead := false
		if r.Method == "HEAD" {
			isHead = true
		} else if r.Method != "GET" {
			// No method
		}

		object := &model.ObjectIdentifier{
			Registry:   registry,
			Repository: repo,
			Ref:        vars["ref"],
			Type:       model.ObjectTypeManifest,
		}

		services.GetManifest(object, isHead, &r.Header, w)
	})

	// I assume registries ensure a form of SHA hash here, but let's not care about that.
	r.HandleFunc("/v2/{repo:"+imageNamePattern+"}/blobs/{digest}", func(w http.ResponseWriter, r *http.Request) {
		logger := zap.L()

		vars := mux.Vars(r)
		repo := vars["repo"]
		registry := r.URL.Query().Get("ns")

		if registry == "" {
			w.WriteHeader(400)
			w.Write([]byte("No ns query string given (are you using containerd?): I don't know what registry to ask for " + repo))

			logger.Warn("Request had no ns query string, not sure what registry this is for", zap.String("repo", repo))
			return
		}

		if registryOverride, ok := registryOverrides[registry]; ok {
			logger.Debug("Replacing registry", zap.String("registry", registry), zap.String("registryOverride", registryOverride))
			registry = registryOverride
		}

		isHead := false
		if r.Method == "HEAD" {
			isHead = true
		} else if r.Method != "GET" {
			// No method
		}

		object := &model.ObjectIdentifier{
			Registry:   registry,
			Repository: repo,
			Ref:        vars["digest"],
			Type:       model.ObjectTypeBlob,
		}

		services.GetBlob(object, isHead, &r.Header, w)
	})

	r.Handle("/metrics", promhttp.Handler())

	return r
}
