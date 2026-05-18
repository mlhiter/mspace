# mspace Local Runbook

> Status: local MVP operations guide, updated 2026-05-15

## Local Data

| Path | Purpose |
| --- | --- |
| `~/.mspace/mspace.db` | SQLite database used by the Go runner. |
| `~/.mspace/repos/<owner>/<repo>` | Cached clone path for GitHub-imported repositories. |
| `~/.mspace/workdirs/<project-id>/<session-id>` | Git worktree created for one local agent session. |
| `~/.mspace/workdirs/_contexts/<session-id>.md` | Markdown session context included in the Codex app-server prompt. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session` | Session artifact directory recorded in `agent_sessions.artifact_dir`. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session/test-environment.json` | Optional deploy/test artifact. When it includes `previewUrl`, the runner copies it back to the issue test environment; completed continuation sessions can also refresh the current issue environment this way. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session/review-evidence.json` | Optional session review artifact. The runner imports compact commands, tests, build/deploy results, agent summary, risks, and follow-ups into `session_review_evidence`. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session/branch-name.json` | Optional agent-proposed source branch name. Use `{ "branch": "fix/short-semantic-name" }`; mspace validates Conventional Commit-style prefixes and renames the source branch before capture when safe. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session/project-runbook.md` | Optional agent-learned project runbook artifact. When present after a successful session, the runner stores it as the current project runbook. |

Reusable cluster configs are stored in runner `clusters`. Workspace automation policy is stored in runner `workspace_settings`. Issue test namespace records are stored in runner `issue_test_environments`. Review snapshots are stored in runner `session_review_evidence`; continueable failed-session and failed-environment records are stored in runner `session_failures`; branch and PR delivery records are stored in runner `issue_handoffs`; raw local validation trails stay in runner `session_logs`. Legacy local unread rows may still exist in runner `inbox_items`, but the product Inbox uses server receipts. Agent definitions are still stored in runner `agent_profiles`. Normal signed-in agent sessions, worker logs, cancellation, and source results live in server Postgres runtime tables rather than in runner SQLite.

The server control plane stores users, GitHub identities, workspaces, memberships, OAuth state, OAuth results, mspace auth sessions, projects, project runbooks, project runbook revisions, issues, comments, comment reactions, issue label definitions, issue labels, issue events, issue-event receipts, issue watchers, agent sessions, runtime registration tokens, runtime workers, runtime tasks, task events, and task logs in Postgres through `DATABASE_URL`. GitHub sign-in creates a personal workspace by default; personal and team workspaces can both register runtime workers and queue runtime tasks. Invitations and shared members require an explicit team workspace. Local GitHub OAuth configuration should live in `.env.local`, which is ignored by git.

For local Codex/Electron development, `scripts/run-mspace-codex-dev.sh` auto-starts Docker Postgres only for local `DATABASE_URL` hosts. The expected durable database is container `mspace-postgres-dev` with named volume `mspace-postgres-data` and image `postgres:16`. The script labels both the container and volume, and refuses to start an existing `mspace-postgres-dev` container if it is mounted to a different `/var/lib/postgresql/data` volume. If the app suddenly looks empty, first check that Docker is serving this named volume rather than a new or anonymous Postgres data directory:

```bash
docker inspect mspace-postgres-dev --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}'
docker exec mspace-postgres-dev psql -U mspace -d mspace -Atc "select count(*) from projects; select count(*) from issues;"
```

If a signed-in personal workspace is empty after moving product data to Postgres, import the legacy runner product rows once:

```bash
node scripts/import-local-sqlite-product-data.mjs
```

The script reads `~/.mspace/mspace.db`, auto-selects the single personal workspace when possible, preserves legacy project, issue, comment, label, reaction, runbook, and Inbox event UUIDs, and writes them into the server control plane. Pass `--workspace-id <id>` when multiple personal workspaces exist. The script is idempotent and can be re-run without duplicating rows.

## Start The App

Install dependencies:

```bash
pnpm install
```

Start desktop:

```bash
pnpm dev:desktop
```

Electron starts the local Go runner and local server control plane automatically if their `GET /health` checks are not already healthy on the configured ports. The runner health response must also advertise the expected protocol capabilities; in dev, a stale local runner on `7788` is terminated and replaced with the current source tree. On runner startup, any persisted `queued` or `running` sessions from a previous runner process are treated as interrupted rather than resumable, because the old Codex app-server process and in-memory cancellation handle are gone.

Run the runner separately for API debugging:

```bash
pnpm runner
pnpm dev:desktop
```

Run the server separately for auth or control-plane debugging:

```bash
cp .env.example .env.local
# edit .env.local with DATABASE_URL and GitHub OAuth values
pnpm run server
```

For local team-mode testing, start a Docker-backed worker from Workspace Settings:

1. Sign in and select the target workspace.
2. Open Workspace Settings.
3. In Runtime, click `Start worker`.

mspace creates a short-lived internal worker bootstrap credential, injects it into the Docker worker process, and refreshes the Workers list after registration. The raw `msw_...` credential is an internal connection detail for worker bootstrap, not the default user-facing setup path. The desktop button starts the Codex-capable Docker worker, so local `CODEX_HOME` must contain a valid `auth.json`.

Run a Server Worker manually only when debugging an external worker or terminal-only setup:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
pnpm worker
```

