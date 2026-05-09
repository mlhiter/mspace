# mspace Local Runbook

> Status: local MVP operations guide, updated 2026-05-09

## Local Data

| Path | Purpose |
| --- | --- |
| `~/.mspace/mspace.db` | SQLite database used by the Go runner. |
| `~/.mspace/repos/<owner>/<repo>` | Cached clone path for GitHub-imported repositories. |
| `~/.mspace/workdirs/<project-id>/<session-id>` | Git worktree created for one local agent session. |
| `~/.mspace/workdirs/_contexts/<session-id>.md` | Markdown session context included in the Codex app-server prompt. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session` | Session artifact directory recorded in `agent_sessions.artifact_dir`. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session/test-environment.json` | Optional deploy/test artifact. When it includes `previewUrl`, the runner copies it back to the issue test environment. |

Reusable cluster configs are stored in `clusters`. Issue test namespace records are stored in `issue_test_environments`. Issue label options are stored in `issue_label_definitions`, issue label selections are stored in `issue_labels`, and type triage state is stored on `issues.triage_status`. Agent definitions are stored in `agent_profiles`. The session worktree path is stored in `agent_sessions.workdir`. Codex-backed sessions also store `agent_profile`, `codex_thread_id`, `codex_turn_id`, `agent_status`, `artifact_dir`, `cleanup_status`, and `cleaned_at`.

The server control plane stores users, GitHub identities, workspaces, memberships, OAuth state, OAuth results, and mspace auth sessions in Postgres through `DATABASE_URL`. Local GitHub OAuth configuration should live in `.env.local`, which is ignored by git.

## Start The App

Install dependencies:

```bash
pnpm install
```

Start desktop:

```bash
pnpm dev:desktop
```

Electron starts the local Go runner and local server control plane automatically if their `GET /health` checks are not already healthy on the configured ports.

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
| `MSPACE_AGENT_PROFILE` | Selected managed agent profile id. |
| `MSPACE_SESSION_BRANCH` | Planned session branch. |
| `MSPACE_SESSION_WORKDIR` | Prepared git worktree path. |
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

GitHub auth start endpoint:

```bash
curl -i http://127.0.0.1:8787/api/auth/github/start
```

This endpoint requires `DATABASE_URL`, `MSPACE_GITHUB_CLIENT_ID`, `MSPACE_GITHUB_CLIENT_SECRET`, and `MSPACE_GITHUB_REDIRECT_URI` to be configured in the server environment.

Runner health:

```bash
curl http://127.0.0.1:7788/health
```

Local actor display snapshots:

```bash
sqlite3 ~/.mspace/mspace.db "select id,title,creator_name,creator_avatar_url from issues order by updated_at desc limit 5;"
sqlite3 ~/.mspace/mspace.db "select issue_id,author_type,author_name,author_avatar_url,created_at from comments order by created_at desc limit 10;"
```

Recent sessions:

```bash
sqlite3 ~/.mspace/mspace.db "select id,provider,agent_profile,status,agent_status,cleanup_status,cleaned_at,codex_thread_id,codex_turn_id,branch,workdir,updated_at from agent_sessions order by updated_at desc limit 5;"
```

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
curl -X POST http://127.0.0.1:7788/api/issues/<issue-id>/test-deploy \
  -H 'Content-Type: application/json' \
  -d "{\"clusterId\":\"${CLUSTER_ID}\",\"exposureMode\":\"nodeport\",\"nodeHost\":\"test-node.example.com\"}"
```

The deploy/test agent uses the selected cluster config, creates the issue namespace, builds and pushes images, deploys resources, exposes NodePort by default or Ingress when selected with a preview domain, probes the preview URL, and can write `previewUrl` to `$MSPACE_SESSION_ARTIFACT_DIR/test-environment.json`.

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
- field
- input
- label
- scroll-area
- separator
- select
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

### User Or Agent Avatars Do Not Load

GitHub avatars require the renderer content security policy to allow GitHub image hosts. Check `apps/desktop/src/renderer/index.html` includes `https://avatars.githubusercontent.com` and `https://*.githubusercontent.com` in `img-src`.

Codex agent avatars should use the shared data URL in `packages/views/src/agent-avatar.ts`. If the UI falls back to a letter or generic icon, verify that the data URL is not truncated and that the renderer imports the shared `codexAvatarDataUrl` instead of embedding another copy.

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

If `GET /api/clusters/discover-defaults` returns `405 Method Not Allowed`, the desktop is probably connected to an older healthy runner that was already listening on `7788`. The old router treats `discover-defaults` as `{clusterID}` and only allows `PUT`/`DELETE`.

Check and restart the runner:

```bash
curl -i http://127.0.0.1:7788/api/clusters/discover-defaults
lsof -nP -iTCP:7788 -sTCP:LISTEN
kill <runner-pid>
cd runner && MSPACE_PORT=7788 go run .
```

Then refresh the desktop app.

### Type Or Priority Options Are Missing

If the UI shows no Type or Priority choices, or `GET /api/issue-label-definitions` returns `404 Not Found`, the desktop is probably connected to an older healthy runner that does not have the label-definition route.

Check and restart the runner:

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

If issue creation fails immediately after project setup, the usual cause is that no project exists yet. The issue modal no longer exposes a project selector; when more than one project exists, include the intended project or repository name in the issue note so the runner can infer the best matching existing project.

### Project Delete Fails

Projects can only be deleted before any issues or sessions are attached. Check:

```bash
sqlite3 ~/.mspace/mspace.db "select id,name,issue_count,session_count from (select p.id,p.name,count(distinct i.id) as issue_count,count(distinct s.id) as session_count from projects p left join issues i on i.project_id = p.id left join agent_sessions s on s.issue_id = i.id group by p.id) order by name;"
```

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
