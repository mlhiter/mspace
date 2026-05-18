# mspace Control Plane

> Status: architecture direction, runtime registry, and server-owned worker sessions, updated 2026-05-18

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
- workspace projects, project runbooks, issues, child issue tasks, comments, reactions, labels, and Inbox receipts;
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
- legacy/local attachment blobs and runner-owned runtime rows during the transition;
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
- `POST /api/workspaces`;
- `GET /api/workspaces/{workspaceID}/inbox`;
- `POST /api/workspaces/{workspaceID}/issue-events`;
- `POST /api/workspaces/{workspaceID}/issue-events/{eventID}/read`;
- `POST /api/workspaces/{workspaceID}/issues/{issueID}/read-through`;
- `GET /api/workspaces/{workspaceID}/projects`;
- `POST /api/workspaces/{workspaceID}/projects`;
- `PUT /api/workspaces/{workspaceID}/projects/{projectID}`;
- `DELETE /api/workspaces/{workspaceID}/projects/{projectID}`;
- `GET /api/workspaces/{workspaceID}/projects/{projectID}/runbook`;
- `PUT /api/workspaces/{workspaceID}/projects/{projectID}/runbook`;
- `GET /api/workspaces/{workspaceID}/issue-label-definitions`;
- `GET /api/workspaces/{workspaceID}/issues`;
- `POST /api/workspaces/{workspaceID}/issues`;
- `GET /api/workspaces/{workspaceID}/issues/{issueID}`;
- `PUT /api/workspaces/{workspaceID}/issues/{issueID}`;
- `POST /api/workspaces/{workspaceID}/issues/{issueID}/tasks`;
- `DELETE /api/workspaces/{workspaceID}/issues/{issueID}/tasks/{taskID}`;
- `PUT /api/workspaces/{workspaceID}/issues/{issueID}/labels`;
- `POST /api/workspaces/{workspaceID}/issues/{issueID}/comments`;
- `PUT /api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}`;
- `PUT /api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}`;
- `DELETE /api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}`;
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
- Postgres migrations for `users`, `user_identities`, `workspaces`, `workspace_members`, `workspace_invitations`, `oauth_states`, `oauth_results`, `auth_sessions`, `issue_events`, `issue_event_receipts`, `issue_watchers`, `projects`, `project_runbooks`, `project_runbook_revisions`, `issues`, `comments`, `comment_reactions`, `issue_label_definitions`, `issue_labels`, `runtime_registration_tokens`, `runtime_workers`, `runtime_tasks`, `runtime_task_events`, and `runtime_task_logs`;
- mspace session tokens with `msp_` prefix;
- workspace invitation tokens with `msi_` prefix, returned only at creation time;
- runtime registration tokens with `msw_` prefix, returned only at creation time;
- a memory-backed store used only by tests.

Workspaces have an explicit `kind`: `personal` or `team`. The first GitHub sign-in creates a default personal workspace. Personal and team workspaces both store projects, runbooks, issues, child tasks, comments, reactions, labels, Inbox receipts, agent sessions, runtime tasks, worker logs, and runtime results in server Postgres so workspace switching has one durable product/runtime boundary. Team collaboration is opt-in: `POST /api/workspaces` creates team workspaces, and invitation/member APIs reject personal workspaces; runtime worker registration and task APIs are shared by personal and team workspaces.

The desktop now requires GitHub sign-in before product data is available. Projects, Issues, Issue Detail comments/tasks/labels, Inbox, Project runbooks, normal agent sessions, runtime task logs, and runtime results use the server APIs for the selected signed-in workspace. Issue Detail writes the PG comment, then calls `POST /api/workspaces/{workspaceID}/issues/{issueID}/sessions`; the server queues an `agent_session` runtime task and later exposes worker logs/results directly from `runtime_task_logs` and `runtime_tasks.result`. Attachment upload/storage, PR handoff, review evidence, clusters, and issue test environments still use the runner path. After sign-in, the renderer sends the current token and workspace id to the local runner only for those remaining local-runner facilities.

