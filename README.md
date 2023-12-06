# Chatty Cacheyd

A simple transparent caching image proxy for Kubernetes clusters using containerd.

`/etc/containerd/certs.d/_default/hosts.toml`
```
[host."http://localhost:30123"]
  capabilities = ["pull", "resolve"]
```

Will fallback to normal server if not given `server=` in the root of the file.
