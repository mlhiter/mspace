# mspace Architecture Notes

> Status: local MVP implementation snapshot, updated 2026-05-10

## Current Implementation Snapshot

The repository currently contains a runnable local-first desktop MVP:

- Electron desktop shell built with electron-vite, React 19, TanStack Router, React Query 5, Tailwind CSS 4, TypeScript, pnpm workspaces, and Turbo.
- Shared UI layer built on shadcn/ui source components, Radix UI primitives, lucide-react icons, Material Icon Theme file icons, and the `cn()` helper in `packages/ui/src/lib/utils.ts`.
- Go server control plane in `server/`, built with chi and PostgreSQL through `pgx`. It owns users, workspaces, membership, GitHub identity, mspace auth sessions, and future GitHub App installation state.
- Desktop GitHub sign-in uses the server OAuth flow, stores an `msp_...` session token, and caches a lightweight display identity for local runner writes while collaboration data still lives in SQLite.
- Issue creation and the Issue Detail reply composer use a local TipTap-backed `IssueDocumentEditor` that emits Markdown. This preserves document-like writing surfaces while keeping runner-side checklist extraction and comment storage text-based. Images can be uploaded, pasted, or dropped into the editor; Markdown stores stable `/api/attachments/<id>` image URLs backed by runner attachment records rather than loose local files, and the renderer shows them as thumbnail node views.
- Go local runner built with chi and SQLite. The Electron main process starts the runner automatically with `go run .` unless `/health` is healthy and advertises the expected runner protocol capabilities; in desktop development, stale local runners on `127.0.0.1:7788` are replaced before startup continues.
- SQLite state lives at `~/.mspace/mspace.db`, including local MVP issue image attachment blobs in `issue_attachments`.
- Imported GitHub repositories are cached under `~/.mspace/repos/<owner>/<repo>`.
- Session worktrees live under `~/.mspace/workdirs/<project-id>/<session-id>`.
- Session context markdown lives under `~/.mspace/workdirs/_contexts/<session-id>.md`.
- The runner stores the session worktree path in `agent_sessions.workdir`.
- Each session creates or attaches a git worktree before starting the runtime provider.
- The Codex provider starts `codex app-server --listen stdio://` in the session worktree and talks to it over newline-delimited JSON-RPC.
- The runner persists Codex agent profile, thread id, turn id, agent status, artifact directory, scoped agent token, cleanup status, and cleaned timestamp on `agent_sessions`.
- Agent profiles live in `agent_profiles` and are exposed through the Agents module. Default Codex-backed agents are seeded, but new sessions resolve profile instructions from SQLite rather than hardcoded profile switches.
- Finished local session worktrees can be cleaned through `POST /api/sessions/{sessionID}/cleanup`; active sessions are rejected, the stored workdir must stay under the mspace workdir root, and session records remain for review.
- Source changes and review evidence are split deliberately. `issue_change_nodes` backs the Commits tab for commit metadata and diffs; `session_review_evidence` backs the Evidence tab for commands, tests, build/deploy results, preview URL, Kubernetes state, agent summary, risks/follow-ups, and cleanup/retain state. Changed-file UI uses Material Icon Theme file icons and filters directory-only placeholder paths while preserving concrete file paths under those directories.
- Session branch defaults to `mspace/<issue-short-id>/<session-short-id>` when the user does not provide one.
- Project import supports existing local folders and GitHub repository URLs. Local repositories auto-detect git remote metadata when available.
- Inbox is an unread review feed. Signed-in team state is built from server `issue_events` and per-user `issue_event_receipts`; the local runner `inbox_items.unread` plus `/api/inbox/stream` remains a fallback and invalidation path.
- Issue comments that mention an enabled agent are saved first, then the desktop calls `POST /api/issues/{issueID}/assign-agent` with the mention-stripped comment as the current turn request and the selected Codex profile.
- Local runner issues and comments keep denormalized display identity snapshots: issue creator name/avatar and comment author name/avatar. Existing anonymous local human rows are backfilled to `mlhiter`; ordinary system comments display as `mspace`. Status-transition comments are authored by the actor that performed the change, so human changes render as that signed-in user and scoped agent changes render as the agent.
- Local runner issue writes are authenticated. Human write requests carry a mspace session token and are verified against control-plane `GET /api/auth/me`; agent write requests carry the session-scoped `MSPACE_AGENT_TOKEN`.
- Issue workflow status values are `open`, `in_progress`, `needs_review`, `changes_requested`, `ready_for_test`, `test_in_progress`, `blocked`, `cancelled`, and `closed`. `cancelled` is the issue-level "closed as not planned" outcome, not a stopped-session state. Only humans can close or cancel top-level issues; agent sessions move issues into review/test/progress states, use `blocked` when they cannot proceed, and may close child tasks under their assigned issue. Every transition is mirrored to the issue timeline and rendered as a compact actor-authored status event with readable badges.
- The desktop shell exposes a global search / Command+K palette backed by the existing issues and projects queries. Active work remains a separate sidebar block because it is an issue subset, not an additional global search source.
- Issue task lists are child issues stored on `issues.parent_issue_id`. Checklist lines submitted during issue creation are extracted into child rows, and Issue Detail renders those children inline with checkbox-style status controls.
- Issue labels use a built-in taxonomy in `issue_label_definitions` and issue links in `issue_labels`. The current dimensions are `type` and `priority`; type is classified asynchronously by the internal `@triage` Codex profile, while priority remains human-selected from Issue Detail.
- Kubernetes is currently represented by reusable cluster configs plus issue-level test environment records. Clusters can be imported from selected kubeconfig files or discovered from regular files under `~/.kube`; each imported context stores `kubeconfig_path`, optional `kube_context`, `image_registry_prefix`, default `exposure_mode`, optional `preview_domain`, optional `ingress_class`, optional `node_host`, and a readiness status from a read-only API check.
- Projects store `default_cluster_id` so issue deploys can use known test cluster access without asking for kubeconfig or registry values each time.
- Each issue can have one `issue_test_environments` record. It stores the selected cluster id, reserved issue namespace, namespace state, cleanup state, preview URL, deployment session id, cleanup session id, and the resolved registry/kubeconfig/routing values used for that issue.
- Test deployment is a manual Issue Detail action. It queues a Codex deploy/test turn; the agent creates the issue namespace, builds and pushes images, deploys resources, exposes NodePort by default or Ingress when configured, probes the preview URL, and writes `test-environment.json` into the session artifact directory when it has a URL to record.
- Scoped kubeconfig and ServiceAccount generation are still future work. The MVP trusts the kubeconfig path stored in the selected cluster.
- Sessions also receive `MSPACE_API_BASE_URL`, `MSPACE_ISSUE_ID`, `MSPACE_SESSION_ID`, `MSPACE_AGENT_TOKEN`, `MSPACE_AGENT_PROFILE`, `MSPACE_SESSION_BRANCH`, `MSPACE_SESSION_WORKDIR`, `MSPACE_SESSION_CONTEXT`, `MSPACE_SESSION_ARTIFACT_DIR`, and resolved cluster/test-environment variables when the issue has a test environment.
- Current desktop visual language is a Notion-like paper workspace: narrow left sidebar, document pages, compact status rows, subdued blocks, and restrained icon actions.

