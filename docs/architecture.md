# mspace Architecture Notes

> Status: local MVP implementation snapshot, updated 2026-05-07

## Current Implementation Snapshot

The repository currently contains a runnable local-first desktop MVP:

- Electron desktop shell built with electron-vite, React 19, React Router 7, React Query 5, Tailwind CSS 4, TypeScript, pnpm workspaces, and Turbo.
- Shared UI layer built on shadcn/ui source components, Radix UI primitives, lucide-react icons, and the `cn()` helper in `packages/ui/src/lib/utils.ts`.
- Go local runner built with chi and SQLite. The Electron main process starts the runner automatically with `go run .` unless a healthy runner is already listening.
- SQLite state lives at `~/.mspace/mspace.db`.
- Imported GitHub repositories are cached under `~/.mspace/repos/<owner>/<repo>`.
- Session worktrees live under `~/.mspace/workdirs/<project-id>/<session-id>`.
- Session context markdown lives under `~/.mspace/workdirs/_contexts/<session-id>.md`.
- The runner stores the session worktree path in `agent_sessions.workdir`.
- Each session creates or attaches a git worktree before executing the session command.
- Session branch defaults to `mspace/<issue-short-id>/<session-short-id>` when the user does not provide one.
- Project import supports existing local folders and GitHub repository URLs. Local repositories auto-detect git remote metadata when available.
- Inbox is an unread review feed powered by server-sent events from `/api/inbox/stream`.
- Issue assignment to an agent currently goes through `POST /api/issues/{issueID}/assign-agent`, which also queues the local session.
- Kubernetes is currently represented by project-level `kube_context` and `namespace`, passed into the session as `MSPACE_KUBE_CONTEXT` and `MSPACE_KUBE_NAMESPACE`. Scoped kubeconfig and ServiceAccount generation are still future work.
- Session commands also receive `MSPACE_ISSUE_ID`, `MSPACE_SESSION_ID`, `MSPACE_SESSION_BRANCH`, `MSPACE_SESSION_WORKDIR`, and `MSPACE_SESSION_CONTEXT`.
- Current desktop visual language is a Notion-like paper workspace: narrow left sidebar, document pages, compact status rows, subdued blocks, and restrained icon actions.

Current shadcn/ui component source:

- `packages/ui/src/components/ui/alert.tsx`
- `packages/ui/src/components/ui/badge.tsx`
- `packages/ui/src/components/ui/button.tsx`
- `packages/ui/src/components/ui/card.tsx`
- `packages/ui/src/components/ui/field.tsx`
- `packages/ui/src/components/ui/input.tsx`
- `packages/ui/src/components/ui/label.tsx`
- `packages/ui/src/components/ui/separator.tsx`
- `packages/ui/src/components/ui/textarea.tsx`

Implemented desktop routes:

- `/inbox`
- `/issues`
- `/issues/:issueId`
- `/projects`
- `/sessions/:sessionId`

Implemented runner API:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Runner health and version. |
| `GET` | `/api/inbox` | List inbox items. |
| `GET` | `/api/inbox/stream` | Stream inbox update events over server-sent events. |
| `GET` | `/api/projects` | List projects with issue/session counts. |
| `POST` | `/api/projects` | Create a project. |
| `PUT` | `/api/projects/{projectID}` | Update a project. |
| `DELETE` | `/api/projects/{projectID}` | Delete a project and cascaded child data. |
| `GET` | `/api/issues` | List issues across the local workspace. |
| `POST` | `/api/issues` | Create an issue and inbox item. |
| `GET` | `/api/issues/{issueID}` | Load issue detail, comments, sessions, and evidence. |
| `POST` | `/api/issues/{issueID}/comments` | Add a human comment. |
| `POST` | `/api/issues/{issueID}/assign-agent` | Assign Codex and queue a local session. |
| `POST` | `/api/issues/{issueID}/sessions` | Queue and start a local agent session. |
| `GET` | `/api/sessions/{sessionID}` | Load session detail, logs, evidence, and workspace snapshot. |
| `POST` | `/api/sessions/{sessionID}/cancel` | Cancel a running session. |
| `GET` | `/api/sessions/{sessionID}/stream` | Server-sent events for session logs and status changes. |

## Architecture Summary

mspace should separate the collaboration layer from the runtime layer.

