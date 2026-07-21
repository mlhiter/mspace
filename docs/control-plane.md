# mspace Control Plane

> Status: fixed Agent engines, server-owned runtime, Workflow Skill, and test-module surfaces, updated 2026-07-17

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
- workspace GitHub App installation state;
- workspace projects, project runbooks, issues, child issue tasks, comments, reactions, labels, and Inbox receipts;
- workspace settings, the fixed Agent catalog contract, built-in and workspace custom workflow Skill catalog/revisions/settings, reusable Environments, Kubernetes cluster compatibility records, issue test environments, issue handoffs, review/failure/source records;
- audit and collaboration sync;
- runtime registration tokens;
- runtime worker identity, liveness, capability snapshots, and sanitized Agent-engine diagnostics;
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
- Agent CLI process lifecycle and protocol adaptation;
- command execution and source capture;
- session artifacts while running;
- streaming logs and final task results back to the server.

The control plane intentionally has no Agent runtime dependency. It does not install Codex, Claude Code, or Pi, read their credentials, or start their processes. It queues work with one exact engine capability, records logs/results, and applies validated results. It may own Workflow Skill content and attach pinned bundles to runtime tasks, but Agent authentication/configuration, Skill materialization, CLI protocol handling, and cancellation stay in Worker runtimes.

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

GitHub remains optional when the environment can reach GitHub. The server reports GitHub OAuth sign-in as `capabilities.githubAuth` from `/health`, which is `true` only when `MSPACE_GITHUB_CLIENT_ID`, `MSPACE_GITHUB_CLIENT_SECRET`, and `MSPACE_GITHUB_REDIRECT_URI` are all configured. GitHub App repository automation is separate: `capabilities.githubApp` is `true` only when `MSPACE_GITHUB_APP_ID`, `MSPACE_GITHUB_APP_CLIENT_ID`, and `MSPACE_GITHUB_APP_PRIVATE_KEY` are configured. The desktop still treats the default local personal server as local-account-only: it starts on account creation and hides GitHub. GitHub sign-in is shown only for an explicitly configured team server, either a saved Team server URL or `MSPACE_SERVER_URL`, when that configured server advertises `capabilities.githubAuth: true`.

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

Auth responses expose the selected identity as `identity.provider` plus `identity.login`. Account UI should use this explicit provider to show local password accounts separately from GitHub accounts, not infer GitHub connection from `user.email` or avatar state. Current-user profile editing lives on Account Settings and updates only the canonical display `users.name` and `users.avatar_url` through `PUT /api/auth/me`; it does not rewrite auth login/provider fields or historical issue/comment display snapshots.

Future GitHub repository automation should use GitHub App installation tokens minted and rotated by the control plane from workspace installation state. The current API slice exposes read-only installation status through `GET /api/workspaces/{workspaceID}/github-app`; it does not mint tokens, publish branches, or create PRs. Do not build long-lived repository automation on personal GitHub OAuth tokens stored by desktop or workers.

Open registration creates a personal workspace only. Server admin status is a control-plane flag derived from configured auth logins: `MSPACE_SERVER_ADMIN_LOGINS` plus `MSPACE_BOOTSTRAP_ADMIN_LOGIN`. Matching uses the local password login or GitHub login, not email, because password auth does not verify email ownership. Only server admins can create team workspaces; ordinary registered users can join a team workspace only through an owner/admin invitation. Team invitations are one-time join links, not email-bound invitations.

## Implemented API Slice

The server module provides:

