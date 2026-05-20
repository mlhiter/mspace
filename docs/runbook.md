# mspace Runbook

> Status: server-owned local MVP operations guide, updated 2026-05-20

## Local Data

The product and runtime state for signed-in workspaces lives in server Postgres through `DATABASE_URL`.

| Path | Purpose |
| --- | --- |
| `~/.mspace/codex-worker-home` | Host-side Codex home copy used by the Docker Codex worker. |
| `/var/lib/mspace-worker/repos/<cache-key>` | Repository cache inside Docker-backed workers. |
| `/var/lib/mspace-worker/workdirs/<project-id>/<session-id>` | Per-session workdir inside Docker-backed workers. |
| `<worker-root>/workdirs/<project-id>/<session-id>/.mspace/session` | Session artifact directory. |
| `<artifact-dir>/test-environment.json` | Optional deploy/test artifact with preview values. |
| `<artifact-dir>/review-evidence.json` | Optional review artifact for commands, tests, build/deploy result, summary, risks, and follow-ups. |
| `<artifact-dir>/branch-name.json` | Optional agent-proposed source branch name. |
| `<artifact-dir>/project-runbook.md` | Optional agent-learned project runbook artifact. |

Server Postgres stores users, local password credentials, GitHub identities, workspaces, memberships, OAuth state, OAuth results, mspace auth sessions, projects, project runbooks, issues, comments, reactions, labels, Inbox receipts, agent profiles, clusters, issue test environments, issue handoffs, agent sessions, runtime registration tokens, runtime workers, runtime tasks, task events, and task logs.

Docker-backed workers store target project source under the worker root volume, not in the host checkout. The dry-run worker defaults to the `mspace-worker-dev-root` Docker volume, and the Codex-capable worker defaults to `mspace-worker-codex-dev-root`; both mount that volume at `/var/lib/mspace-worker`.

For local Codex/Electron development, `scripts/run-mspace-codex-dev.sh` auto-starts Docker Postgres only for local `DATABASE_URL` hosts. The expected durable database is container `mspace-postgres-dev` with named volume `mspace-postgres-data` and image `postgres:16`.

Check that Docker Postgres is using the expected volume:

```bash
docker inspect mspace-postgres-dev --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}'
docker exec mspace-postgres-dev psql -U mspace -d mspace -Atc "select count(*) from projects; select count(*) from issues;"
```

## Start The App

Install dependencies:

```bash
pnpm install
```

Start desktop:

```bash
pnpm dev:desktop
```

Electron starts the local server control plane automatically if `GET /health` is not already healthy on the configured server URL.

Run the server separately for auth or control-plane debugging:

```bash
cp .env.example .env.local
# edit .env.local with DATABASE_URL; GitHub OAuth values are optional
pnpm run server
```

For local worker testing, start a Docker-backed worker from Workspace Settings:

1. Sign in with a local account or configured GitHub OAuth, then select the target workspace.
2. Open Workspace Settings.
3. In Runtime, click `Start worker`.

mspace creates a short-lived internal worker bootstrap credential, injects it into the Docker worker process, and refreshes the Workers list after registration. The desktop button starts the Codex-capable Docker worker, so local `CODEX_HOME` must contain valid `auth.json` and `config.toml` files.

Run a worker manually only when debugging an external worker or terminal-only setup:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
pnpm worker
```

The worker registers with the server, sends heartbeat state, claims matching runtime tasks, completes `protocol_smoke` / `noop` tasks, runs `issue_type_triage` tasks from server payloads, and can execute `agent_session` tasks by preparing its own repository cache and session worktree under `MSPACE_WORKER_WORK_ROOT`, then starting `codex app-server --listen stdio://` there.

Codex configuration and authentication belong to worker runtimes. The server control plane queues work and applies runtime results, but it does not install Codex or mount Codex credentials.

Issue title suggestion is deterministic server fallback only. Issue type triage is LLM-backed, but it still runs through the worker queue as `runtime_tasks.kind="issue_type_triage"` with `requiredCapabilities={"codex":true}`. The server validates the worker result before writing the type label.

Dry-run Docker worker:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
scripts/run-server-worker-dev.sh
```

Codex-capable Docker worker:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
scripts/run-server-worker-codex-dev.sh
```

Worker-issued Codex sessions should not start or keep development servers running by default. Prefer non-interactive validation such as lint, tests, typecheck, build, or short internal probes. If a temporary server is needed, stop it before the session finishes and do not present container-local `localhost` or `127.0.0.1` as a user-facing preview.

Cancellation is cooperative. Stopping a worker-backed session requests cancellation on the server task; the worker polls its claimed task and interrupts Codex app-server when it sees `cancelled`.

## Kubernetes Customer Deployment