The collaboration layer is the product entry point: Inbox, Issue, comments, subscribers, agent sessions, and evidence. The runtime layer is where the agent edits and runs code. The validation environment layer is where the changed project gets deployed and inspected. In the MVP, the runtime should be local-first and the validation environment should be namespace-scoped Kubernetes.

```text
Web UI
  -> mspace API
      -> Inbox Review / Issue Service
      -> Session Service
      -> Runtime Manager
          -> Local Runtime Provider
          -> Remote Runtime Provider
          -> Future Kubernetes Runtime Provider
      -> Validation Environment Manager
          -> Kubernetes Cluster
              -> Namespace
              -> ServiceAccount / Role / RoleBinding
              -> Project Workloads
```

## Main Concepts

### Workspace

A Workspace is the team boundary for users, agents, issues, and runtime policy.

Required fields:

- name;
- slug;
- members;
- default agent providers;
- runtime policy;
- project list.

The local MVP has not implemented a `workspaces` table yet. It behaves as a single local workspace.

### Inbox Item

An Inbox Item is the unread review unit for issue activity.

Required fields:

- linked issue;
- linked project;
- title;
- status snapshot;
- assignee snapshot;
- read state;
- last update time.

### Project

A Project is a repository plus runtime policy.

Current implemented fields:

- name;
- source type;
- local repository path;
- remote URL;
- git provider;
- git owner;
- git repo;
- default branch;
- Kubernetes context;
- Kubernetes namespace;
- deploy command;
- validation command.

### Issue

An Issue is the durable collaboration document for one unit of work.

Current implemented fields:

- project;
- title;
- body;
- status;
- assignee;
- assignee type;
- comments and progress updates;
- linked sessions;
- environment evidence.

### Agent Session

An Agent Session is one agent run attached to one issue.

Current implemented fields:

- issue;
- provider;
- runtime mode;
- command;
- branch;
- worktree path;
- status;
- terminal stream;
- evidence summary.

### Namespace Policy

There are two supported policies:

- project namespace: one long-lived namespace per project;
- session namespace: one temporary namespace per agent session.

The recommended default is session namespace once concurrency matters. The project namespace is acceptable for the first internal prototype because it is simpler and matches the current manual workflow.

### Runtime Provider

A Runtime Provider starts the actual agent environment.

Provider options in the product model:

- local runtime for development and bring-your-own CLI operation;
- remote runtime for hosted or cluster-adjacent execution only when it preserves the same operational contract;
- DevBox-like runtime if available internally;
- future Kubernetes-hosted runtime when the product grows into that model;
- local daemon bridge only as an adapter, not as the primary product model.

The first production-grade MVP path should be local runtime because it keeps iteration simple and matches the current intended workflow.

The implemented MVP uses a desktop-managed local runner process, not a long-running daemon. The runner owns SQLite, HTTP APIs, session process execution, log capture, and git worktree preparation.

### Validation Environment

A Validation Environment is where the changed project is deployed and inspected.

Initial environment options:

- Kubernetes namespace in a shared test cluster;
- future local container or ephemeral environment only if it preserves enough realism for the product.

The first serious environment should be Kubernetes because the product value depends on real environment isolation, scoped cluster access, and runtime evidence.

### Kubernetes-First Validation Principle

Kubernetes should be visible as a core design assumption in the validation layer:

- project setup includes cluster and namespace policy by default;
- session startup can allocate namespace access and scoped kubeconfig;
- issue evidence assumes pod, event, rollout, and ingress data are available;
- cleanup assumes namespace or workload lifecycle management.

Other validation targets are adapters around this core shape, not peers that redefine the product.

## Kubernetes Permission Model

Each session that can deploy or inspect the Kubernetes environment gets a dedicated ServiceAccount or equivalent scoped kubeconfig.

Allowed by default:

- get/list/watch pods, services, endpoints, deployments, statefulsets, jobs, configmaps, events, ingress resources;
- get pod logs;
- create/update/patch/delete namespaced workloads only inside the assigned namespace when write mode is enabled;
- rollout-related operations through patch/apply equivalents.

Denied by default:

- cluster-scoped writes;
- namespace create/delete by the agent itself;
- secrets read;
- node access;
- persistent volume cluster operations;
- cross-namespace reads unless explicitly granted.

The mspace controller, not the agent, creates namespaces, RoleBindings, quotas, and cleanup jobs.

## Resource Guardrails

Every Kubernetes-backed test namespace should have:

