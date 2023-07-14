#!/bin/bash

apt-get update && apt-get install go node jq

# execute this on devx spaces to setup once
curl -skLx '' https://releases.mlp.dev.bloomberg.com/install.sh | sh -s tools/kubectl
curl -skLx '' https://releases.mlp.dev.bloomberg.com/install.sh | sh -s tools/kit
curl -skLx '' https://releases.mlp.dev.bloomberg.com/install.sh | sh -s tools/k3d

# deploy airgapped kubernetes
export K3D_IMAGE_LOADBALANCER=artprod.dev.bloomberg.com/mlp/ext/ghcr.io/k3d-io/k3d-proxy:5.5.1
export K3D_IMAGE_TOOLS=artprod.dev.bloomberg.com/mlp/ext/ghcr.io/k3d-io/k3d-tools:5.5.1
k3d registry create myregistry.localhost \
    --port 12345 \
    --image artprod.dev.bloomberg.com/mlp/ext/docker.io/library/registry:2
k3d cluster create \
    --image artprod.dev.bloomberg.com/mlp/ext/k3s-air-gapped:v1.26.6-k3s1 \
    --api-port 127.0.0.1:6443 \
    --registry-use k3d-myregistry.localhost:12345

cat <<EOF >>/etc/hosts
127.0.0.1 dex
127.0.0.1 minio
127.0.0.1 postgres
127.0.0.1 mysql
127.0.0.1 azurite

# setup golang
export GOPRIVATE="*.dev.bloomberg.com"
export GOPROXY="https://goproxy.dev.bloomberg.com,direct"

# download dependencies and do first-pass compile
CI=1 kit pre-up

