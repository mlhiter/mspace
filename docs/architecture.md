# mspace Architecture Notes

> Status: server-owned runtime and test-module surfaces, updated 2026-06-05

## Current Implementation Snapshot

The repository contains a runnable desktop MVP with server-owned product and runtime state:

- Electron desktop shell built with electron-vite, React 19, TanStack Router, React Query 5, Tailwind CSS 4, TypeScript, pnpm workspaces, and Turbo.
- Public website in `apps/website`, built with Vite, React 19, Tailwind CSS 4, and lucide-react, deployed as a static Vercel site from the root `vercel.json`.
- Shared UI layer built on shadcn/ui source components, Radix UI primitives, lucide-react icons, Material Icon Theme file icons, and the `cn()` helper in `packages/ui/src/lib/utils.ts`.
- Shared desktop localization lives in `packages/i18n` for `en` and `zh-CN`.
- Go server control plane in `server/`, built with chi. Team/shared deployments use PostgreSQL through `pgx`; packaged personal desktop mode can use a server-owned SQLite snapshot store. Project-level test cases and case suggestions plus workspace-level plans, plan setup steps, and runs live in this same server store.
- Go runtime worker in `worker/`, registered to workspaces with `msw_...` tokens.
- Local username/password auth creates a personal workspace by default and works without GitHub access. Default local personal sign-in opens on account creation and hides GitHub. GitHub sign-in remains an optional external identity provider for explicitly configured team servers, from a saved Team server URL or `MSPACE_SERVER_URL`, when `/health` reports `capabilities.githubAuth: true`. Auth responses include `identity.provider` and `identity.login`, so the desktop can show local accounts and GitHub accounts without guessing from `user.email`. Personal and team workspaces both use the server store; team/shared deployments use Postgres, while packaged personal desktop mode can stay local on SQLite.
- Team collaboration features require an explicit team workspace created from the workspace menu. Team access is through one-time join links rather than email-targeted invitations, with a safe unauthenticated preview for signed-out recipients and desktop deep links that carry the invited team server context.
- The desktop uses `MSPACE_SERVER_URL`, then a saved Team server URL, then the local bundled/dev server when needed. It renders server state and does not own product truth or runtime persistence.

The control plane owns:

- users, local password credentials, workspaces, membership, GitHub identity, and mspace auth sessions;
- projects, project runbooks, project test cases, test case revisions, test case proposals, workspace test plans, workspace test runs, issues, child issue tasks, comments, reactions, labels, Inbox events, and per-user receipts;
- workspace settings, agent profiles, environments, Kubernetes cluster compatibility records, issue test environments, issue handoffs, review evidence, failures, and source change records;
- runtime worker registration, task queue state, task logs, task events, cancellation, and task results;
- future GitHub App installation state.

Workers own:

- repository cache and per-session workdir preparation;
- Codex app-server lifecycle;
- command execution and source capture;
- session artifacts while running;
- streaming logs and final results back to the server.

The server does not own any Codex process or credential lifecycle. It queues tasks that require `{"codex":true}`, records task events/logs/results, and reconciles final output back into product state. The Codex CLI, `CODEX_HOME`, `auth.json`, and `config.toml` belong to worker runtimes only.

## Runtime Flow

```text
Desktop Issue Detail
  -> verify attached project and matching active Codex worker
  -> POST server comment
  -> POST server agent session
Server
  -> validate workspace membership and project attachment
  -> snapshot issue/project/runbook context
  -> create agent_sessions row
  -> enqueue runtime_tasks(kind=agent_session)
Worker
  -> claim matching task
  -> prepare repo cache and session workdir
  -> start codex app-server --listen stdio://
  -> stream logs to runtime_task_logs
  -> capture source metadata and artifacts
  -> complete runtime task with result JSON
Server
  -> derives Session Detail and Issue change nodes from task records
Desktop
  -> refreshes Issue Detail, Commits, Sessions, Resources, and Evidence
```

Personal and team workspaces use the same server API and runtime task protocol. `runtimeMode` controls which workers can claim the task, not which product model is used.