Current shadcn/ui component source:

- `packages/ui/src/components/ui/alert.tsx`
- `packages/ui/src/components/ui/badge.tsx`
- `packages/ui/src/components/ui/button.tsx`
- `packages/ui/src/components/ui/card.tsx`
- `packages/ui/src/components/ui/field.tsx`
- `packages/ui/src/components/ui/input.tsx`
- `packages/ui/src/components/ui/label.tsx`
- `packages/ui/src/components/ui/scroll-area.tsx`
- `packages/ui/src/components/ui/separator.tsx`
- `packages/ui/src/components/ui/select.tsx`
- `packages/ui/src/components/ui/textarea.tsx`

Implemented desktop routes:

- `/inbox`
- `/issues`
- `/issues/:issueId`
- `/agents`
- `/clusters`
- `/projects`
- `/sessions/:sessionId`

Implemented server control-plane API:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Server health. |
| `GET` | `/api/auth/github/start` | Create an OAuth state and return a GitHub authorization URL. |
| `GET` | `/api/auth/github/callback` | Validate OAuth state, exchange the GitHub code, link identity, ensure a default workspace, issue an mspace session token, and render a browser success page. |
| `GET` | `/api/auth/github/result` | Desktop polling endpoint for the state-bound auth result. Returns `202` while pending and consumes the short-lived result once ready. |
| `GET` | `/api/auth/me` | Load the authenticated user and workspaces from `Authorization: Bearer msp_...`. |
| `GET` | `/api/workspaces` | List workspaces for the authenticated user. |
| `GET` | `/api/workspaces/{workspaceID}/inbox` | List unread issue-event receipts for the authenticated user. |
| `POST` | `/api/workspaces/{workspaceID}/issue-events` | Append a reviewable issue event and create per-user receipts. |
| `POST` | `/api/workspaces/{workspaceID}/issue-events/{eventID}/read` | Mark one event receipt read for the authenticated user. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/read-through` | Mark unread receipts for one issue read through an optional event boundary. |

Implemented runner API:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Runner health, version, protocol, and feature capabilities. |
| `POST` | `/api/control-plane/session` | Cache the current server base URL, bearer token, and workspace id so the runner can report reviewable issue events to the control plane. |
| `GET` | `/api/inbox` | List inbox items. |
| `POST` | `/api/inbox/issues/{issueID}/read` | Mark the local fallback inbox item for one issue read. Does not mutate server receipts. |
| `GET` | `/api/inbox/stream` | Stream inbox update events over server-sent events. |
| `GET` | `/api/active-work` | List recent issue work for the shell sidebar. |
| `GET` | `/api/agents` | List managed agent profiles. |
| `POST` | `/api/agents` | Create a new Codex-backed agent profile. |
| `PUT` | `/api/agents/{agentID}` | Update a managed agent profile. |
| `GET` | `/api/clusters` | List reusable test cluster configs. |
| `POST` | `/api/clusters` | Create a reusable test cluster config. |
| `GET` | `/api/clusters/discover-defaults` | Discover selectable kubeconfig candidates and contexts under `~/.kube` without importing them. |
| `POST` | `/api/clusters/import` | Import selected kubeconfig file paths and check API reachability. |
| `POST` | `/api/clusters/import-defaults` | Scan `~/.kube`, import kubeconfig contexts, and check API reachability. |
| `PUT` | `/api/clusters/{clusterID}` | Update a reusable test cluster config. |
| `DELETE` | `/api/clusters/{clusterID}` | Delete a cluster config when no project or test env references it. |
| `GET` | `/api/projects` | List projects with issue/session counts. |
| `POST` | `/api/projects` | Create a project. |
| `PUT` | `/api/projects/{projectID}` | Update a project. |
| `DELETE` | `/api/projects/{projectID}` | Delete a project and cascaded child data. |
| `GET` | `/api/issue-label-definitions` | List built-in issue label options for Type and Priority controls. |
| `POST` | `/api/attachments` | Upload one issue/comment image attachment as multipart form field `file`. |
| `GET` | `/api/attachments/{attachmentID}` | Serve a stored image attachment by id. |
| `GET` | `/api/issues` | List issues across the local workspace. |
| `POST` | `/api/issues` | Create an issue and inbox item, optionally including creator display name and avatar snapshots. |
| `GET` | `/api/issues/{issueID}` | Load issue detail, comments, sessions, and evidence. |
| `PUT` | `/api/issues/{issueID}` | Update issue title, body, or status. Human auth is required for title/body and top-level `closed`; scoped agent auth may update allowed workflow states and child task completion. |
| `POST` | `/api/issues/{issueID}/tasks` | Create a child issue task under a parent issue. |
| `DELETE` | `/api/issues/{issueID}/tasks/{taskID}` | Delete a child issue task after verifying it belongs to the parent issue. |
| `PUT` | `/api/issues/{issueID}/labels` | Replace issue-local labels. |
| `POST` | `/api/issues/{issueID}/comments` | Add a human comment, optionally including author display name and avatar snapshots. |
| `POST` | `/api/issues/{issueID}/assign-agent` | Queue a Codex turn from an issue comment. |
| `POST` | `/api/issues/{issueID}/sessions` | Queue and start a local agent session. |
| `POST` | `/api/issues/{issueID}/test-deploy` | Manually queue an issue-scoped test deployment agent turn. |
| `POST` | `/api/issues/{issueID}/test-environment/cleanup` | Manually queue an agent turn to clean the issue test namespace. |
| `POST` | `/api/issues/{issueID}/test-environment/retain` | Record that the issue test namespace should be retained. |
| `GET` | `/api/sessions/{sessionID}` | Load session detail, logs, evidence, and workspace snapshot. |
| `POST` | `/api/sessions/{sessionID}/cancel` | Cancel a running session. |
| `POST` | `/api/sessions/{sessionID}/cleanup` | Remove a retained, non-active local session worktree. |
| `GET` | `/api/sessions/{sessionID}/stream` | Server-sent events for session logs and status changes. |

## Architecture Summary

mspace should separate the control plane, collaboration layer, runtime layer, and validation layer.

The control plane is the durable multiplayer authority: users, workspaces, members, auth sessions, GitHub identity, and future GitHub App installations. The collaboration layer is the product entry point: Inbox, Issue, comments, subscribers, agent sessions, and evidence. The runtime layer is where the agent edits and runs code. The validation environment layer is where the changed project gets deployed and inspected. In the MVP, the runtime should be local-first and the validation environment should be namespace-scoped Kubernetes.

Identity boundary:

- server-side users, workspaces, memberships, GitHub identities, and auth sessions are authoritative;
- desktop stores only the current bearer token and a lightweight display identity cache;
- runner `creator_name`, `creator_avatar_url`, `author_name`, and `author_avatar_url` are local MVP display snapshots, not a parallel account system;
- future shared issue ownership, comments, audit, and collaboration permissions should move behind the control plane.

```text
Desktop / Web UI
  -> mspace control plane
      -> Identity / Workspace / Membership
      -> GitHub identity and future GitHub App installation state
      -> Collaboration API
          -> Inbox Review / Issue Service
          -> Session Service
          -> Runtime Registry
              -> Local Runner Client
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

