#!/bin/sh
set -e

log_dir="${MSPACE_CODEX_RUN_LOG_DIR:-$HOME/.mspace/logs}"
mkdir -p "$log_dir"
log_file="$log_dir/codex-run-$(date +%Y%m%d-%H%M%S).log"
latest_log="$log_dir/codex-run-latest.log"
status_file="$log_file.status"
ln -sf "$log_file" "$latest_log"

(
echo "mspace Codex run log: $log_file"
echo "latest log symlink: $latest_log"
echo "started at: $(date)"
echo "initial cwd: $(pwd)"

on_exit() {
  status=$?
  echo "finished at: $(date)"
  echo "exit status: $status"
  echo "log file: $log_file"
  if [ "$status" -ne 0 ]; then
    echo "last 120 log lines:"
    tail -n 120 "$log_file" || true
  fi
  printf '%s\n' "$status" > "$status_file"
  exit "$status"
}
trap on_exit EXIT

project_root="${MSPACE_PROJECT_ROOT:-/Users/mlhiter/personal-projects/mspace}"
if [ ! -f "$project_root/package.json" ]; then
  project_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
fi
cd "$project_root"
echo "project root: $project_root"

load_env_file() {
  if [ ! -f "$1" ]; then
    return 0
  fi

  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      ""|\#*) continue ;;
      export\ *) line="${line#export }" ;;
    esac

    key="${line%%=*}"
    value="${line#*=}"
    if [ "$key" = "$line" ]; then
      continue
    fi

    key="$(printf '%s' "$key" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    case "$key" in
      ""|[0-9]*|*[!A-Za-z0-9_]*) continue ;;
    esac

    case "$value" in
      \"*\") value="${value#\"}"; value="${value%\"}" ;;
      \'*\') value="${value#\'}"; value="${value%\'}" ;;
    esac
    export "$key=$value"
  done < "$1"
}

for env_file in .env .env.local server/.env server/.env.local; do
  if [ -f "$env_file" ]; then
    echo "loading env file: $env_file"
  fi
  load_env_file "$env_file"
done

if [ ! -f node_modules/.modules.yaml ]; then
  echo "node_modules missing; running pnpm install --frozen-lockfile"
  pnpm install --frozen-lockfile
fi

if [ -z "${DATABASE_URL:-}" ]; then
  echo "DATABASE_URL is not configured after loading env files from $project_root."
  echo "Add it to $project_root/.env.local or $project_root/server/.env.local before running mspace."
  exit 1
fi
echo "DATABASE_URL is configured."

db_info="$(node - <<'NODE'
const databaseUrl = process.env.DATABASE_URL || "";
try {
  const parsed = new URL(databaseUrl);
  if (!/^postgres(ql)?:$/.test(parsed.protocol)) {
    throw new Error(`unsupported protocol ${parsed.protocol}`);
  }
  console.log(parsed.hostname || "127.0.0.1");
  console.log(parsed.port || "5432");
  console.log(decodeURIComponent(parsed.username || "mspace"));
  console.log(decodeURIComponent(parsed.password || "mspace"));
  console.log(decodeURIComponent((parsed.pathname || "/mspace").replace(/^\//, "") || "mspace"));
} catch (error) {
  console.error(`DATABASE_URL is invalid: ${error.message}`);
  process.exit(1);
}
NODE
)"
db_host="$(printf '%s\n' "$db_info" | sed -n '1p')"
db_port="$(printf '%s\n' "$db_info" | sed -n '2p')"
db_user="$(printf '%s\n' "$db_info" | sed -n '3p')"
db_password="$(printf '%s\n' "$db_info" | sed -n '4p')"
db_name="$(printf '%s\n' "$db_info" | sed -n '5p')"
dev_postgres_container="${MSPACE_DEV_POSTGRES_CONTAINER:-mspace-postgres-dev}"
dev_postgres_volume="${MSPACE_DEV_POSTGRES_VOLUME:-mspace-postgres-data}"
dev_postgres_image="${MSPACE_DEV_POSTGRES_IMAGE:-postgres:16}"

container_data_volume() {
  docker inspect "$1" --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}' 2>/dev/null || true
}

validate_local_postgres_container() {
  case "$db_host" in
    127.0.0.1|localhost|::1) ;;
    *) return 0 ;;
  esac
  if ! command -v docker >/dev/null 2>&1; then
    return 0
  fi
  if ! docker inspect "$dev_postgres_container" >/dev/null 2>&1; then
    return 0
  fi

  mounted_volume="$(container_data_volume "$dev_postgres_container")"
  if [ "$mounted_volume" != "$dev_postgres_volume" ]; then
    echo "Local Postgres container $dev_postgres_container uses volume '$mounted_volume', expected '$dev_postgres_volume'."
    echo "Refusing to use an unexpected database volume. Remove or recreate the container after backing up any needed data."
    exit 1
  fi
}

