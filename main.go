package main

import (
	"net/http"
	"os"
	"strconv"

	"github.com/jamesorlakin/cacheyd/pkg/cache"
	"github.com/jamesorlakin/cacheyd/pkg/mux"
	"github.com/jamesorlakin/cacheyd/pkg/service"
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
	logger = zap.Must(zap.NewDevelopment())
	zap.ReplaceGlobals(logger)
}

func main() {
	logger.Info("Starting cacheyd")

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

	cache := cache.FileCache{
		CacheDirectory: "/tmp/cacheyd",
	}
	router := mux.NewRouter(&service.CacheydService{
		Cache: &cache,
	})

	everything := func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		router.ServeHTTP(w, r)
	}

	err := http.ListenAndServe(listenStr, http.HandlerFunc(everything))
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
