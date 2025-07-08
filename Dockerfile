FROM golang:1.24 AS builder
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN make build

FROM gcr.io/distroless/static-debian11:nonroot
COPY --from=builder /app/containerd-registry-cache /containerd-registry-cache
ENTRYPOINT ["/containerd-registry-cache"]
CMD ["--help"]
