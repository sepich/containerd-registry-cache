package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/common/version"
	"github.com/sepich/containerd-registry-cache/pkg/cache"
	"github.com/sepich/containerd-registry-cache/pkg/mux"
	"github.com/sepich/containerd-registry-cache/pkg/service"
	"github.com/spf13/pflag"
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

func main() {
	var cacheDir = pflag.StringP("cache-dir", "d", "/tmp/data", "Cache directory")
	var credsFile = pflag.StringP("creds-file", "f", "", "Default credentials file to use for registries")
	var port = pflag.IntP("port", "p", 3000, "Port to listen on")
	var ignoreTags = pflag.StringP("ignore", "i", "latest", "RegEx of tags to ignore caching")
	var cacheManifests = pflag.BoolP("cache-manifests", "m", true, "Cache manifests")
	//var privateRegistries = pflag.StringArrayP("private-registry", "p", []string{}, "Private registries to skip caching of Manifests (can be specified multiple times)")
	var logLevel = pflag.StringP("log-level", "l", "info", "Log level to use (debug, info)")
	var ver = pflag.BoolP("version", "v", false, "Show version and exit")
	pflag.Parse()
	if *ver {
		fmt.Println(version.Print("containerd-registry-cache"))
		os.Exit(0)
	}

	logger := getLogger(*logLevel)
	host, _ := os.Hostname()
	logger.Info("Starting containerd-registry-cache", "version", version.Version, "hostname", host, "port", *port, "cacheDir", *cacheDir)
	logger.Debug("Debug logging active, headers will be logged and may include credentials")

	err := os.MkdirAll(*cacheDir, os.ModePerm)
	if err != nil {
		logger.Error("Could not create cache directory", "error", err)
		os.Exit(1)
	}
	cache := cache.FileCache{
		CacheDirectory: *cacheDir,
	}

	defaultCreds := map[string]service.RegistryCreds{}
	if *credsFile != "" {
		defaultCredsFile, err := os.ReadFile(*credsFile)
		if err != nil {
			logger.Error("Could not read default credentials file",
				"error", err)
			os.Exit(1)
		}

		err = yaml.Unmarshal(defaultCredsFile, &defaultCreds)
		if err != nil {
			logger.Error("Could not parse default credentials file",
				"error", err)
			os.Exit(1)
		}
		for k, v := range defaultCreds {
			if v.Username == "" || v.Password == "" {
				logger.Error("Default credentials file contains invalid credentials",
					"registry", k)
				os.Exit(1)
			}
		}
		logger.Info("Loaded default credentials",
			"registries", len(defaultCreds))
	}

	router := mux.NewRouter(&service.CacheService{
		Cache:          &cache,
		IgnoredTags:    regexp.MustCompile(*ignoreTags),
		DefaultCreds:   defaultCreds,
		CacheManifests: *cacheManifests,
	})
	handler := func(w http.ResponseWriter, r *http.Request) {
		logRequest(logger, r)
		router.ServeHTTP(w, r)
	}
	err = http.ListenAndServe(":"+strconv.Itoa(*port), http.HandlerFunc(handler))
	if err != nil {
		logger.Error("could not listen",
			"error", err)
		os.Exit(1)
	}
}

func logRequest(logger *slog.Logger, r *http.Request) {
	if r.RequestURI == "/metrics" {
		return
	}
	logger.Debug("client request", "method", r.Method, "header", r.Header, "host", r.Host, "addr", r.RemoteAddr, "uri", r.RequestURI)
}

func getLogger(logLevel string) *slog.Logger {
	var l = slog.LevelInfo
	if logLevel == "debug" {
		l = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     l,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}
			if a.Key == slog.SourceKey {
				s := a.Value.String()
				i := strings.LastIndex(s, "/")
				j := strings.LastIndex(s, " ")
				a.Value = slog.StringValue(s[i+1:j] + ":" + s[j+1:len(s)-1])
			}
			if a.Key == slog.LevelKey {
				a.Value = slog.StringValue(strings.ToLower(a.Value.String()))
			}
			return a
		},
	}))
}