The worker registers with the server, sends heartbeat state, claims matching runtime tasks, completes `protocol_smoke` / `noop` tasks, and can execute `agent_session` tasks by preparing its own repository cache and session worktree under `MSPACE_WORKER_WORK_ROOT`, then starting `codex app-server --listen stdio://` there. Worker logs are appended to the server task log stream. Issue Detail sessions queue an `agent_session` task with repository and session metadata; returned Codex thread/turn ids, source commit metadata, changed files, and diff preview stay in the server runtime task result and feed Session Detail plus the issue's Commits view.

To simulate a fixed development server without using the local machine environment from a terminal, run the worker in Docker:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
scripts/run-server-worker-dev.sh
```

The script builds `worker/Dockerfile.dev`, runs `mspace-worker` inside a container, and stores worker-managed repos/workdirs/artifacts in the `mspace-worker-dev-root` Docker volume. The container connects to the host control plane through `http://host.docker.internal:8787` by default. It advertises `codex:true,dryRun:true` so UI-triggered Team sessions can complete a full control-plane loop without local Codex credentials: the worker clones the repository, creates an isolated worktree, writes `TEAM_RUNTIME_DRY_RUN.md`, commits it, and returns commit/diff metadata to the issue. Real Codex execution still requires Codex CLI/auth to be available inside the worker image or a mounted worker environment; set `MSPACE_WORKER_CAPABILITIES='{"protocolSmoke":true,"codex":true,"dryRun":false}'` only after that worker image is actually able to run `codex app-server`.

To test real Codex execution from a Docker-backed fixed worker, run the Codex-capable dev worker:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
scripts/run-server-worker-codex-dev.sh
```

The script builds `worker/Dockerfile.codex-dev`, installs the Linux Codex CLI inside the image, and runs the worker with `codex:true,dryRun:false`. It copies `auth.json` and `config.toml` from `${CODEX_HOME:-~/.codex}` into `~/.mspace/codex-worker-home`, mounts that directory as the container `CODEX_HOME`, and adds the container worker root as a trusted Codex project. Use `MSPACE_WORKER_CODEX_HOME_SOURCE` to point at a different prepared Codex home, `MSPACE_WORKER_CODEX_HOME_DIR` to change the mounted copy, and `MSPACE_WORKER_CODEX_CLI_VERSION` to pin a different `@openai/codex` version. This is a local development convenience for the Docker VM simulation; a real team worker should get Codex auth, git credentials, registry credentials, and kubeconfig from its own managed runtime environment.

Cancellation is cooperative. Stopping a worker-backed session requests cancellation on the server task; the worker polls its claimed task and interrupts Codex app-server when it sees `cancelled`.

To test team collaboration through the UI only:

1. Sign in with GitHub from the workspace menu.
2. Confirm the workspace menu labels the current workspace as Personal.
3. Create a team workspace from the workspace menu.
4. Confirm the new team workspace is selected, or use the workspace menu to switch into it.
5. Open Workspace Settings and use Team access to create an invite link.
6. Copy the invite link and open it as another signed-in GitHub identity.
7. Accept the invite from the Join workspace page.
8. Confirm Workspace Settings shows both members, then continue with Runtime token, worker, task, and Issue Detail session testing.

## Website

Production site:

- [mspace-website-blue.vercel.app](https://mspace-website-blue.vercel.app)

Local development:

```bash
pnpm dev:website
pnpm build:website
pnpm preview:website
```

The website app lives in `apps/website`. It uses product screenshots from `docs/images/`, keeps the full desktop app icon at `apps/desktop/assets/brand/mspace-icon.png`, and uses the transparent navigation mark at `apps/website/src/assets/mspace-mark-transparent.png` so the website logo can sit inside its own gray-white tile without a nested white background.

Screenshot placement should stay selective. The root README should carry only one or two representative current screenshots. The public website may show a compact curated set of running surfaces, but it should not become a full gallery of every Issue Detail sub-tab. Keep full tab coverage in `docs/images/` for repository documentation, or upload article images to the external image host used by the publishing workflow and embed those cloud URLs from article pages.

The public changelog is a static website view, not product runtime state. Update `apps/website/src/changelog.ts` whenever a task ships meaningful product, engineering, documentation, or website progress. Entries are grouped by calendar day and should stay public-facing: describe what changed without exposing private cluster names, credentials, local paths beyond repository paths, or temporary debugging noise.

Vercel deployment is configured from the repository root:

```bash
npx vercel@latest --prod
```

The root `vercel.json` uses `pnpm install --frozen-lockfile`, builds with `pnpm --filter @mspace/website build`, and publishes `apps/website/dist`. The local `.vercel/` project link is intentionally ignored by git. Vercel CLI authentication is machine/account-level rather than per shell session; check with `npx vercel@latest whoami` if a fresh terminal cannot deploy.

## Environment Variables

| Variable | Used by | Default | Purpose |
| --- | --- | --- | --- |
| `MSPACE_RUNNER_PORT` | Electron main process | `7788` | Port used when desktop starts the local runner. |
| `MSPACE_RUNNER_URL` | Electron preload/renderer | `http://127.0.0.1:7788` | API base URL exposed to the renderer. |
| `MSPACE_RUNNER_START_TIMEOUT_MS` | Electron main process | `60000` | How long the desktop waits for the runner health check before startup fails. |
| `MSPACE_PORT` | Go runner | `7788` | Port used by a standalone runner. |
| `MSPACE_SERVER_ADDR` | Server and Electron main process | `127.0.0.1:8787` | Address used when the server control plane listens or is started by desktop. |
| `MSPACE_SERVER_URL` | Electron preload/renderer | `http://127.0.0.1:8787` | Server control-plane base URL exposed to the renderer. |
| `MSPACE_SERVER_START_TIMEOUT_MS` | Electron main process | `30000` | How long the desktop waits for the server health check before startup fails. |
| `MSPACE_RUNTIME_TOKEN` | Worker | none | Internal runtime bootstrap credential with `msw_` prefix. The desktop UI creates and injects this for local Docker workers; set it manually only for external workers or debugging. |
| `MSPACE_WORKER_NAME` | Worker | host-derived | Stable worker name shown in Workspace Settings. |
| `MSPACE_WORKER_MODE` | Worker | `team` | Runtime mode reported to the server; `team` or `personal`. |
| `MSPACE_WORKER_CAPABILITIES` | Worker | `{"protocolSmoke":true,"codex":false,"dryRun":true}` | JSON object used by server-side capability matching. The Docker dev script overrides this to `{"protocolSmoke":true,"codex":true,"dryRun":true}` so UI-triggered Team sessions can be tested without Codex auth. Set `codex:true` and `dryRun:false` for workers intended to run real Codex `agent_session` tasks. |
| `MSPACE_WORKER_LABELS` | Worker | `{}` | JSON object for placement or inventory labels. |
| `MSPACE_WORKER_POLL_INTERVAL` | Worker | `5s` | How often the worker polls the runtime task queue. |
| `MSPACE_WORKER_HEARTBEAT_INTERVAL` | Worker | `10s` | How often the worker reports liveness and load. |
| `MSPACE_WORKER_CODEX_HOME_SOURCE` | Docker Codex worker script | `${CODEX_HOME:-~/.codex}` | Source Codex home copied into a dedicated worker Codex home before container startup. |
| `MSPACE_WORKER_CODEX_HOME_DIR` | Docker Codex worker script | `~/.mspace/codex-worker-home` | Host directory mounted into the Codex-capable dev worker as `CODEX_HOME`. |
| `MSPACE_WORKER_CODEX_CLI_VERSION` | Docker Codex worker script | `0.130.0` | `@openai/codex` npm version installed into `worker/Dockerfile.codex-dev`. |
| `DATABASE_URL` | Server | none | Postgres connection string for control-plane storage. |
| `MSPACE_GITHUB_CLIENT_ID` | Server | none | GitHub OAuth App client id. |
| `MSPACE_GITHUB_CLIENT_SECRET` | Server | none | GitHub OAuth App client secret; keep it server-side only. |
| `MSPACE_GITHUB_REDIRECT_URI` | Server | none | OAuth callback URL, usually `http://127.0.0.1:8787/api/auth/github/callback` locally. |

