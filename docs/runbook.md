# mspace Local Runbook

> Status: local MVP operations guide, updated 2026-05-11

## Local Data

| Path | Purpose |
| --- | --- |
| `~/.mspace/mspace.db` | SQLite database used by the Go runner. |
| `~/.mspace/repos/<owner>/<repo>` | Cached clone path for GitHub-imported repositories. |
| `~/.mspace/workdirs/<project-id>/<session-id>` | Git worktree created for one local agent session. |
| `~/.mspace/workdirs/_contexts/<session-id>.md` | Markdown session context included in the Codex app-server prompt. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session` | Session artifact directory recorded in `agent_sessions.artifact_dir`. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session/test-environment.json` | Optional deploy/test artifact. When it includes `previewUrl`, the runner copies it back to the issue test environment. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session/review-evidence.json` | Optional session review artifact. The runner imports compact commands, tests, build/deploy results, agent summary, risks, and follow-ups into `session_review_evidence`. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session/project-runbook.md` | Optional agent-learned project runbook artifact. When present after a successful session, the runner stores it as the current project runbook. |

Reusable cluster configs are stored in `clusters`. Project runbooks are stored in `project_runbooks`, with edit and agent-learning history in `project_runbook_revisions`. Issue test namespace records are stored in `issue_test_environments`. Review snapshots are stored in `session_review_evidence`; raw execution trails stay in `session_logs`. Local fallback unread rows are stored in `inbox_items`. Issue label options are stored in `issue_label_definitions`, issue label selections are stored in `issue_labels`, and type triage state is stored on `issues.triage_status`. Comment reactions are stored in `comment_reactions` so reaction counts do not mutate comment Markdown. Agent definitions are stored in `agent_profiles`. The session worktree path is stored in `agent_sessions.workdir`. Codex-backed sessions also store `agent_profile`, `codex_thread_id`, `codex_turn_id`, `agent_status`, `artifact_dir`, `cleanup_status`, and `cleaned_at`.

The server control plane stores users, GitHub identities, workspaces, memberships, OAuth state, OAuth results, mspace auth sessions, issue events, issue-event receipts, and issue watchers in Postgres through `DATABASE_URL`. Local GitHub OAuth configuration should live in `.env.local`, which is ignored by git.

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

## Website

Production site:

- [mspace-website-blue.vercel.app](https://mspace-website-blue.vercel.app)

Local development:

```bash
pnpm dev:website
pnpm build:website
pnpm preview:website
```

The website app lives in `apps/website`. It reuses the desktop brand mark from `apps/desktop/assets/brand/mspace-icon.png` and product screenshots from `docs/images/`.

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

Team Inbox receipt checks:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/inbox
```

Runner health:

```bash
curl http://127.0.0.1:7788/health
```

Local fallback Inbox checks:

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
- dropdown-menu
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

If issue creation fails immediately after project setup, the usual cause is that no project exists yet. The issue modal no longer exposes a project selector; when more than one project exists, include the intended project or repository name in the issue note so the runner can infer the best matching existing project.

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

Current runner startup reconciles orphaned active rows automatically: source/deploy sessions become `failed` with `agent_status='interrupted'`, the issue moves to `blocked`, and a linked deploy or cleanup environment moves from `deploying`/`cleanup_requested` to `deploy_failed`/`cleanup_failed`. If the UI was loaded before that reconciliation, restart the runner or the desktop app and refresh Issue Detail:

```bash
curl http://127.0.0.1:7788/health
lsof -nP -iTCP:7788 -sTCP:LISTEN
kill <runner-pid>
cd runner && MSPACE_PORT=7788 go run .
```

If a stale active row is still shown while the current runner is serving, the Stop button should now succeed. It marks only the session `cancelled`, appends a system log explaining that the in-memory runner handle was lost, and leaves the top-level issue status unchanged so the issue can be retried.

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
