FROM golang:1.24 AS builder
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN make build

# gcr.io/distroless/static-debian11:nonroot
FROM alpine:3
COPY --from=builder /app/containerd-registry-cache /containerd-registry-cache
ENTRYPOINT ["/containerd-registry-cache"]
CMD ["--help"]