Cluster, project, and issue test environment fields are passed into sessions as:

| Variable | Source |
| --- | --- |
| `MSPACE_CLUSTER_ID` | Selected issue test environment cluster id when present. |
| `MSPACE_KUBE_CONTEXT` | Selected issue test environment `kube_context`, or project `kube_context` for legacy commands. |
| `KUBECONFIG` / `MSPACE_KUBECONFIG` | Selected issue test environment `kubeconfig_path`, or project `kubeconfig_path` for legacy commands. |
| `MSPACE_KUBE_NAMESPACE` | Project fallback `namespace`, or issue test namespace when present. |
| `MSPACE_TEST_NAMESPACE` | Issue test environment namespace. |
| `MSPACE_IMAGE_REGISTRY_PREFIX` | Selected cluster or issue test environment image registry prefix. |
| `MSPACE_EXPOSURE_MODE` | Selected issue test environment exposure mode. |
| `MSPACE_PREVIEW_DOMAIN` | Optional issue preview domain. |
| `MSPACE_INGRESS_CLASS` | Optional issue ingress class. |
| `MSPACE_NODE_HOST` | Optional issue NodePort host. |

Session metadata is also passed into the Codex app-server process environment as:

| Variable | Source |
| --- | --- |
| `MSPACE_ISSUE_ID` | Current issue id. |
| `MSPACE_SESSION_ID` | Current session id. |
| `MSPACE_API_BASE_URL` | Local runner API base URL. |
| `MSPACE_AGENT_TOKEN` | Scoped bearer token for agent writes back to the local runner API. |
| `MSPACE_AGENT_PROFILE` | Selected managed agent profile id. |
| `MSPACE_SESSION_BRANCH` | Planned session branch. |
| `MSPACE_SESSION_WORKDIR` | Prepared git worktree path. |
| `MSPACE_SOURCE_SESSION_ID` | Selected source session for a deploy/test continuation, when present. |
| `MSPACE_SOURCE_COMMIT_SHA` | Selected source commit for a deploy/test continuation, when present. |
| `MSPACE_SESSION_CONTEXT` | Markdown context file written under `~/.mspace/workdirs/_contexts/`. |
| `MSPACE_SESSION_ARTIFACT_DIR` | Session artifact directory under the prepared worktree. |