The control plane implements `workspaces` and `workspace_members`. The local runner still behaves as a single local workspace for issue/session data until those collaboration APIs move behind the server.

### Inbox Event

An Inbox Event is the append-only review fact for issue activity. A user's Inbox is built from `issue_event_receipts`, not from a global issue-level unread flag.

Required fields:

- linked workspace;
- linked issue;
- actor user when the event was created by a human;
- event kind;
- summary;
- structured payload snapshot;
- created time.

Receipt fields:

- linked event;
- linked workspace and issue;
- recipient user;
- state (`unread`, `read`, or `archived`);
- read/archive timestamps.

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
- default cluster id;
- runbook status, source, source session id, and updated time through `project_runbooks`;
- legacy deploy and validation command columns remain in SQLite for compatibility, but they are no longer user-facing configuration fields;
- legacy Kubernetes context, kubeconfig path, namespace, registry, preview domain, ingress class, and node host fields for compatibility.

### Cluster

A Cluster is reusable access to a shared Kubernetes test cluster.

Current implemented fields:

- name;
- kubeconfig path;
- optional Kubernetes context;
- image registry prefix;
- default exposure mode (`nodeport` or `ingress`);
- optional NodePort host;
- optional preview domain;
- optional ingress class;
- status (`configured`, `ready`, or `unreachable`);
- last checked time;
- project and test-environment reference counts.

