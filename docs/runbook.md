# mspace Runbook

> Status: server-owned local MVP operations guide, updated 2026-06-03

## Local Data

The product and runtime state for signed-in workspaces lives in the server store. Team/customer/shared deployments use Postgres through `DATABASE_URL`; packaged personal desktop mode uses the same server APIs with local SQLite.

| Path | Purpose |
| --- | --- |
| `<Electron userData>/mspace.db` | Packaged personal desktop SQLite store used by the bundled local server. |
| `<Electron userData>/server-config.json` | Saved Team server URL for this device. `MSPACE_SERVER_URL` overrides it for the launch. |
| `~/.mspace/codex-worker-home` | Host-side Codex home copy used by the Docker Codex worker. |
| `/var/lib/mspace-worker/repos/<cache-key>` | Repository cache inside Docker-backed workers. |
| `/var/lib/mspace-worker/workdirs/<project-id>/<session-id>` | Per-session workdir inside Docker-backed workers. |
| `<worker-root>/workdirs/<project-id>/<session-id>/.mspace/session` | Session artifact directory. |
| `<artifact-dir>/test-environment.json` | Optional deploy/test artifact with preview values. |
| `<artifact-dir>/review-evidence.json` | Optional review artifact for commands, tests, build/deploy result, summary, risks, and follow-ups. |
| `<artifact-dir>/test-case-proposals.json` | Optional Codex case suggestion artifact reconciled into project Case suggestions. |
| `<artifact-dir>/test-result.json` | Optional Codex test run artifact reconciled into test run items and run acceptance state. Prefer `{"runId":"...","items":[...]}`; the worker also accepts a top-level array of result items when each item carries `runId`. If evidence references screenshot files inside the artifact directory, the worker can embed small image data URLs for transfer; the server persists supported screenshots as `test_artifacts` and rewrites evidence to authenticated artifact refs. |
| `<artifact-dir>/branch-name.json` | Optional agent-proposed source branch name. |
| `<artifact-dir>/project-runbook.md` | Optional agent-learned project runbook artifact. |

The server store records users, local password credentials, GitHub identities, workspaces, memberships, OAuth state, OAuth results, mspace auth sessions, projects, project runbooks, project test cases, test case revisions, test case suggestions, test plans, test runs, issues, comments, reactions, labels, Inbox receipts, agent profiles, environments, Kubernetes cluster compatibility records, issue test environments, issue handoffs, agent sessions, runtime registration tokens, runtime workers, runtime tasks, task events, and task logs. Postgres stores these in migrated tables. SQLite personal mode stores a server-owned snapshot in `store_snapshots` and persists after mutating requests plus the OAuth GET routes that create or consume login state.

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

Electron uses `MSPACE_SERVER_URL` first, then a saved Team server URL, then the local bundled/dev server. It starts the local server control plane automatically only for the default personal path if `GET /health` is not already healthy.

Run the server separately for auth or control-plane debugging:

```bash
cp .env.example .env.local
# set DATABASE_URL only when testing Postgres; GitHub OAuth values are optional
pnpm run server
```

For personal desktop workspaces, the app starts and keeps alive a host-local Codex worker before it submits an agent mention. The worker uses the active desktop server URL, a short-lived workspace registration credential, `MSPACE_WORKER_MODE=personal`, and the user's local Codex configuration. Electron writes the credential to `<Electron userData>/worker/tokens/<workspace-id>.token`, renews the 12-hour credential before expiry, and revokes the previous credential after a short grace period. The worker rereads the token file for runtime API calls, so credential renewal should be invisible during normal personal use. Workspace Settings labels these rows as automatic desktop credentials and keeps expired/replaced rows under credential history. Set `MSPACE_AUTO_PERSONAL_WORKER=0` to disable this behavior while debugging.

For team or self-hosted worker runtime hosts, connect a worker from Workspace Settings:

1. Sign in with a local account or configured GitHub OAuth, then select the target workspace.
2. Open Workspace Settings.
3. In Runtime, click `Connect environment`.
4. Copy the generated install command.
5. Run it on the server, VM, DevBox, or other Docker-capable host that should execute mspace agent work.

