# containerd-registry-cache

A pull-through registry cache for `containerd` intended for Kubernetes clusters.

### Alternatives
- [jamesorlakin/cacheyd](https://github.com/jamesorlakin/cacheyd) - the original project this fork is based on
- [aceeric/ociregistry](https://github.com/aceeric/ociregistry) - uses stateful disk/memory data, which limits horizontal scaling and high availability

### How it works
- Starting from `containerd` v2, mirror registry requests [include](https://github.com/containerd/containerd/blob/v2.1.3/docs/hosts.md#registry-host-namespace) `?ns=<origin>` query argument. This allows you to set up `containerd` with a single mirror like so: 
  ```bash
  # check that /etc/containerd/config.toml has this already set:
  [plugins."io.containerd.grpc.v1.cri".registry]
  config_path = "/etc/containerd/certs.d"
  
  # configure default mirror for all registries
  mkdir -p /etc/containerd/certs.d/_default
  cat >/etc/containerd/certs.d/_default/hosts.toml <<EOF
  [host."http://localhost:30123"]
    capabilities = ["pull", "resolve"]
  EOF
  # no need to restart containerd, changes are applied automatically
  ```
- With this setup, running `docker pull alpine` results in a request like: 
  `http://localhost:30123/v2/library/alpine/manifests/latest?ns=registry-1.docker.io`
- And `containerd-registry-cache`, listening on `localhost:30123`, has information that this specific manifest should be fetched from `registry-1.docker.io`. That allows to use single cache endpoint for different upstream registries.
- In case `localhost:30123` is not available, `containerd` falls back to the original registry

### How to run
Available as a docker image on dockerhub: https://hub.docker.com/r/sepa/containerd-registry-cache
```bash
$ docker run sepa/containerd-registry-cache -h
Usage of ./containerd-registry-cache:
  -d, --cache-dir string    Cache directory (default "/tmp/data")
  -m, --cache-manifests     Cache manifests (default true)
  -f, --creds-file string   Default credentials to use for registries
  -i, --ignore string       RegEx of tags to ignore caching (default "latest")
  -p, --port int            Port to listen on (default 3000)
  -v, --version             Show version and exit
```
Run it as: 
- `Nodeport` service. This way you can access it from any node on `localhost:<port>`, but only after CNI is started. 
- To make it useful for node-init level images too (like CNI), expose in as `Ingress`. Empty non-configured node should be able to make http requests to such address. But you probably want to prevent external requests to your cache.

### Notes
- Cache volume data could be cleaned up at any time. There is no expiration and built-in auto cleaning. You can implement any cleanup policy via sidecar and `find -del` 
- By default, both Blobs and Manifests (excluding `:latest`) are cached. You can disable Manifests caching altogether (`--cache-manifests=no`), to always re-query all tags (like mutable `:3`, `:3.1`, with immutable `:3.1.2`)
- Pulls for cached private Manifests require no auth. Use with care for private registries!
- You can use "default credentials" `--creds-file` to avoid dockerHub unauthenticated rate limits for example. File is `yaml` with section names equal to corresponding registry hosts:
  ```yaml
  registry-1.docker.io:
    username: puller
    password: secret1
  ghcr.io:
    username: org-pull
    password: secret2
  ```
- Prometheus metrics are available at the same `--port` on `/metrics` endpoint:
  ```ini
  containerd_cache_total{result="hit"}
  containerd_cache_total{result="miss"}
  containerd_cache_total{result="skip"}
  ```
  where `skip` shows number of requests bypassed cache due to `--ignore` or `--cache-manifests=no`

### TODO
- Blobs are cached with no separation of registries "content-addressable storage", so layer space should be reused
- S3 mode
- lock on caching the same uri?
- verify content digest before caching

### How to test locally
Docker distribution [API spec](https://distribution.github.io/distribution/spec/api/) example:
```bash
# get manifest
curl -I localhost:3000/v2/kube-scheduler/manifests/v1.29.1?ns=registry.k8s.io
# get first image manifest
curl -i localhost:3000/v2/kube-scheduler/manifests/sha256:019d7877d15b45951df939efcb941de9315e8381476814a6b6fdf34fc1bee24c?ns=registry.k8s.io
# get first blob
curl -i localhost:3000/v2/kube-scheduler/blobs/sha256:aba5379b9c6dc7c095628fe6598183d680b134c7f99748649dddf07ff1422846?ns=registry.k8s.io
# check cache
ls -lh /tmp/data/
```

[Authentication flow](https://distribution.github.io/distribution/spec/auth/token/) example:
```bash
$ curl -I https://registry-1.docker.io/v2/library/alpine/manifests/latest
HTTP/1.1 401 Unauthorized
www-authenticate: Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/alpine:pull"
# or in case of Basic auth:
# WWW-Authenticate: Basic realm="Registry Realm"

$ curl -i 'https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/alpine:pull' [-u user:pass]
{"token": "xxx", ...

$ curl -i https://registry-1.docker.io/v2/library/alpine/manifests/latest -H'Authorization: Bearer xxx'
```