### Issue

An Issue is the durable collaboration document for one unit of work.

Current implemented fields:

- project;
- title;
- body;
- status;
- parent issue id and sort order for inline child issue task lists;
- labels;
- creator display name and avatar snapshot;
- assignee;
- assignee type;
- comments and progress updates;
- linked sessions;
- environment evidence;
- optional issue test environment record.

### Issue Test Environment

An Issue Test Environment is the per-issue Kubernetes validation record.

Current implemented fields:

- issue id;
- selected cluster id;
- issue namespace;
- namespace status;
- cleanup status;
- preview URL;
- image registry prefix;
- kubeconfig path;
- Kubernetes context;
- exposure mode;
- preview domain;
- ingress class;
- NodePort host;
- last deploy session id;
- last cleanup session id.

### Agent Session

An Agent Session is one agent run attached to one issue.

Current implemented fields:

- issue;
- provider;
- agent profile;
- runtime mode;
- command;
- branch;
- worktree path;
- status;
- cleanup status and cleaned timestamp;
- terminal stream;
- evidence summary.

### Namespace Policy

There are three policies in the product model:

- project namespace: one long-lived namespace per project;
- issue namespace: one test namespace per issue;
- session namespace: one temporary namespace per agent runtime session.

The current local MVP uses one issue test namespace when the user manually triggers deployment. Session namespace becomes relevant later for Kubernetes-hosted agent runtimes. Project namespace remains a compatibility fallback for older project-level validation fields; current project operation knowledge belongs in the mspace runbook.

### Runtime Provider

A Runtime Provider starts the actual agent environment.

Provider options in the product model:

- local runtime for development and bring-your-own CLI operation;
- remote runtime for hosted or cluster-adjacent execution only when it preserves the same operational contract;
- DevBox-like runtime if available internally;
- future Kubernetes-hosted runtime when the product grows into that model;
- local daemon bridge only as an adapter, not as the primary product model.