mspace creates a short-lived internal worker bootstrap credential and embeds it in the install command once. The worker host must have Docker and a usable Codex home with `auth.json` and `config.toml`. The command starts the Codex-capable worker container, and Workspace Settings refreshes the Workers list after registration.

Run a worker manually only when debugging or recovering an external worker or terminal-only setup:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
pnpm worker
```

The worker registers with the server, sends heartbeat state, claims matching runtime tasks, completes `protocol_smoke` / `noop` tasks, runs `issue_type_triage` tasks from server payloads, and can execute `agent_session` tasks by preparing its own repository cache and session worktree under `MSPACE_WORKER_WORK_ROOT`, then starting `codex app-server --listen stdio://` there. Workspace Settings shows these tasks as issue-linked operational rows and keeps protocol payloads, results, events, and logs in expandable details.

Codex configuration and authentication belong to worker runtimes. The server control plane queues work and applies runtime results, but it does not install Codex or mount Codex credentials.

Issue title suggestion is deterministic server fallback only. Issue type triage is LLM-backed, but it still runs through the worker queue as `runtime_tasks.kind="issue_type_triage"` with `requiredCapabilities={"codex":true}`. The server validates the worker result before writing the type label.

## Tests Workflow

Use Tests after a project exists. Personal projects can use a local folder or GitHub URL. Team projects must use a GitHub URL so the team worker can clone source into its own repo cache.

Recommended smoke order:

1. Open Tests and pick a project.
2. Verify the Cases list shows compact operational columns: title, type, status, priority, executability, latest result, and updated time.
3. Create one case from the modal and verify the case detail page opens.
4. Import Markdown/text cases and confirm non-empty lines become draft cases.
5. Import CSV or Excel `.xlsx` cases with `title`, `type`, `area`, `priority`, `preconditions`, `steps`, `expected_result`, `environment_requirements`, and `tags` headers. Rows without `title` should appear in the skipped list. Use `functional`, `ui`, `api`, or `deployment` for `type`; common aliases such as `UI 测试`, `接口测试`, and `部署测试` normalize to the fixed values.
6. Edit a case and verify revisions show newest first, with non-initial revisions showing changed fields and before/after values rather than only the case title.
7. Exercise Optimize or Generate with no connected worker; the UI should surface the worker/session blocker rather than silently claiming success.
8. Connect a Codex-capable worker, then verify `test-case-proposals.json` becomes Case suggestions and `test-result.json` updates run items. The Cases list should show the latest final run item status. Case Detail should expose `Details`, `Run history`, and `Revisions` tabs, with Run history showing all runs for that case. Case Detail / Run Detail should expose structured evidence such as authenticated screenshot thumbnails, assertions, network summaries, DOM snapshots, and raw evidence.
9. Accept or block the run manually. A Codex-completed run is not release acceptance until a human records that decision.

The current case library accepts `functional`, `ui`, `api`, and `deployment` as case types. Specialized UI/CDP automation, API harnessing, deployment orchestration, and multi-worker scheduling are still later execution work; the first loop keeps execution behind Issues, Agent Sessions, Workers, and Evidence.

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

## Workspace Automation

Workspace Settings has two automation switches:

- Source commit capture is always on and records source changes as issue change nodes.
- `autoDeployTestEnvironment` is opt-in and queues a deploy/test session after a successful source session captures a commit.

Automatic test deploy uses the same `agent_session` path as a manual test deploy. It is intentionally conservative: the triggering task must be completed, non-dry-run, and not itself a deploy/test task; it must have a source commit and no source error; the issue must have an attached project; Kubernetes Environment and deploy settings must resolve; no other agent session can be active for the issue; and a matching online Codex worker must exist. If no worker is connected or deploy settings cannot be resolved, the server adds a compact system comment explaining why the deploy was skipped.

## Kubernetes Customer Deployment

The first customer deployment path is a Kubernetes-hosted fixed Server Worker, not a per-session Kubernetes Runtime Provider. Use:

```bash
deploy/scripts/build-images.sh
helm upgrade --install mspace deploy/helm/mspace -n mspace-system -f /tmp/mspace-values.yaml
```

