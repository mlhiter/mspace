# mspace Control Plane

> Status: architecture direction, runtime registry, and first task-queue skeleton, updated 2026-05-12

## Decision

mspace should split out a real server/control-plane for multiplayer collaboration.

The desktop app and local runner should not own collaboration identity. They should become runtime clients that authenticate to the control plane with mspace-issued tokens.

## Ownership

The control plane owns:

- users;
- workspaces;
- workspace members and roles;
- mspace auth sessions;
- GitHub identity links;
- future GitHub App installation state;
- audit and collaboration sync;
- runtime registration tokens;
- runtime worker identity, liveness, and capability snapshots;
- runtime task queue state and claim audit.

The desktop app owns:

- native shell behavior;
- local UI state;
- local file pickers;
- opening external auth flows;
- local runtime UX.

The runner owns:

- local repo checkout and worktree execution;
- Codex app-server process lifecycle;
- local logs and artifacts while running;
- Kubernetes deploy/test execution until this moves into runtime workers.

## Auth Shape

GitHub is an identity provider, not the product session authority.

```text
Desktop
  -> GET /api/auth/github/start
  -> open GitHub authorizeUrl in the browser
Browser
  -> GitHub OAuth
  -> GET /api/auth/github/callback
mspace server
  -> validate state
  -> exchange code with server-side client secret
  -> user_identities(provider=github)
  -> mspace auth session token
Desktop
  -> poll GET /api/auth/github/result?state=...
  -> store msp_... session token
  -> call mspace APIs with Authorization: Bearer msp_...
```

The server may use a GitHub OAuth client secret because it is a trusted backend environment. The desktop app must not embed GitHub client secrets.

The callback endpoint returns a small success page, not raw auth JSON. The desktop app receives the `msp_...` token through the state-bound result polling endpoint. Poll results are single-use and short-lived.

Future GitHub repository automation should use GitHub App installation tokens stored and rotated by the control plane. Do not build long-lived repository automation on personal GitHub OAuth tokens stored by the desktop or runner.

## First Implementation Slice

The initial `server/` module provides:

- `GET /health`;
- `GET /api/auth/github/start`;
- `GET /api/auth/github/callback`;
- `GET /api/auth/github/result`;
- `GET /api/auth/me`;
- `GET /api/workspaces`;
- `GET /api/workspaces/{workspaceID}/inbox`;
- `POST /api/workspaces/{workspaceID}/issue-events`;
- `POST /api/workspaces/{workspaceID}/issue-events/{eventID}/read`;
- `POST /api/workspaces/{workspaceID}/issues/{issueID}/read-through`;
- `GET /api/workspaces/{workspaceID}/members`;
- `POST /api/workspaces/{workspaceID}/invitations`;
- `GET /api/workspaces/{workspaceID}/invitations`;
- `DELETE /api/workspaces/{workspaceID}/invitations/{invitationID}`;
- `POST /api/workspace-invitations/accept`;
- `POST /api/workspaces/{workspaceID}/runtime-registration-tokens`;
- `GET /api/workspaces/{workspaceID}/runtime-registration-tokens`;
- `DELETE /api/workspaces/{workspaceID}/runtime-registration-tokens/{tokenID}`;
- `GET /api/workspaces/{workspaceID}/runtime-workers`;
- `POST /api/workspaces/{workspaceID}/runtime-tasks`;
- `GET /api/workspaces/{workspaceID}/runtime-tasks`;
- `GET /api/workspaces/{workspaceID}/runtime-tasks/{taskID}/events`;
- `GET /api/workspaces/{workspaceID}/runtime-tasks/{taskID}/logs`;
- `POST /api/workspaces/{workspaceID}/runtime-tasks/{taskID}/cancel`;
- `POST /api/runtime/workers/register`;
- `POST /api/runtime/workers/{workerID}/heartbeat`;
- `POST /api/runtime/workers/{workerID}/tasks/claim`;
- `GET /api/runtime/workers/{workerID}/tasks/{taskID}`;
- `POST /api/runtime/workers/{workerID}/tasks/{taskID}/status`;
- `POST /api/runtime/workers/{workerID}/tasks/{taskID}/logs`;
- Postgres migrations for `users`, `user_identities`, `workspaces`, `workspace_members`, `workspace_invitations`, `oauth_states`, `oauth_results`, `auth_sessions`, `issue_events`, `issue_event_receipts`, `issue_watchers`, `runtime_registration_tokens`, `runtime_workers`, `runtime_tasks`, `runtime_task_events`, and `runtime_task_logs`;
- mspace session tokens with `msp_` prefix;
- workspace invitation tokens with `msi_` prefix, returned only at creation time;
- runtime registration tokens with `msw_` prefix, returned only at creation time;
- a memory-backed store used only by tests.

