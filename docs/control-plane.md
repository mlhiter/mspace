# mspace Control Plane

> Status: server-owned runtime and test-module surfaces, updated 2026-06-05

## Decision

mspace uses the server control plane as the single product and runtime state owner for signed-in workspaces. Desktop is a UI shell. Workers execute tasks. Team/shared deployments use Postgres; packaged personal desktop mode may run the same server on local SQLite. Do not add a renderer-owned product store or local execution bridge for collaboration features.

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
- workspace settings, agent profiles, reusable Environments, Kubernetes cluster compatibility records, issue test environments, issue handoffs, review/failure/source records;
- audit and collaboration sync;
- runtime registration tokens;
- runtime worker identity, liveness, and capability snapshots;
- runtime task queue state, claim audit, worker logs, cancellation, and task results.

The desktop app owns:

- native shell behavior;
- local UI state;
- local file pickers;
- opening external auth flows;
- saved Team server URL preference for this device;
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

GitHub remains optional when the environment can reach GitHub. The server reports this as `capabilities.githubAuth` from `/health`, which is `true` only when `MSPACE_GITHUB_CLIENT_ID`, `MSPACE_GITHUB_CLIENT_SECRET`, and `MSPACE_GITHUB_REDIRECT_URI` are all configured. The desktop still treats the default local personal server as local-account-only: it starts on account creation and hides GitHub. GitHub sign-in is shown only for an explicitly configured team server, either a saved Team server URL or `MSPACE_SERVER_URL`, when that configured server advertises `capabilities.githubAuth: true`.

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

Auth responses expose the selected identity as `identity.provider` plus `identity.login`. The desktop workspace menu should use this explicit provider to show local password accounts separately from GitHub accounts, not infer GitHub connection from `user.email` or avatar state.

Future GitHub repository automation should use GitHub App installation tokens stored and rotated by the control plane. Do not build long-lived repository automation on personal GitHub OAuth tokens stored by desktop or workers.

Open registration creates a personal workspace only. Server admin status is a control-plane flag derived from configured auth logins: `MSPACE_SERVER_ADMIN_LOGINS` plus `MSPACE_BOOTSTRAP_ADMIN_LOGIN`. Matching uses the local password login or GitHub login, not email, because password auth does not verify email ownership. Only server admins can create team workspaces; ordinary registered users can join a team workspace only through an owner/admin invitation. Team invitations are one-time join links, not email-bound invitations.

## Implemented API Slice

The server module provides:

- local password auth, GitHub auth, and mspace session endpoints: `/api/auth/password/register`, `/api/auth/password/login`, `/api/auth/github/start`, `/api/auth/github/callback`, `/api/auth/github/result`, `/api/auth/me`;
- workspace listing and creation: `/api/workspaces`;
- team access: members, safe unauthenticated invitation previews, one-time join links, and invite acceptance;
- Inbox events and per-user receipts;
- workspace projects and project runbooks;
- project test cases, test case revisions, test case suggestions, test plans, test runs, and run items;
- issue labels, issues, child tasks, comments, comment edits, and comment reactions;
- workspace settings;
- workspace agent profiles;
- Environment APIs plus Kubernetes cluster compatibility APIs and kubeconfig discovery/import;
- issue test deployment, cleanup, retain, preview probe, and namespace resources;
- issue PR handoff create/refresh records;
- session creation/cancellation/detail derived from server runtime tasks;
- runtime registration tokens, workers, tasks, events, logs, cancellation, worker register/heartbeat/claim/status/log endpoints;
- deterministic fallback issue-title suggestion and worker-backed `issue_type_triage` classification;
- Postgres migrations for the above tables;
- a server-owned SQLite personal store selected by `MSPACE_STORE=sqlite` or by omitting `DATABASE_URL`;
- memory-backed store used only by tests.