## Codex App-Server Runtime

Codex-backed sessions start:

```bash
codex app-server --listen stdio://
```

The runner launches the process inside the prepared worktree, sends `initialize`, `thread/start`, and `turn/start` over newline-delimited JSON-RPC, then records app-server notifications into `session_logs`.

Current local-session defaults:

| Setting | Value | Reason |
| --- | --- | --- |
| `approvalPolicy` | `never` | mspace does not yet have an approval UI, so unattended sessions must not hang on approval prompts. |
| `sandbox` | `danger-full-access` | Local macOS worktrees and Codex desktop behavior match this mode most reliably for now. |
| Transport | `stdio://` | Matches the Multica-style provider shape and preserves thread, turn, status, and notification state better than `codex exec`. |

Check that the CLI is available:

```bash
codex app-server --help
```

## Smoke Checks

Server health:

```bash
curl http://127.0.0.1:8787/health
```

The health response should include `serverProtocol: 1` and these capabilities: `workspaceInboxIssueGrouping`, `workspaceCollaboration`, `teamWorkspaceCreation`, `workspaceInvitations`, `workspaceKinds`, `runtimeWorkerRegistration`, and `runtimeTaskQueue`. Electron treats a local server that lacks those capabilities as stale and replaces it during desktop development.

GitHub auth start endpoint:

```bash
curl -i http://127.0.0.1:8787/api/auth/github/start
```

This endpoint requires `DATABASE_URL`, `MSPACE_GITHUB_CLIENT_ID`, `MSPACE_GITHUB_CLIENT_SECRET`, and `MSPACE_GITHUB_REDIRECT_URI` to be configured in the server environment.

Workspace Inbox receipt checks:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/inbox
```

Runtime worker smoke:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-registration-tokens" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"local worker","expiresInHours":12}'

export MSPACE_RUNTIME_TOKEN="msw_..."
pnpm worker -- -once
```

Use any signed-in workspace id for Inbox checks, worker tokens, worker lists, and runtime task APIs. Use a team workspace id for invitations and shared member APIs; personal workspaces intentionally reject those team-only collaboration-management APIs with `403 Forbidden`.

Queue a protocol task for that worker:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"protocol_smoke","runtimeMode":"team","requiredCapabilities":{"protocolSmoke":true},"payload":{"source":"curl"}}'
```

Inspect the task audit trail:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks/<task-id>/events"
```

Inspect worker logs for a claimed or completed task:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks/<task-id>/logs"
```

Cancel a queued, claimed, or running task:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks/<task-id>/cancel" \
  -H "Authorization: Bearer <msp-token>" \
  -H "Content-Type: application/json" \
  -d '{"reason":"manual stop"}'
```

Queue an agent-session task for a Codex-capable worker:

```bash
export MSPACE_WORKER_CAPABILITIES='{"protocolSmoke":true,"codex":true,"dryRun":false}'
export MSPACE_RUNTIME_TOKEN="msw_..."
pnpm worker

curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "kind":"agent_session",
    "runtimeMode":"team",
    "requiredCapabilities":{"codex":true},
    "payload":{
      "workdir":"/absolute/path/to/repo",
      "prompt":"Inspect the repository and report the current test command without changing files."
    }
  }'
```

Runner health:

```bash
curl http://127.0.0.1:7788/health
```

Legacy local Inbox checks:

```bash
curl http://127.0.0.1:7788/api/inbox
sqlite3 ~/.mspace/mspace.db "select issue_id,title,status,unread,updated_at from inbox_items order by updated_at desc limit 10;"
```

Image attachment smoke check:

```bash
curl -sS -o /tmp/mspace-attachment.png -w '%{http_code} %{content_type} %{size_download}\n' \
  http://127.0.0.1:7788/api/attachments/<attachment-id>
file /tmp/mspace-attachment.png
```

Local actor display snapshots:

```bash
sqlite3 ~/.mspace/mspace.db "select id,title,creator_name,creator_avatar_url from issues order by updated_at desc limit 5;"
sqlite3 ~/.mspace/mspace.db "select issue_id,author_type,author_name,author_avatar_url,created_at from comments order by created_at desc limit 10;"
sqlite3 ~/.mspace/mspace.db "select issue_id,comment_id,reaction,user_id,created_at from comment_reactions order by created_at desc limit 10;"
```

Status-transition author checks:

```bash
sqlite3 ~/.mspace/mspace.db "select issue_id,author_type,author_name,body,created_at from comments where body like 'Issue status changed from%' or body like 'Task `% status changed from%' order by created_at desc limit 10;"
```

Status-change rows should be authored by the actor that made the change. Human UI/API status updates should show `author_type='human'` and the signed-in user name from control-plane `GET /api/auth/me`; scoped agent token updates should show `author_type='agent'` and the agent name.

Recent sessions:

```bash
sqlite3 ~/.mspace/mspace.db "select id,provider,agent_profile,status,agent_status,cleanup_status,cleaned_at,codex_thread_id,codex_turn_id,branch,workdir,updated_at from agent_sessions order by updated_at desc limit 5;"
```

Failure evidence:

```bash
sqlite3 ~/.mspace/mspace.db "select issue_id,session_id,phase,status,failed_command,error_summary,namespace,resource_kind,resource_name,updated_at from session_failures order by updated_at desc limit 10;"
```

Issue branch or PR handoff records:

```bash
sqlite3 ~/.mspace/mspace.db "select issue_id,kind,branch,pr_url,pr_state,error,updated_at from issue_handoffs order by updated_at desc limit 10;"
```

Workspace automation policy:

```bash
curl http://127.0.0.1:7788/api/workspace/settings
sqlite3 ~/.mspace/mspace.db "select id,auto_create_draft_pr,updated_at from workspace_settings;"
```

Source commit capture is always on. `auto_create_draft_pr` only controls whether the runner automatically creates or refreshes the current issue-level draft PR after a source commit is captured.

Issue labels:

```bash
curl http://127.0.0.1:7788/api/issue-label-definitions
sqlite3 ~/.mspace/mspace.db "select key,name,dimension,sort_order from issue_label_definitions order by dimension,sort_order;"
sqlite3 ~/.mspace/mspace.db "select issue_id,label_id,name,created_at from issue_labels order by created_at desc limit 20;"
sqlite3 ~/.mspace/mspace.db "select id,title,triage_status,updated_at from issues where parent_issue_id is null order by updated_at desc limit 10;"
```

Managed agents:

```bash
curl http://127.0.0.1:7788/api/agents
sqlite3 ~/.mspace/mspace.db "select id,name,mention,provider,enabled,built_in,updated_at from agent_profiles order by sort_order,created_at;"
```

Project runbooks:

```bash
curl http://127.0.0.1:7788/api/projects/<project-id>/runbook
sqlite3 ~/.mspace/mspace.db "select project_id,status,source,source_session_id,updated_at from project_runbooks order by updated_at desc;"
sqlite3 ~/.mspace/mspace.db "select project_id,author_type,session_id,created_at from project_runbook_revisions order by created_at desc limit 10;"
```

Agents should not edit repository docs for runbook learning by default. To update the mspace runbook from a session, write Markdown to:

```bash
cat > "$MSPACE_SESSION_ARTIFACT_DIR/project-runbook.md" <<'EOF'
# Runbook

## Dependencies
## Local Start
## Tests
## Build
## Image Build
## Deploy
## Health Check
## Common Failures
EOF
```

To provide a clean Evidence tab without forcing reviewers through raw logs, write a compact review artifact from the session:

```bash
cat > "$MSPACE_SESSION_ARTIFACT_DIR/review-evidence.json" <<'EOF'
{
  "agentSummary": "Implemented the requested change and verified it.",
  "commandsRun": ["pnpm typecheck", "pnpm --filter @mspace/desktop build"],
  "tests": {
    "pnpm typecheck": "passed"
  },
  "buildResult": "passed: desktop build completed",
  "deploymentResult": {
    "status": "not_reported",
    "summary": "Deployment result was not reported."
  },
  "risks": [],
  "followUps": []
}
EOF
```

If no review artifact exists, the runner derives evidence from `session_logs` and deployment state. It persists only evidence-worthy commands such as tests, builds, deploys, dependency installs, `git diff --check`, `git commit`, Playwright checks, and issue-status update calls. Exploratory commands such as `sed`, `rg`, and `find` remain in raw session logs.

In the product UI, the default Evidence tab is a read-only current review packet. `Command evidence` shows the compact commands that support that packet. Older reviews, failed attempts, interruptions, blockers, and Kubernetes snapshot tables open from the Evidence tab into full-width history pages so the right rail does not become a logs or resources browser.

Reusable test clusters:

```bash
curl http://127.0.0.1:7788/api/clusters
curl http://127.0.0.1:7788/api/clusters/discover-defaults
curl -X POST http://127.0.0.1:7788/api/clusters/import-defaults
curl -X POST http://127.0.0.1:7788/api/clusters/import \
  -H 'Content-Type: application/json' \
  -d '{"paths":["/Users/mlhiter/.kube/test"]}'