The first production-grade MVP path should be local runtime because it keeps iteration simple and matches the current intended workflow.

The implemented MVP uses a desktop-managed local runner process, not a long-running daemon. The runner owns SQLite, HTTP APIs, session process execution, log capture, git worktree preparation, and issue test-environment bookkeeping.

### Validation Environment

A Validation Environment is where the changed project is deployed and inspected.

Initial environment options:

- Kubernetes namespace in a shared test cluster;
- future local container or ephemeral environment only if it preserves enough realism for the product.

The first serious environment should be Kubernetes because the product value depends on real environment isolation, scoped cluster access, runtime evidence, and a team-accessible preview URL.

### Kubernetes-First Validation Principle

Kubernetes should be visible as a core design assumption in the validation layer:

- project setup includes kubeconfig, image registry, and preview routing defaults;
- issue test deployment reserves one namespace per issue and lets the agent create it;
- issue evidence assumes pod, event, rollout, and ingress data are available;
- cleanup assumes namespace or workload lifecycle management.

Other validation targets are adapters around this core shape, not peers that redefine the product.

## Kubernetes Permission Model

Current MVP behavior:

- the user supplies kubeconfig access through the selected Cluster;
- the deploy/test agent uses that kubeconfig directly;
- the prompt restricts the agent to the issue namespace and requires it to report preview URL, namespace, image reference, validation result, and blockers;
- namespace cleanup is manually triggered from Issue Detail.

Target hardened behavior:

Each session that can deploy or inspect the Kubernetes environment gets a dedicated ServiceAccount or equivalent scoped kubeconfig.

Allowed by default:

- get/list/watch pods, services, endpoints, deployments, statefulsets, jobs, configmaps, events, ingress resources;
- get pod logs;
- create/update/patch/delete namespaced workloads only inside the assigned namespace when write mode is enabled;
- rollout-related operations through patch/apply equivalents.

Denied by default:

- cluster-scoped writes;
- namespace create/delete by the agent itself in the hardened target model;
- secrets read;
- node access;
- persistent volume cluster operations;
- cross-namespace reads unless explicitly granted.

In the hardened target model, the mspace controller, not the agent, creates namespaces, RoleBindings, quotas, and cleanup jobs.

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
  -> user writes an issue comment with an enabled @agent mention
  -> desktop saves the comment through POST /api/issues/{issueID}/comments
  -> desktop calls POST /api/issues/{issueID}/assign-agent with the comment text as command
  -> runner marks the issue assigned to an agent and creates a queued session
  -> runner loads the agent profile and instructions from agent_profiles
  -> runner stores queued session in SQLite
  -> runner plans workdir under ~/.mspace/workdirs/<project-id>/<session-id>
  -> runner creates a git worktree from the project's default branch
  -> runner checks out the session branch
  -> runner writes ~/.mspace/workdirs/_contexts/<session-id>.md
  -> runner injects the current project runbook from project_runbooks when one exists
  -> runner starts codex app-server --listen stdio:// inside the session worktree
  -> runner sends initialize, thread/start, and turn/start
  -> runner stores codex_thread_id and codex_turn_id
  -> runner maps app-server notifications into session_logs and agent_status
  -> desktop watches GET /api/sessions/{sessionID}/stream
  -> desktop refreshes Issues and Issue Detail from runner state
  -> Inbox refreshes server receipts when signed in, with GET /api/inbox/stream as local fallback invalidation
  -> session detail reads git status, commits, diff, and base comparison from workdir
```

Codex sessions receive the selected managed agent profile first, then the current turn request, followed by the issue, comments, prior sessions, project metadata, project runbook, branch, worktree, Kubernetes context, namespace, and the generated context markdown path in the turn prompt. The current turn request is treated like a Multica-style triggering comment so Codex does not confuse a follow-up question with the original issue body. Non-Codex providers can still use the older shell-command path as a compatibility adapter.

The local Codex thread currently uses `approvalPolicy: never` and `sandbox: danger-full-access`. This matches the unattended local-session shape and avoids hidden approval hangs while mspace lacks an approval UI. The tradeoff is that sessions should only be launched for trusted local repositories and reviewed through the retained worktree and logs.

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
  -> Local worktree is retained or explicitly cleaned up
  -> Future session namespace is retained or cleaned up by policy
```

## Data Model Sketch

