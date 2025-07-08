package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sepich/containerd-registry-cache/pkg/cache"
	"github.com/sepich/containerd-registry-cache/pkg/model"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Service interface {
	GetManifest(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter)
	GetBlob(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter)
}

var client = &http.Client{}

var cacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "cache_hits",
})
var cacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "cache_misses",
})
var cacheMissByIgnore = promauto.NewCounter(prometheus.CounterOpts{
	Name: "cache_miss_by_ignore",
})

var pool = sync.Pool{
	New: func() any {
		buf := make([]byte, 1024*1024)
		return &buf
	},
}

type CService struct {
	Cache         cache.CachingService
	IgnoredImages map[string]struct{}
	IgnoredTags   map[string]struct{}
}

var _ Service = &CService{}

func (s *CService) GetManifest(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	logger := zap.L().With(zap.Any("object", object))
	if logger.Level().Enabled(zapcore.DebugLevel) {
		logger.Info("GetManifest", zap.Any("headers", headers))
	} else {
		logger.Info("GetManifest")
	}

	s.cacheOrProxy(logger, object, isHead, headers, w)
}

func (s *CService) GetBlob(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	logger := zap.L().With(zap.Any("object", object))
	if logger.Level().Enabled(zapcore.DebugLevel) {
		logger.Info("GetBlob", zap.Any("headers", headers))
	} else {
		logger.Info("GetBlob")
	}

	s.cacheOrProxy(logger, object, isHead, headers, w)
}

func (s *CService) cacheOrProxy(logger *zap.Logger, object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	w.Header().Add("X-Proxied-By", "containerd-registry-cache")
	w.Header().Add("X-Proxied-For", object.Registry)

	cached, cacheWriter, err := s.Cache.GetCache(object)

	if err != nil {
		logger.Error("error getting from cache", zap.Error(err))
		w.WriteHeader(500)
		return
	}

	shouldSkipCache := false
	// No point skipping blobs - the client either wants them or not.
	// Unless there's heavy heavy blobs we shouldn't cache?
	if object.Type == model.ObjectTypeManifest {
		if _, ignoredTag := s.IgnoredTags[object.Ref]; ignoredTag {
			logger.Info("Skipping tag due to being on ignore list")
			shouldSkipCache = true
		}
		if _, ignoredImage := s.IgnoredImages[object.Repository]; ignoredImage {
			logger.Info("Skipping image due to being on ignore list")
			shouldSkipCache = true
		}
	}

	if shouldSkipCache {
		cacheMissByIgnore.Inc()
	} else if cached != nil {
		cacheHits.Inc()
		logger.Info("Cache hit", zap.Any("cached", cached))

		w.Header().Add("X-Proxy-Date", cached.CacheDate.String())
		w.Header().Add("Age", strconv.Itoa(int(time.Since(cached.CacheDate).Seconds())))
		w.Header().Add(model.HeaderContentLength, strconv.Itoa(int(cached.SizeBytes)))
		w.Header().Add(model.HeaderContentType, cached.ContentType)
		if cached.DockerContentDigest != "" {
			w.Header().Add(model.HeaderDockerContentDigest, cached.DockerContentDigest)
		}

		reader, _ := cached.GetReader()
		readIntoWriters(logger, []io.Writer{w}, reader)
		reader.Close()

		logger.Debug("Returning cache hit", zap.Any("headers", w.Header()))
		return
	} else {
		logger.Info("Cache miss")
		cacheMisses.Inc()
	}

	url := "https://%s/v2/%s/blobs/%s"
	if object.Type == model.ObjectTypeManifest {
		url = "https://%s/v2/%s/manifests/%s"
	}

	upstreamResp, err := proxySuccessOrError(fmt.Sprintf(url, object.Registry, object.Repository, object.Ref), "GET", headers)
	if err != nil {
		logger.Error("cacheOrProxy err", zap.Error(err))
		// If it's a non-200 status from upstream then just pass it through
		// This should handle 404s and 401s to request auth
		if errors.Is(err, &Non200Error{}) {
			copyHeaders(w.Header(), upstreamResp.Header)
			w.WriteHeader(upstreamResp.StatusCode)

			if !isHead {
				readIntoWriters(logger, []io.Writer{w}, upstreamResp.Body)
			}
			upstreamResp.Body.Close()
		} else {
			w.WriteHeader(500)
		}
		return
	}
	logger.Debug("Got upstream response", zap.Int("status", upstreamResp.StatusCode), zap.Any("headers", upstreamResp.Header))
	defer upstreamResp.Body.Close()

	copyHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)

	writers := []io.Writer{cacheWriter}

	var manifestBytes bytes.Buffer
	if object.Type == model.ObjectTypeManifest {
		writers = append(writers, &manifestBytes)
	}

	if !isHead {
		writers = append(writers, w)
	}

	readIntoWriters(logger, writers, upstreamResp.Body)

	if object.Type == model.ObjectTypeManifest {
		logger.Debug("Upstream returned manifest", zap.ByteString("manifest", manifestBytes.Bytes()))
	}

	cacheWriter.Close(upstreamResp.Header.Get(model.HeaderContentType), upstreamResp.Header.Get(model.HeaderDockerContentDigest))
}

func readIntoWriters(logger *zap.Logger, dst []io.Writer, src io.Reader) error {
	buf := *pool.Get().(*[]byte)
	defer pool.Put(&buf)

	for {
		read, rerr := src.Read(buf)
		if rerr != nil && rerr != io.EOF {
			err := fmt.Errorf("read error during copy: %w", rerr)
			logger.Error("", zap.Error(err))
			return err
		}
		if read > 0 {
			for _, v := range dst {
				written, werr := v.Write(buf[:read])
				if werr == nil && read != written {
					werr = io.ErrShortWrite
				}
				if werr != nil {
					err := fmt.Errorf("write error during copy: %w", rerr)
					logger.Error("", zap.Error(err))
					return err
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				rerr = nil
			}
			return rerr
		}
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func proxySuccessOrError(url, method string, headers *http.Header) (*http.Response, error) {
	resp, err := proxy(url, method, headers)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode/100 == 2 {
		return resp, err
	} else {
		return resp, &Non200Error{Code: resp.StatusCode}
	}
}

func proxy(url, method string, headers *http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.TODO(), method, url, nil)
	if err != nil {
		return nil, err
	}

	// Copy headers such as Accept and Authorization, are there any we want to skip?
	copyHeaders(req.Header, *headers)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