```

Discovery uses `kubectl config view` to list contexts before import. The Clusters UI shows the discovered files and contexts first, then imports only the selected file paths. Import runs a read-only `/version` check for each selected context. Imported clusters are marked `ready` or `unreachable`; unreachable clusters can still be edited later.

Inspect a session worktree:

```bash
git -C ~/.mspace/workdirs/<project-id>/<session-id> status --short
```

Clean a retained, non-active session worktree:

```bash
curl -X POST http://127.0.0.1:7788/api/sessions/<session-id>/cleanup
sqlite3 ~/.mspace/mspace.db "select id,status,cleanup_status,cleaned_at,workdir from agent_sessions where id = '<session-id>';"
```

Cleanup removes the git worktree only. Logs, comments, evidence, and session metadata stay in SQLite. Queued or running sessions return `409 Conflict`; a missing worktree is marked cleaned idempotently after the path safety check passes.

Queue an issue test deployment:

```bash
CLUSTER_ID="$(sqlite3 ~/.mspace/mspace.db "select id from clusters order by updated_at desc limit 1;")"
SOURCE_SESSION_ID="$(sqlite3 ~/.mspace/mspace.db "select session_id from issue_change_nodes where issue_id = '<issue-id>' order by created_at desc limit 1;")"
SOURCE_COMMIT_SHA="$(sqlite3 ~/.mspace/mspace.db "select commit_sha from issue_change_nodes where issue_id = '<issue-id>' order by created_at desc limit 1;")"
curl -X POST http://127.0.0.1:7788/api/issues/<issue-id>/test-deploy \
  -H 'Content-Type: application/json' \
  -d "{\"clusterId\":\"${CLUSTER_ID}\",\"sourceSessionId\":\"${SOURCE_SESSION_ID}\",\"sourceCommitSha\":\"${SOURCE_COMMIT_SHA}\",\"exposureMode\":\"nodeport\",\"nodeHost\":\"test-node.example.com\"}"
```

The deploy/test agent uses the selected source commit plus selected cluster config, creates the issue namespace, builds and pushes images, deploys resources, exposes NodePort by default or Ingress when selected with a preview domain, and can write `previewUrl` to `$MSPACE_SESSION_ARTIFACT_DIR/test-environment.json`. The runner then captures Kubernetes evidence, discovers preview candidates, checks whether the preview URL opens, and records a `session_failures` row when deploy, preview, interruption, or cleanup state needs attention.

Force the same preview status check that Issue Detail normally runs in the background:

```bash
curl -X POST http://127.0.0.1:7788/api/issues/<issue-id>/test-environment/probe
```

Use that route for debugging or automation only. In the product UI, users should open the preview link; mspace refreshes status without a separate Probe button. A preview status check updates `issue_test_environments` and the Test environment sidebar `Checked` state only; it should not create deployment/review evidence rows, failure rows, or top-level issue status events.

Fetch the live namespace resources shown by the Issue Detail Resources tab:

```bash
curl http://127.0.0.1:7788/api/issues/<issue-id>/test-environment/resources \
  -H "Authorization: Bearer <msp-token>"
```

The route derives the namespace from the issue test environment. If `?namespace=...` is present, it returns `400 Bad Request`; use this as a quick safety check when debugging the resource tab. The payload is intentionally limited to Pods, Services, Deployments, Ingresses, Events, and per-section errors.

Record or trigger namespace cleanup:

```bash
curl -X POST http://127.0.0.1:7788/api/issues/<issue-id>/test-environment/retain
curl -X POST http://127.0.0.1:7788/api/issues/<issue-id>/test-environment/cleanup -H 'Content-Type: application/json' -d '{}'
```

Run validation commands:

```bash
pnpm typecheck
pnpm --filter @mspace/desktop build
(cd packages/ui && pnpm dlx shadcn@latest info --json)
(cd runner && go test ./...)
(cd runner && go build ./...)
```

UI component check:

```bash
cd packages/ui && pnpm dlx shadcn@latest info --json
```

Expected shadcn/ui source components currently include:

- alert
- badge
- button
- card
- dropdown-menu
- field
- input
- label
- scroll-area
- separator
- select
- switch
- textarea

## Common Troubleshooting

### GitHub Login Fails With `failed to fetch`

The renderer calls the server control plane, not the local runner, for GitHub login. Check the server first:

```bash
curl -i http://127.0.0.1:8787/health
lsof -nP -iTCP:8787 -sTCP:LISTEN
```

If the server is not healthy, verify `.env.local` contains the server-only auth configuration and restart the server or desktop:

```bash
pnpm run server
```

If the server is healthy but the renderer still fails, check that `MSPACE_SERVER_URL` points at the same origin exposed by Electron preload. For local desktop development the default is `http://127.0.0.1:8787`.

If a newly added control-plane route returns `404` or `405`, inspect `curl http://127.0.0.1:8787/health`. A current server must advertise `capabilities.teamWorkspaceCreation=true` and `capabilities.workspaceKinds=true`; otherwise restart `pnpm run server` or relaunch the desktop so Electron can replace the stale server.

### User Or Agent Avatars Do Not Load

GitHub avatars require the renderer content security policy to allow GitHub image hosts. Check `apps/desktop/src/renderer/index.html` includes `https://avatars.githubusercontent.com` and `https://*.githubusercontent.com` in `img-src`.

Codex agent avatars should use the shared data URL in `packages/views/src/agent-avatar.ts`. If the UI falls back to a letter or generic icon, verify that the data URL is not truncated and that the renderer imports the shared `codexAvatarDataUrl` instead of embedding another copy.

### Pasted Images Show A Placeholder Instead Of A Thumbnail

First verify the runner can serve the attachment:

```bash
curl -sS -o /tmp/mspace-attachment.png -w '%{http_code} %{content_type} %{size_download}\n' \
  http://127.0.0.1:7788/api/attachments/<attachment-id>
file /tmp/mspace-attachment.png
```

