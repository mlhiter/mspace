# mspace Server

`server/` is the mspace control plane. It owns identity, workspaces, membership, auth sessions, workspace product data, runtime-facing product surfaces, and future GitHub App installation state.

The desktop app and runtime workers are clients of this service. They do not own collaboration identity or product truth.

The server is intentionally Codex-free. It does not install the Codex CLI, mount `CODEX_HOME`, read Codex credentials, or start `codex app-server`. LLM-backed work is expressed as runtime tasks and executed by workers.

## Run

Create a local env file in the project root:

```bash
cp .env.example .env.local
```

Then edit `.env.local`. Without `DATABASE_URL`, the server defaults to the local SQLite personal store:

```dotenv
# Optional. Set for Postgres-backed shared development or deployment.
# DATABASE_URL=postgres://mspace:mspace@127.0.0.1:5432/mspace?sslmode=disable
# Optional. Use sqlite for packaged/local personal mode; use postgres for shared deployments.
# MSPACE_STORE=sqlite
# MSPACE_SQLITE_PATH=/path/to/mspace.db
# MSPACE_DATA_DIR=/path/to/mspace-data
# Optional. Only needed for GitHub OAuth sign-in.
# MSPACE_GITHUB_CLIENT_ID=...
# MSPACE_GITHUB_CLIENT_SECRET=...
# MSPACE_GITHUB_REDIRECT_URI=http://127.0.0.1:8787/api/auth/github/callback
MSPACE_SERVER_ADMIN_LOGINS=admin,mlhiter
MSPACE_BOOTSTRAP_ADMIN_LOGIN=admin
MSPACE_BOOTSTRAP_ADMIN_PASSWORD=change-me-long-random-password
MSPACE_BOOTSTRAP_ADMIN_NAME=Admin
MSPACE_SERVER_ADDR=127.0.0.1:8787
MSPACE_DEV_POSTGRES_CONTAINER=mspace-postgres-dev
MSPACE_DEV_POSTGRES_VOLUME=mspace-postgres-data
MSPACE_DEV_POSTGRES_IMAGE=postgres:16
```

Start the server:

```bash
pnpm run server
```

The server loads `.env`, `.env.local`, `server/.env`, and `server/.env.local` from the project root. Shell environment variables still take precedence over values from those files.

For local personal runs without Postgres, use the SQLite store. Production and shared development should use Postgres. The in-memory store is for tests only.

When `scripts/run-mspace-codex-dev.sh` needs to auto-start local Docker Postgres, it expects the durable data volume above. It labels the container and volume and rejects an existing `mspace-postgres-dev` container if it points at a different Postgres data volume.

## Auth Shape

Local password auth and GitHub OAuth are both identity providers. The product session is always a server-issued `msp_...` token.

Password auth is the default path for restricted or offline environments:

1. Desktop posts `POST /api/auth/password/register` or `POST /api/auth/password/login`.
2. The server validates the username/password payload, stores only a bcrypt password hash for registrations, ensures a default personal workspace, and issues an mspace session token.
3. Desktop stores the returned `msp_...` token.
4. Desktop clients call mspace APIs with `Authorization: Bearer <msp_...>`.

GitHub OAuth is optional. The server advertises OAuth availability through `/health` with `capabilities.githubAuth: true`, which requires `MSPACE_GITHUB_CLIENT_ID`, `MSPACE_GITHUB_CLIENT_SECRET`, and `MSPACE_GITHUB_REDIRECT_URI` to all be configured. The desktop also gates the button by server source: the default local personal server stays local-account-only, while saved Team server URLs and `MSPACE_SERVER_URL` launches can show GitHub when this capability is true.

1. Desktop starts GitHub login through `GET /api/auth/github/start`.
2. Desktop opens the returned `authorizeUrl` in the browser.
3. GitHub redirects to `GET /api/auth/github/callback`.
4. The server validates OAuth state, exchanges the code with GitHub using the server-side client secret, upserts the mspace user, ensures a default personal workspace, issues an mspace session token, and stores a short-lived single-use login result for that OAuth state.
5. The callback renders a success page. It does not return raw auth JSON.
6. Desktop polls `GET /api/auth/github/result?state=...` and stores the returned `msp_...` token.
7. Desktop clients call mspace APIs with `Authorization: Bearer <msp_...>`.

GitHub tokens are not the product session. They are used only to prove GitHub identity. Local password registration does not verify email ownership, so it must not merge identities by email. Repository automation should later use GitHub App installation tokens owned by this service.

Only server admins can create team workspaces. `MSPACE_SERVER_ADMIN_LOGINS` lists local password logins or GitHub logins with that right. Emails are not used for admin matching because local password registration does not verify email ownership. `MSPACE_BOOTSTRAP_ADMIN_LOGIN` plus `MSPACE_BOOTSTRAP_ADMIN_PASSWORD` creates the first local admin account on startup if it does not already exist; the server does not reset the password for an existing account. Other registered users still get a personal workspace and can join a team workspace through an owner/admin invitation.

