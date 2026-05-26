# mspace Architecture Notes

> Status: server-owned runtime surfaces, updated 2026-05-25

## Current Implementation Snapshot

The repository contains a runnable desktop MVP with server-owned product and runtime state:

- Electron desktop shell built with electron-vite, React 19, TanStack Router, React Query 5, Tailwind CSS 4, TypeScript, pnpm workspaces, and Turbo.
- Public website in `apps/website`, built with Vite, React 19, Tailwind CSS 4, and lucide-react, deployed as a static Vercel site from the root `vercel.json`.
- Shared UI layer built on shadcn/ui source components, Radix UI primitives, lucide-react icons, Material Icon Theme file icons, and the `cn()` helper in `packages/ui/src/lib/utils.ts`.
- Shared desktop localization lives in `packages/i18n` for `en` and `zh-CN`.
- Go server control plane in `server/`, built with chi. Team/shared deployments use PostgreSQL through `pgx`; packaged personal desktop mode can use a server-owned SQLite snapshot store.
- Go runtime worker in `worker/`, registered to workspaces with `msw_...` tokens.
- Local username/password auth creates a personal workspace by default and works without GitHub access. Default local personal sign-in opens on account creation and hides GitHub. GitHub sign-in remains an optional external identity provider for explicitly configured team servers, from a saved Team server URL or `MSPACE_SERVER_URL`, when `/health` reports `capabilities.githubAuth: true`. Auth responses include `identity.provider` and `identity.login`, so the desktop can show local accounts and GitHub accounts without guessing from `user.email`. Personal and team workspaces both use the server store; team/shared deployments use Postgres, while packaged personal desktop mode can stay local on SQLite.
- Team collaboration features require an explicit team workspace created from the workspace menu.
- The desktop uses `MSPACE_SERVER_URL`, then a saved Team server URL, then the local bundled/dev server when needed. It renders server state and does not own product truth or runtime persistence.

The control plane owns:

- users, local password credentials, workspaces, membership, GitHub identity, and mspace auth sessions;
- projects, project runbooks, issues, child issue tasks, comments, reactions, labels, Inbox events, and per-user receipts;
- workspace settings, agent profiles, clusters, issue test environments, issue handoffs, review evidence, failures, and source change records;
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
- Workspaces: `workspaces`, `workspace_members`, `workspace_invitations`.
- Product state: `projects`, `project_runbooks`, `project_runbook_revisions`, `issues`, `comments`, `comment_reactions`, `issue_label_definitions`, `issue_labels`.
- Inbox: `issue_events`, `issue_event_receipts`, `issue_watchers`.
- Runtime surfaces: `workspace_settings`, `agent_profiles`, `clusters`, `issue_test_environments`, `issue_handoffs`.
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
- `/agents`
- `/clusters`
- `/projects`
- `/settings`
- `/sessions/:sessionId`

Server route groups:

- auth and workspace APIs;
- Inbox event/receipt APIs;
- project and runbook APIs;
- issue/comment/task/label/reaction APIs;
- workspace setting, agent profile, and cluster APIs;
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

## Test Environments

Clusters are reusable workspace records with:

- kubeconfig path;
- optional context;
- image registry prefix;
- exposure mode;
- optional preview domain;
- optional ingress class;
- optional NodePort host;
- readiness status and last check time.

Each issue can have one `issue_test_environments` record. It stores selected cluster id, namespace, namespace state, cleanup state, preview URL, deployment session id, cleanup session id, selected source session/commit, registry, kubeconfig, context, exposure mode, domain, ingress class, and NodePort host.

The Resources API uses Kubernetes typed clients and fixes the namespace from the server environment record. It should expose only Pods, Services, Deployments, Ingresses, and recent Events for that issue namespace.

Preview probing is internal/background-driven. It updates test-environment state only and should not create review evidence, deployment evidence, failure records, or top-level issue timeline events.

## UI Architecture

Workspace Settings is accessed from the workspace identity menu. It owns:

- workspace automation;
- team-only members and invitations;
- worker registration tokens for owner/admin users;
- worker liveness/capabilities;
- runtime tasks, task events, and worker logs.

Open account registration creates a personal workspace and a personal runtime boundary. Only server-admin logins configured by `MSPACE_SERVER_ADMIN_LOGINS` or `MSPACE_BOOTSTRAP_ADMIN_LOGIN` can create team workspaces. Team server runners are reachable only through membership in a team workspace, and runtime worker/task mode must match the workspace kind.

Agents stays focused on mentionable Codex-backed role behavior. Clusters stays focused on reusable validation access. Projects stays focused on repository metadata and project runbooks.

The desktop visual language is a quiet Notion-like workspace: narrow left sidebar, document pages, compact status rows, subdued blocks, restrained icon actions, and no decorative dashboard language.

## Future Work

- Server-owned image attachment upload/storage.
- GitHub App installation state and server-owned PR executor.
- Stronger remote credential policy for fixed workers.
- Lower-latency cancellation guarantees.
- Kubernetes Runtime Provider that starts isolated per-session Pods or Jobs behind the same runtime task protocol.
- Generated scoped kubeconfig, ServiceAccount, Role, RoleBinding, ResourceQuota, LimitRange, and TTL policy for Kubernetes-hosted execution.