Workspaces have an explicit `kind`: `personal` or `team`. The first password registration or GitHub sign-in creates a default personal workspace. Personal and team workspaces both store projects, runbooks, test cases, test case suggestions, test plans, test runs, issues, child tasks, comments, reactions, labels, Inbox receipts, agent profiles, environments, Kubernetes cluster compatibility records, issue test environments, PR handoffs, agent sessions, runtime tasks, worker logs, and runtime results in the server store. Team/customer/shared deployments use Postgres. Packaged personal desktop mode can use the local SQLite store under the Electron user-data path. Team collaboration is opt-in: server admins create team workspaces through `POST /api/workspaces`, and invitation/member APIs reject personal workspaces.

The desktop requires an mspace session before product data is available. For agent mentions, Issue Detail first verifies a matching active Codex worker; personal desktop mode may start the host-local personal worker, while team workspaces require an explicitly registered team worker. Only after that preflight does the renderer write the server comment and call `POST /api/workspaces/{workspaceID}/issues/{issueID}/sessions`. The server repeats the worker-liveness check and returns HTTP `409` with `no active codex worker` if the task cannot be claimed, so unsupported `@codex` comments do not sit in the queue waiting for a worker that is not there.

New issue type classification is also a runtime task. When an issue has `triage_status=pending` and no explicit type label, the server queues `runtime_tasks.kind="issue_type_triage"` with `required_capabilities={"codex":true}` and a classification-only prompt. A matching worker runs Codex, returns a compact JSON result, and the server validates the type before writing the `type:*` label. The server never falls back to keyword matching or an in-process Codex client.

The test module follows the same ownership rule. Canonical cases, revisions, suggestions, plans, runs, setup state, run items, and test evidence artifacts live in server tables. Markdown/text/CSV/Excel imports are parsed by the server. Optimize/generate actions and test run execution create issue-backed agent sessions; workers may return `test-case-proposals.json`, `test-setup-result.json`, or `test-result.json`, but the server validates those artifacts. Human apply actions are required before Codex suggestions change canonical case knowledge, while test run accept/block decisions are lightweight review records until a later release or plan gate consumes them. Plan setup is a lightweight free-text pre-run session, not a template library or separate workflow engine: setup outputs are copied into the run context and only a completed passing setup starts case execution. Screenshot evidence may be transferred through worker artifacts, then the server persists it as `test_artifacts` and rewrites run item evidence to artifact refs.

## Runtime Registry

The registry supports both fixed Server Worker deployments and later Kubernetes Runtime Providers without splitting the issue/session model. Workers are registered executors; Environments are operated targets. A worker may use Kubernetes, SSH, or a future provider-specific access path to operate the selected Environment, but worker identity and environment identity are intentionally not the same record.

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

Worker mode is part of the workspace trust boundary. Personal workspace tokens can register only personal workers and can queue only personal runtime tasks. Team workspace tokens can register only team workers and can queue only team runtime tasks. This keeps open self-registration useful for local personal runners without granting access to shared server runners until the user has been invited into the team workspace.

Desktop personal workers are managed by Electron rather than by a human copying a token. Before a personal agent turn is queued, the desktop creates a 12-hour workspace registration credential, writes it to an Electron user-data token file, starts or reuses a host-local worker in `personal` mode, and schedules renewal before expiry. The worker reads the token file for runtime API calls, so renewal is normally invisible; Electron revokes the previous credential after a short grace period and also revokes the active credential when the personal worker is stopped or the server source changes.

The first worker daemon exists as `worker/`. It registers, heartbeats, claims matching tasks, completes `protocol_smoke` / `noop` tasks, runs `issue_type_triage` tasks from server payloads, and can run `agent_session` tasks by preparing its own repository cache and session worktree from the task payload, then starting `codex app-server --listen stdio://` in that worker-managed workdir. Docker-backed workers keep repository caches and worktrees under `/var/lib/mspace-worker`, backed by a Docker volume, so target project source is isolated from the host checkout.

