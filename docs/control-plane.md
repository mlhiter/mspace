# mspace Control Plane

> Status: server-owned runtime surfaces, updated 2026-05-20

## Decision

mspace uses the server control plane as the single product and runtime state owner for signed-in workspaces. Desktop is a UI shell. Workers execute tasks. Do not add a local product store or local execution bridge for collaboration features.

## Ownership

The control plane owns:

- users;
- local password credentials;
- workspaces;
- workspace members and roles;
- mspace auth sessions;
- GitHub identity links;
- future GitHub App installation state;
- workspace projects, project runbooks, issues, child issue tasks, comments, reactions, labels, and Inbox receipts;
- workspace settings, agent profiles, reusable clusters, issue test environments, issue handoffs, review/failure/source records;
- audit and collaboration sync;
- runtime registration tokens;
- runtime worker identity, liveness, and capability snapshots;
- runtime task queue state, claim audit, worker logs, cancellation, and task results.

The desktop app owns:

- native shell behavior;
- local UI state;
- local file pickers;
- opening external auth flows;
- presentation of server state.

Runtime workers own:

- repository cache;
- per-session workdir preparation;
- Codex app-server process lifecycle;
- command execution and source capture;
- session artifacts while running;
- streaming logs and final task results back to the server.

The control plane intentionally has no Codex runtime dependency. It does not install the Codex CLI, mount `CODEX_HOME`, read Codex credentials, or start `codex app-server`. It queues work, records logs/results, and applies validated results. Codex auth/config is injected only into worker runtimes.

## Auth Shape

Local password auth and GitHub OAuth are identity providers, not the product session authority. Both routes end by issuing an `msp_...` mspace auth session.

Password auth is the default path for restricted or offline environments:

```text
Desktop
  -> POST /api/auth/password/register or /api/auth/password/login
mspace server
  -> user_password_credentials(login, password_hash)
  -> user_identities(provider=password)
  -> mspace auth session token
Desktop
  -> store msp_... session token
  -> call mspace APIs with Authorization: Bearer msp_...
```

GitHub remains optional when the environment can reach GitHub:

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

The server may use a GitHub OAuth client secret because it is a trusted backend environment. The desktop app must not embed GitHub client secrets. Password hashes are server-side only and stored separately from `user_identities`. Local password registration does not verify email ownership, so the user row keeps its canonical email blank and stores any provided email only on the password identity record; password auth must not merge into an OAuth identity by matching email.

Future GitHub repository automation should use GitHub App installation tokens stored and rotated by the control plane. Do not build long-lived repository automation on personal GitHub OAuth tokens stored by desktop or workers.

## Implemented API Slice

The server module provides:

- local password auth, GitHub auth, and mspace session endpoints: `/api/auth/password/register`, `/api/auth/password/login`, `/api/auth/github/start`, `/api/auth/github/callback`, `/api/auth/github/result`, `/api/auth/me`;
- workspace listing and creation: `/api/workspaces`;
- team access: members, invitations, and invite acceptance;
- Inbox events and per-user receipts;
- workspace projects and project runbooks;
- issue labels, issues, child tasks, comments, comment edits, and comment reactions;
- workspace settings;
- workspace agent profiles;
- cluster configs and kubeconfig discovery/import;
- issue test deployment, cleanup, retain, preview probe, and namespace resources;
- issue PR handoff create/refresh records;
- session creation/cancellation/detail derived from server runtime tasks;
- runtime registration tokens, workers, tasks, events, logs, cancellation, worker register/heartbeat/claim/status/log endpoints;
- deterministic fallback issue-title suggestion and worker-backed `issue_type_triage` classification;
- Postgres migrations for the above tables;
- memory-backed store used only by tests.

Workspaces have an explicit `kind`: `personal` or `team`. The first password registration or GitHub sign-in creates a default personal workspace. Personal and team workspaces both store projects, runbooks, issues, child tasks, comments, reactions, labels, Inbox receipts, agent profiles, clusters, issue test environments, PR handoffs, agent sessions, runtime tasks, worker logs, and runtime results in server Postgres. Team collaboration is opt-in: `POST /api/workspaces` creates team workspaces, and invitation/member APIs reject personal workspaces.

The desktop requires an mspace session before product data is available. Issue Detail writes the server comment, then calls `POST /api/workspaces/{workspaceID}/issues/{issueID}/sessions`; the server queues an `agent_session` runtime task and later exposes worker logs/results directly from `runtime_task_logs` and `runtime_tasks.result`.

New issue type classification is also a runtime task. When an issue has `triage_status=pending` and no explicit type label, the server queues `runtime_tasks.kind="issue_type_triage"` with `required_capabilities={"codex":true}` and a classification-only prompt. A matching worker runs Codex, returns a compact JSON result, and the server validates the type before writing the `type:*` label. The server never falls back to keyword matching or an in-process Codex client.

## Runtime Registry

The registry supports both fixed Server Worker deployments and later Kubernetes Runtime Providers without splitting the issue/session model.

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

Registration tokens are workspace-scoped bootstrap secrets for worker daemons. The raw token is returned only once when created; the server stores only its hash and prefix.

The first worker daemon exists as `worker/`. It registers, heartbeats, claims matching tasks, completes `protocol_smoke` / `noop` tasks, runs `issue_type_triage` tasks from server payloads, and can run `agent_session` tasks by preparing its own repository cache and session worktree from the task payload, then starting `codex app-server --listen stdio://` in that worker-managed workdir. Docker-backed workers keep repository caches and worktrees under `/var/lib/mspace-worker`, backed by a Docker volume, so target project source is isolated from the host checkout.

Workers forward system, status, agent, command, file, and tool logs to `runtime_task_logs`, poll claimed tasks for cancellation, interrupt Codex when requested, capture a source commit when code changed, and return worker workdir, artifact dir, source commit, changed files, and diff preview in the task result.

For UI-only local testing, the Docker dev worker can advertise `codex:true,dryRun:true`; it still uses the same queue and workspace preparation path, but writes a deterministic dry-run source file and returns a dry-run commit instead of launching Codex. Dry-run commits are diagnostic runtime records, not PR source candidates.

Real Codex worker sessions should prefer non-interactive validation and must not present container-local `localhost` or `127.0.0.1` URLs as user-facing previews unless mspace provides an explicit preview/test-environment URL or a known host mapping was requested.

## Test Environment And Handoff

Cluster configs live in server `clusters`. One issue can have one `issue_test_environments` row. Test deployment is queued through the same agent-session/runtime-task protocol as normal coding turns, with resolved cluster, namespace, source commit, registry, and exposure settings in the payload.

The Resources tab reads live namespace state through `GET /api/workspaces/{workspaceID}/issues/{issueID}/test-environment/resources`. The server uses Kubernetes client APIs and fixes the namespace from the issue environment record; the frontend must not pass arbitrary namespace input.

PR handoff records live in server `issue_handoffs`. The current implementation records the selected source branch/commit, preview URL, evidence summary, and handoff state. GitHub App-backed PR creation/refresh is still future work, but the record itself is server-owned.

## Migration Rule

New features should land in `server/` first. If a feature involves users, membership, shared issue ownership, GitHub identity, GitHub App installation credentials, audit, runtime state, clusters, test environments, handoffs, evidence, or cross-device sync, do not add it to a local sidecar store.