See `docs/kubernetes-deployment.md` for the customer install shape. The default fixed-worker path now sets `bootstrap.teamWorkspace.enabled=true` so Helm installs the server and worker together: the chart creates or preserves one `MSPACE_RUNTIME_TOKEN`, server startup registers it against the admin-owned default team workspace, and the worker registers with that same token. Before installing, create `mspace-codex-home` with a worker-scoped `auth.json` and `deploy/codex/worker-config.toml` or an untracked private-provider variant.

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

## Desktop Team Server Selection

Personal desktop mode starts with the local bundled server, so local users usually do not need to think about a server URL. The default local sign-in opens on account creation and hides GitHub sign-in, even when a local dev server is configured with OAuth variables. The sign-in screen has a collapsed Team server entry for customer or team deployments such as `https://mspace.example.com`; opening it lets the app check `/health`, save the URL in Electron user data, and reuse it on later launches.

`MSPACE_SERVER_URL` remains the launch-time override. If it is set, it takes precedence over the saved UI value and the Team server entry opens in a locked state for that launch.

Use the same override when testing GitHub OAuth against the local server:

```bash
MSPACE_SERVER_URL=http://127.0.0.1:8787 pnpm dev:desktop
```

That explicit source makes the desktop use the configured-team-server sign-in path, so GitHub can appear when `/health` reports `capabilities.githubAuth: true`.

## Workspace Invitations

Team workspace owners and admins create one-time join links from Workspace Settings. The normal link format is:

```text
mspace://invite/<token>?server=<team-server-url>
```

The `server` value is operationally important: when the link is opened from Feishu, WeChat, a browser, or another app, Electron switches to that team server before calling the preview or accept APIs. If the recipient is not signed in, the invite route shows only safe preview data, lets them sign in or create a local account, accepts the invitation after authentication, and opens the invited team workspace directly.

Unauthenticated preview smoke check:

```bash
curl "$MSPACE_SERVER_BASE/api/workspace-invitations/preview?token=msi_..."
```

If preview returns `404`, check these before changing UI code:

- the link's `server` query points at the deployed team server that created the invite;
- the backend image is updated and `/health` reports `workspaceInvitationPreview: true`;
- the token has the expected `msi_...` shape and was created on the same server;
- the desktop did not fall back to the default local personal server before handling the link.

If preview succeeds but returns `accepted`, `revoked`, or `expired`, the route is healthy and the invitation is no longer usable. Create a new join link from Workspace Settings instead.

## Environment Variables

