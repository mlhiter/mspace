# mspace Architecture Notes

> Status: server-owned runtime surfaces, updated 2026-05-19

## Current Implementation Snapshot

The repository contains a runnable desktop MVP with server-owned product and runtime state:

- Electron desktop shell built with electron-vite, React 19, TanStack Router, React Query 5, Tailwind CSS 4, TypeScript, pnpm workspaces, and Turbo.
- Public website in `apps/website`, built with Vite, React 19, Tailwind CSS 4, and lucide-react, deployed as a static Vercel site from the root `vercel.json`.
- Shared UI layer built on shadcn/ui source components, Radix UI primitives, lucide-react icons, Material Icon Theme file icons, and the `cn()` helper in `packages/ui/src/lib/utils.ts`.
- Shared desktop localization lives in `packages/i18n` for `en` and `zh-CN`.
- Go server control plane in `server/`, built with chi and PostgreSQL through `pgx`.
- Go runtime worker in `worker/`, registered to workspaces with `msw_...` tokens.
- GitHub sign-in creates a personal workspace by default. Personal and team workspaces both store product data plus runtime state in server Postgres.
- Team collaboration features require an explicit team workspace created from the workspace menu.
- The desktop starts the server control plane when needed and renders server state. It does not own product truth or runtime persistence.

The control plane owns:

- users, workspaces, membership, GitHub identity, and mspace auth sessions;
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

## Data Model Summary

Main server-owned table groups:

- Identity: `users`, `user_identities`, `auth_sessions`, `oauth_states`, `oauth_results`.
- Workspaces: `workspaces`, `workspace_members`, `workspace_invitations`.
- Product state: `projects`, `project_runbooks`, `project_runbook_revisions`, `issues`, `comments`, `comment_reactions`, `issue_label_definitions`, `issue_labels`.
- Inbox: `issue_events`, `issue_event_receipts`, `issue_watchers`.
- Runtime surfaces: `workspace_settings`, `agent_profiles`, `clusters`, `issue_test_environments`, `issue_handoffs`.
- Runtime queue: `runtime_registration_tokens`, `runtime_workers`, `runtime_tasks`, `runtime_task_events`, `runtime_task_logs`.

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
- worker registration tokens;
- worker liveness/capabilities;
- runtime tasks, task events, and worker logs.

Agents stays focused on mentionable Codex-backed role behavior. Clusters stays focused on reusable validation access. Projects stays focused on repository metadata and project runbooks.

The desktop visual language is a quiet Notion-like workspace: narrow left sidebar, document pages, compact status rows, subdued blocks, restrained icon actions, and no decorative dashboard language.

## Future Work

- Server-owned image attachment upload/storage.
- GitHub App installation state and server-owned PR executor.
- Stronger remote credential policy for fixed workers.
- Lower-latency cancellation guarantees.
- Kubernetes Runtime Provider that starts isolated per-session Pods or Jobs behind the same runtime task protocol.
- Generated scoped kubeconfig, ServiceAccount, Role, RoleBinding, ResourceQuota, LimitRange, and TTL policy for Kubernetes-hosted execution.