The desktop now has a lightweight GitHub sign-in entrypoint in the sidebar. Product issue/session data still talks to the local runner for the local MVP, but Inbox read state now has a server-backed team model. After sign-in, the renderer sends the current token and workspace id to the local runner so reviewable issue events can be reported to the control plane.

The Workspace Settings page is the first UI surface for this server-side team state. It lets an owner/admin invite teammates, inspect workspace members, create and revoke worker registration tokens, inspect registered worker heartbeat/capability state, and inspect or queue protocol-level runtime tasks. The Agents route remains focused on Codex-backed profile behavior, not worker infrastructure.

Workspace invitations are deliberately narrow. They are one-time `msi_...` links scoped to one workspace and one role (`member` or `admin`). The raw token is shown once; the server stores a hash and prefix. Accepting an invite requires an authenticated mspace user and adds that user to `workspace_members`, then the desktop workspace switcher can select the shared workspace for UI-only testing.

## Runtime Registry

Team mode starts with a server-owned registry. The registry must support both fixed Server Worker deployments and later Kubernetes Runtime Providers without splitting the issue/session model.

```text
Workspace owner/admin
  -> creates runtime registration token
  -> can list token prefixes/expiry/last-use state
  -> can revoke leaked or stale tokens
Runtime provider
  -> registers with Authorization: Bearer msw_...
  -> reports name, mode, version, capabilities, labels, and load
  -> sends heartbeats while online
Server
  -> stores worker liveness and capability snapshot
  -> lets online workers claim queued tasks that match mode and capabilities
```

Registration tokens are not user sessions. They are workspace-scoped bootstrap secrets for worker daemons and should be short-lived. The token is returned only once when created; the server stores only its hash and prefix.

The registry and queue slice establishes the control-plane boundary needed for personal and team runtime modes:

- Personal mode can continue to use the desktop-started local runner.
- Team mode can register a separately deployed Server Worker process for non-Kubernetes teams.
- The same task protocol can later register a Kubernetes Runtime Provider that starts an isolated Pod or Job per agent session.
- Team workers can pull queued task records without the server needing direct network access into the worker environment.
- Workers can append task logs while the task is claimed/running, so Codex app-server notifications can be streamed back through the server instead of direct server-to-worker networking.
- Workspace users can request cancellation for queued, claimed, or running tasks; workers poll their claimed task while executing and interrupt Codex app-server when cancellation is requested.
- All runtime backends should report through the same runtime registry, task, log, cancellation, session/evidence, and PR handoff protocol.
- Kubernetes Job execution remains explicitly deferred as a backend implementation, not as a separate product model.

The first worker daemon exists as `worker/`. It registers, heartbeats, claims matching tasks, completes `protocol_smoke` / `noop` tasks, and can run `agent_session` tasks by preparing its own repository cache and session worktree from the task payload, then starting `codex app-server --listen stdio://` in that worker-managed workdir. The worker forwards system, status, agent, command, file, and tool logs to `runtime_task_logs`, polls its claimed task for cancellation, interrupts Codex when requested, captures a source commit when code changed, and returns worker workdir, artifact dir, source commit, changed files, and diff preview in the task result. For UI-only local testing, the Docker dev worker can advertise `codex:true,dryRun:true`; it still uses the same queue and workspace preparation path, but writes a deterministic dry-run source file and returns a dry-run commit instead of launching Codex.

The local runner now has the first issue/session bridge. Issue Detail can route an agent mention to Team worker, the runner writes session context, queues a server-side `agent_session` task with repository/session metadata, imports worker logs back into local `session_logs`, records returned Codex thread/turn ids, and adopts returned source commit metadata into issue change nodes. The remaining integration work is stronger artifact transfer, remote credential policy, lower-latency cancellation guarantees, and a Kubernetes provider that preserves this same server-side task contract.

## Migration Rule

New multiplayer features should land in `server/` first. If a feature involves users, membership, shared issue ownership, GitHub identity, GitHub App installation credentials, audit, or cross-device sync, do not add it only to the local SQLite runner schema.