- local password auth, GitHub auth, current-user profile editing, and mspace session endpoints: `/api/auth/password/register`, `/api/auth/password/login`, `/api/auth/github/start`, `/api/auth/github/callback`, `/api/auth/github/result`, `/api/auth/me`;
- workspace listing and creation: `/api/workspaces`;
- team access: members, safe unauthenticated invitation previews, one-time join links, and invite acceptance;
- Inbox events and per-user receipts;
- workspace projects and project runbooks;
- project test cases, test case revisions, test case suggestions, test plans, test runs, and run items;
- issue labels, issues, child tasks, comments, comment edits, and comment reactions;
- workspace settings and workspace GitHub App installation status;
- workflow skill metadata and basic management through `/api/workspaces/{workspaceID}/skills`;
- fixed Agent catalog reads (`codex`, `claude_code`, `pi`); no Agent write APIs;
- Environment APIs plus Kubernetes cluster compatibility APIs and kubeconfig discovery/import;
- issue test deployment, cleanup, retain, preview probe, and namespace resources;
- issue source handoff create/refresh records;
- session creation/cancellation/detail derived from server runtime tasks;
- runtime registration tokens, workers, runtime availability, tasks, events, logs, cancellation, worker register/heartbeat/claim/status/log endpoints;
- deterministic fallback issue-title suggestion and worker-backed `issue_type_triage` title/type refinement;
- Postgres migrations for the above tables;
- a server-owned SQLite personal store selected by `MSPACE_STORE=sqlite` or by omitting `DATABASE_URL`;
- memory-backed store used only by tests.

Workspaces have an explicit `kind`: `personal` or `team`. Personal and team workspaces store projects, runbooks, test knowledge, issues, comments, reactions, labels, Inbox receipts, Skills, Environments, issue test environments, PR handoffs, Agent Sessions, runtime tasks, Worker logs, and runtime results in the server store. Agent definitions are not workspace records: `GET /agents` returns the same code-owned catalog for each authorized workspace, and PostgreSQL migration 030 removes the old `agent_profiles` table. Team/customer/shared deployments use Postgres; packaged personal desktop mode can use local server-owned SQLite.

The desktop requires an mspace session before product data is available. In personal mode, Electron ensures one generic host-local Worker and discovers installed Agent executables without launching them. Issue Detail maps `@codex`, `@claude`, or `@pi` to `codex`, `claudeCode`, or `pi`, checks runtime availability, asks Electron to ensure that capability, then writes the trigger comment and posts a Session with `agentEngine`. Team workspaces require a connected team Worker. The server repeats engine, project, runtime-mode, and liveness checks and returns HTTP `409` with `no active agent worker` when no matching Worker can claim the task. Browser-backed Tests remain Codex Workflows and use a separately named browser companion Worker.

Issue titles are plain text while Issue bodies remain Markdown. The desktop derives a draft title from TipTap's plain-text document projection, marks it with `titleSource="plain_text"`, creates the Issue immediately, and never waits for final refinement; omitting `titleSource` preserves compatibility with older clients whose draft still contains Markdown from the body. After project analysis is queued, the server passes the normalized draft as `expectedTitle` in the existing priority-0 `issue_type_triage` task. Issue list/detail reads remain side-effect free. If a compatibility task is already active without `expectedTitle`, the create path upgrades it atomically instead of dropping the title request or starting a duplicate turn. Updated workers return a rewritten title with the type result, while older title-less results remain valid for rolling upgrades. The server projects the untrusted title through CommonMark/GFM to at most 72 plain-text characters, and Memory/Postgres update only when the stored title still equals `expectedTitle`. Human edits win, body storage is unchanged, and the deterministic `suggest-title` route remains only a compatibility fallback.

New issue type classification is also a runtime task. When an issue has `triage_status=pending` and no explicit type label, the server queues `runtime_tasks.kind="issue_type_triage"` with `required_capabilities={"codex":true}` and a combined title/type prompt. Active-task reuse and creation are serialized by the Memory/SQLite mutex or an issue-scoped Postgres advisory transaction. A matching updated worker returns a compact JSON result with `title`, `type`, `confidence`, and `reason`; older workers may omit `title`. The server conditionally applies the title independently from validating and writing the `type:*` label, so a manual title or type edit wins its own field without suppressing the other reconciliation. Postgres manual label writes and worker reconciliation both lock `issues` before `issue_labels`, avoiding a lock-order inversion. The server never falls back to keyword matching or an in-process Codex client.

