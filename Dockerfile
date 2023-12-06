FROM golang:1.19-alpine AS deps

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

FROM deps as build
COPY . .
RUN CGO_ENABLED=0 go build -ldflags '-w -extldflags "-static"' .

FROM alpine:3.9
COPY --from=build /workspace/cacheyd /usr/local/bin/cacheyd
ENTRYPOINT ["cacheyd"]