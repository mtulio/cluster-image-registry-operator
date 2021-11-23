#!/bin/sh

DT_VERSION=$(date +%Y%m%d%H%M%S)
export VERSION="v${DT_VERSION}"

PRID=724
VER_REPO=ups_pr${PRID}_${VERSION}

podman build --authfile ~/.redhat/pull-secret.json  -f Dockerfile -t quay.io/${QUAY_USER}/cluster-image-registry-operator:${VER_REPO} .

podman push quay.io/${QUAY_USER}/cluster-image-registry-operator:${VER_REPO}

echo ${VER_REPO}
echo "quay.io/${QUAY_USER}/cluster-image-registry-operator:${VER_REPO}"