| Variable | Used by | Default | Purpose |
| --- | --- | --- | --- |
| `MSPACE_SERVER_ADDR` | Server and Electron main process | `127.0.0.1:8787` | Address used when the server control plane listens or is started by desktop. |
| `MSPACE_SERVER_URL` | Electron main/preload/renderer | `http://127.0.0.1:8787` | Launch-time server control-plane override. Takes precedence over the saved Team server setting. |
| `MSPACE_SERVER_START_TIMEOUT_MS` | Electron main process | `30000` | How long the desktop waits for the server health check before startup fails. |
| `MSPACE_STORE` | Server | inferred | Storage mode. Uses `postgres` when `DATABASE_URL` is set, otherwise `sqlite` for local personal mode. |
| `MSPACE_SQLITE_PATH` | Server and packaged desktop | app/user config path | SQLite file path for local personal mode. |
| `MSPACE_DATA_DIR` | Server | none | Optional directory used to derive the default SQLite path. |
| `DATABASE_URL` | Server | none | Postgres connection string for control-plane storage. |
| `MSPACE_GITHUB_CLIENT_ID` | Server | none | Optional GitHub OAuth App client id. |
| `MSPACE_GITHUB_CLIENT_SECRET` | Server | none | Optional GitHub OAuth App client secret; keep it server-side only. |
| `MSPACE_GITHUB_REDIRECT_URI` | Server | none | Optional OAuth callback URL, usually `http://127.0.0.1:8787/api/auth/github/callback` locally. |
| `MSPACE_SERVER_ADMIN_LOGINS` | Server | empty | Comma-separated local password logins or GitHub logins allowed to create team workspaces. If empty, no signed-in user can create a team workspace unless listed through bootstrap admin settings. |
| `MSPACE_BOOTSTRAP_ADMIN_LOGIN` | Server | empty | Optional local password login to create on server startup and treat as a server admin. |
| `MSPACE_BOOTSTRAP_ADMIN_PASSWORD` | Server | empty | Required with `MSPACE_BOOTSTRAP_ADMIN_LOGIN`; used only when the bootstrap account does not already exist. |
| `MSPACE_BOOTSTRAP_ADMIN_NAME` | Server | login | Optional display name for the bootstrap admin account. |
| `MSPACE_BOOTSTRAP_ADMIN_EMAIL` | Server | empty | Optional display email stored on the bootstrap identity. It is not used for admin matching. |
| `MSPACE_BOOTSTRAP_TEAM_WORKSPACE_NAME` | Server | empty | Optional team workspace name to create or find during server startup. Used by Helm fixed-worker bootstrap. |
| `MSPACE_BOOTSTRAP_RUNTIME_TOKEN` | Server | empty | Optional `msw_...` token to register against the bootstrap team workspace. Must be set with `MSPACE_BOOTSTRAP_TEAM_WORKSPACE_NAME`. |
| `MSPACE_BOOTSTRAP_RUNTIME_TOKEN_NAME` | Server | `Helm fixed worker` | Metadata name for the bootstrap runtime registration token. |
| `MSPACE_BOOTSTRAP_RUNTIME_TOKEN_TTL_HOURS` | Server | `2160` | Runtime token TTL in hours for the bootstrap fixed worker. The server and chart cap this at 90 days. |
| `MSPACE_RUNTIME_TOKEN` | Worker | none | Internal runtime bootstrap credential with `msw_` prefix. |
| `MSPACE_WORKER_NAME` | Worker | host-derived | Stable worker name shown in Workspace Settings. |
| `MSPACE_WORKER_MODE` | Worker | `team` | Runtime mode reported to the server; `team` or `personal`. |
| `MSPACE_WORKER_IMAGE` | Docker worker install command | `crpi-7jr40k6elhldekqp.cn-hangzhou.personal.cr.aliyuncs.com/mlhiter/mspace-worker-codex:dev` | Docker image used by generated self-host worker install commands. |
| `MSPACE_WORKER_CONTAINER` | Docker worker install command | `${MSPACE_WORKER_NAME}` | Docker container name used by generated self-host worker install commands. |
| `MSPACE_WORKER_CAPABILITIES` | Worker | worker-dependent | JSON object used by server-side capability matching. The plain worker default is `{"protocolSmoke":true,"codex":false,"dryRun":true}`; generated self-host install commands default to `{"protocolSmoke":true,"codex":true,"docker":true,"kubectl":true,"buildkit":false,"dryRun":false}`. |
| `MSPACE_WORKER_LABELS` | Worker | worker-dependent | JSON object for placement or inventory labels. Generated self-host install commands default to `{"provider":"self-host","environment":"docker"}`. |
| `MSPACE_WORKER_POLL_INTERVAL` | Worker | `5s` | How often the worker polls the runtime task queue. |
| `MSPACE_WORKER_HEARTBEAT_INTERVAL` | Worker | `10s` | How often the worker reports liveness and load. |
| `MSPACE_WORKER_WORK_ROOT` | Worker | `/tmp/mspace-worker` or `/var/lib/mspace-worker` in Docker | Root directory for worker-managed repository caches, session worktrees, and artifacts. |
| `MSPACE_WORKER_VOLUME` | Docker worker scripts | `mspace-worker-${MSPACE_WORKER_NAME}` in install commands | Docker volume mounted at `/var/lib/mspace-worker`. Dev helper scripts may use their own fixed dev volume names. |
| `MSPACE_WORKER_CODEX_HOME_SOURCE` | Docker Codex worker script | `${CODEX_HOME:-~/.codex}` | Source Codex home copied into a dedicated worker Codex home before container startup. |
| `MSPACE_WORKER_CODEX_HOME_DIR` | Docker Codex worker script | `~/.mspace/codex-worker-home` | Host directory mounted into the Codex-capable dev worker as `CODEX_HOME`. |
| `MSPACE_WORKER_CODEX_CLI_VERSION` | Docker Codex worker script | `0.130.0` | `@openai/codex` npm version installed into `worker/Dockerfile.codex-dev`. |
| `MSPACE_AUTO_PERSONAL_WORKER` | Electron main process | enabled | Set to `0` to prevent the desktop from auto-starting a host-local personal worker before agent mentions. |

