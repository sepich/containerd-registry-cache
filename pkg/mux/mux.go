package mux

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sepich/containerd-registry-cache/pkg/model"
	"github.com/sepich/containerd-registry-cache/pkg/service"
)

// Based off the result of remoteName from https://github.com/distribution/distribution's regexp.go
const imageNamePattern = "[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*)*"

var registryOverrides = map[string]string{
	"docker.io": "registry-1.docker.io",
}

func NewRouter(s service.Service, logger *slog.Logger) *mux.Router {
	r := mux.NewRouter()

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<h1>containerd-registry-cache</h1>
		<a href="/metrics">/metrics</a> - prometheus metrics</br>
		`))
	})

	r.HandleFunc("/v2/{repo:"+imageNamePattern+"}/manifests/{ref}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		handleService(s, vars, model.ObjectTypeManifest, r, w, logger)
	})

	r.HandleFunc("/v2/{repo:"+imageNamePattern+"}/blobs/{ref}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		handleService(s, vars, model.ObjectTypeBlob, r, w, logger)
	})

	r.Handle("/metrics", promhttp.Handler())

	return r
}

func handleService(s service.Service, vars map[string]string, t model.ObjectType, r *http.Request, w http.ResponseWriter, logger *slog.Logger) {
	repo := vars["repo"]
	registry := r.URL.Query().Get("ns")
	ip := r.RemoteAddr
	if i := strings.LastIndex(r.RemoteAddr, ":"); i != -1 {
		ip = r.RemoteAddr[:i]
	}
	logger = logger.With("method", r.Method, "uri", r.RequestURI, "addr", ip, "request_id", r.Header.Get("X-Request-ID"))

	if registry == "" {
		w.WriteHeader(400)
		w.Write([]byte("No `ns` query string found (are you using containerd?): I don't know what registry to ask for " + repo))
		logger.Warn("Request had no `ns` query string, not sure what registry this is for", "host", r.Host, "headers", r.Header)
		return
	}

	if registryOverride, ok := registryOverrides[registry]; ok {
		registry = registryOverride
	}

	isHead := false
	if r.Method == "HEAD" {
		isHead = true
	} else if r.Method != "GET" {
		w.WriteHeader(400)
		logger.Warn("Method is not supported", "host", r.Host, "headers", r.Header)
		return
	}

	object := &model.ObjectIdentifier{
		Registry:   registry,
		Repository: repo,
		Ref:        vars["ref"],
		Type:       t,
	}
	s.GetObject(object, isHead, &r.Header, w, logger)
}