The left workspace menu is the primary UI surface for switching workspaces and creating an explicit team workspace. Workspace Settings stays scoped to the selected workspace: in team workspaces it lets an owner/admin invite teammates and inspect workspace members, while both personal and team workspaces can create and revoke worker registration tokens, inspect registered worker heartbeat/capability state, and inspect or queue protocol-level runtime tasks. The Agents route remains focused on Codex-backed profile behavior, not worker infrastructure.

Workspace invitations are deliberately narrow. They are one-time `msi_...` links scoped to one team workspace and one role (`member` or `admin`). The raw token is shown once; the server stores a hash and prefix. Accepting an invite requires an authenticated mspace user and adds that user to `workspace_members`, then the desktop workspace switcher can select the shared workspace and see only that workspace's server-backed projects and issues.

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

- Personal mode uses a worker registered to the user's personal workspace; in local development this can be a Docker or host worker that reuses the user's prepared credentials.
- Team mode can register a separately deployed Server Worker process for non-Kubernetes teams.
- The same task protocol can later register a Kubernetes Runtime Provider that starts an isolated Pod or Job per agent session.
- Team workers can pull queued task records without the server needing direct network access into the worker environment.
- Workers can append task logs while the task is claimed/running, so Codex app-server notifications can be streamed back through the server instead of direct server-to-worker networking.
- Workspace users can request cancellation for queued, claimed, or running tasks; workers poll their claimed task while executing and interrupt Codex app-server when cancellation is requested.
- All runtime backends should report through the same runtime registry, task, log, cancellation, session/evidence, and PR handoff protocol.
- Kubernetes Job execution remains explicitly deferred as a backend implementation, not as a separate product model.

The first worker daemon exists as `worker/`. It registers, heartbeats, claims matching tasks, completes `protocol_smoke` / `noop` tasks, and can run `agent_session` tasks by preparing its own repository cache and session worktree from the task payload, then starting `codex app-server --listen stdio://` in that worker-managed workdir. Docker-backed workers keep those repository caches and worktrees under `/var/lib/mspace-worker`, backed by a Docker volume, so the target project source is isolated from the host checkout. The worker forwards system, status, agent, command, file, and tool logs to `runtime_task_logs`, polls its claimed task for cancellation, interrupts Codex when requested, captures a source commit when code changed, and returns worker workdir, artifact dir, source commit, changed files, and diff preview in the task result.

For UI-only local testing, the Docker dev worker can advertise `codex:true,dryRun:true`; it still uses the same queue and workspace preparation path, but writes a deterministic dry-run source file and returns a dry-run commit instead of launching Codex. Dry-run commits are diagnostic runtime records, not PR source candidates. Real Codex worker sessions should prefer non-interactive validation and must not present container-local `localhost` or `127.0.0.1` URLs as user-facing previews unless mspace provides an explicit preview/test-environment URL or a known host mapping was requested.

Issue Detail now routes agent mentions on server-owned issues directly to the control plane. It writes the server comment, calls `POST /api/workspaces/{workspaceID}/issues/{issueID}/sessions`, and the server queues a runtime `agent_session` task with repository/session metadata. Workers append logs to `runtime_task_logs`, report Codex thread/turn ids and source metadata in `runtime_tasks.result`, and the server derives session detail plus issue change nodes from those server records. The runner no longer mirrors worker logs/results as source of truth. The remaining integration work is remote credential policy, lower-latency cancellation guarantees, server-owned attachments, and a Kubernetes provider that preserves this same server-side task contract.

## Migration Rule

New multiplayer features should land in `server/` first. If a feature involves users, membership, shared issue ownership, GitHub identity, GitHub App installation credentials, audit, or cross-device sync, do not add it only to the local SQLite runner schema.