In desktop personal mode, Electron manages the local worker bootstrap credential lifecycle. It creates a short-lived personal `msw_...` credential, writes it to an Electron user-data token file, starts a host-local worker with `MSPACE_RUNTIME_TOKEN_FILE`, renews the credential before expiry, and revokes the previous token after a grace period. The worker rereads the token file for runtime calls, so renewal normally does not require user action or a worker restart.

In customer Helm deployments, the fixed-worker path can bootstrap the team workspace and runtime token in one install. When `bootstrap.teamWorkspace.enabled=true`, the chart stores one `msw_...` token in the release Secret, passes it to the server as `MSPACE_BOOTSTRAP_RUNTIME_TOKEN`, and passes the same Secret key to the worker as `MSPACE_RUNTIME_TOKEN`. Server startup ensures the bootstrap admin, creates or finds the named team workspace owned by that admin, and registers the token against that workspace. Codex auth/config still stays out of the server: the worker mounts `mspace-codex-home` with `auth.json` and `config.toml`, while the server only sees the mspace runtime registration token.

Agent session creation is guarded rather than left to wait in the queue. Issue Detail refreshes runtime worker liveness before writing the trigger comment; personal desktop mode may start the host-local worker and wait for a fresh heartbeat. The server repeats the same active Codex worker check and returns HTTP `409` with `no active codex worker` when no matching online worker exists.

Issue type triage follows the same boundary:

```text
Issue create/update
  -> server sets triage_status=pending when no type label exists
  -> server enqueues runtime_tasks(kind=issue_type_triage, requiredCapabilities={"codex":true})
Worker
  -> claims the task in the workspace runtime mode
  -> runs Codex app-server from a temporary worker directory
  -> returns {"type":"fix","confidence":...,"reason":...}
Server
  -> validates the type against the fixed Conventional Commit set
  -> replaces the issue's type label and marks triage_status=classified
```

Failed, cancelled, or invalid triage task results mark the issue triage as failed rather than falling back to keyword classification.

## Test Module Flow

Tests use project-level cases and case suggestions with workspace-level plans/runs. Execution still uses Issues and worker-claimed agent sessions:

```text
Tests / Cases
  -> create or import cases as project objects
  -> optional Optimize or Generate action
Server
  -> creates an issue-backed agent session for Codex work
Worker
  -> writes test-case-proposals.json
Server
  -> validates proposals and stores them as Case suggestions
Human
  -> accepts or rejects suggestions before canonical cases change

Tests / Plans
  -> select ready cases from one or more workspace projects and start a run
Server
  -> creates run items linked to cases and preserves each item's project
  -> if setupSteps exist, creates one setup issue/session first
Worker
  -> writes test-setup-result.json for setup, when required
Server
  -> stores setupResult, copies outputs to runContext
  -> starts case execution only after completed passing setup
Worker
  -> executes project-grouped case batches through the existing agent_session runtime path
  -> writes test-result.json
Server
  -> reconciles run item status and evidence
  -> persists screenshot evidence as server-owned test_artifacts
Human
  -> retries failed items or records a review decision
```

The Tests UI is intentionally not a separate test-management product. It owns durable project case knowledge, project case suggestions, workspace plans, and run review state. Collaboration, execution, worker logs, and evidence remain in the existing Issue, Agent Session, Runtime Task, and Evidence model.

For Tests setup and execution sessions, the artifact contract is a completion signal as well as evidence. A worker may complete an `agent_session` runtime task from a matching `test-setup-result.json` or `test-result.json` when Codex has produced the artifact but the app-server turn does not emit a final completion notification. For `test-result.json`, local screenshot references are part of readiness: the worker waits briefly for referenced files under the session artifact directory, embeds readable screenshots as transfer data URLs, and only then submits the artifact-backed result so the server can persist authenticated `test_artifacts` instead of path-only placeholders. A restarted worker may reclaim its own stale `running` task before claiming new queued work, recover the existing session workdir, apply the same screenshot readiness rule, and submit the artifact-backed final result so the server can reconcile the run without duplicating a freshly running task.

Plan setup is the first compatibility layer for real-world preconditions such as Kubernetes image updates, SSH/VM commands, platform login, mock data, or preview checks. It remains free-text setup steps plus a worker artifact contract, not a reusable template library or workflow DAG. A failed, cancelled, or missing setup artifact marks the run `setup_failed` before any case session starts.