If the response is `200 image/*`, check the renderer content security policy in `apps/desktop/src/renderer/index.html`. Local attachment thumbnails require `img-src` to allow the local runner origin, normally `http://127.0.0.1:*` and `http://localhost:*`. Reload the Electron window or restart `pnpm dev:desktop` after changing CSP because it is applied when the page loads.

If the response is `404` or `405`, check `curl http://127.0.0.1:7788/health`. The health payload should include `runnerProtocol` and `capabilities.issueAttachments=true`; otherwise the desktop is talking to a stale runner.

### Desktop Shows Unstyled HTML

Tailwind CSS 4 must scan the monorepo package sources and map shadcn semantic tokens. Check that `apps/desktop/src/renderer/src/globals.css` contains:

```css
@import "tailwindcss";
@source "../../../../../packages/ui/src";
@source "../../../../../packages/views/src";

@theme inline {
  --color-background: var(--paper);
  --color-foreground: var(--text);
  --color-card: var(--surface);
  --color-primary: var(--ink);
  --color-border: var(--line);
}
```

The exact token list may grow, but `@theme inline` should keep shadcn color tokens mapped to the mspace palette.

### shadcn Imports Fail During Desktop Build

Check the desktop Vite aliases:

```bash
sed -n '/alias:/,/},/p' apps/desktop/electron.vite.config.ts
```

Required aliases:

- `@mspace/ui/components`
- `@mspace/ui/lib`
- `@mspace/ui`

Then verify both shadcn config files exist:

```bash
test -f components.json
test -f packages/ui/components.json
cd packages/ui && pnpm dlx shadcn@latest info --json
```

### Runner Port Is Already In Use

Check the health endpoint first:

```bash
curl http://127.0.0.1:7788/health
```

If another process owns the port, choose another port:

```bash
MSPACE_RUNNER_PORT=7790 MSPACE_RUNNER_URL=http://127.0.0.1:7790 pnpm dev:desktop
```

For standalone runner debugging:

```bash
cd runner && MSPACE_PORT=7790 go run .
```

### Clusters Page Shows 405 For Discovery

If `GET /api/clusters/discover-defaults` returns `405 Method Not Allowed`, the desktop is probably connected to an older runner that was already listening on `7788`. The old router treats `discover-defaults` as `{clusterID}` and only allows `PUT`/`DELETE`. Current desktop startup should replace stale runners automatically, but the manual check is:

```bash
curl -i http://127.0.0.1:7788/api/clusters/discover-defaults
lsof -nP -iTCP:7788 -sTCP:LISTEN
kill <runner-pid>
cd runner && MSPACE_PORT=7788 go run .
```

Then refresh the desktop app.

### Type Or Priority Options Are Missing

If the UI shows no Type or Priority choices, or `GET /api/issue-label-definitions` returns `404 Not Found`, the desktop is probably connected to an older runner that does not have the label-definition route. Current desktop startup should replace stale runners automatically, but the manual check is:

```bash
curl -i http://127.0.0.1:7788/api/issue-label-definitions
lsof -nP -iTCP:7788 -sTCP:LISTEN
kill <runner-pid>
cd runner && MSPACE_PORT=7788 go run .
```

The renderer has built-in fallback options for the human UI, but the current runner is still required before label writes can persist against the seeded taxonomy.

### Project Creation Fails

Local-folder projects must point to an absolute git repository path. Check:

```bash
test -d /absolute/path/to/repo/.git && git -C /absolute/path/to/repo status --short
```

If the project is created from the desktop app, prefer the folder picker instead of typing the path manually.

GitHub imports must use a GitHub repository URL. The runner clones them into `~/.mspace/repos/<owner>/<repo>`. Check:

```bash
test -d ~/.mspace/repos/<owner>/<repo>/.git && git -C ~/.mspace/repos/<owner>/<repo> remote -v
```

Signed-in workspace issue creation no longer requires a project. A new issue can stay workspace-level until the repository is known; Issue Detail shows `No project` and lets the user attach an existing project from the Project sidebar section. Agent execution, PR handoff, and test environments stay disabled until a project is attached.

If issue creation still fails with a project-related error, check whether the request is using the legacy runner API instead of the server workspace API. The legacy runner path still requires an existing inferred project. The signed-in desktop product should create issues through `POST /api/workspaces/{workspaceID}/issues`.

### Project Delete Fails

Projects can only be deleted before any issues or sessions are attached. Check:

```bash
sqlite3 ~/.mspace/mspace.db "select id,name,issue_count,session_count from (select p.id,p.name,count(distinct i.id) as issue_count,count(distinct s.id) as session_count from projects p left join issues i on i.project_id = p.id left join agent_sessions s on s.issue_id = i.id group by p.id) order by name;"
```

### Session Shows Working But Stop Says `session is not running`

This means SQLite still has an active session row, but the current runner process does not have the in-memory canceller for that session. The usual cause is closing or restarting Electron while a Codex app-server deploy/test turn was still active. The old turn cannot be resumed; the useful state is the preserved session logs, worktree, source commit, and test-environment record.

Check the persisted state:

```bash
sqlite3 ~/.mspace/mspace.db "select id,issue_id,status,agent_status,codex_thread_id,codex_turn_id,updated_at from agent_sessions where status in ('queued','running') order by updated_at desc;"
sqlite3 ~/.mspace/mspace.db "select issue_id,namespace,namespace_status,cleanup_status,last_deploy_session_id,last_cleanup_session_id,preview_url from issue_test_environments order by updated_at desc limit 10;"
ps -axo pid,ppid,stat,etime,command | rg -i 'codex app-server|mspace|runner' | rg -v 'rg -i'
```

Current runner startup reconciles orphaned active rows automatically: source/deploy sessions become `failed` with `agent_status='interrupted'`, the issue moves to `blocked`, and a linked deploy or cleanup environment moves from `deploying`/`cleanup_requested` to `deploy_interrupted`/`cleanup_failed`. If the UI was loaded before that reconciliation, restart the runner or the desktop app and refresh Issue Detail:

```bash
curl http://127.0.0.1:7788/health
lsof -nP -iTCP:7788 -sTCP:LISTEN
kill <runner-pid>
cd runner && MSPACE_PORT=7788 go run .
```

If a stale active row is still shown while the current runner is serving, the Stop button should now succeed. It marks only the session `cancelled`, appends a system log explaining that the in-memory runner handle was lost, and leaves the top-level issue status unchanged so the issue can be retried.

### Failure Evidence Does Not Appear In Issue Detail

Failure cards render only when `session_failures` has rows for the issue or session. The runner creates rows when a session fails, when deploy/preview/cleanup reconciliation needs attention, when Stop resolves an orphaned active row, and when startup backfills older failed or cancelled sessions that predate the structured failure table.

Check whether the table has data:

```bash
sqlite3 ~/.mspace/mspace.db "select count(*) from session_failures;"
sqlite3 ~/.mspace/mspace.db "select issue_id,session_id,phase,status,error_summary,updated_at from session_failures order by updated_at desc limit 10;"
```

If the table exists but the count is `0` while old failed sessions exist, restart the runner so startup migrations and backfill run:

```bash
curl http://127.0.0.1:7788/health
lsof -nP -iTCP:7788 -sTCP:LISTEN
kill <runner-pid>
cd runner && MSPACE_PORT=7788 go run .
```

After backfill, open the affected issue and check Issue Detail `Overview` for `Failure needs attention`. For older failures or interruptions, open Evidence and then `Previous attempts`; the default Evidence view stays focused on the current review packet.

### Session Fails Before Starting Runtime

The runner creates a git worktree before starting Codex app-server or the fallback shell runtime. Check:

```bash
git --version
git -C /absolute/path/to/repo worktree list
sqlite3 ~/.mspace/mspace.db "select id,status,agent_status,cleanup_status,cleaned_at,codex_thread_id,codex_turn_id,branch,workdir from agent_sessions order by updated_at desc limit 5;"
codex app-server --help
```

Common causes:

- `git` is not on `PATH`;
- `codex` is not on `PATH`;
- the runner was launched with an isolated `HOME` and Codex cannot find an authenticated `CODEX_HOME`, which can surface as `401 Unauthorized: Missing bearer or basic authentication`;
- the project repo path is not a git repository;
- the session branch already exists in an unexpected state;
- the planned worktree directory already exists.

### Session Fails While Recording Source Commit

When a source-code session finishes with git changes, the runner first checks whether the session branch is already ahead of the project base branch. If the agent already created a commit, the runner records that HEAD commit as the issue change node instead of creating an extra commit. If the worktree still has uncommitted changes, it stages the worktree, excludes `.mspace` artifacts, and creates the captured source commit. It retries transient `.git/index.lock` conflicts during `git add`, `git reset`, and `git commit`.

If failures persist, check for a stale lock only after confirming no git process is active for the worktree:

```bash
sqlite3 ~/.mspace/mspace.db "select id,status,workdir from agent_sessions order by updated_at desc limit 5;"
git -C <workdir> status --short
lsof <worktree-git-dir>/index.lock
```

Remove `index.lock` only when no running git process owns it.

### Kubernetes Validation Does Not Run

The local MVP does not create scoped kubeconfigs or ServiceAccounts yet. It uses the kubeconfig stored on the selected Cluster and passes the resolved issue namespace to the deploy/test session.

Check:

```bash
sqlite3 ~/.mspace/mspace.db "select id,name,kubeconfig_path,kube_context,status from clusters order by updated_at desc;"
sqlite3 ~/.mspace/mspace.db "select issue_id,cluster_id,namespace,namespace_status,cleanup_status,preview_url from issue_test_environments order by updated_at desc limit 5;"
KUBECONFIG=<kubeconfig-path> kubectl --context <context> -n <issue-namespace> get pods
```

### Session Detail Has No Diff

The workspace snapshot is read from the session worktree stored in `agent_sessions.workdir`. Check:

```bash
sqlite3 ~/.mspace/mspace.db "select id,workdir from agent_sessions order by updated_at desc limit 1;"
git -C <workdir> status --short
git -C <workdir> diff --stat --patch --find-renames HEAD
```

If `cleanup_status` is `cleaned`, a missing worktree is expected. Session Detail should still show retained logs and metadata, but workspace inspection will report that the workspace is not available.
