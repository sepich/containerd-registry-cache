# Chatty Cacheyd

A simple transparent caching image proxy for Kubernetes clusters using containerd.

## Goals

- Ephemeral storage: Cacheyd should be ok to be wiped at any time. It might cause an issue for in-progress pulls but they should retry.
- Sync: Replicas of cacheyd should gossip and share their cache
- Transparent: Using containerd's namespace parameter we can support multiple registries transparently. Any cache misses are passed through to the registry unaltered.

## Configuring containerd

`/etc/containerd/certs.d/_default/hosts.toml`
```
[host."http://localhost:30123"]
  capabilities = ["pull", "resolve"]
```

Will fallback to normal server if not given `server=` in the root of the file.
