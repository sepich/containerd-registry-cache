package mux

import (
	"net/http"

	"github.com/gorilla/mux"
)

// Based off the result of remoteName from https://github.com/distribution/distribution/
const imageNamePattern = "[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*)*"

type Service interface {
	GetManifest(repo string, ref string, registry string) string
}

func NewRouter(services Service) *mux.Router {
	r := mux.NewRouter()

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello world"))
	})

	r.HandleFunc("/v2/{repo:"+imageNamePattern+"}/manifests/{ref}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		repo := vars["repo"]
		registry := r.URL.Query().Get("ns")

		if registry == "" {
			w.WriteHeader(400)
			w.Write([]byte("No ns query string given (are you using containerd?): I don't know what registry to ask for " + repo))
		}

		w.Write([]byte(services.GetManifest(repo, vars["ref"], registry)))
	})

	return r
}
