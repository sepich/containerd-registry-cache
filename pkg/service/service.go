package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
)

type Service interface {
	GetManifest(repo, ref, registry string, headers *http.Header, w http.ResponseWriter)
	GetBlob(repo, digest, registry string, headers *http.Header, w http.ResponseWriter)
}

var errIsNon200 = errors.New("Registry returned non-200 code")
var client = *&http.Client{}

type CacheydService struct {
	dir string
}

var _ Service = &CacheydService{}

func (s *CacheydService) GetManifest(repo, ref, registry string, headers *http.Header, w http.ResponseWriter) {
	log.Printf("GetManifest: %s, %s, %s", repo, ref, registry)
	log.Printf("GetManifest: %v", headers)

	cacheOrProxy(true, repo, ref, registry, headers, w)
}

func (s *CacheydService) GetBlob(repo, digest, registry string, headers *http.Header, w http.ResponseWriter) {
	cacheOrProxy(false, repo, digest, registry, headers, w)
}

func cacheOrProxy(isManifest bool, repo, ref, registry string, headers *http.Header, w http.ResponseWriter) {
	// TODO: Lookup cache

	w.Header().Add("X-Proxied-By", "cacheyd")
	w.Header().Add("X-Proxied-For", registry)

	url := "https://%s/v2/%s/blobs/%s"
	if isManifest {
		url = "https://%s/v2/%s/manifests/%s"
	}

	upstreamResp, err := proxySuccessOrError(fmt.Sprintf(url, registry, repo, ref), headers.Get("Authorization"))
	if err != nil {
		log.Printf("cacheOrProxy err: %v", err)
		if errors.Is(err, errIsNon200) {
			copyHeaders(w.Header(), upstreamResp.Header)
			w.WriteHeader(upstreamResp.StatusCode)

			readIntoWriters([]io.Writer{w}, upstreamResp.Body)
		}
		return
	}

	copyHeaders(w.Header(), upstreamResp.Header)
	log.Print("Got status ", upstreamResp.StatusCode)
	w.WriteHeader(upstreamResp.StatusCode)
	// TODO: Cache!
	readIntoWriters([]io.Writer{w, &StdouteyBoi{}}, upstreamResp.Body)

	upstreamResp.Body.Close()
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

func proxySuccessOrError(url string, auth string) (*http.Response, error) {
	resp, err := proxy(url, auth)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 200 {
		return resp, err
	} else {
		return resp, errIsNon200
	}
}

func proxy(url string, auth string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.TODO(), "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if auth != "" {
		req.Header.Add("Authorization", auth)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