Project-backed issue creation can also queue one read-only `runtime_tasks.kind="agent_session"` with payload `automation:"issue_analysis"` when a matching Codex worker is already online. Attaching a project to a previously projectless top-level issue runs the same opportunistic queueing path. That task is queued before asynchronous type triage when created with a project, uses `sandbox:"read-only"` plus `sourceCapture:false`, includes the built-in server-owned `think` skill bundle from `mlhiter/skills`, asks the worker to materialize it under the session artifact directory, and produces first-pass analysis without requiring an immediate manual `@codex` mention. Missing project, missing worker, child issue, or existing analysis task are recoverable skips and must not fail issue creation or project attachment. Server reconciliation ignores source/test/deploy/review artifacts from `issue_analysis`.

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
  -> reports name, mode, version, capabilities, labels, load, and optional Agent diagnostics
  -> sends heartbeats while online
Server
  -> validates and stores worker liveness, capability, and diagnostic snapshots
  -> lets diagnostics downgrade advertised engine capabilities, never upgrade them
  -> lets online workers claim queued tasks that match mode and capabilities
```

Registration tokens are workspace-scoped bootstrap secrets for worker daemons. The raw token is returned only once when created; the server stores only its hash and prefix.

The public runtime-task debug endpoint is owner/admin-only and accepts only unbound `protocol_smoke` and `noop`. It rejects raw Agent, Skill, automation, Issue/Session/Project, repository, workdir, environment, and other server-owned control fields case-insensitively. User Agent Sessions must enter through the Issue Session route, which owns engine selection, Issue/Project links, automation markers, environment, Skills, and artifact contracts. Helm Workers receive only their runtime-token Secret key, and Agent subprocesses strip control-plane and Worker-registration variables before launch.

Worker mode is part of the workspace trust boundary. Personal workspace tokens can register only personal workers and can queue only personal runtime tasks. Team workspace tokens can register only team workers and can queue only team runtime tasks. This keeps open self-registration useful for local personal runners without granting access to shared server runners until the user has been invited into the team workspace.

Desktop personal Workers are managed by Electron rather than by a human copying a token. Once a personal workspace is selected, the desktop creates a short-lived workspace registration credential, starts or reuses one generic Worker in `personal` mode, and advertises discovered engines as an execution allowlist. Electron persists an anonymous stable host id, includes its short suffix in Worker names, and labels Workers as `primary` or `browser_companion`, so multiple Macs cannot upsert the same row and the renderer can identify This Mac through trusted IPC. Worker diagnostics may then downgrade engines that are missing, unauthenticated, or fail their safe probe. Browser-required Codex Workflows use the companion Worker against the same credential file and an isolated repo/workdir root.

`runtime_workers.agent_engine_diagnostics` stores only fixed fields: `status`, `reasonCode`, sanitized `version`, and `checkedAt`. Migration 031 adds this JSONB column for Postgres; the SQLite personal store carries the same snapshot field. The server permits only the fixed engine/status/reason enums, valid timestamps, and path-free version text; Pi model reasons are retained only as `pi + unverified + model_available` or `pi + needs_setup + model_unavailable`. Omitted heartbeat diagnostics preserve the locked stored value, while explicit `{}` clears it. `GET /runtime/availability` returns `claimableWorkerCount`, derived by the server with the same workspace mode, status, heartbeat TTL, load, and capability rules used for claiming. Older Workers remain compatible when they omit diagnostics; their historical capability can still route tasks, but clients must show the diagnostic as not reported rather than ready.

The Worker daemon in `worker/` registers, heartbeats, and claims tasks by exact capability. Shared Worker Core prepares repository caches/worktrees, materializes pinned Skills under the artifact directory, races required Tests artifacts against engine completion, captures source, and assembles generic results. The Codex adapter uses app-server stdio; Claude Code uses print-mode stream JSON and requires a terminal `result`; Pi uses official RPC, sends `abort` on cancellation, and requires `agent_end`. Missing `agentEngine` maps to Codex only for historical payloads; explicit unknown values fail closed. Docker-backed workers keep repository caches and worktrees under `/var/lib/mspace-worker`.

Workers forward normalized logs to `runtime_task_logs`, poll claimed tasks for cancellation, interrupt the selected engine, capture source when code changed, and return `agentEngine`, opaque `engineSessionRef`/`engineRunRef`, workdir, artifacts, source commit, changed files, and diff preview. Codex results retain `threadId`/`turnId` compatibility; Pi session file paths never leave the Worker.

For UI-only local testing, the Docker dev worker can advertise `codex:true,dryRun:true`; it still uses the same queue and workspace preparation path, but writes a deterministic dry-run source file and returns a dry-run commit instead of launching Codex. Dry-run commits are diagnostic runtime records, not PR source candidates.

Real Codex worker sessions should prefer non-interactive validation and must not present container-local `localhost` or `127.0.0.1` URLs as user-facing previews unless mspace provides an explicit preview/test-environment URL or a known host mapping was requested.

## Test Module, Test Environment, And Handoff

Test cases and Case suggestions are project-scoped and can be typed as `functional`, `ui`, `api`, or `deployment`. Test plans and test runs are workspace-scoped orchestration records; they may include ready cases from multiple projects while preserving each plan case/run item project id. Execution batches must group by project so one agent session never spans multiple repositories. Specialized UI/CDP, API harness, deployment orchestration, multi-worker scheduling, and formal environment templates are later execution layers behind the same runtime task protocol.

Environments are the product target records and are limited to `kubernetes` and `virtual_machine`. Kubernetes environments are currently projected from server `clusters` compatibility records so old cluster APIs and deployment code keep working while the product vocabulary moves forward. Virtual machine environments live in the Environment store with SSH host/user/port, server-owned credential material, working directory, service hint, labels, and readiness from a password/private-key SSH login check during create/update/recheck. Environment responses expose credential configuration state, not raw passwords or private keys. The store must never treat preview URLs as Environments; preview URLs are outputs from deploy/test or run evidence.

Test plans and test runs can select an Environment. The server resolves that selection at create/run time and freezes `environment_id`, `environment_kind`, and `environment_snapshot` so historical plans/runs keep the target context even if the Environment record is edited later. The existing free-text `environment` field remains human-readable notes for the agent, not the structured target.

One issue can have one `issue_test_environments` row. It stores the selected Environment id/kind/snapshot plus Kubernetes compatibility fields such as cluster id, namespace, source commit, registry, and exposure settings. Current issue deploy, cleanup, Resources, and preview probing are Kubernetes-only; if a VM Environment is selected for issue deploy, the server should reject the request until a VM-specific deploy provider exists.

Manual deploy/test remains available from Issue Detail. Workspace owners/admins can also enable `autoDeployTestEnvironment` in Workspace Settings. When enabled, the server watches completed non-dry-run source sessions; if the task produced a source commit, is not itself a deploy/test task, the issue has an attached project, deploy settings can be resolved, no other agent session is active for the issue, and a matching active Codex worker is online, the server queues one automated deploy/test `agent_session` for that exact source commit. Skips are either silent when the issue is not deployable or recorded as a compact system comment when the user needs to reconnect a worker or fix deploy settings.

The Resources tab reads live namespace state through `GET /api/workspaces/{workspaceID}/issues/{issueID}/test-environment/resources`. The server uses Kubernetes client APIs and fixes the namespace from the issue environment record; the frontend must not pass arbitrary namespace input.

PR handoff records live in server `issue_handoffs`. The current implementation records the selected source branch/commit, preview URL, evidence summary, and handoff state. Workspace GitHub App installation metadata lives in `workspace_github_app_installations` and is exposed as read-only status for Workspace Settings. GitHub App-backed token minting, branch publishing, and PR creation/refresh are still future work, but the records themselves are server-owned.

## Migration Rule

New features should land in `server/` first. If a feature involves users, membership, shared issue ownership, GitHub identity, GitHub App installation credentials, audit, runtime state, tests, environments, Kubernetes compatibility records, test environments, handoffs, evidence, or cross-device sync, do not add it to a local sidecar store. Update the server store contract and Postgres migrations; the SQLite personal store should remain a local packaged mode, not a separate product model.
