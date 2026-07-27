#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REGISTRY="${REGISTRY:-crpi-7jr40k6elhldekqp.cn-hangzhou.personal.cr.aliyuncs.com/mlhiter}"
REPOSITORY_VERSION="$(node -p "require('${ROOT}/package.json').version")"
SOURCE_COMMIT_SHA="$(git -C "${ROOT}" rev-parse HEAD)"
BUILD_VERSION="${BUILD_VERSION:-${REPOSITORY_VERSION}}"
BUILD_COMMIT_SHA="${BUILD_COMMIT_SHA:-${SOURCE_COMMIT_SHA}}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
BUILD_SOURCE="${BUILD_SOURCE:-https://github.com/mlhiter/mspace}"
TAG="${TAG:-v${BUILD_VERSION}-${BUILD_COMMIT_SHA:0:12}}"
PLATFORM="${PLATFORM:-linux/amd64}"
PUSH="${PUSH:-1}"
BUILDER="${BUILDER:-}"

SERVER_IMAGE="${SERVER_IMAGE:-${REGISTRY}/mspace-server:${TAG}}"
WORKER_IMAGE="${WORKER_IMAGE:-${REGISTRY}/mspace-worker-codex:${TAG}}"

if [[ ! "${BUILD_VERSION}" =~ ^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$ ]]; then
  echo "Invalid BUILD_VERSION: ${BUILD_VERSION}" >&2
  exit 1
fi
if [[ "${BUILD_VERSION}" != "${REPOSITORY_VERSION}" ]]; then
  echo "BUILD_VERSION must match package.json (${REPOSITORY_VERSION})" >&2
  exit 1
fi
if [[ ! "${BUILD_COMMIT_SHA}" =~ ^[0-9a-f]{40,64}$ ]]; then
  echo "BUILD_COMMIT_SHA must be a full lowercase Git SHA" >&2
  exit 1
fi
if [[ "${BUILD_COMMIT_SHA}" != "${SOURCE_COMMIT_SHA}" ]]; then
  echo "BUILD_COMMIT_SHA must match checkout HEAD (${SOURCE_COMMIT_SHA})" >&2
  exit 1
fi
build_inputs=(
  package.json
  .dockerignore
  server
  worker
  deploy/docker
  deploy/scripts/build-images.sh
)
dirty_build_inputs="$(git -C "${ROOT}" status --porcelain --untracked-files=all -- "${build_inputs[@]}")"
if [[ -n "${dirty_build_inputs}" ]]; then
  echo "Container build inputs must be clean and committed:" >&2
  echo "${dirty_build_inputs}" >&2
  exit 1
fi
node -e '
const value = process.argv[1];
if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value) || Number.isNaN(Date.parse(value))) {
  console.error("BUILD_TIME must be an explicit RFC3339 timestamp");
  process.exit(1);
}
' "${BUILD_TIME}"

metadata_dir="$(mktemp -d)"
trap 'rm -rf "${metadata_dir}"' EXIT
server_metadata="${metadata_dir}/server.json"
worker_metadata="${metadata_dir}/worker.json"

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
  --build-arg "BUILD_VERSION=${BUILD_VERSION}" \
  --build-arg "BUILD_COMMIT_SHA=${BUILD_COMMIT_SHA}" \
  --build-arg "BUILD_TIME=${BUILD_TIME}" \
  --build-arg "BUILD_SOURCE=${BUILD_SOURCE}" \
  --metadata-file "${server_metadata}" \
  -f "${ROOT}/deploy/docker/server.Dockerfile" \
  -t "${SERVER_IMAGE}" \
  "${ROOT}"

docker buildx build "${build_args[@]}" \
  --platform "${PLATFORM}" \
  --build-arg "BUILD_VERSION=${BUILD_VERSION}" \
  --build-arg "BUILD_COMMIT_SHA=${BUILD_COMMIT_SHA}" \
  --build-arg "BUILD_TIME=${BUILD_TIME}" \
  --build-arg "BUILD_SOURCE=${BUILD_SOURCE}" \
  --metadata-file "${worker_metadata}" \
  -f "${ROOT}/deploy/docker/worker-codex.Dockerfile" \
  -t "${WORKER_IMAGE}" \
  "${ROOT}"

read_digest() {
  node -e '
const fs = require("node:fs");
const metadata = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
process.stdout.write(metadata["containerimage.digest"] || "");
' "$1"
}

SERVER_DIGEST="$(read_digest "${server_metadata}")"
WORKER_DIGEST="$(read_digest "${worker_metadata}")"
if [[ "${PUSH}" == "1" && ( -z "${SERVER_DIGEST}" || -z "${WORKER_DIGEST}" ) ]]; then
  echo "Pushed image metadata did not include both content digests" >&2
  exit 1
fi
for digest in "${SERVER_DIGEST}" "${WORKER_DIGEST}"; do
  if [[ -n "${digest}" && ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "Build metadata returned an invalid image digest: ${digest}" >&2
    exit 1
  fi
done

cat <<EOF
SERVER_IMAGE=${SERVER_IMAGE}
SERVER_DIGEST=${SERVER_DIGEST:-not-reported}
WORKER_IMAGE=${WORKER_IMAGE}
WORKER_DIGEST=${WORKER_DIGEST:-not-reported}
TAG=${TAG}
PLATFORM=${PLATFORM}
BUILD_VERSION=${BUILD_VERSION}
BUILD_COMMIT_SHA=${BUILD_COMMIT_SHA}
BUILD_TIME=${BUILD_TIME}
BUILD_SOURCE=${BUILD_SOURCE}
EOF
