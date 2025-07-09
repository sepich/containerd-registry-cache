package main

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"

	"github.com/prometheus/common/version"
	"github.com/sepich/containerd-registry-cache/pkg/cache"
	"github.com/sepich/containerd-registry-cache/pkg/mux"
	"github.com/sepich/containerd-registry-cache/pkg/service"
	"github.com/spf13/pflag"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
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
	var cacheDir = pflag.StringP("cache-dir", "d", "/tmp/data", "Cache directory")
	var credsFile = pflag.StringP("creds-file", "f", "", "Default credentials file to use for registries")
	var port = pflag.IntP("port", "p", 3000, "Port to listen on")
	var ignoreTags = pflag.StringP("ignore", "i", "latest", "RegEx of tags to ignore caching")
	var cacheManifests = pflag.BoolP("cache-manifests", "m", true, "Cache manifests")
	var ver = pflag.BoolP("version", "v", false, "Show version and exit")
	pflag.Parse()
	if *ver {
		fmt.Println(version.Print("containerd-registry-cache"))
		os.Exit(0)
	}

	host, _ := os.Hostname()
	logger.Info("Starting containerd-registry-cache", zap.String("version", version.Version), zap.String("hostname", host), zap.Int("port", *port), zap.String("cacheDir", *cacheDir))

	err := os.MkdirAll(*cacheDir, os.ModePerm)
	if err != nil {
		logger.Error("Could not create cache directory", zap.Error(err))
		os.Exit(1)
	}
	cache := cache.FileCache{
		CacheDirectory: *cacheDir,
	}

	defaultCreds := map[string]service.RegistryCreds{}
	if *credsFile != "" {
		defaultCredsFile, err := os.ReadFile(*credsFile)
		if err != nil {
			logger.Error("Could not read default credentials file", zap.Error(err))
			os.Exit(1)
		}

		err = yaml.Unmarshal(defaultCredsFile, &defaultCreds)
		if err != nil {
			logger.Error("Could not parse default credentials file", zap.Error(err))
			os.Exit(1)
		}
		for k, v := range defaultCreds {
			if v.Username == "" || v.Password == "" {
				logger.Error("Default credentials file contains invalid credentials", zap.String("registry", k))
				os.Exit(1)
			}
		}
		logger.Info("Loaded default credentials", zap.Int("registries", len(defaultCreds)))
	}

	router := mux.NewRouter(&service.CacheService{
		Cache:          &cache,
		IgnoredTags:    regexp.MustCompile(*ignoreTags),
		DefaultCreds:   defaultCreds,
		CacheManifests: *cacheManifests,
	})
	handler := func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		router.ServeHTTP(w, r)
	}
	err = http.ListenAndServe(":"+strconv.Itoa(*port), http.HandlerFunc(handler))
	if err != nil {
		logger.Error("could not listen", zap.Error(err))
		os.Exit(1)
	}
}

func logRequest(r *http.Request) {
	if r.RequestURI == "/metrics" {
		return
	}
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
