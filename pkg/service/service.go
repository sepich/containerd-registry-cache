package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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
)

type Service interface {
	GetObject(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter, logger *slog.Logger)
}

type RegistryCreds struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var client = &http.Client{
	Transport: &http.Transport{
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second, // establishing TCP
		}).DialContext,
	},
}

var cacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name:        "containerd_cache_total",
	ConstLabels: map[string]string{"result": "hit"},
})
var cacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name:        "containerd_cache_total",
	ConstLabels: map[string]string{"result": "miss"},
})
var cacheSkips = promauto.NewCounter(prometheus.CounterOpts{
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
	SkipImages        map[string]struct{}
	SkipTags          *regexp.Regexp
	DefaultCreds      map[string]RegistryCreds
	CacheManifests    bool
	PrivateRegistries map[string]bool
}

var _ Service = &CacheService{}

func (s *CacheService) GetObject(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter, logger *slog.Logger) {
	w.Header().Add("X-Proxied-By", "containerd-registry-cache")
	w.Header().Add("X-Proxied-For", object.Registry)

	skipCacheReason := s.getSkipReason(object)
	var cacheWriter cache.CacheWriter
	if skipCacheReason == "" {
		var cached *cache.CachedObject
		var err error
		cached, cacheWriter, err = s.Cache.GetCache(object)
		if err != nil {
			logger.Error("error getting from cache", "error", err)
			w.WriteHeader(500)
			return
		}

		if cached != nil {
			logger.Info("Served from cache", "cache", "hit", "cached", cached)
			cacheHits.Inc()

			w.Header().Add("X-Proxy-Date", cached.CacheDate.String())
			w.Header().Add("Age", strconv.Itoa(int(time.Since(cached.CacheDate).Seconds())))
			w.Header().Add(model.HeaderContentLength, strconv.Itoa(int(cached.SizeBytes)))
			w.Header().Add(model.HeaderContentType, cached.ContentType)
			if cached.DockerContentDigest != "" {
				w.Header().Add(model.HeaderDockerContentDigest, cached.DockerContentDigest)
			}
			logger.Debug("Client response", "headers", w.Header())

			reader, _ := cached.GetReader()
			if err = readIntoWriters([]io.Writer{w}, reader); err != nil {
				logger.Error("Error reading from cache", "error", err)
				return
			}
			reader.Close()
			return
		}
		// will cache response for all, but some clients can dislike zstd/gzip, so cache as raw full-range
		headers.Del("Accept-Encoding")
		headers.Del("Range")
	}

	url := "https://%s/v2/%s/blobs/%s"
	if object.Type == model.ObjectTypeManifest {
		url = "https://%s/v2/%s/manifests/%s"
	}

	upstreamResp, err := s.reqWithCreds(fmt.Sprintf(url, object.Registry, object.Repository, object.Ref), "GET", headers, &logger)
	if err != nil {
		logger.Error("Error proxying request", "error", err)
		w.WriteHeader(500)
		return
	}
	defer upstreamResp.Body.Close()

	logger.Debug("Upstream response", "status", upstreamResp.StatusCode, "headers", upstreamResp.Header)
	copyHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)
	// If it's a non-200 status from upstream then don't cache
	// This should handle 404s and 401s to request auth
	if upstreamResp.StatusCode/100 != 2 {
		skipCacheReason = "non-2xx upstream response"
	}

	writers := []io.Writer{}
	var manifestBytes bytes.Buffer
	if skipCacheReason == "" {
		logger = logger.With("cache", "miss")
		cacheMisses.Inc()
		writers = append(writers, cacheWriter)
		if object.Type == model.ObjectTypeManifest {
			writers = append(writers, &manifestBytes)
		}
	} else {
		logger = logger.With("cache", "skip", "reason", skipCacheReason)
		cacheSkips.Inc()
	}
	if !isHead {
		writers = append(writers, w)
	}

	err = readIntoWriters(writers, upstreamResp.Body)
	if err != nil {
		logger.Error("Error reading upstream response body", "error", err)
		return // don't cache on error
	}

	if skipCacheReason == "" {
		if object.Type == model.ObjectTypeManifest {
			logger.Debug("Upstream returned manifest", "manifest", manifestBytes.Bytes())
		}
		cacheWriter.Close(upstreamResp.Header.Get(model.HeaderContentType), upstreamResp.Header.Get(model.HeaderDockerContentDigest))
	}
	logger.Info("Served from upstream", "status", upstreamResp.StatusCode)
}

func (s *CacheService) getSkipReason(object *model.ObjectIdentifier) (res string) {
	// No point skipping blobs - the client either wants them or not.
	// Unless there's heavy heavy blobs we shouldn't cache?
	if object.Type == model.ObjectTypeManifest {
		if !s.CacheManifests {
			res = "manifests cache disabled"
		}
		if s.SkipTags != nil {
			if s.SkipTags.MatchString(object.Ref) {
				res = "tag match skip regex"
			}
		}
		if s.PrivateRegistries != nil {
			if _, isPrivate := s.PrivateRegistries[object.Registry]; isPrivate {
				res = "private registry"
			}
		}
		if _, ignoredImage := s.SkipImages[object.Repository]; ignoredImage {
			res = "image on ignore list"
		}
	}
	return res
}

func readIntoWriters(dst []io.Writer, src io.Reader) error {
	buf := *pool.Get().(*[]byte)
	defer pool.Put(&buf)

	for {
		read, rerr := src.Read(buf)
		if rerr != nil && rerr != io.EOF {
			return fmt.Errorf("read error during copy: %w", rerr)
		}
		if read > 0 {
			for _, v := range dst {
				written, werr := v.Write(buf[:read])
				if werr == nil && read != written {
					werr = io.ErrShortWrite
				}
				if werr != nil {
					return fmt.Errorf("write error during copy: %w", rerr)
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

func (s *CacheService) reqWithCreds(url, method string, headers *http.Header, l **slog.Logger) (*http.Response, error) {
	resp, err := request(url, method, headers)
	if err != nil {
		return nil, err
	}

	// retry once with default creds if none provided
	if resp.StatusCode == 401 && headers.Get("Authorization") == "" {
		if defaultCreds, ok := s.DefaultCreds[resp.Request.URL.Host]; ok {
			(*l).Debug("Received 401, retrying with default credentials", "url", url)
			*l = (*l).With("creds", defaultCreds.Username+"@"+resp.Request.URL.Host)
			realm := resp.Header.Get("WWW-Authenticate")
			if strings.HasPrefix(realm, "Basic") {
				headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(defaultCreds.Username+":"+defaultCreds.Password)))
				resp, err = request(url, method, headers)
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
				tokenResp, err := request(tokenUrl, "GET", &theaders)
				if err != nil {
					return nil, err
				}
				body, err := io.ReadAll(tokenResp.Body)
				if err != nil {
					return nil, err
				}
				tokenResp.Body.Close()
				data := map[string]string{}
				json.Unmarshal(body, &data) // only parse out strings, skip ints
				if data["token"] == "" {
					return nil, errors.New("token not found in response")
				}
				headers.Set("Authorization", "Bearer "+data["token"])
				resp, err = request(url, method, headers)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return resp, err
}

func request(url, method string, headers *http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.TODO(), method, url, nil)
	if err != nil {
		return nil, err
	}

	copyHeaders(req.Header, *headers)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
