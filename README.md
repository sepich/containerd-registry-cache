# containerd-registry-cache

A pull-through registry cache for containerd intended for Kubernetes clusters.

### Alternatives
- [jamesorlakin/cacheyd](https://github.com/jamesorlakin/cacheyd) - origin of this fork
- [aceeric/ociregistry](https://github.com/aceeric/ociregistry) - stateful disk/mem state, preventing horizontal scaling or HA

### How it works
- Starting with v2 of `containerd` mirror registry requests [now have](https://github.com/containerd/containerd/blob/v2.1.3/docs/hosts.md#registry-host-namespace) `?ns=<origin>` query argument. That means you can configure containerd to use a single mirror: 
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
- That makes `docker pull alpine` to make such request: `http://localhost:30123/v2/library/alpine/manifests/latest?ns=registry-1.docker.io`
- And `containerd-registry-cache` (listening on localhost:30123) can understand that such manifest should be downloaded (and cached) from `registry-1.docker.io`
- In case `localhost:30123` is not available, containerd falls back to original registry

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
- Cache volume data could be cleaned up at any time. There is no expiration and built-in auto cleaning. You can implement any cleanup policy you want via sidecar and `find -del` 
- By default, both Blobs and Manifests (excluding `:latest`) are cached. You can disable (`--cache-manifests=no`) Manifests caching altogether, to always re-query all tags, like mutable `:3`, `:3.1` with immutable `:3.1.2`.
- Pulls for cached private Manifests require no auth. Use with care for private registries!
- You can use "default credentials" to avoid dockerHub unauthenticated rate limits for example. File is `yaml` with section names equal to corresponding registry hosts:
  ```yaml
  registry-1.docker.io:
    username: puller
    password: secret1
  ghcr.io:
    username: org-pull
    password: secret2
  ```

### TODO
- Blobs are cached with no separation of registries "content-addressable storage", so layer space should be reused
- S3 mode

### How to test locally

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

Challenge–response authentication flow:
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