The first customer deployment path is a Kubernetes-hosted fixed Server Worker, not a per-session Kubernetes Runtime Provider. Use:

```bash
deploy/scripts/build-images.sh
helm upgrade --install mspace deploy/helm/mspace -n mspace-system -f /tmp/mspace-values.yaml
```

See `docs/kubernetes-deployment.md` for the two-stage install: server first, then create a workspace-scoped `msw_...` runtime token and enable the worker.

## Website

Production site:

- [mspace-website-blue.vercel.app](https://mspace-website-blue.vercel.app)

Local development:

```bash
pnpm dev:website
pnpm build:website
pnpm preview:website
```

The public changelog is a static website view, not product runtime state. Update `apps/website/src/changelog.ts` whenever a task ships meaningful product, engineering, documentation, or website progress.

## Desktop Localization

Desktop localization is centralized in `packages/i18n` and currently supports English (`en`) and Simplified Chinese (`zh-CN`). The selected locale is stored in `localStorage["mspace.locale"]` and mirrored to `<html lang="...">`.

Visible product copy in `apps/desktop`, `packages/ui`, or `packages/views` should use `packages/i18n/src/index.ts`. Leave logs, user-authored Markdown, branch names, commit hashes, Kubernetes object names, storage values, and runtime protocol fields literal unless a separate product decision says otherwise.

## Environment Variables

| Variable | Used by | Default | Purpose |
| --- | --- | --- | --- |
| `MSPACE_SERVER_ADDR` | Server and Electron main process | `127.0.0.1:8787` | Address used when the server control plane listens or is started by desktop. |
| `MSPACE_SERVER_URL` | Electron preload/renderer | `http://127.0.0.1:8787` | Server control-plane base URL exposed to the renderer. |
| `MSPACE_SERVER_START_TIMEOUT_MS` | Electron main process | `30000` | How long the desktop waits for the server health check before startup fails. |
| `DATABASE_URL` | Server | none | Postgres connection string for control-plane storage. |
| `MSPACE_GITHUB_CLIENT_ID` | Server | none | Optional GitHub OAuth App client id. |
| `MSPACE_GITHUB_CLIENT_SECRET` | Server | none | Optional GitHub OAuth App client secret; keep it server-side only. |
| `MSPACE_GITHUB_REDIRECT_URI` | Server | none | Optional OAuth callback URL, usually `http://127.0.0.1:8787/api/auth/github/callback` locally. |
| `MSPACE_RUNTIME_TOKEN` | Worker | none | Internal runtime bootstrap credential with `msw_` prefix. |
| `MSPACE_WORKER_NAME` | Worker | host-derived | Stable worker name shown in Workspace Settings. |
| `MSPACE_WORKER_MODE` | Worker | `team` | Runtime mode reported to the server; `team` or `personal`. |
| `MSPACE_WORKER_CAPABILITIES` | Worker | `{"protocolSmoke":true,"codex":false,"dryRun":true}` | JSON object used by server-side capability matching. |
| `MSPACE_WORKER_LABELS` | Worker | `{}` | JSON object for placement or inventory labels. |
| `MSPACE_WORKER_POLL_INTERVAL` | Worker | `5s` | How often the worker polls the runtime task queue. |
| `MSPACE_WORKER_HEARTBEAT_INTERVAL` | Worker | `10s` | How often the worker reports liveness and load. |
| `MSPACE_WORKER_WORK_ROOT` | Worker | `/tmp/mspace-worker` or `/var/lib/mspace-worker` in Docker | Root directory for worker-managed repository caches, session worktrees, and artifacts. |
| `MSPACE_WORKER_VOLUME` | Docker worker scripts | `mspace-worker-dev-root` or `mspace-worker-codex-dev-root` | Docker volume mounted at `/var/lib/mspace-worker`. |
| `MSPACE_WORKER_CODEX_HOME_SOURCE` | Docker Codex worker script | `${CODEX_HOME:-~/.codex}` | Source Codex home copied into a dedicated worker Codex home before container startup. |
| `MSPACE_WORKER_CODEX_HOME_DIR` | Docker Codex worker script | `~/.mspace/codex-worker-home` | Host directory mounted into the Codex-capable dev worker as `CODEX_HOME`. |
| `MSPACE_WORKER_CODEX_CLI_VERSION` | Docker Codex worker script | `0.130.0` | `@openai/codex` npm version installed into `worker/Dockerfile.codex-dev`. |

Cluster, project, and issue test environment fields are passed into sessions as:

| Variable | Source |
| --- | --- |
| `MSPACE_CLUSTER_ID` | Selected issue test environment cluster id when present. |
| `MSPACE_KUBE_CONTEXT` | Selected issue test environment `kube_context`. |
| `KUBECONFIG` / `MSPACE_KUBECONFIG` | Selected issue test environment `kubeconfig_path`. |
| `MSPACE_KUBE_NAMESPACE` | Issue test namespace. |
| `MSPACE_TEST_NAMESPACE` | Issue test namespace. |
| `MSPACE_IMAGE_REGISTRY_PREFIX` | Selected cluster or issue test environment image registry prefix. |
| `MSPACE_EXPOSURE_MODE` | Selected issue test environment exposure mode. |
| `MSPACE_PREVIEW_DOMAIN` | Optional issue preview domain. |
| `MSPACE_INGRESS_CLASS` | Optional issue ingress class. |
| `MSPACE_NODE_HOST` | Optional issue NodePort host. |

Session metadata is also passed into the Codex app-server process environment as:

| Variable | Source |
| --- | --- |
| `MSPACE_API_BASE_URL` | Server control-plane API base URL. |
| `MSPACE_ISSUE_ID` | Current issue id. |
| `MSPACE_SESSION_ID` | Current session id. |
| `MSPACE_AGENT_TOKEN` | Scoped bearer token for agent writes. |
| `MSPACE_AGENT_PROFILE` | Selected managed agent profile id. |
| `MSPACE_SESSION_BRANCH` | Planned session branch. |
| `MSPACE_SESSION_WORKDIR` | Prepared git worktree path. |
| `MSPACE_SOURCE_SESSION_ID` | Selected source session for a deploy/test continuation, when present. |
| `MSPACE_SOURCE_COMMIT_SHA` | Selected source commit for a deploy/test continuation, when present. |
| `MSPACE_SESSION_CONTEXT` | Markdown context file written by the worker. |
| `MSPACE_SESSION_ARTIFACT_DIR` | Session artifact directory under the prepared worktree. |

## Smoke Checks

Server health:

```bash
curl http://127.0.0.1:8787/health
```

GitHub auth start endpoint:

```bash
curl -i http://127.0.0.1:8787/api/auth/github/start
```

Workspace Inbox:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/inbox
```

Runtime worker token:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-registration-tokens" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"local worker","expiresInHours":12}'
```

Queue a protocol task:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"protocol_smoke","runtimeMode":"team","requiredCapabilities":{"protocolSmoke":true},"payload":{"source":"curl"}}'
```

Inspect task events and logs:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks/<task-id>/events"

curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks/<task-id>/logs"
```

