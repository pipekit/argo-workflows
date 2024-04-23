## #syntax=docker/dockerfile:1.2
ARG GIT_COMMIT=unknown
ARG GIT_TAG=unknown
ARG GIT_TREE_STATE=unknown

FROM artprod.dev.bloomberg.com/rhel7-dpkg:latest as builder
RUN yum install -y mailcap


RUN apt-get update && apt-get install -y \
    go \
    make \
    ca-certificates \
    wget \
    curl \
    bash

WORKDIR /go/src/github.com/argoproj/argo-workflows
COPY go.mod .
COPY go.sum .
ENV GOPRIVATE="*.dev.bloomberg.com"
ENV GOPROXY="https://goproxy.dev.bloomberg.com,direct"
ENV K3D_IMAGE_TOOLS=artprod.dev.bloomberg.com/mlp/ext/ghcr.io/k3d-io/k3d-tools:5.5.1
ENV K3D_IMAGE_LOADBALANCER=artprod.dev.bloomberg.com/mlp/ext/ghcr.io/k3d-io/k3d-proxy:5.5.1

RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

####################################################################################################

FROM artprod.dev.bloomberg.com/node:21-rhel8-dpkg as argo-ui

RUN apt-get update && apt-get install -y yarn git

# Nuke proxy configs on npm to not mess with host setup
RUN npm config delete noproxy proxy https-proxy -g

# Add support for git credential helper to read from secret
RUN git config --system credential.helper '!f() { sleep 1; echo "username=token"; echo "password=$(cat /run/secrets/GIT_PASSWORD)"; }; f'

COPY ui/package.json ui/yarn.lock ui/

RUN --mount=type=secret,id=GIT_PASSWORD --mount=type=cache,target=/root/.yarn \
  YARN_CACHE_FOLDER=/root/.yarn JOBS=max \
  yarn --cwd ui install --network-timeout 1000000

COPY ui ui
COPY api api

RUN --mount=type=cache,target=/root/.yarn \
  YARN_CACHE_FOLDER=/root/.yarn JOBS=max \
  NODE_OPTIONS="--openssl-legacy-provider --max-old-space-size=2048" JOBS=max yarn --cwd ui build

####################################################################################################

FROM builder as argoexec-build

ARG GIT_COMMIT
ARG GIT_TAG
ARG GIT_TREE_STATE

ENV GOPRIVATE="*.dev.bloomberg.com"
ENV GOPROXY="https://goproxy.dev.bloomberg.com,direct"
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build make dist/argoexec GIT_COMMIT=${GIT_COMMIT} GIT_TAG=${GIT_TAG} GIT_TREE_STATE=${GIT_TREE_STATE}

####################################################################################################

FROM builder as workflow-controller-build

ARG GIT_COMMIT
ARG GIT_TAG
ARG GIT_TREE_STATE

ENV GOPRIVATE="*.dev.bloomberg.com"
ENV GOPROXY="https://goproxy.dev.bloomberg.com,direct"
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build make dist/workflow-controller GIT_COMMIT=${GIT_COMMIT} GIT_TAG=${GIT_TAG} GIT_TREE_STATE=${GIT_TREE_STATE}

####################################################################################################

FROM builder as argocli-build

ARG GIT_COMMIT
ARG GIT_TAG
ARG GIT_TREE_STATE

RUN mkdir -p ui/dist
COPY --from=argo-ui ui/dist/app ui/dist/app
# update timestamp so that `make` doesn't try to rebuild this -- it was already built in the previous stage
RUN touch ui/dist/app/index.html

ENV GOPRIVATE="*.dev.bloomberg.com"
ENV GOPROXY="https://goproxy.dev.bloomberg.com,direct"
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build STATIC_FILES=true make dist/argo GIT_COMMIT=${GIT_COMMIT} GIT_TAG=${GIT_TAG} GIT_TREE_STATE=${GIT_TREE_STATE}

####################################################################################################

FROM artprod.dev.bloomberg.com/distroless/static:latest as argoexec

COPY --from=argoexec-build /go/src/github.com/argoproj/argo-workflows/dist/argoexec /bin/
COPY --from=argoexec-build /etc/mime.types /etc/mime.types
COPY hack/ssh_known_hosts /etc/ssh/
COPY hack/nsswitch.conf /etc/

ENTRYPOINT [ "argoexec" ]

####################################################################################################

FROM artprod.dev.bloomberg.com/distroless/static:latest as workflow-controller

USER 8737

COPY hack/ssh_known_hosts /etc/ssh/
COPY hack/nsswitch.conf /etc/
COPY --chown=8737 --from=workflow-controller-build /go/src/github.com/argoproj/argo-workflows/dist/workflow-controller /bin/

ENTRYPOINT [ "workflow-controller" ]

####################################################################################################

FROM artprod.dev.bloomberg.com/distroless/static:latest as argocli

USER 8737

WORKDIR /home/argo

COPY hack/ssh_known_hosts /etc/ssh/
COPY hack/nsswitch.conf /etc/
COPY --from=argocli-build /go/src/github.com/argoproj/argo-workflows/dist/argo /bin/

ENTRYPOINT [ "argo" ]