Workers forward system, status, agent, command, file, and tool logs to `runtime_task_logs`, poll claimed tasks for cancellation, interrupt Codex when requested, capture a source commit when code changed, and return worker workdir, artifact dir, source commit, changed files, and diff preview in the task result.

For UI-only local testing, the Docker dev worker can advertise `codex:true,dryRun:true`; it still uses the same queue and workspace preparation path, but writes a deterministic dry-run source file and returns a dry-run commit instead of launching Codex. Dry-run commits are diagnostic runtime records, not PR source candidates.

Real Codex worker sessions should prefer non-interactive validation and must not present container-local `localhost` or `127.0.0.1` URLs as user-facing previews unless mspace provides an explicit preview/test-environment URL or a known host mapping was requested.

## Test Module, Test Environment, And Handoff

Test cases and Case suggestions are project-scoped and can be typed as `functional`, `ui`, `api`, or `deployment`. Test plans and test runs are workspace-scoped orchestration records; they may include ready cases from multiple projects while preserving each plan case/run item project id. Execution batches must group by project so one agent session never spans multiple repositories. Specialized UI/CDP, API harness, deployment orchestration, multi-worker scheduling, and formal environment templates are later execution layers behind the same runtime task protocol.

Environments are the product target records and are limited to `kubernetes` and `virtual_machine`. Kubernetes environments are currently projected from server `clusters` compatibility records so old cluster APIs and deployment code keep working while the product vocabulary moves forward. Virtual machine environments live in the Environment store with SSH host/user/port, server-owned credential material, working directory, service hint, labels, and readiness from a password/private-key SSH login check during create/update/recheck. Environment responses expose credential configuration state, not raw passwords or private keys. The store must never treat preview URLs as Environments; preview URLs are outputs from deploy/test or run evidence.

Test plans and test runs can select an Environment. The server resolves that selection at create/run time and freezes `environment_id`, `environment_kind`, and `environment_snapshot` so historical plans/runs keep the target context even if the Environment record is edited later. The existing free-text `environment` field remains human-readable notes for the agent, not the structured target.

One issue can have one `issue_test_environments` row. It stores the selected Environment id/kind/snapshot plus Kubernetes compatibility fields such as cluster id, namespace, source commit, registry, and exposure settings. Current issue deploy, cleanup, Resources, and preview probing are Kubernetes-only; if a VM Environment is selected for issue deploy, the server should reject the request until a VM-specific deploy provider exists.

Manual deploy/test remains available from Issue Detail. Workspace owners/admins can also enable `autoDeployTestEnvironment` in Workspace Settings. When enabled, the server watches completed non-dry-run source sessions; if the task produced a source commit, is not itself a deploy/test task, the issue has an attached project, deploy settings can be resolved, no other agent session is active for the issue, and a matching active Codex worker is online, the server queues one automated deploy/test `agent_session` for that exact source commit. Skips are either silent when the issue is not deployable or recorded as a compact system comment when the user needs to reconnect a worker or fix deploy settings.

The Resources tab reads live namespace state through `GET /api/workspaces/{workspaceID}/issues/{issueID}/test-environment/resources`. The server uses Kubernetes client APIs and fixes the namespace from the issue environment record; the frontend must not pass arbitrary namespace input.

PR handoff records live in server `issue_handoffs`. The current implementation records the selected source branch/commit, preview URL, evidence summary, and handoff state. GitHub App-backed PR creation/refresh is still future work, but the record itself is server-owned.

## Migration Rule

New features should land in `server/` first. If a feature involves users, membership, shared issue ownership, GitHub identity, GitHub App installation credentials, audit, runtime state, tests, environments, Kubernetes compatibility records, test environments, handoffs, evidence, or cross-device sync, do not add it to a local sidecar store. Update the server store contract and Postgres migrations; the SQLite personal store should remain a local packaged mode, not a separate product model.