## Workspace Runtime Checks

List server-owned agent profiles:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/agents
```

List cluster configs:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/clusters
```

Discover kubeconfigs:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/clusters/discover-defaults
```

List namespace resources for an issue test environment:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/issues/<issue-id>/test-environment/resources
```

## Validation

```bash
pnpm typecheck
pnpm --filter @mspace/desktop build
pnpm --filter @mspace/website build
pnpm test:server
(cd packages/ui && pnpm dlx shadcn@latest info --json)
(cd worker && go test ./...)
(cd worker && go build ./...)
```

## Troubleshooting

### GitHub sign-in does not complete

Local password sign-in does not require GitHub. Use GitHub only when the server has OAuth values and the environment can reach GitHub.

Check server env first:

```bash
curl -i http://127.0.0.1:8787/api/auth/github/start
```

Required variables for this GitHub OAuth path: `DATABASE_URL`, `MSPACE_GITHUB_CLIENT_ID`, `MSPACE_GITHUB_CLIENT_SECRET`, and `MSPACE_GITHUB_REDIRECT_URI`.

### Workspace looks empty

Check that the desktop is pointing at the expected server and Postgres volume:

```bash
curl http://127.0.0.1:8787/health
docker inspect mspace-postgres-dev --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}'
docker exec mspace-postgres-dev psql -U mspace -d mspace -Atc "select count(*) from workspaces; select count(*) from projects; select count(*) from issues;"
```

### Worker does not claim tasks

Check the worker is registered and has matching mode/capabilities:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-workers

curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks
```

The task's `runtimeMode` and `requiredCapabilities` must match the worker heartbeat.

### Codex worker fails authentication

For Docker Codex workers, make sure the mounted worker Codex home contains both auth and config:

```bash
ls -la ~/.mspace/codex-worker-home
test -s ~/.mspace/codex-worker-home/auth.json
test -s ~/.mspace/codex-worker-home/config.toml
```

Re-run `scripts/run-server-worker-codex-dev.sh` after refreshing `${CODEX_HOME:-~/.codex}`.

### Test environment resources fail to load

Check the server-owned cluster and issue environment:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/clusters

curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/issues/<issue-id>
```

The issue must have a `testEnvironment` with a namespace, kubeconfig path, and reachable cluster context.
