package main

import (
	"net/http"
	"os"
	"strconv"

	"github.com/sepich/containerd-registry-cache/pkg/cache"
	"github.com/sepich/containerd-registry-cache/pkg/mux"
	"github.com/sepich/containerd-registry-cache/pkg/service"
	"go.uber.org/zap"
)

type JsonableRequest struct {
	Method     string
	Proto      string
	Header     http.Header
	Host       string
	RemoteAddr string
	RequestURI string
}

var logger *zap.Logger

func init() {
	logger = zap.Must(zap.NewProduction())
	if os.Getenv("DEBUG") != "" {
		logger = zap.Must(zap.NewDevelopment())
		logger.Debug("Debug logging active, headers will be logged and may include credentials")
	}
	zap.ReplaceGlobals(logger)
}

func main() {
	host, _ := os.Hostname()
	logger.Info("Starting containerd-registry-cache", zap.String("hostname", host))

	port := 3000
	portEnv := os.Getenv("PORT")
	if portEnv != "" {
		portInt, err := strconv.Atoi(portEnv)
		if err == nil {
			port = portInt
		}
	}
	listenStr := ":" + strconv.Itoa(port)
	logger.Info("Listening over HTTP", zap.String("port", listenStr))

	cacheDir := os.Getenv("CACHE_DIR")
	if cacheDir == "" {
		cacheDir = "/tmp/data"
	}
	err := os.MkdirAll(cacheDir, os.ModePerm)
	if err != nil {
		logger.Panic("Could not create cache directory", zap.Error(err))
	}
	logger.Info("Using cache directory", zap.String("cacheDirectory", cacheDir))

	cache := cache.FileCache{
		CacheDirectory: cacheDir,
	}
	router := mux.NewRouter(&service.CService{
		Cache: &cache,
		IgnoredTags: map[string]struct{}{
			"latest": {},
		},
	})

	everything := func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		router.ServeHTTP(w, r)
	}

	err = http.ListenAndServe(listenStr, http.HandlerFunc(everything))
	if err != nil {
		logger.Panic("could not listen", zap.Error(err))
	}
}

func logRequest(r *http.Request) {
	// http.Request contains methods which the JSON marshaller doesn't like.
	jsonRequest := JsonableRequest{
		Method:     r.Method,
		Proto:      r.Proto,
		Header:     r.Header,
		Host:       r.Host,
		RemoteAddr: r.RemoteAddr,
		RequestURI: r.RequestURI,
	}
	logger.Debug("Received HTTP request", zap.Any("request", jsonRequest))

}
