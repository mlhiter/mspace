package control

const workerInstallScript = `#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${MSPACE_SERVER_URL:-}" ]]; then
  echo "MSPACE_SERVER_URL is required." >&2
  exit 2
fi
if [[ -z "${MSPACE_RUNTIME_TOKEN:-}" ]]; then
  echo "MSPACE_RUNTIME_TOKEN is required." >&2
  exit 2
fi

MSPACE_WORKER_MODE="${MSPACE_WORKER_MODE:-team}"
MSPACE_WORKER_NAME="${MSPACE_WORKER_NAME:-mspace-worker-$(hostname | tr -cd '[:alnum:]-' | tr '[:upper:]' '[:lower:]')}"
MSPACE_WORKER_IMAGE="${MSPACE_WORKER_IMAGE:-crpi-7jr40k6elhldekqp.cn-hangzhou.personal.cr.aliyuncs.com/mlhiter/mspace-worker-codex:dev}"
MSPACE_WORKER_VOLUME="${MSPACE_WORKER_VOLUME:-mspace-worker-${MSPACE_WORKER_NAME}}"
MSPACE_WORKER_CONTAINER="${MSPACE_WORKER_CONTAINER:-${MSPACE_WORKER_NAME}}"
MSPACE_WORKER_CODEX_HOME_SOURCE="${MSPACE_WORKER_CODEX_HOME_SOURCE:-${CODEX_HOME:-${HOME}/.codex}}"
MSPACE_WORKER_CODEX_HOME_DIR="${MSPACE_WORKER_CODEX_HOME_DIR:-${HOME}/.mspace/codex-worker-home}"
MSPACE_WORKER_CAPABILITIES="${MSPACE_WORKER_CAPABILITIES:-{\"protocolSmoke\":true,\"codex\":true,\"docker\":true,\"kubectl\":true,\"buildkit\":false,\"dryRun\":false}}"
MSPACE_WORKER_LABELS="${MSPACE_WORKER_LABELS:-{\"provider\":\"self-host\",\"environment\":\"docker\"}}"

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is required to install the mspace worker." >&2
  exit 2
fi
if [[ ! -f "${MSPACE_WORKER_CODEX_HOME_SOURCE}/auth.json" ]]; then
  echo "Codex auth file was not found at ${MSPACE_WORKER_CODEX_HOME_SOURCE}/auth.json." >&2
  echo "Run codex login first, or set MSPACE_WORKER_CODEX_HOME_SOURCE." >&2
  exit 2
fi

mkdir -p "${MSPACE_WORKER_CODEX_HOME_DIR}"
chmod 700 "${MSPACE_WORKER_CODEX_HOME_DIR}"
cp "${MSPACE_WORKER_CODEX_HOME_SOURCE}/auth.json" "${MSPACE_WORKER_CODEX_HOME_DIR}/auth.json"
chmod 600 "${MSPACE_WORKER_CODEX_HOME_DIR}/auth.json"
if [[ -f "${MSPACE_WORKER_CODEX_HOME_SOURCE}/config.toml" ]]; then
  cp "${MSPACE_WORKER_CODEX_HOME_SOURCE}/config.toml" "${MSPACE_WORKER_CODEX_HOME_DIR}/config.toml"
else
  : > "${MSPACE_WORKER_CODEX_HOME_DIR}/config.toml"
fi
if ! grep -Fq '[projects."/var/lib/mspace-worker"]' "${MSPACE_WORKER_CODEX_HOME_DIR}/config.toml"; then
  {
    printf '\n[projects."/var/lib/mspace-worker"]\n'
    printf 'trust_level = "trusted"\n'
  } >> "${MSPACE_WORKER_CODEX_HOME_DIR}/config.toml"
fi

docker rm -f "${MSPACE_WORKER_CONTAINER}" >/dev/null 2>&1 || true
docker pull "${MSPACE_WORKER_IMAGE}"
docker run -d \
  --restart unless-stopped \
  --name "${MSPACE_WORKER_CONTAINER}" \
  -e MSPACE_SERVER_URL="${MSPACE_SERVER_URL}" \
  -e MSPACE_RUNTIME_TOKEN="${MSPACE_RUNTIME_TOKEN}" \
  -e MSPACE_WORKER_NAME="${MSPACE_WORKER_NAME}" \
  -e MSPACE_WORKER_MODE="${MSPACE_WORKER_MODE}" \
  -e MSPACE_WORKER_CAPABILITIES="${MSPACE_WORKER_CAPABILITIES}" \
  -e MSPACE_WORKER_LABELS="${MSPACE_WORKER_LABELS}" \
  -e MSPACE_WORKER_WORK_ROOT="/var/lib/mspace-worker" \
  -e CODEX_HOME="/var/lib/mspace-codex" \
  -v "${MSPACE_WORKER_VOLUME}:/var/lib/mspace-worker" \
  -v "${MSPACE_WORKER_CODEX_HOME_DIR}:/var/lib/mspace-codex" \
  "${MSPACE_WORKER_IMAGE}"

echo "mspace worker '${MSPACE_WORKER_NAME}' is starting."
echo "Check Workspace Settings after the first heartbeat."
`
