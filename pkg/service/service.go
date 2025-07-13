package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
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

type RegistryCreds struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var client = &http.Client{}

var cacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name:        "containerd_cache_total",
	ConstLabels: map[string]string{"result": "hit"},
})
var cacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name:        "containerd_cache_total",
	ConstLabels: map[string]string{"result": "miss"},
})
var cacheMissByIgnore = promauto.NewCounter(prometheus.CounterOpts{
	Name:        "containerd_cache_total",
	ConstLabels: map[string]string{"result": "skip"},
})

var pool = sync.Pool{
	New: func() any {
		buf := make([]byte, 1024*1024)
		return &buf
	},
}

type CacheService struct {
	Cache             cache.CachingService
	IgnoredImages     map[string]struct{}
	IgnoredTags       *regexp.Regexp
	DefaultCreds      map[string]RegistryCreds
	CacheManifests    bool
	PrivateRegistries map[string]bool
}

var _ Service = &CacheService{}

func (s *CacheService) GetManifest(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	logger := zap.L().With(zap.Any("object", object))
	if logger.Level().Enabled(zapcore.DebugLevel) {
		logger.Info("GetManifest", zap.Any("headers", headers))
	} else {
		logger.Info("GetManifest")
	}

	s.cacheOrProxy(logger, object, isHead, headers, w)
}

func (s *CacheService) GetBlob(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	logger := zap.L().With(zap.Any("object", object))
	if logger.Level().Enabled(zapcore.DebugLevel) {
		logger.Info("GetBlob", zap.Any("headers", headers))
	} else {
		logger.Info("GetBlob")
	}

	s.cacheOrProxy(logger, object, isHead, headers, w)
}

func (s *CacheService) cacheOrProxy(logger *zap.Logger, object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	w.Header().Add("X-Proxied-By", "containerd-registry-cache")
	w.Header().Add("X-Proxied-For", object.Registry)

	shouldSkipCache := false
	// No point skipping blobs - the client either wants them or not.
	// Unless there's heavy heavy blobs we shouldn't cache?
	if object.Type == model.ObjectTypeManifest {
		if !s.CacheManifests {
			logger.Info("Skipping manifest due to cache manifests disabled")
			shouldSkipCache = true
		}
		if s.IgnoredTags != nil {
			if s.IgnoredTags.MatchString(object.Ref) {
				logger.Info("Skipping tag due to match ignore regex")
				shouldSkipCache = true
			}
		}
		if s.PrivateRegistries != nil {
			if _, isPrivate := s.PrivateRegistries[object.Registry]; isPrivate {
				logger.Info("Skipping as private registry")
				shouldSkipCache = true
			}
		}
		if _, ignoredImage := s.IgnoredImages[object.Repository]; ignoredImage {
			logger.Info("Skipping image due to being on ignore list")
			shouldSkipCache = true
		}
	}

	var cacheWriter *cache.CacheWriter
	if shouldSkipCache {
		cacheMissByIgnore.Inc()
	} else {
		var cached *cache.CachedObject
		var err error
		cached, cacheWriter, err = s.Cache.GetCache(object)
		if err != nil {
			logger.Error("error getting from cache", zap.Error(err))
			w.WriteHeader(500)
			return
		}

		if cached != nil {
			logger.Info("Cache hit", zap.Any("cached", cached))
			cacheHits.Inc()

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
		}
		logger.Info("Cache miss")
		cacheMisses.Inc()
		// will cache response for all, but some clients can dislike zstd/gzip, so cache as raw full-range
		headers.Del("Accept-Encoding")
		headers.Del("Range")
	}

	url := "https://%s/v2/%s/blobs/%s"
	if object.Type == model.ObjectTypeManifest {
		url = "https://%s/v2/%s/manifests/%s"
	}

	upstreamResp, err := s.proxySuccessOrError(fmt.Sprintf(url, object.Registry, object.Repository, object.Ref), "GET", headers)
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

	writers := []io.Writer{}
	var manifestBytes bytes.Buffer
	if !shouldSkipCache {
		writers = append(writers, cacheWriter)
		if object.Type == model.ObjectTypeManifest {
			writers = append(writers, &manifestBytes)
		}
	}
	if !isHead {
		writers = append(writers, w)
	}

	readIntoWriters(logger, writers, upstreamResp.Body)

	if !shouldSkipCache {
		if object.Type == model.ObjectTypeManifest {
			logger.Debug("Upstream returned manifest", zap.ByteString("manifest", manifestBytes.Bytes()))
		}
		cacheWriter.Close(upstreamResp.Header.Get(model.HeaderContentType), upstreamResp.Header.Get(model.HeaderDockerContentDigest))
	}
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

func (s *CacheService) proxySuccessOrError(url, method string, headers *http.Header) (*http.Response, error) {
	resp, err := proxy(url, method, headers)
	if err != nil {
		return nil, err
	}

	// retry once with default creds if none provided
	if resp.StatusCode == 401 && headers.Get("Authorization") == "" {
		if defaultCreds, ok := s.DefaultCreds[resp.Request.URL.Host]; ok {
			zap.L().Debug("Received 401, retrying with default credentials", zap.String("url", url))
			realm := resp.Header.Get("WWW-Authenticate")
			if strings.HasPrefix(realm, "Basic") {
				headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(defaultCreds.Username+":"+defaultCreds.Password)))
				resp, err = proxy(url, method, headers)
				if err != nil {
					return nil, err
				}
			}
			if strings.HasPrefix(realm, "Bearer") {
				params := make(map[string]string)
				for param := range strings.SplitSeq(realm[len("Bearer"):], ",") {
					tmp := strings.SplitN(strings.TrimSpace(param), "=", 2)
					if len(tmp) != 2 {
						continue
					}
					params[tmp[0]] = strings.Trim(tmp[1], "\"")
				}
				tokenUrl := params["realm"] + "?"
				for k, v := range params {
					tokenUrl += k + "=" + v + "&"
				}
				tokenUrl = tokenUrl[:len(tokenUrl)-1]

				theaders := http.Header{
					"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte(defaultCreds.Username+":"+defaultCreds.Password))},
				}
				tokenResp, err := proxy(tokenUrl, "GET", &theaders)
				if err != nil {
					return nil, err
				}
				body, err := io.ReadAll(tokenResp.Body)
				if err != nil {
					return nil, err
				}
				tokenResp.Body.Close()
				data := map[string]string{}
				json.Unmarshal(body, &data) // only parse strings, skip ints
				if data["token"] == "" {
					return nil, errors.New("token not found in response")
				}
				headers.Set("Authorization", "Bearer "+data["token"])
				resp, err = proxy(url, method, headers)
				if err != nil {
					return nil, err
				}
			}
		}
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