## API Slice

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Server health. |
| `POST` | `/api/auth/password/register` | Create a local username/password identity, default personal workspace, and mspace session. |
| `POST` | `/api/auth/password/login` | Authenticate a local account and issue a mspace session. |
| `GET` | `/api/auth/github/start` | Create OAuth state and return the GitHub authorization URL plus polling path. |
| `GET` | `/api/auth/github/callback` | Complete GitHub OAuth, link identity, create an mspace session, and render the browser success page. |
| `GET` | `/api/auth/github/result` | Poll the state-bound login result from the desktop app. Returns `202` while pending and consumes the result once ready. |
| `GET` | `/api/auth/me` | Return the current mspace user and workspaces for a bearer token. |
| `GET` | `/api/workspaces` | List the authenticated user's workspaces. |
| `POST` | `/api/workspaces` | Create an explicit team workspace for the authenticated user. |
| `GET` | `/api/workspaces/{workspaceID}/members` | List workspace members with role and display identity. |
| `POST` | `/api/workspaces/{workspaceID}/invitations` | Create a one-time `msi_...` workspace invitation link. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/invitations` | List recent workspace invitations without raw tokens. Owner/admin only. |
| `DELETE` | `/api/workspaces/{workspaceID}/invitations/{invitationID}` | Revoke a workspace invitation. Owner/admin only. |
| `POST` | `/api/workspace-invitations/accept` | Accept a workspace invitation as the current authenticated user. |
| `GET` | `/api/workspaces/{workspaceID}/inbox` | List unread workspace issue-event receipts for the authenticated user. |
| `POST` | `/api/workspaces/{workspaceID}/issue-events` | Append a reviewable issue event and create per-user receipts. |
| `POST` | `/api/workspaces/{workspaceID}/issue-events/{eventID}/read` | Mark one event receipt read for the authenticated user. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/read-through` | Mark unread receipts for one issue read through an optional event boundary. |
| `GET` | `/api/workspaces/{workspaceID}/projects` | List workspace projects. |
| `POST` | `/api/workspaces/{workspaceID}/projects` | Create a workspace project. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}` | Update a workspace project. |
| `DELETE` | `/api/workspaces/{workspaceID}/projects/{projectID}` | Delete a project when no issues reference it. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/runbook` | Read the workspace project runbook. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}/runbook` | Replace the workspace project runbook and record a revision. |
| `GET` | `/api/workspaces/{workspaceID}/workspace/settings` | Read workspace automation settings. |
| `PUT` | `/api/workspaces/{workspaceID}/workspace/settings` | Update workspace automation settings. |
| `GET` | `/api/workspaces/{workspaceID}/agents` | List workspace agent profiles. |
| `POST` | `/api/workspaces/{workspaceID}/agents` | Create a workspace agent profile. |
| `PUT` | `/api/workspaces/{workspaceID}/agents/{agentID}` | Update a workspace agent profile. |
| `GET` | `/api/workspaces/{workspaceID}/clusters` | List workspace cluster configs. |
| `POST` | `/api/workspaces/{workspaceID}/clusters` | Create a workspace cluster config. |
| `PUT` | `/api/workspaces/{workspaceID}/clusters/{clusterID}` | Update a workspace cluster config. |
| `DELETE` | `/api/workspaces/{workspaceID}/clusters/{clusterID}` | Delete an unused workspace cluster config. |
| `GET` | `/api/workspaces/{workspaceID}/clusters/discover-defaults` | Discover kubeconfig candidates under `~/.kube`. |
| `POST` | `/api/workspaces/{workspaceID}/clusters/import` | Import selected kubeconfig files into workspace clusters. |
| `GET` | `/api/workspaces/{workspaceID}/issue-label-definitions` | List issue type and priority label definitions. |
| `GET` | `/api/workspaces/{workspaceID}/issues` | List top-level workspace issues. |
| `POST` | `/api/workspaces/{workspaceID}/issues` | Create a workspace issue. `projectId` is optional; issues can remain projectless until execution is needed. |
| `GET` | `/api/workspaces/{workspaceID}/issues/{issueID}` | Load issue detail with optional project, child tasks, labels, comments, and sessions. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}` | Update issue project attachment, title, body, or workflow status. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/tasks` | Create a child issue task. |
| `DELETE` | `/api/workspaces/{workspaceID}/issues/{issueID}/tasks/{taskID}` | Delete a child issue task under the parent. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/labels` | Replace selected issue label keys. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments` | Add a Markdown human comment. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}` | Edit the current user's eligible human comment. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}` | Add the current user's reaction to a comment. |
| `DELETE` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}` | Remove the current user's reaction from a comment. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-deploy` | Queue a server-owned test deployment session for an issue. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/cleanup` | Queue test namespace cleanup for an issue. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/retain` | Retain the issue test namespace for debugging. |
| `GET` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/resources` | List namespace-scoped Kubernetes resources for the issue test environment. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/probe` | Refresh preview reachability state for the issue test environment. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/create-pr` | Store or update the issue PR handoff from captured source evidence. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/{handoffID}/refresh` | Refresh the server-owned issue handoff record. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | Create a short-lived worker registration token. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | List worker registration token metadata without raw token values. Owner/admin only. |
| `DELETE` | `/api/workspaces/{workspaceID}/runtime-registration-tokens/{tokenID}` | Revoke a worker registration token. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-workers` | List registered runtime workers and their latest heartbeat state. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks` | Queue a runtime task for the workspace. Current product task kinds include `protocol_smoke`, `noop`, `issue_type_triage`, and `agent_session`. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks` | List recent runtime tasks for the workspace. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/events` | List audit events for one runtime task. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/logs` | List worker-appended logs for one runtime task. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/cancel` | Request cancellation for a queued, claimed, or running runtime task. |
| `POST` | `/api/runtime/workers/register` | Register or refresh a worker using `Authorization: Bearer msw_...`. |
| `POST` | `/api/runtime/workers/{workerID}/heartbeat` | Update worker liveness, status, load, and optional capability metadata. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/claim` | Claim the next queued task that matches the worker mode and required capabilities. |
| `GET` | `/api/runtime/workers/{workerID}/tasks/{taskID}` | Let the claiming worker inspect its task status while executing. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/{taskID}/logs` | Append a log line to a claimed/running worker-owned task. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/{taskID}/status` | Move a claimed task to `running`, `completed`, `failed`, or `cancelled`. |

## Workspace Inbox Model

The workspace Inbox is event-based. `issue_events` stores the append-only review fact, `issue_event_receipts` stores each recipient user's unread/read/archive state, and `issue_watchers` stores the issue-level recipient set. Opening or polling an issue must not clear unread state; clients should call the read-through endpoint after the user intentionally reviews an Inbox row.

Personal workspaces are the default result of password registration or GitHub sign-in. Personal and team workspaces both store projects, runbooks, issues, comments, reactions, labels, Inbox receipts, agent profiles, clusters, test environments, PR handoffs, agent sessions, runtime tasks, worker logs, and runtime results in the server store. Team/shared deployments use Postgres; packaged personal desktop mode can use local SQLite. Runtime worker registration and task APIs are available to both personal and team workspaces, but the runtime mode must match the workspace kind: personal workspaces use personal workers, while team workspaces use team workers. Team workspaces additionally unlock invitations and shared membership.

Issue `project_id` is optional in the control plane. A user can capture a workspace-level issue before the repository is known, comment on it, and attach a project later through `PUT /api/workspaces/{workspaceID}/issues/{issueID}` with `projectId`. If a create request omits `projectId` and the workspace has exactly one project, the server auto-attaches it; zero or multiple projects leave the issue unassigned. Agent execution, PR handoff, and issue test environments require an attached project.

## Workspace Invitations

Workspace Settings exposes the first UI-testable collaboration loop. Owners and admins can create one-time `msi_...` invite links, copy the link, inspect pending/accepted/revoked invitations, and revoke unused invitations. A signed-in teammate opens the invite route, accepts it through the UI, and then sees the shared workspace in the workspace switcher. The accepted member can inspect shared members, Inbox receipts, workers, and runtime tasks according to their workspace role.

## Runtime Worker Registry

Runtime registration tokens use the `msw_` prefix and are returned only once. The server stores a hash and prefix, then workers use the token to register, heartbeat, claim eligible tasks, and report task status.

The current queue is intentionally narrow: it records workspace task metadata, required capability JSON, payload/result JSON, claim ownership, timestamps, a compact audit event stream, and worker-appended task logs. Workers can stream Codex app-server status and output back through the log endpoint without the server needing direct network access to the worker environment or any Codex runtime dependency. Workspace users can request task cancellation; workers poll their claimed task while executing and interrupt Codex app-server when cancellation is requested.

The first worker-side implementation lives in `../worker`. It uses only the server HTTP contract: register with `Authorization: Bearer msw_...`, send heartbeat updates, claim matching tasks, inspect its claimed task for cancellation, append logs, and report status. It completes `protocol_smoke` and `noop` tasks, runs `issue_type_triage` tasks for issue type classification, and it can run `agent_session` tasks by preparing its own repository cache and session worktree from the task payload, then starting `codex app-server --listen stdio://` in that worker-managed workdir. Docker-backed workers store target project source under `/var/lib/mspace-worker/repos` and `/var/lib/mspace-worker/workdirs` on the configured worker volume, not in the host repository checkout. Issue Detail routes agent turns directly to `POST /api/workspaces/{workspaceID}/issues/{issueID}/sessions`; returned worker source commit metadata, changed files, and diff preview are exposed from the runtime task result. Dry-run worker commits are diagnostic records and should not be used as PR source candidates.
