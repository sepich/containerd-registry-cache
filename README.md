# Cacheyd

A simple and transparent registry-agnostic pull-through cache for containerd intended for Kubernetes clusters.

> Why?

Kubernetes makes it really convenient to manage containers, and alongside a managed Kubernetes offerings from cloud vendors it can be quite easy to have a cluster with autoscaling nodes.

However, in some cases being able to burst quickly can bring some unintended side effects.
Chances are you'll have DaemonSets such as node-exporter and Promtail running but these will have to be pulled on every new node you spin up.
Docker Hub has a pull limit over a period spanning 6 hours which can be quite easy to run into.
Bandwidth has costs too, both in terms of cloud egress fees as well as potentially hosted registries such as GitHub Packages.

You could look to baking a virtual machine image with these pre-pulled, but this adds a huge maintenance burden - you'll need to rebuild these images often as vendors release patches or you bump versions of the images in use.

> Why this instead of [Spegel](https://github.com/XenitAB/spegel)?

I think they both have their place.
I only discovered this _after_ starting cacheyd, but I think it's certainly an impressive solution.
Using both could let cacheyd be a more durable cache for complete node replacements and/or infrequently pulled images which may be cleaned up from nodes.

## Goals

- Ephemeral: Cacheyd should be ok to be wiped at any time. It might cause an issue for in-progress pulls but they should retry.
- Transparent: containerd provides the namespace (image registry) in requests. Any cache misses are passed by cacheyd through to the original registry unaltered.
- Simple: Using a NodePort, no customisation beyond light containerd config to any virtual machine image used for Kubernetes nodes.

## Limitations

- cacheyd does **not** currently expire any items in the cache.
- Pulls of cached images will not require authentication, including images from private registries. This is true of any pod created without `imagePullSecrets` if the image happens to already exist on a node even without cacheyd.
- This is effectively alpha-level software at this point in time, but it's quite simple and there's not much to go wrong.


## Installation

Run an instance (or more, assuming there's shared storage or session affinity). This need not live in a Kubernetes cluster and could be shared between them if it works for your needs, either running in a container or just running the Go binary.

### Binary Options

Cacheyd can be configured via environment variables:
- `PORT`: The port to listen on over HTTP, defaulting to `3000`.
- `CACHE_DIR`: The directory to write cache data to. Will be auto-created if it doesn't exist. Defaults to `/tmp/cacheyd`.

### Kubernetes Manifest Example

This will get a simple copy of cacheyd up and running while being accessible on port `30123`.
It's using ephemeral storage, so I recommend changing it to use a PersistentVolumeClaim.

<details>
  <summary>See YAML</summary>

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: cacheyd
  name: cacheyd
  namespace: kube-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cacheyd
  template:
    metadata:
      labels:
        app: cacheyd
    spec:
      containers:
      - image: cacheyd
        imagePullPolicy: IfNotPresent
        name: cacheyd
        ports:
        - containerPort: 3000
          name: http
---
apiVersion: v1
kind: Service
metadata:
  name: cacheyd
  namespace: kube-system
  labels:
    app: cacheyd
spec:
  ports:
  - port: 3000
    targetPort: http
    name: http
    nodePort: 30123
  selector:
    app: cacheyd
  type: NodePort
  sessionAffinity: ClientIP
```

If you also use the Prometheus operator:
```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: cacheyd
  namespace: monitoring
spec:
  selector:
    matchLabels:
      app: cacheyd
  namespaceSelector:
    matchNames:
      - kube-system
  endpoints:
    - port: http
      path: /metrics
      scheme: http
```
</details>

### Configuring containerd

The main `/etc/containerd/config.toml` should have a configuration to use registry configurations living in a folder.
Beware that some Kubernetes implementations may have a script that populates this config upon node boot (e.g EKS' `bootstrap.sh`) or have it in a different location (e.g. K3S generating it to somewhere in `/var/lib/rancher/k3s/...`).

This is already configured correctly out of the box for AWS EKS' AMIs.

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

That's all the config needed for containerd to try `localhost:30123` first before falling back to whatever registry it would've tried originally if cacheyd is unavailable.

If you'd like to use cacheyd for only one registry, replace `_default` with whatever registry you'd like to mirror, e.g. `/etc/containerd/certs.d/docker.io/hosts.toml`.
Any matching `hosts.toml` will be used instead of the one in `_default` if it exists.

If instead you'd like to _only_ use cacheyd and not let the node fallback to the registry itself, use this in `/etc/containerd/certs.d/_default/hosts.toml` instead:
```toml
server = "http://localhost:30123"
```

#### A Terraform & EKS example

A (managed) EKS node group can have a custom launch template defined.
You'll need to edit the `user_data` so that it writes the simple containerd config before joining the cluster, avoiding the need to create your own AMI.

<details>
  <summary>See Terraform</summary>

```
locals {
  eks_cluster_name = "my-cluster"
}

resource "aws_launch_template" "template" {
  name_prefix            = locals.eks_cluster_name
  update_default_version = true

  # Other required params ommitted for brevity (e.g. instance type and AMI ID)

  user_data = base64encode(<<-EOF
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="==BOUNDARY=="
--==BOUNDARY==
Content-Type: text/x-shellscript; charset="us-ascii"
#!/bin/bash

mkdir -p /etc/containerd/certs.d/_default
cat << __EOF__ > /etc/containerd/certs.d/_default/hosts.toml
[host."http://localhost:30123"]
  capabilities = ["pull", "resolve"]
__EOF__

/etc/eks/bootstrap.sh ${locals.eks_cluster_name}
--==BOUNDARY==--\
  EOF
  )
}

resource "aws_eks_node_group" "primary" {
  cluster_name           = aws_eks_cluster.cluster.name
  node_group_name_prefix = locals.eks_cluster_name

  # Other required params ommitted for brevity

  launch_template {
    id      = aws_launch_template.template["primary"].id
    version = aws_launch_template.template["primary"].latest_version
  }
}
```
</details>

## Monitoring cacheyd

Cacheyd provides metrics on the `/metrics` HTTP endpoint. This currently includes the hits and misses counters for the cache.