ensure_ready_local_postgres_is_expected() {
  case "$db_host" in
    127.0.0.1|localhost|::1) ;;
    *) return 0 ;;
  esac
  if ! command -v docker >/dev/null 2>&1; then
    return 0
  fi
  if ! docker inspect "$dev_postgres_container" >/dev/null 2>&1; then
    return 0
  fi

  running="$(docker inspect "$dev_postgres_container" --format '{{.State.Running}}' 2>/dev/null || true)"
  if [ "$running" != "true" ]; then
    echo "PostgreSQL is responding at $db_host:$db_port, but expected container $dev_postgres_container is not running."
    echo "Refusing to continue because mspace may be pointed at a different local database."
    exit 1
  fi

  bindings="$(docker inspect "$dev_postgres_container" --format '{{range index .NetworkSettings.Ports "5432/tcp"}}{{.HostIp}}:{{.HostPort}} {{end}}' 2>/dev/null || true)"
  case " $bindings " in
    *":$db_port "*) ;;
    *)
      echo "Expected container $dev_postgres_container is running, but it is not publishing Postgres on host port $db_port."
      echo "Current bindings: ${bindings:-none}"
      exit 1
      ;;
  esac
}

postgres_ready() {
  if command -v pg_isready >/dev/null 2>&1; then
    pg_isready -h "$db_host" -p "$db_port" -U "$db_user" -d "$db_name" >/dev/null 2>&1
    return $?
  fi

  case "$db_host" in
    127.0.0.1|localhost|::1)
      lsof -tiTCP:"$db_port" -sTCP:LISTEN >/dev/null 2>&1
      return $?
      ;;
  esac
  return 1
}

start_local_postgres() {
  case "$db_host" in
    127.0.0.1|localhost|::1) ;;
    *)
      echo "PostgreSQL is not responding at $db_host:$db_port, and this action only auto-starts local Docker Postgres."
      exit 1
      ;;
  esac

  if ! command -v docker >/dev/null 2>&1; then
    echo "PostgreSQL is not responding at $db_host:$db_port, and Docker is not available to start a local dev database."
    exit 1
  fi
  if ! docker info >/dev/null 2>&1; then
    echo "PostgreSQL is not responding at $db_host:$db_port, and Docker is not running."
    exit 1
  fi

  if docker inspect "$dev_postgres_container" >/dev/null 2>&1; then
    mounted_volume="$(container_data_volume "$dev_postgres_container")"
    if [ "$mounted_volume" != "$dev_postgres_volume" ]; then
      echo "Local Postgres container $dev_postgres_container uses volume '$mounted_volume', expected '$dev_postgres_volume'."
      echo "Refusing to start it because it would point mspace at the wrong persisted database."
      exit 1
    fi
    echo "Starting existing local Postgres container: $dev_postgres_container"
    docker start "$dev_postgres_container" >/dev/null
  else
    docker volume create \
      --label app=mspace \
      --label mspace.role=dev-postgres \
      --label mspace.managed=true \
      "$dev_postgres_volume" >/dev/null
    echo "Creating local Postgres container: $dev_postgres_container on $db_host:$db_port using volume $dev_postgres_volume"
    docker run -d \
      --name "$dev_postgres_container" \
      --label app=mspace \
      --label mspace.role=dev-postgres \
      --label mspace.data-volume="$dev_postgres_volume" \
      -e POSTGRES_USER="$db_user" \
      -e POSTGRES_PASSWORD="$db_password" \
      -e POSTGRES_DB="$db_name" \
      -p "$db_host:$db_port:5432" \
      -v "$dev_postgres_volume:/var/lib/postgresql/data" \
      "$dev_postgres_image" >/dev/null
  fi
}

validate_local_postgres_container

if command -v pg_isready >/dev/null 2>&1; then
  if ! postgres_ready; then
    start_local_postgres
    deadline=$(($(date +%s) + 45))
    until postgres_ready; do
      if [ "$(date +%s)" -ge "$deadline" ]; then
        echo "Timed out waiting for PostgreSQL at $db_host:$db_port."
        exit 1
      fi
      sleep 1
    done
  fi
  ensure_ready_local_postgres_is_expected
elif ! postgres_ready; then
  start_local_postgres
  sleep 3
  ensure_ready_local_postgres_is_expected
else
  ensure_ready_local_postgres_is_expected
fi

for port in 8787; do
  pids="$(lsof -tiTCP:$port -sTCP:LISTEN || true)"
  if [ -n "$pids" ]; then
    echo "Stopping existing mspace process on 127.0.0.1:$port: $pids"
    kill $pids || true
  fi
done

sleep 1

desktop_server_url="${MSPACE_DESKTOP_SERVER_URL:-http://127.0.0.1:8787}"
MSPACE_SERVER_URL="$desktop_server_url" pnpm dev:desktop
) 2>&1 | tee -a "$log_file"

run_status="$(cat "$status_file" 2>/dev/null || printf '1\n')"
rm -f "$status_file"
exit "$run_status"