### Current SQLite Schema

The local MVP uses these SQLite tables from `runner/migrations/001_init.sql`:

```text
clusters
  id
  name
  kubeconfig_path
  kube_context
  image_registry_prefix
  exposure_mode
  node_host
  preview_domain
  ingress_class
  status
  last_checked_at
  created_at
  updated_at

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
  kubeconfig_path
  namespace
  image_registry_prefix
  preview_domain
  ingress_class
  node_host
  default_cluster_id
  created_at
  updated_at

project_runbooks
  project_id
  content
  status
  source
  source_session_id
  content_hash
  created_at
  updated_at

project_runbook_revisions
  id
  project_id
  session_id
  author_type
  author_name
  content
  content_hash
  status
  created_at

issues
  id
  project_id
  parent_issue_id
  sort_order
  title
  body
  status
  triage_status
  assignee
  assignee_type
  creator_name
  creator_avatar_url
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
  author_name
  author_avatar_url
  body
  created_at

issue_attachments
  id
  issue_id
  comment_id
  filename
  content_type
  size_bytes
  storage_backend
  storage_key
  content
  created_at
  updated_at
  bound_at

issue_label_definitions
  id
  key
  name
  dimension
  color
  sort_order
  built_in
  created_at
  updated_at

issue_labels
  id
  issue_id
  label_id
  name
  color
  created_at

agent_profiles
  id
  name
  mention
  provider
  description
  instructions
  enabled
  built_in
  sort_order
  created_at
  updated_at

agent_sessions
  id
  issue_id
  provider
  agent_profile
  runtime_mode
  command
  status
  branch
  workdir
  codex_thread_id
  codex_turn_id
  agent_status
  artifact_dir
  source_session_id
  source_commit_sha
  agent_token
  cleanup_status
  cleaned_at
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

issue_change_nodes
  id
  issue_id
  session_id
  commit_sha
  branch
  subject
  files_changed
  created_at

session_review_evidence
  id
  issue_id
  session_id
  source_session_id
  source_commit_sha
  branch
  agent_summary
  commands_json
  tests_json
  build_result_json
  deployment_result_json
  risks_json
  follow_ups_json
  preview_url
  cluster
  namespace
  namespace_status
  cleanup_status
  created_at
  updated_at

issue_test_environments
  issue_id
  namespace
  namespace_status
  cleanup_status
  preview_url
  cluster_id
  image_registry_prefix
  kubeconfig_path
  kube_context
  exposure_mode
  preview_domain
  ingress_class
  node_host
  last_deploy_session_id
  last_cleanup_session_id
  created_at
  updated_at
```

### Target Product Schema

```text
workspaces
  id
  name
  slug
  runtime_policy

issue_events
  id
  workspace_id
  issue_id
  actor_user_id
  kind
  summary
  payload
  created_at

issue_event_receipts
  event_id
  workspace_id
  issue_id
  user_id
  state
  read_at
  archived_at
  created_at
  updated_at

issue_watchers
  id
  workspace_id
  issue_id
  user_id
  reason
  muted
  created_at
  updated_at

projects
  id
  workspace_id
  name
  repo_url
  default_branch
  cluster_ref
  namespace_policy
  runbook_ref

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

agent_profiles
  id
  name
  mention
  provider
  description
  instructions
  enabled
  built_in
  sort_order
  created_at
  updated_at

comments
  id
  issue_id
  type
  author_type
  author_id
  author_display_name
  author_avatar_url
  body
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

Shows unread review events for the current user. When signed in, the list comes from control-plane issue-event receipts and is marked read through the server read-through endpoint. Local runner inbox rows remain as a fallback for unsigned-in or not-yet-reported issue updates.

### Issues

Shows document-style issue pages with discussion, session history, and evidence.

### Projects

Shows configured repositories, runbook status, active issue and session count, and a quiet icon-only settings action.

### Project Settings

Shows:

- project name;
- repository metadata;
- default cluster;
- mspace-owned runbook edited as Markdown;
- delete action, disabled once issues or sessions exist.

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
2. Issue detail page with comments, managed agent turns, and session list.
3. Session creation from an enabled agent mention with one runtime provider.
4. Local runtime implementation for the first agent provider.
5. Kubernetes validation environment with scoped kubeconfig and ServiceAccount generation.
6. Terminal/progress stream.
7. Namespace resource viewer and environment evidence capture.
8. Session cleanup and retention rules for local worktrees first, then namespaces.
