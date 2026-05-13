# mspace Server

`server/` is the mspace control plane. It owns identity, workspaces, membership, auth sessions, and future GitHub App installation state.

The desktop app and local runner should become runtime clients of this service instead of owning collaboration identity themselves.

## Run

Create a local env file in the project root:

```bash
cp .env.example .env.local
```

Then edit `.env.local`:

```dotenv
DATABASE_URL=postgres://mspace:mspace@127.0.0.1:5432/mspace?sslmode=disable
MSPACE_GITHUB_CLIENT_ID=...
MSPACE_GITHUB_CLIENT_SECRET=...
MSPACE_GITHUB_REDIRECT_URI=http://127.0.0.1:8787/api/auth/github/callback
MSPACE_SERVER_ADDR=127.0.0.1:8787
```

Start the server:

```bash
pnpm run server
```

The server loads `.env`, `.env.local`, `server/.env`, and `server/.env.local` from the project root. Shell environment variables still take precedence over values from those files.

For local API-shape tests without Postgres, use the in-memory store from tests only. Production and shared development should use Postgres.

## Auth Shape

1. Desktop starts GitHub login through `GET /api/auth/github/start`.
2. Desktop opens the returned `authorizeUrl` in the browser.
3. GitHub redirects to `GET /api/auth/github/callback`.
4. The server validates OAuth state, exchanges the code with GitHub using the server-side client secret, upserts the mspace user, ensures a default workspace, issues an mspace session token, and stores a short-lived single-use login result for that OAuth state.
5. The callback renders a success page. It does not return raw auth JSON.
6. Desktop polls `GET /api/auth/github/result?state=...` and stores the returned `msp_...` token.
7. Desktop and runner clients call mspace APIs with `Authorization: Bearer <msp_...>`.

GitHub tokens are not the product session. They are used only to prove GitHub identity. Repository automation should later use GitHub App installation tokens owned by this service.

## API Slice

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Server health. |
| `GET` | `/api/auth/github/start` | Create OAuth state and return the GitHub authorization URL plus polling path. |
| `GET` | `/api/auth/github/callback` | Complete GitHub OAuth, link identity, create an mspace session, and render the browser success page. |
| `GET` | `/api/auth/github/result` | Poll the state-bound login result from the desktop app. Returns `202` while pending and consumes the result once ready. |
| `GET` | `/api/auth/me` | Return the current mspace user and workspaces for a bearer token. |
| `GET` | `/api/workspaces` | List the authenticated user's workspaces. |
| `GET` | `/api/workspaces/{workspaceID}/members` | List workspace members with role and display identity. |
| `POST` | `/api/workspaces/{workspaceID}/invitations` | Create a one-time `msi_...` workspace invitation link. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/invitations` | List recent workspace invitations without raw tokens. Owner/admin only. |
| `DELETE` | `/api/workspaces/{workspaceID}/invitations/{invitationID}` | Revoke a workspace invitation. Owner/admin only. |
| `POST` | `/api/workspace-invitations/accept` | Accept a workspace invitation as the current authenticated user. |
| `GET` | `/api/workspaces/{workspaceID}/inbox` | List unread issue-event receipts for the authenticated user. |
| `POST` | `/api/workspaces/{workspaceID}/issue-events` | Append a reviewable issue event and create per-user receipts. |
| `POST` | `/api/workspaces/{workspaceID}/issue-events/{eventID}/read` | Mark one event receipt read for the authenticated user. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/read-through` | Mark unread receipts for one issue read through an optional event boundary. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | Create a short-lived worker registration token. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | List worker registration token metadata without raw token values. Owner/admin only. |
| `DELETE` | `/api/workspaces/{workspaceID}/runtime-registration-tokens/{tokenID}` | Revoke a worker registration token. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-workers` | List registered runtime workers and their latest heartbeat state. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks` | Queue a runtime task for the workspace. |
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

## Team Inbox Model

The team Inbox is event-based. `issue_events` stores the append-only review fact, `issue_event_receipts` stores each recipient user's unread/read/archive state, and `issue_watchers` stores the issue-level recipient set. Opening or polling an issue must not clear unread state; clients should call the read-through endpoint after the user intentionally reviews an Inbox row.

## Workspace Invitations

Workspace Settings exposes the first UI-testable collaboration loop. Owners and admins can create one-time `msi_...` invite links, copy the link, inspect pending/accepted/revoked invitations, and revoke unused invitations. A signed-in teammate opens the invite route, accepts it through the UI, and then sees the shared workspace in the workspace switcher. The accepted member can inspect shared members, Inbox receipts, workers, and runtime tasks according to their workspace role.

## Runtime Worker Registry

Runtime registration tokens use the `msw_` prefix and are returned only once. The server stores a hash and prefix, then workers use the token to register, heartbeat, claim eligible tasks, and report task status.

The current queue is intentionally narrow: it records workspace task metadata, required capability JSON, payload/result JSON, claim ownership, timestamps, a compact audit event stream, and worker-appended task logs. Workers can stream Codex app-server status and output back through the log endpoint without the server needing direct network access to the worker environment. Workspace users can request task cancellation; workers poll their claimed task while executing and interrupt Codex app-server when cancellation is requested.

The first worker-side implementation lives in `../worker`. It uses only the server HTTP contract: register with `Authorization: Bearer msw_...`, send heartbeat updates, claim matching tasks, inspect its claimed task for cancellation, append logs, and report status. It completes `protocol_smoke` and `noop` tasks, and it can run `agent_session` tasks by preparing its own repository cache and session worktree from the task payload, then starting `codex app-server --listen stdio://` in that worker-managed workdir. Issue Detail can now route an agent turn to Team worker through the local runner bridge and adopt returned worker source commit metadata, changed files, and diff preview; remaining hardening is stronger artifact transfer, remote credential policy, and Kubernetes-provider parity.
