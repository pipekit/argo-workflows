#!/bin/bash
yum install -y jq
VERSION="$(jq -r .version < jaazy.json).${USER}"

GIT_COMMIT=$(git rev-parse HEAD)
set -eux
for TARGET in argocli workflow-controller argoexec; do
docker build --target ${TARGET} --secret id=GIT_PASSWORD,env=GIT_PASSWORD  \
  --target workflow-controller --build-arg GIT_COMMIT=${GIT_COMMIT} \
  --build-arg GIT_TREE_STATE=dirty \
  --build-arg GIT_TAG=${VERSION} \
  --build-arg VERSION=${VERSION} \
  -t ${TARGET}:${VERSION} .
done
