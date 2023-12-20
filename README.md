# Cacheyd

A simple and transparent registry-agnostic pull-through cache for Kubernetes clusters using containerd.

> Why?

Kubernetes makes it really convenient to manage containers, and alongside a managed Kubernetes offerings from cloud vendors it can be quite easy to have a cluster with autoscaling nodes.

However, in some cases being able to burst quickly can bring some unintended side effects.
Chances are you'll have DaemonSets such as node-exporter and Promtail running but these will have to be pulled on every new node you spin up.
Docker Hub has a pull limit over a period spanning 6 hours which can be quite easy to run into.
Bandwidth has costs too.

You could look to baking a virtual machine image with these pre-pulled, but this adds a huge maintenance burden - you'll need to rebuild these images often as vendors release patches or you bump versions of the images in use.

## Goals

- Ephemeral: Cacheyd should be ok to be wiped at any time. It might cause an issue for in-progress pulls but they should retry.
- Transparent: Using containerd's namespace parameter we can support multiple registries transparently. Any cache misses are passed through to the original registry unaltered.
- Simple: Using a NodePort, no customisation beyond light containerd config to any virtual machine image used for Kubernetes nodes.

## Installation

Run an instance (or more, assuming there's shared storage or session affinity). This need not live in a Kubernetes cluster and could be shared between them if it works for your needs.

### Binary Options

Cacheyd can be configured via environment variables:
> TODO

### Kubernetes Manifest Example

> TODO

### Configuring containerd

The main `containerd.toml` should have a configuration to use the host configuraitons in a folder. This is true out of the box for AWS EKS' AMIs:
```toml
[plugins."io.containerd.grpc.v1.cri".registry]
config_path = "/etc/containerd/certs.d"
```

You can then specify a `_default/hosts.toml` in the referenced directory pointing to wherever cacheyd can be reached.

e.g. `/etc/containerd/certs.d/_default/hosts.toml`:
```toml
[host."http://localhost:30123"]
  capabilities = ["pull", "resolve"]
```

That's all the config needed for containerd to try `localhost:30123`` first before falling back to whatever registry it would've tried originally.

If you'd like to use cacheyd for only one registry, replace `_default` with whatever registry you'd like to mirror, e.g. `docker.io/hosts.toml`.

Any matching `hosts.toml` will be used instead of the one in `_default` if it exists.

If instead you'd like to _only_ use cacheyd and not let the node fallback to the registry itself, use this in `/etc/containerd/certs.d/_default/hosts.toml` instead:
```toml
server = "http://localhost:30123"
```

#### A Terraform & EKS example

Using a launch template we can run...
> TODO

## Monitoring cacheyd

Cacheyd provides metrics on the `/metrics` HTTP endpoint. This currently includes the hits and misses counters for the cache.
