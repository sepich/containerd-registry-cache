FROM golang:1.19-alpine AS build
WORKDIR /workspace
COPY . .
RUN CGO_ENABLED=0 go build -ldflags '-w -extldflags "-static"' .

FROM alpine:3.9
COPY --from=build /workspace/cacheyd /usr/local/bin/cacheyd
ENTRYPOINT ["cacheyd"]