Case creation and import use modal flows. Import first calls a read-only preview endpoint so users can see importable count, skipped rows, missing field counts, quality findings, source-column mappings, and samples before confirming the write. Markdown and text imports treat each non-empty line as a case. CSV and Excel `.xlsx` imports share a canonical column contract: `title`, `type`, `area`, `priority`, `preconditions`, `steps`, `expected_result`, `environment_requirements`, and `tags`. Unknown, localized, or product-specific headers go through a `test_case_import_mapping` runtime task claimed by a Codex-capable worker; the worker returns suggestions only, the user reviews them, and the client re-previews/imports with explicit `columnMappings`. Business category columns become `tags`, not fixed system types, unless the source has a real mspace `type` column. Historical execution-state columns can map to `latest_result` for preview context. Import is capped at 1,000 cases per request; text-like content is capped at 2 MB, while Excel workbooks are base64-encoded in the API request and capped at a 2 MB decoded workbook. The server reads the first non-empty sheet, skips rows without a title, validates case types, and opens the workbook with explicit unzip limits.

Case browsing is server-paginated with `limit` and `offset` because projects can accumulate large case libraries. The default list hides archived cases; user-facing deletion is an archive status transition that records a revision and preserves detail views, run history, plan membership, artifacts, and proposal links. Clients can inspect archived cases explicitly with `status=archived`.

## Storage Modes

The store boundary stays inside `server/`.

- `PostgresStore` is the canonical team/customer/shared deployment store. New shared product or runtime state needs server APIs plus Postgres migrations.
- `SQLiteStore` is the packaged personal desktop store. It wraps `MemoryStore`, persists one `store_snapshots` row, and is selected by `MSPACE_STORE=sqlite` or by omitting `DATABASE_URL`.
- The desktop sets `MSPACE_SQLITE_PATH=<Electron userData>/mspace.db` when it starts the local bundled server. Standalone server runs can use `MSPACE_SQLITE_PATH` directly or `MSPACE_DATA_DIR` to derive the path.
- SQLite persistence runs after mutating HTTP requests and the GitHub OAuth GET routes that create or consume OAuth state. It is for single-user personal mode, not team collaboration or cross-device truth.

Do not add a renderer-owned product database or local sidecar API. The local SQLite path is still the server control plane, just using a file-backed personal store.

## Data Model Summary

Main server-owned state groups:

- Identity: `users`, `user_password_credentials`, `user_identities`, `auth_sessions`, `oauth_states`, `oauth_results`. `/api/auth/me` and auth result payloads expose a lightweight `identity` object derived from `user_identities` for UI display and admin-login matching.
- Workspaces: `workspaces`, `workspace_members`, `workspace_invitations`. Invitation tokens are stored as server-side secrets and surfaced to users only as one-time join links. The signed-out preview API returns safe metadata such as workspace name, role, inviter display fields, expiry, and status; it does not expose member lists, raw internal ids, or token debug fields.
- Product state: `projects`, `project_runbooks`, `project_runbook_revisions`, `issues`, `comments`, `comment_reactions`, `issue_label_definitions`, `issue_labels`.
- Test module: `test_cases`, `test_case_revisions`, `test_case_proposals`, `test_plans`, `test_plan_cases`, `test_runs`, `test_run_items`, and `test_artifacts`. Cases and suggestions are project-level. Plans and runs are workspace-level orchestration records that keep a primary project for compatibility and preserve per-case/per-item project identity. Plans can store lightweight setup steps; runs freeze setup text plus setup status, setup issue/session, setup result, and run context. Valid test case types are `functional`, `ui`, `api`, and `deployment`; specialized UI/CDP, API harness, deployment orchestration, and multi-worker scheduling remain later execution capabilities behind the same Issue/Worker loop.
- Inbox: `issue_events`, `issue_event_receipts`, `issue_watchers`.
- Runtime surfaces: `workspace_settings`, `agent_profiles`, `environments`, `clusters`, `issue_test_environments`, `issue_handoffs`.
- Runtime queue: `runtime_registration_tokens`, `runtime_workers`, `runtime_tasks`, `runtime_task_events`, `runtime_task_logs`.
- Issue type triage is represented as `runtime_tasks.kind="issue_type_triage"` and reconciled when the task reaches a final state.

