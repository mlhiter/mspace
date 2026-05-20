#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REGISTRY="${REGISTRY:-crpi-7jr40k6elhldekqp.cn-hangzhou.personal.cr.aliyuncs.com/mlhiter}"
TAG="${TAG:-k8s-$(date +%Y%m%d%H%M)-$(git -C "${ROOT}" rev-parse --short HEAD)}"
PLATFORM="${PLATFORM:-linux/amd64}"
PUSH="${PUSH:-1}"
BUILDER="${BUILDER:-}"

SERVER_IMAGE="${SERVER_IMAGE:-${REGISTRY}/mspace-server:${TAG}}"
WORKER_IMAGE="${WORKER_IMAGE:-${REGISTRY}/mspace-worker-codex:${TAG}}"

build_args=()
if [[ -n "${BUILDER}" ]]; then
  build_args+=(--builder "${BUILDER}")
fi
if [[ "${PUSH}" == "1" ]]; then
  build_args+=(--push)
else
  build_args+=(--load)
fi

docker buildx build "${build_args[@]}" \
  --platform "${PLATFORM}" \
  -f "${ROOT}/deploy/docker/server.Dockerfile" \
  -t "${SERVER_IMAGE}" \
  "${ROOT}"

docker buildx build "${build_args[@]}" \
  --platform "${PLATFORM}" \
  -f "${ROOT}/deploy/docker/worker-codex.Dockerfile" \
  -t "${WORKER_IMAGE}" \
  "${ROOT}"

cat <<EOF
SERVER_IMAGE=${SERVER_IMAGE}
WORKER_IMAGE=${WORKER_IMAGE}
TAG=${TAG}
PLATFORM=${PLATFORM}
EOF
