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

	"github.com/jamesorlakin/cacheyd/pkg/cache"
	"github.com/jamesorlakin/cacheyd/pkg/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
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

type CacheydService struct {
	Cache         cache.CachingService
	IgnoredImages map[string]struct{}
	IgnoredTags   map[string]struct{}
}

var _ Service = &CacheydService{}

func (s *CacheydService) GetManifest(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	logger := zap.L().With(zap.Any("object", object))
	logger.Debug("GetManifest", zap.Any("headers", headers))

	s.cacheOrProxy(logger, object, isHead, headers, w)
}

func (s *CacheydService) GetBlob(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	logger := zap.L().With(zap.Any("object", object))
	logger.Debug("GetBlob", zap.Any("headers", headers))

	s.cacheOrProxy(logger, object, isHead, headers, w)
}

func (s *CacheydService) cacheOrProxy(logger *zap.Logger, object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	w.Header().Add("X-Proxied-By", "cacheyd")
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
		logger.Debug("Cache hit", zap.Any("cached", cached))
		w.Header().Add("X-Proxy-Date", cached.CacheDate.String())
		w.Header().Add(model.HeaderContentLength, strconv.Itoa(int(cached.SizeBytes)))
		w.Header().Add(model.HeaderContentType, cached.ContentType)
		w.Header().Add(model.HeaderDockerContentDigest, cached.DockerContentDigest)

		reader, _ := cached.GetReader()
		readIntoWriters(logger, []io.Writer{w}, reader)
		reader.Close()
		return
	} else {
		logger.Debug("Cache miss")
		cacheMisses.Inc()
	}

	url := "https://%s/v2/%s/blobs/%s"
	if object.Type == model.ObjectTypeManifest {
		url = "https://%s/v2/%s/manifests/%s"
	}

	upstreamResp, err := proxySuccessOrError(fmt.Sprintf(url, object.Registry, object.Repository, object.Ref), "GET", headers)
	logger.Debug("Got upstream response", zap.Int("status", upstreamResp.StatusCode), zap.Any("headers", upstreamResp.Header))

	if err != nil {
		logger.Debug("cacheOrProxy err", zap.Error(err))
		// If it's a non-200 status from upstream then just pass it through
		// This should handle 404s and 401s to request auth
		if errors.Is(err, &Non200Error{}) {
			copyHeaders(w.Header(), upstreamResp.Header)
			w.WriteHeader(upstreamResp.StatusCode)

			if !isHead {
				readIntoWriters(logger, []io.Writer{w}, upstreamResp.Body)
			}
		}
		upstreamResp.Body.Close()
		return
	}
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

	var written int64
	for {
		nr, rerr := src.Read(buf)
		if rerr != nil && rerr != io.EOF && rerr != context.Canceled {
			logger.Error("Read error during copy", zap.Error(rerr))
		}
		if nr > 0 {
			written += int64(nr)
			// var werr error
			for _, v := range dst {
				// nw, werr := v.Write(buf[:nr])
				v.Write(buf[:nr])
				// TODO: Error handling...
				// if werr != nil {
				// 	return written, werr
				// }
				// if nr != nw {
				// 	return written, io.ErrShortWrite
				// }
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				logger.Debug("EOF reached")
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

	if resp.StatusCode == 200 {
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
