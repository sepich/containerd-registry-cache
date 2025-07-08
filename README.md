# containerd-registry-cache

A pull-through cache for containerd intended for Kubernetes clusters.

### Alternatives
- [jamesorlakin/cacheyd](https://github.com/jamesorlakin/cacheyd) - origin of this fork
- [aceeric/ociregistry](https://github.com/aceeric/ociregistry) - stateful disk state, preventing horizontal scaling or HA


### Local test

```bash
# get manifest
curl -i localhost:3000/v2/kube-scheduler/manifests/v1.29.1?ns=registry.k8s.io
# get first image manifest
curl -i localhost:3000/v2/kube-scheduler/manifests/sha256:019d7877d15b45951df939efcb941de9315e8381476814a6b6fdf34fc1bee24c?ns=registry.k8s.io
# get first blob
curl -i localhost:3000/v2/kube-scheduler/blobs/sha256:aba5379b9c6dc7c095628fe6598183d680b134c7f99748649dddf07ff1422846?ns=registry.k8s.io
# check cache
ls -lh /tmp/data/
```

Challenge–response authentication flow:
```bash
$ curl -i https://registry-1.docker.io/v2/library/alpine/manifests/latest
HTTP/1.1 401 Unauthorized
www-authenticate: Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/alpine:pull"

# or in case of Basic auth:
# WWW-Authenticate: Basic realm="Registry Realm"

$ curl -i 'https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/alpine:pull' [-u u:p]
{"token": "xxx", ...

$ curl -i https://registry-1.docker.io/v2/library/alpine/manifests/latest -H'Authorization: Bearer xxx'
```