Issue Detail should treat this server state as authoritative. Do not create a second local issue/session/environment store.

## Implemented Routes

Desktop routes:

- `/inbox`
- `/issues`
- `/issues/:issueId`
- `/issues/:issueId/commits/:commitSha`
- `/issues/:issueId/evidence/history`
- `/issues/:issueId/evidence/snapshots`
- `/tests`
- `/tests/cases/:caseId`
- `/tests/plans/:planId`
- `/tests/runs/:runId`
- `/agents`
- `/environments` plus `/clusters` compatibility
- `/projects`
- `/settings`
- `/invite/:token`
- `/sessions/:sessionId`

Server route groups:

- auth and workspace APIs;
- Inbox event/receipt APIs;
- project and runbook APIs;
- project test case, case revision, case proposal, test plan, and test run APIs;
- issue/comment/task/label/reaction APIs;
- workspace setting, agent profile, environment, and cluster compatibility APIs;
- issue test environment deploy/cleanup/retain/resources/probe APIs;
- issue handoff create/refresh APIs;
- session creation/detail/cancellation APIs;
- runtime registration token, worker, task, event, log, cancellation, register, heartbeat, claim, and status APIs.

See `docs/integration-guide.md` for endpoint tables and curl examples.

## Issue And Session Rules

Issue status values are durable handoff states:

- `open`
- `needs_review`
- `changes_requested`
- `ready_for_test`
- `blocked`
- `cancelled`
- `closed`

Transient execution state belongs to `agent_sessions`, `runtime_tasks`, and `issue_test_environments`, not to issue status.

Issue task lists are child issues stored on `issues.parent_issue_id`. Checklist lines submitted during issue creation are extracted into child rows, and Issue Detail renders those children inline with checkbox-style status controls.

Only the latest human-authored issue comment may be edited, and only before an agent session has consumed it. Agent-triggering sessions store `agent_sessions.trigger_comment_id`.

## Source And Evidence

Source changes, delivery handoff, live namespace resources, review evidence, and failure evidence are deliberately separate:

- Commits tab: source commits, changed files, and diff previews.
- PR handoff: one current issue-level delivery artifact, keyed by source branch.
- Resources tab: live Kubernetes objects from the issue's fixed namespace.
- Evidence tab: current review packet, compact command evidence, agent summary, risks, source facts, and links to history pages.
- Failures: continueable failure records with phase, command, summary, excerpt, namespace/resource hints, and retry/continue affordances.

Dry-run worker commits are diagnostic records and should not be offered as PR source branch candidates.

## Environments And Test Environments

Environments are reusable workspace targets with kind `kubernetes` or `virtual_machine`. They describe what a worker may operate, while workers remain independent executors that claim tasks by mode and capabilities. Preview URLs are not environments; they are outputs recorded on runs, handoffs, or issue test deployments.

Kubernetes environments are currently projected from `clusters` compatibility records with:

- kubeconfig path;
- optional context;
- image registry prefix;
- exposure mode;
- optional preview domain;
- optional ingress class;
- optional NodePort host;
- readiness status and last check time.

Creating, updating, importing, or manually checking a Kubernetes Environment refreshes readiness from the server side. The check loads the selected kubeconfig/context, reaches the Kubernetes API server, and verifies lightweight namespace list permission before the Environment is marked `ready`; failures leave it `unreachable` with a refreshed check time.

Virtual machine environments store SSH target metadata: host, port, user, credential reference, workdir, service hints, labels, readiness status, timestamps, and a server-owned SSH credential. Creating a VM Environment requires SSH auth material, either password or private key; updating or manually rechecking can use the saved credential or replace it with new `sshAuth`. The server runs an `ssh user@host`-level login check, stores usable credentials for later worker access, and never returns raw passwords or private keys in Environment payloads. A successful check marks the Environment `ready`; connection or authentication failure saves it as `unreachable`; missing or unusable auth material is rejected when no saved credential exists.

Test plans and test runs can select an Environment. The server stores `environment_id`, `environment_kind`, and a frozen `environment_snapshot` so historical runs keep their meaning even if the reusable environment changes later. The older free-text `environment` field remains human notes/prompt context. Formal plans may also carry setup steps; each run freezes those steps and records setup output context separately from the Environment snapshot.

