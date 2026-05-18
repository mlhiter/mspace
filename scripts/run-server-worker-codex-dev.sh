#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${MSPACE_RUNTIME_TOKEN:-}" ]]; then
  echo "MSPACE_RUNTIME_TOKEN is required. Create a Runtime token in Workspace Settings and export it first." >&2
  exit 2
fi

IMAGE="${MSPACE_WORKER_IMAGE:-mspace-worker-codex-dev:local}"
SERVER_URL="${MSPACE_SERVER_URL:-http://host.docker.internal:8787}"
WORK_ROOT_VOLUME="${MSPACE_WORKER_VOLUME:-mspace-worker-codex-dev-root}"
CONTAINER="${MSPACE_WORKER_CONTAINER:-mspace-worker-codex-dev}"
CODEX_HOME_SOURCE="${MSPACE_WORKER_CODEX_HOME_SOURCE:-${CODEX_HOME:-${HOME}/.codex}}"
CODEX_HOME_DIR="${MSPACE_WORKER_CODEX_HOME_DIR:-${HOME}/.mspace/codex-worker-home}"
CODEX_CLI_VERSION="${MSPACE_WORKER_CODEX_CLI_VERSION:-0.130.0}"

if [[ ! -f "${CODEX_HOME_SOURCE}/auth.json" ]]; then
  echo "Codex auth file was not found at ${CODEX_HOME_SOURCE}/auth.json. Run codex login first, or set MSPACE_WORKER_CODEX_HOME_SOURCE." >&2
  exit 2
fi

mkdir -p "${CODEX_HOME_DIR}"
chmod 700 "${CODEX_HOME_DIR}"
cp "${CODEX_HOME_SOURCE}/auth.json" "${CODEX_HOME_DIR}/auth.json"
chmod 600 "${CODEX_HOME_DIR}/auth.json"
if [[ -f "${CODEX_HOME_SOURCE}/config.toml" ]]; then
  cp "${CODEX_HOME_SOURCE}/config.toml" "${CODEX_HOME_DIR}/config.toml"
else
  : > "${CODEX_HOME_DIR}/config.toml"
fi
if ! grep -Fq '[projects."/var/lib/mspace-worker"]' "${CODEX_HOME_DIR}/config.toml"; then
  {
    printf '\n[projects."/var/lib/mspace-worker"]\n'
    printf 'trust_level = "trusted"\n'
  } >> "${CODEX_HOME_DIR}/config.toml"
fi

RUN_FLAGS=(--rm)
if [[ "${MSPACE_WORKER_DOCKER_TTY:-auto}" != "0" && -t 0 && -t 1 ]]; then
  RUN_FLAGS+=(-it)
fi

docker build \
  -f worker/Dockerfile.codex-dev \
  --build-arg "CODEX_CLI_VERSION=${CODEX_CLI_VERSION}" \
  -t "${IMAGE}" \
  .
docker rm -f "${CONTAINER}" >/dev/null 2>&1 || true

docker run "${RUN_FLAGS[@]}" \
  --name "${CONTAINER}" \
  -e MSPACE_SERVER_URL="${SERVER_URL}" \
  -e MSPACE_RUNTIME_TOKEN="${MSPACE_RUNTIME_TOKEN}" \
  -e MSPACE_WORKER_NAME="${MSPACE_WORKER_NAME:-docker-codex-worker}" \
  -e MSPACE_WORKER_MODE="${MSPACE_WORKER_MODE:-team}" \
  -e MSPACE_WORKER_CAPABILITIES="${MSPACE_WORKER_CAPABILITIES:-{\"protocolSmoke\":true,\"codex\":true,\"dryRun\":false}}" \
  -e MSPACE_WORKER_LABELS="${MSPACE_WORKER_LABELS:-{\"provider\":\"server-worker\",\"environment\":\"docker-codex-dev\"}}" \
  -e MSPACE_WORKER_WORK_ROOT="/var/lib/mspace-worker" \
  -e CODEX_HOME="/var/lib/mspace-codex" \
  -v "${WORK_ROOT_VOLUME}:/var/lib/mspace-worker" \
  -v "${CODEX_HOME_DIR}:/var/lib/mspace-codex" \
  "${IMAGE}"