Environment, project, and issue test environment fields are passed into sessions as:

| Variable | Source |
| --- | --- |
| `MSPACE_CLUSTER_ID` | Selected Kubernetes issue test environment cluster id when present. |
| `MSPACE_KUBE_CONTEXT` | Selected issue test environment `kube_context`. |
| `KUBECONFIG` / `MSPACE_KUBECONFIG` | Selected issue test environment `kubeconfig_path`. |
| `MSPACE_KUBE_NAMESPACE` | Issue test namespace. |
| `MSPACE_TEST_NAMESPACE` | Issue test namespace. |
| `MSPACE_IMAGE_REGISTRY_PREFIX` | Selected Kubernetes environment or issue test environment image registry prefix. |
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

Manual runtime worker credential for external worker debugging:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-registration-tokens" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"local worker","expiresInHours":12}'
```

Queue a protocol task through the API debug path:

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

List environments:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/environments
```

List Kubernetes cluster compatibility records:

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

Create a test case:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/projects/<project-id>/test-cases" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Local password login succeeds","type":"functional","status":"ready","steps":[{"action":"Submit valid credentials"}],"expectedResult":"The workspace opens.","environmentRequirements":"Personal desktop server is running."}'
```

Import Markdown cases:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/projects/<project-id>/test-cases/import" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"format":"markdown","content":"- Invalid password shows an error\n- Invite link opens the workspace"}'
```

Import `.xlsx` cases by base64-encoding the workbook bytes:

```bash
python3 - <<'PY'
import base64, json, pathlib
path = pathlib.Path("cases.xlsx")
print(json.dumps({"format": "xlsx", "fileName": path.name, "content": base64.b64encode(path.read_bytes()).decode()}))
PY
```

Then send that JSON body to:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/projects/<project-id>/test-cases/import" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  --data-binary @/tmp/test-cases-import.json
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

Required variables for this GitHub OAuth path: `MSPACE_GITHUB_CLIENT_ID`, `MSPACE_GITHUB_CLIENT_SECRET`, and `MSPACE_GITHUB_REDIRECT_URI`. The desktop shows GitHub login only for an explicitly configured team server when `/health` reports `capabilities.githubAuth: true`. If the button is hidden while debugging local OAuth, launch desktop with `MSPACE_SERVER_URL=http://127.0.0.1:8787 pnpm dev:desktop` instead of relying on the default local personal server source.

### Workspace looks empty

Check that the desktop is pointing at the expected server. In packaged personal mode, inspect the Electron user-data SQLite path; in Postgres development, inspect the Docker volume:

```bash
curl http://127.0.0.1:8787/health
find "$HOME/Library/Application Support" -maxdepth 3 -name mspace.db 2>/dev/null
docker inspect mspace-postgres-dev --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}'
docker exec mspace-postgres-dev psql -U mspace -d mspace -Atc "select count(*) from workspaces; select count(*) from projects; select count(*) from issues;"
```

### Worker does not claim tasks

Agent mentions are rejected before queueing when no matching active Codex worker exists. Check the worker is registered, online, fresh, and has matching mode/capabilities:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-workers

curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks?limit=10&offset=0"
```

The task's `runtimeMode` and `requiredCapabilities` must match the worker heartbeat.

### Personal worker credential expires

The desktop-managed personal worker should renew credentials automatically. Check the Electron log for `[personal-worker] credential renewal failed` or `restart failed` first. If renewal keeps failing, confirm the user is still signed in, the selected server URL is reachable, and the personal workspace id has not changed. As a last local debug step, stop and restart the personal worker by switching server source or relaunching the desktop app; do not create a long-lived manual worker credential for normal personal mode.

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