- ResourceQuota;
- LimitRange;
- optional NetworkPolicy;
- TTL/expiration annotation;
- owner labels linking it to project and session;
- audit labels on every object created by mspace.

Suggested labels:

```yaml
app.mspace.dev/project: "<project-name>"
app.mspace.dev/session: "<session-id>"
app.mspace.dev/managed-by: "mspace"
```

## Execution Flow

### Implemented Local Session Flow

```text
User creates an issue in /issues or opens an existing one
  -> desktop calls POST /api/issues/{issueID}/assign-agent
  -> runner marks the issue assigned to an agent and creates a queued session
  -> runner stores queued session in SQLite
  -> runner plans workdir under ~/.mspace/workdirs/<project-id>/<session-id>
  -> runner creates a git worktree from the project's default branch
  -> runner checks out the session branch
  -> runner writes ~/.mspace/workdirs/_contexts/<session-id>.md
  -> runner injects MSPACE_SESSION_CONTEXT and session metadata env vars
  -> runner starts the configured command inside the session worktree
  -> runner writes stdout/stderr to session_logs
  -> desktop watches GET /api/sessions/{sessionID}/stream
  -> desktop refreshes Inbox, Issues, and Issue Detail through GET /api/inbox/stream
  -> session detail reads git status, commits, diff, and base comparison from workdir
```

The default command is generated from the issue and project configuration. If deploy or validation commands are configured, the generated command runs them before taking a Kubernetes resource snapshot through `kubectl`.

### Target Product Flow

```text
User creates issue
  -> API stores issue context and collaborators
  -> User starts session from issue
  -> API validates project and runtime policy
  -> Runtime Manager selects local provider by default
  -> Validation Environment Manager creates namespace or selects project namespace
  -> Validation Environment Manager creates ServiceAccount and RBAC
  -> Runtime starts agent workspace
  -> Agent receives repo, issue context, commands, and scoped kubeconfig
  -> Agent works and streams progress
  -> Deployment and validation produce namespace evidence
  -> Evidence is attached back to the issue
  -> Runtime is retained or cleaned up
```

## Data Model Sketch

### Current SQLite Schema

The local MVP uses these SQLite tables from `runner/migrations/001_init.sql`:

```text
projects
  id
  name
  repo_path
  source_type
  remote_url
  git_provider
  git_owner
  git_repo
  default_branch
  deploy_command
  validation_command
  kube_context
  namespace
  created_at
  updated_at

issues
  id
  project_id
  title
  body
  status
  assignee
  assignee_type
  environment_url
  created_at
  updated_at

inbox_items
  id
  issue_id
  project_id
  title
  status
  unread
  created_at
  updated_at

comments
  id
  issue_id
  author_type
  body
  created_at

agent_sessions
  id
  issue_id
  provider
  runtime_mode
  command
  status
  branch
  workdir
  created_at
  updated_at

session_logs
  id
  session_id
  stream
  message
  created_at

deployment_evidence
  id
  issue_id
  session_id
  cluster
  namespace
  summary
  details
  created_at
```

### Target Product Schema

```text
workspaces
  id
  name
  slug
  runtime_policy

inbox_items
  id
  workspace_id
  source_type
  title
  details
  recipient_type
  recipient_id
  issue_id
  read
  created_at

projects
  id
  workspace_id
  name
  repo_url
  default_branch
  cluster_ref
  namespace_policy
  bootstrap_command
  deploy_command
  validation_command

issues
  id
  workspace_id
  project_id
  title
  body
  status
  assignee_type
  assignee_id
  pr_url
  environment_url
  created_at
  updated_at

issue_subscribers
  issue_id
  user_type
  user_id

comments
  id
  issue_id
  type
  body
  created_by
  created_at

agent_sessions
  id
  issue_id
  agent_provider
  runtime_provider
  runtime_mode
  namespace
  service_account
  branch
  status
  pr_url
  environment_url
  created_at
  completed_at

session_events
  id
  session_id
  type
  message
  payload
  created_at
```

## UI Surfaces

### Inbox

Shows incoming and active work across the workspace.

### Issues

Shows document-style issue pages with discussion, session history, and evidence.

### Projects

Shows configured repositories and their active issue and session count.

### Project Detail

Shows:

- repository;
- default branch;
- namespace policy;
- active and historical issues;
- active and historical sessions;
- environment links;
- recent failures or blockers.

