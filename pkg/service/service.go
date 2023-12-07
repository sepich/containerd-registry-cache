package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/jamesorlakin/cacheyd/pkg/cache"
	"github.com/jamesorlakin/cacheyd/pkg/model"
)

type Service interface {
	GetManifest(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter)
	GetBlob(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter)
}

var client = *&http.Client{}

type CacheydService struct {
	Cache cache.CachingService
}

var _ Service = &CacheydService{}

func (s *CacheydService) GetManifest(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	log.Printf("GetManifest: %s, %s, %s", object.Repository, object.Ref, object.Registry)
	log.Printf("GetManifest: isHead %v", isHead)
	log.Printf("GetManifest: %v", headers)

	s.cacheOrProxy(object, isHead, headers, w)
}

func (s *CacheydService) GetBlob(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	log.Printf("GetBlob: %s, %s, %s", object.Repository, object.Ref, object.Registry)
	log.Printf("GetBlob: isHead %v", isHead)
	log.Printf("GetBlob: %v", headers)

	s.cacheOrProxy(object, isHead, headers, w)
}

func (s *CacheydService) cacheOrProxy(object *model.ObjectIdentifier, isHead bool, headers *http.Header, w http.ResponseWriter) {
	w.Header().Add("X-Proxied-By", "cacheyd")
	w.Header().Add("X-Proxied-For", object.Registry)

	// TODO: Lookup cache
	cached, cacheWriter, err := s.Cache.GetCache(object)

	if err != nil {
		w.WriteHeader(500)
		return
	}

	if cached != nil {
		log.Printf("cacheOrProxy got cache!: %v", cached)
		w.Header().Add("Content-Size", strconv.Itoa(int(cached.SizeBytes)))

		reader, _ := cached.GetReader()
		readIntoWriters([]io.Writer{w}, reader)
		reader.Close()
		return
	}

	url := "https://%s/v2/%s/blobs/%s"
	if object.Type == model.ObjectTypeManifest {
		url = "https://%s/v2/%s/manifests/%s"
	}

	method := "GET"
	if isHead {
		method = "HEAD"
	}
	upstreamResp, err := proxySuccessOrError(fmt.Sprintf(url, object.Registry, object.Repository, object.Ref), method, headers)
	log.Printf("cacheOrProxy got headers: %v", upstreamResp.Header)

	if err != nil {
		log.Printf("cacheOrProxy err: %v", err)
		if errors.Is(err, &Non200Error{}) {
			copyHeaders(w.Header(), upstreamResp.Header)
			w.WriteHeader(upstreamResp.StatusCode)

			if !isHead {
				readIntoWriters([]io.Writer{w}, upstreamResp.Body)
			}
		}
		upstreamResp.Body.Close()
		return
	}
	defer upstreamResp.Body.Close()

	copyHeaders(w.Header(), upstreamResp.Header)
	log.Print("Got status ", upstreamResp.StatusCode)
	w.WriteHeader(upstreamResp.StatusCode)

	// TODO: Cache!

	writers := []io.Writer{w, cacheWriter}
	if object.Type == model.ObjectTypeManifest {
		writers = append(writers, &StdouteyBoi{})
	}
	if !isHead {
		readIntoWriters(writers, upstreamResp.Body)
		cacheWriter.Close()
	}
}

type StdouteyBoi struct{}

func (s *StdouteyBoi) Write(p []byte) (n int, err error) {
	return log.Writer().Write(p)
}

func readIntoWriters(dst []io.Writer, src io.Reader) error {
	buf := make([]byte, 32*1024)
	var written int64
	for {
		nr, rerr := src.Read(buf)
		if rerr != nil && rerr != io.EOF && rerr != context.Canceled {
			log.Printf("httputil: ReverseProxy read error during body copy: %v", rerr)
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
				log.Printf("httputil: EOF reached")
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

	// auth := headers.Get("Authorization")
	// if auth != "" {
	// 	req.Header.Add("Authorization", auth)
	// }

	// Copy headers such as Accept and Authorization, are there any we want to skip?
	copyHeaders(req.Header, *headers)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