Each issue can have one `issue_test_environments` record. It stores selected environment id/kind/snapshot, Kubernetes cluster id, namespace, namespace state, cleanup state, preview URL, deployment session id, cleanup session id, selected source session/commit, registry, kubeconfig, context, exposure mode, domain, ingress class, and NodePort host. Current issue deploy and Resources flows are Kubernetes-only; VM deployment is a later provider-specific execution path.

Workspace Settings can opt into automatic test deployment. The server queues it only after a completed, non-dry-run source session reports a source commit, and it reuses the normal deploy/test session path with the captured source session and commit. The guardrails are intentionally conservative: no recursive deploy task, no missing project, no unresolved cluster/deploy settings, no active issue session, and no queueing without a matching online Codex worker.

The Resources API uses Kubernetes typed clients and fixes the namespace from the server environment record. It should expose only Pods, Services, Deployments, Ingresses, and recent Events for that issue namespace.

Preview probing is internal/background-driven. It updates test-environment state only and should not create review evidence, deployment evidence, failure records, or top-level issue timeline events.

## UI Architecture

Workspace Settings is accessed from the workspace identity menu. It owns:

- team workspace identity editing for owner/admin users: name, mark, and description;
- workspace automation;
- team-only members and invitations;
- worker runtime host connection for owner/admin users through a short-lived install command;
- worker credential audit history, separating active bootstrap credentials from expired or replaced history;
- worker liveness/capabilities;
- issue-linked runtime tasks, task events, and worker logs.

The runtime task table is an operations/readability surface, not a generic task creation form. Rows should lead with the user-facing task purpose, show the linked Issue title when an issue exists, link agent-session tasks back to the relevant Issue Detail session, and leave protocol kind, capabilities, payload, result, events, and logs in expanded details. Manual protocol-smoke task creation remains available through the API for debugging, but the normal Workspace Settings UI should not ask users to create raw runtime tasks.

Normal worker setup should not ask users to create or copy raw `msw_...` credentials. The product path returns a one-time install command for the target server, VM, DevBox, or Docker-capable worker host. The command embeds the short-lived bootstrap credential once, starts the Docker-backed worker, and leaves subsequent liveness and capability inspection to the Workers list. Raw credential endpoints stay available for Electron's automatic personal worker lifecycle and API-level recovery/debugging.

Helm-managed customer clusters are the operational exception to the UI-first setup path. The chart can create and preserve a fixed worker token in the release Secret so server and worker agree on the same workspace-scoped credential without an operator copying it from Workspace Settings. This exception does not weaken the server/worker boundary: the token is a mspace registration credential, not Codex auth material.

Team invitation setup follows the same user-centered rule. Workspace Settings creates a one-time `mspace://invite/<token>?server=<team-server-url>` join link and places the copy action beside the link. Recipients see inviter, role, and workspace context before authentication, then the app accepts the invitation after login or registration and switches directly into the invited team workspace. Normal UI should not show join codes, invitation ids, token prefixes, or require users to choose the server when the link already carries that state.

Open account registration creates a personal workspace and a personal runtime boundary. Only server-admin logins configured by `MSPACE_SERVER_ADMIN_LOGINS` or `MSPACE_BOOTSTRAP_ADMIN_LOGIN` can create team workspaces. Team server runners are reachable only through membership in a team workspace, and runtime worker/task mode must match the workspace kind.

Tests stays focused on project-level cases/suggestions plus workspace-level plans, runs, retry, and run review records. Agents stays focused on mentionable Codex-backed role behavior. Environments stays focused on reusable Kubernetes and VM validation targets. Projects stays focused on repository metadata and project runbooks.

The desktop visual language is a quiet Notion-like workspace: narrow left sidebar, document pages, compact status rows, subdued blocks, restrained icon actions, and no decorative dashboard language.

## Future Work

- Server-owned image attachment upload/storage.
- GitHub App installation state and server-owned PR executor.
- Stronger remote credential policy for fixed workers.
- Lower-latency cancellation guarantees.
- Kubernetes Runtime Provider that starts isolated per-session Pods or Jobs behind the same runtime task protocol.
- Generated scoped kubeconfig, ServiceAccount, Role, RoleBinding, ResourceQuota, LimitRange, and TTL policy for Kubernetes-hosted execution.
