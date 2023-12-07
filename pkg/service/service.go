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
	GetManifest(repo, ref, registry string, isHead bool, headers *http.Header, w http.ResponseWriter)
	GetBlob(repo, digest, registry string, isHead bool, headers *http.Header, w http.ResponseWriter)
}

var errIsNon200 = errors.New("Registry returned non-200 code")
var client = *&http.Client{}

type CacheydService struct {
	dir string
}

var _ Service = &CacheydService{}

func (s *CacheydService) GetManifest(repo, ref, registry string, isHead bool, headers *http.Header, w http.ResponseWriter) {
	log.Printf("GetManifest: %s, %s, %s", repo, ref, registry)
	log.Printf("GetManifest: isHead %v", isHead)
	log.Printf("GetManifest: %v", headers)

	cacheOrProxy(true, isHead, repo, ref, registry, headers, w)
}

func (s *CacheydService) GetBlob(repo, digest, registry string, isHead bool, headers *http.Header, w http.ResponseWriter) {
	log.Printf("GetBlob: %s, %s, %s", repo, digest, registry)
	log.Printf("GetBlob: isHead %v", isHead)
	log.Printf("GetBlob: %v", headers)

	cacheOrProxy(false, isHead, repo, digest, registry, headers, w)
}

func cacheOrProxy(isManifest bool, isHead bool, repo, ref, registry string, headers *http.Header, w http.ResponseWriter) {
	// TODO: Lookup cache

	w.Header().Add("X-Proxied-By", "cacheyd")
	w.Header().Add("X-Proxied-For", registry)

	url := "https://%s/v2/%s/blobs/%s"
	if isManifest {
		url = "https://%s/v2/%s/manifests/%s"
	}

	// In thory a HEAD should return the same headers as asking for a GET
	// However, at least for quay.io, the Docker-Content-Digest is wrong for a GET of a manifest.
	// In my tinkering this resulted in a hash which couldn't be queried as a manifest or a blob.
	// So lets just actually ask the regstry directly like how containerd is trying.
	method := "GET"
	if isHead {
		method = "HEAD"
	}
	upstreamResp, err := proxySuccessOrError(fmt.Sprintf(url, registry, repo, ref), method, headers)
	log.Printf("cacheOrProxy got headers: %v", upstreamResp.Header)

	if err != nil {
		log.Printf("cacheOrProxy err: %v", err)
		if errors.Is(err, errIsNon200) {
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

	writers := []io.Writer{w}
	if isManifest {
		writers = append(writers, &StdouteyBoi{})
	}
	if !isHead {
		readIntoWriters(writers, upstreamResp.Body)
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
		return resp, errIsNon200
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

	// Copy headers such as Accept and Authorization
	copyHeaders(req.Header, *headers)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
