#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${MSPACE_RUNTIME_TOKEN:-}" ]]; then
  echo "MSPACE_RUNTIME_TOKEN is required. Create a Team Runtime token in Workspace Settings and export it first." >&2
  exit 2
fi

IMAGE="${MSPACE_WORKER_IMAGE:-mspace-worker-dev:local}"
SERVER_URL="${MSPACE_SERVER_URL:-http://host.docker.internal:8787}"
WORK_ROOT_VOLUME="${MSPACE_WORKER_VOLUME:-mspace-worker-dev-root}"

docker build -f worker/Dockerfile.dev -t "${IMAGE}" .

docker run --rm -it \
  --name "${MSPACE_WORKER_CONTAINER:-mspace-worker-dev}" \
  -e MSPACE_SERVER_URL="${SERVER_URL}" \
  -e MSPACE_RUNTIME_TOKEN="${MSPACE_RUNTIME_TOKEN}" \
  -e MSPACE_WORKER_NAME="${MSPACE_WORKER_NAME:-docker-server-worker}" \
  -e MSPACE_WORKER_MODE="${MSPACE_WORKER_MODE:-team}" \
  -e MSPACE_WORKER_CAPABILITIES="${MSPACE_WORKER_CAPABILITIES:-{\"protocolSmoke\":true,\"codex\":true,\"dryRun\":true}}" \
  -e MSPACE_WORKER_LABELS="${MSPACE_WORKER_LABELS:-{\"provider\":\"server-worker\",\"environment\":\"docker-dev\"}}" \
  -e MSPACE_WORKER_WORK_ROOT="/var/lib/mspace-worker" \
  -v "${WORK_ROOT_VOLUME}:/var/lib/mspace-worker" \
  "${IMAGE}"