### Issue Detail

Shows:

- problem statement;
- comments and progress updates;
- assignee and subscribers;
- linked sessions;
- PR/branch output;
- environment evidence.

### Session Detail

Shows:

- issue context;
- agent status;
- terminal/progress stream;
- runtime details;
- Kubernetes namespace resources when applicable;
- PR/branch output;
- cleanup controls.

## UI Component System

The canonical shadcn/ui source lives in `packages/ui/src/components/ui`. Generated components should stay close to the shadcn source shape, while product-facing wrappers and app shell components are exported from `packages/ui/src/index.tsx` through `@mspace/ui`.

The shadcn config is present at both repository root and `packages/ui/components.json`. The package-local config points generated components at:

- components: `@mspace/ui/components`
- UI components: `@mspace/ui/components/ui`
- utilities: `@mspace/ui/lib/utils`
- icons: lucide

The desktop renderer resolves those aliases in `apps/desktop/electron.vite.config.ts`. If the aliases are removed, imports from generated shadcn components will fail during desktop builds.

Tailwind CSS 4 semantic tokens are mapped in `apps/desktop/src/renderer/src/globals.css` through `@theme inline`. The same file owns the Notion-like mspace palette (`--paper`, `--sidebar`, `--block`, `--line`, `--ink`, and related tokens) and keeps Tailwind scanning the monorepo UI packages through `@source`.

### Namespace View

For the first version, keep this narrow:

- Pods;
- Services;
- Ingress;
- Events;
- recent logs for selected pod;
- rollout status for deployments.

## Technical Reference Decisions

### Keep Collaboration and Runtime Separate

Do not let Kubernetes details leak into every product object. Inbox items and issues should remain coherent on their own. Also do not collapse runtime and environment into one concept. Local development and Kubernetes validation should be modeled separately in the MVP.

### Use Kubernetes as the Source of Runtime Truth

Do not depend on Sealos UI APIs as the primary workflow contract. They may be useful later for integration, but the Kubernetes validation environment should operate against Kubernetes resources because the product is specifically about namespace-scoped deployment and test environments.

### Start with `kubectl`, Graduate to Dynamic Client

`kubectl` is acceptable for the prototype because it mirrors the current manual workflow. The production path should move critical environment operations to Kubernetes clients and structured JSON outputs:

- less fragile than shell text;
- easier to audit;
- easier to enforce dry-run and patch previews;
- easier to render in UI.

### Keep Agent Execution Replaceable

Agent providers should be adapters:

- Codex;
- Claude Code;
- Cursor;
- Kimi;
- OpenCode;
- other local or containerized CLIs.

The platform should not assume one provider is permanent.

### Do Not Fork Multica

Use Multica as a reference for structure, workflow, and implementation ideas, but keep mspace as an independent codebase. Borrow product shape and data model ideas without inheriting Multica-specific runtime assumptions or licensing constraints.

## Main Risks

### Inbox Without Runtime Is Too Weak

If the issue layer becomes only a thin wrapper around runtime jobs, the product collapses into another agent dashboard. The issue itself must remain useful as a durable collaboration document.

### Runtime and Environment Can Be Confused

If the product talks about local development, remote execution, and Kubernetes validation with one overloaded "runtime" term, the implementation model will become muddy. Keep "where the agent runs" separate from "where the project gets deployed and tested."

### Namespace Isolation Is Not a Full Security Boundary

The product must not imply that namespace isolation alone is equivalent to a hardened tenant boundary. RBAC, quotas, network policy, secret policy, and audit must be part of the first serious deployment.

### Shared Test Clusters Can Be Exhausted

At 10x usage, the first failure is likely resource pressure from many concurrent sessions. ResourceQuota, TTL cleanup, and maximum concurrent sessions per project should be first-class.

### Agent Runs Can Block on External Systems

Git providers, package registries, image registries, and model providers can all fail. A session should degrade into a visible blocked state instead of spinning forever.

## Minimal Build Order

1. Workspace, inbox, and issue data model.
2. Issue detail page with comments and session list.
3. Session creation from issue with one agent provider.
4. Local runtime implementation for the first agent provider.
5. Kubernetes validation environment with scoped kubeconfig and ServiceAccount generation.
6. Terminal/progress stream.
7. Namespace resource viewer and environment evidence capture.
8. Session cleanup and retention rules.
