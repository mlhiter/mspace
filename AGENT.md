# AGENT.md

This project is the runnable local MVP workspace for mspace.

## Product Direction

mspace is an Inbox and Issue workspace for coding agents. It lets a team manage work as document-style issues, run development sessions locally in the current phase, and deploy or validate those changes in a real namespace-scoped Kubernetes environment.

The product should stay narrow:

- interaction inspiration: Multica-style inbox, issue, and teammate workflow;
- technical inspiration: Optio-style Kubernetes runtime and git worktree isolation;
- core difference: document-first issue collaboration with attachable real test environments for coding agents.

## Current Implementation

- Desktop app: Electron, electron-vite, React 19, TanStack Router, React Query 5, Tailwind CSS 4, TypeScript.
- Public website: Vite, React 19, Tailwind CSS 4, and lucide-react in `apps/website`, deployed through Vercel with the root `vercel.json`. It has a homepage plus a static `Changelog` navigation view backed by `apps/website/src/changelog.ts`; the nav logo uses `apps/website/src/assets/mspace-mark-transparent.png` inside its own gray-white tile.
- UI system: shadcn/ui source components in `packages/ui/src/components/ui`, Radix UI primitives, lucide-react icons, Material Icon Theme file icons for file surfaces, and shared exports through `@mspace/ui`.
- Workspace tooling: pnpm workspaces and Turbo.
- Server control plane: Go, chi, PostgreSQL through `pgx`, with embedded migrations under `server/internal/control/migrations`.
- Runner: Go, chi, SQLite through `modernc.org/sqlite`.
- The control plane owns users, workspaces, workspace membership, mspace auth sessions, GitHub identity, and future GitHub App installation state. Desktop and runner code should become runtime clients of this service instead of owning collaboration identity.
- Desktop GitHub sign-in starts at `GET /api/auth/github/start`, opens the returned GitHub URL in the browser, lets the server-side callback complete OAuth, then polls `GET /api/auth/github/result?state=...` for a single-use `msp_...` session token. Do not move the GitHub client secret into Electron or the runner.
- The renderer stores the mspace session token under `localStorage["mspace.authToken"]` and a lightweight display identity under `localStorage["mspace.authIdentity"]`; the latter is used only to populate local runner `creator*` and `author*` fields until collaboration state moves fully behind the control plane.
- The Electron main process auto-starts the local runner with `go run .` unless `GET /health` is healthy and advertises the expected runner protocol capabilities. In dev, a stale local runner on `127.0.0.1:7788` is replaced before startup continues.
- After migrations, runner startup reconciles any persisted `agent_sessions` left in `queued` or `running` by a previous runner process. These cannot be resumed because the in-memory canceller and Codex app-server process are gone, so the runner marks them `failed` with `agent_status='interrupted'`, writes a system log/comment, moves the issue to `blocked`, and moves the linked issue test environment from `deploying`/`cleanup_requested` to `deploy_interrupted`/`cleanup_failed`.
- Sidebar navigation currently exposes Inbox, Issues, Agents, Clusters, and Projects, plus a workspace menu entry for Workspace Settings, a global search / Command+K palette for issues and projects, a quick link that opens issue creation from the left rail, and an Active work block for recent issue/session/test-environment activity.
- Inbox is a review-only feed for per-user team issue events. The server control plane stores `issue_events`, `issue_event_receipts`, and `issue_watchers`; the local runner `inbox_items.unread` path remains a fallback until issue collaboration fully moves behind `server/`.
- The issue creation modal, Issue Detail reply composer, Project runbook editor, and Issue Detail runbook modal use `IssueDocumentEditor`, a TipTap-backed Markdown editor/viewer in `packages/views/src/issue-document-editor.tsx`, so document writing stays rich while the runner still receives Markdown. Use the `runbook-viewer` read-only variant for Issue Detail runbook display; do not fall back to ReactMarkdown or the editable runbook shell there. Issue/comment editors support direct image upload, paste, and drop through stable `/api/attachments/<id>` Markdown image URLs backed by runner `issue_attachments` rows and rendered as thumbnail node views. Issue creation does not expose a project selector; mspace infers the project from the note.
- Issue task lists are modeled as child issues via `issues.parent_issue_id`, then rendered inline on the parent Issue Detail page. Markdown checklist lines typed during issue creation are converted into child issues so the checkbox text is not a second source of truth.
- Local runner issues store denormalized display fields `creator_name` and `creator_avatar_url`; comments store `author_user_id`, `author_name`, `author_avatar_url`, `updated_at`, and `edited_at`; `comment_reactions` stores lightweight human reaction rows and aggregated per-comment summaries without changing Markdown comment bodies or agent prompt history. Existing anonymous human rows are backfilled to `mlhiter`; ordinary system comments display as `mspace`. Status-transition comments should be stored with the actor that made the change so the timeline says the human or agent performed the update, not `mspace`.
- Only the latest human-authored issue comment may be edited, and only before an agent session has consumed it. Agent-triggering sessions store `agent_sessions.trigger_comment_id` so editing cannot rewrite the prompt history of an already queued, running, completed, failed, or cancelled turn.
- Human owners and comments should render with the real stored avatar/name when available. Codex-backed agents should render with the shared Codex avatar asset from `packages/views/src/agent-avatar.ts`, not a generic robot icon, unless image loading genuinely fails.
- Renderer CSP in `apps/desktop/src/renderer/index.html` must allow local runner image origins (`http://127.0.0.1:*` and `http://localhost:*`) so attachment thumbnails can load.
- SQLite database path: `~/.mspace/mspace.db`; local MVP issue images are stored as `sqlite_blob` attachment rows so issue Markdown does not depend on loose local files.
- Imported GitHub repositories are cloned or reused under `~/.mspace/repos/<owner>/<repo>`.
- Session worktree root: `~/.mspace/workdirs/<project-id>/<session-id>`.
- Session context markdown is written to `~/.mspace/workdirs/_contexts/<session-id>.md`.
- The runner stores the real worktree path in `agent_sessions.workdir`.
- Codex sessions start `codex app-server --listen stdio://` in the prepared worktree instead of shelling out through `codex exec`.
- The runner sends `initialize`, `thread/start`, and `turn/start` over newline-delimited JSON-RPC on stdio, then maps app-server notifications into `session_logs`.
- The runner persists `agent_profile`, `codex_thread_id`, `codex_turn_id`, `agent_status`, `artifact_dir`, `trigger_comment_id`, `agent_token`, `cleanup_status`, and `cleaned_at` on `agent_sessions`.
- Normal source-code agent sessions are captured as `issue_change_nodes` when they finish with git changes. If the session worktree already has an ahead-of-base commit, the runner records that HEAD commit instead of creating an extra commit. Otherwise it stages the worktree, excludes `.mspace` artifacts, retries transient `.git/index.lock` write conflicts during `git add`, `git reset`, and `git commit`, creates the captured source commit, and exposes commit metadata plus diff preview from Issue Detail.
- Review evidence is captured separately in `session_review_evidence`. Keep code diffs and changed files in the Commits tab; the Evidence tab is for compact commands run, tests, build/deploy results, Kubernetes resources, preview URL, agent summary, risks/follow-ups, and cleanup/retain state tied to the session and source commit. `commands_json` should contain review-worthy evidence, not the full raw command trail; exploratory output stays in `session_logs`.
- Issue Detail keeps the right metadata sidebar only on Overview. Commits, Sessions, Resources, and Evidence should render as full-width review surfaces inside the page frame so diffs, session paths, live namespace resources, commands, and Kubernetes evidence are not squeezed.
- Branch / PR handoff is captured in `issue_handoffs` as an issue-level delivery artifact: one issue should have one current PR, while commits are evidence/source context for that PR. The local MVP uses the user's current git, `gh`, and `gitleaks` identity to push the selected issue source branch, run a local secret scan, create the PR or auto-detect an existing PR for that branch, refresh PR state, and include issue metadata, preview URL, commit list, and review evidence summary in the PR body. Source commit capture is always on; Workspace Settings can opt into automatic draft PR creation after source commit capture. The productized direction is a server-owned GitHub App installation token, not a required standalone agent GitHub account.
- Failures are captured as `session_failures`, linked to the issue, session, optional deployment evidence, and optional review evidence. Failed sessions and failed deploy/preview/cleanup reconciliation should preserve the failed command, compact error summary/excerpt, phase (`build`, `test`, `image_push`, `pod_startup`, `network_exposure`, `preview_probe`, `agent_interrupted`, or `cleanup`), namespace/resource hints, and continue/retry/stop affordances in Issue Detail. Do not add an issue-level `failed` status for this; keep the issue in a handoff state such as `blocked`.
- Changed-file chips and rows use `packages/views/src/file-type-icon.tsx`, backed by `material-icon-theme`, so file surfaces match IDE-style file type icons. Directory-only placeholder changes such as `.mspace/` are filtered from changed-file displays; concrete files under those directories still show normally.
- Agent definitions live in `agent_profiles`; defaults are seeded for internal `@triage` plus user-facing `@codex`, `@bugfix`, and `@design`, but the Issue composer and runner resolve profiles from SQLite instead of hardcoded frontend constants.
- `POST /api/sessions/{sessionID}/cleanup` removes retained, non-active local session worktrees after validating the path stays under `~/.mspace/workdirs`; session logs, comments, evidence, and metadata remain in SQLite.
- Session branches default to `mspace/<issue-short-id>/<session-short-id>`.
- Project creation supports either a desktop folder picker for local repositories or a GitHub repository URL that is cloned into the local cache.
- Local project imports auto-detect remote metadata when a git remote exists and persist `source_type`, `remote_url`, `git_provider`, `git_owner`, and `git_repo`.
- Project runbooks are agent-discovered project memory stored in SQLite `project_runbooks` with revision history in `project_runbook_revisions`. Projects settings are a full page, not a modal; the runbook is edited through the TipTap Markdown editor, Issue Detail shows a compact sidebar entry that opens a TipTap read-only modal, and project rows use a quiet ghost gear icon for settings. Do not reintroduce install/test/build/deploy command form fields. Codex sessions receive the runbook as advisory context and can update it by writing `${MSPACE_SESSION_ARTIFACT_DIR}/project-runbook.md`.
- Inbox invalidation still listens to the local runner `GET /api/inbox/stream` for fallback data, while the signed-in team Inbox polls server receipt state. The renderer passes the current server token/workspace to the runner through `POST /api/control-plane/session`, and the runner reports reviewable issue events to the server when configured. Issue detail starts Codex by saving a new agent-mention comment, or by updating an eligible unconsumed comment to add an agent mention, before calling `POST /api/issues/{issueID}/assign-agent`.
- Local runner issue write APIs require `Authorization: Bearer ...`. Human writes use the mspace session token verified through control-plane `GET /api/auth/me` and workspace membership; agent writes use the scoped `MSPACE_AGENT_TOKEN` issued for that session. Display-only `creator*` and `author*` request fields must not be treated as authorization.
- Issue statuses are explicit handoff states: `open`, `needs_review`, `changes_requested`, `ready_for_test`, `blocked`, `cancelled`, and `closed`. `cancelled` means the issue is closed as not planned; it is not used for a stopped agent session. Transient progress belongs to `agent_sessions` and `issue_test_environments`, not issue status. Legacy `queued`, `running`, `in_progress`, `review`, `testing`, `test_in_progress`, `completed`, and `done` normalize into durable issue states, while existing test-data outcome statuses such as `test_passed`, `test_failed`, and `failed` are reset to `open` when the local database is upgraded. UI surfaces should show readable labels such as `Needs review`, `Ready for test`, and `Closed as not planned`, not raw underscored values.
- Supported Codex-backed agents are managed from the Agents route. They share the app-server runtime and differ by stored `agent_profile` prompt instructions.
- Issue labels use the built-in `issue_label_definitions` taxonomy and `issue_labels` links. The current dimensions are `type` and `priority`.
- New issues start with `triage_status=pending` unless a type is set manually. A background `@triage` Codex classifier assigns exactly one Conventional Commit type label asynchronously.
- Priority labels are manual only. Do not auto-classify priority.
- Reusable cluster configs live in `clusters`; they store kubeconfig path, optional context, registry prefix, exposure defaults, readiness status, and last check time.
- The Clusters route imports kubeconfig files through the desktop file picker. On first empty-state entry, it discovers regular files under `~/.kube`, lists candidates and contexts, and lets the user choose which files to import.
- Kubeconfig import creates one cluster per context and marks it `ready` or `unreachable` after a read-only `kubectl get --raw=/version` check. Unreachable clusters remain editable.
- Projects store `default_cluster_id`; issue test deployments can override cluster and exposure mode per run.
- Each issue can have one `issue_test_environments` record with cluster id, issue namespace, namespace state, cleanup state, preview URL, deploy/cleanup session ids, selected source session/commit, registry, kubeconfig, context, exposure mode, domain, ingress class, and NodePort host.
- Issue Detail has a Resources tab for live namespace inspection. It refreshes on entry and via a manual refresh button, calls `GET /api/issues/{issueID}/test-environment/resources`, and renders only the issue's fixed test namespace: Pods, Services, Deployments, Ingresses, and recent Events. Do not add frontend namespace input, cross-namespace browsing, Secrets, Nodes, or other cluster-wide inventory to this tab.
- Deploy/test reconciliation captures Kubernetes evidence, discovers preview candidates, checks the selected preview URL, updates the issue test environment to `active`, `preview_unverified`, `deploy_failed`, `deploy_interrupted`, `cleanup_failed`, or `cleaned` as appropriate, and records a `session_failures` row when the environment needs attention. A completed continuation session that writes `${MSPACE_SESSION_ARTIFACT_DIR}/test-environment.json` can also be adopted as the current deploy session for the issue.
- Preview verification should stay internal and background-driven. Issue Detail automatically refreshes preview status when an environment is present; do not expose a user-facing `Probe` button. The internal `/api/issues/{issueID}/test-environment/probe` route is a preview status check hook for Issue Detail, debugging, and automation only; it should update `issue_test_environments`/sidebar state, not create `deployment_evidence`, `session_review_evidence`, `session_failures`, or top-level issue status/timeline events.
- Deploy/test sessions receive resolved Kubernetes and preview values through `KUBECONFIG`, `MSPACE_KUBECONFIG`, `MSPACE_CLUSTER_ID`, `MSPACE_KUBE_CONTEXT`, `MSPACE_KUBE_NAMESPACE`, `MSPACE_TEST_NAMESPACE`, `MSPACE_IMAGE_REGISTRY_PREFIX`, `MSPACE_EXPOSURE_MODE`, `MSPACE_PREVIEW_DOMAIN`, `MSPACE_INGRESS_CLASS`, and `MSPACE_NODE_HOST`.
- Sessions also receive `MSPACE_API_BASE_URL`, `MSPACE_ISSUE_ID`, `MSPACE_SESSION_ID`, `MSPACE_AGENT_TOKEN`, `MSPACE_AGENT_PROFILE`, `MSPACE_SESSION_BRANCH`, `MSPACE_SESSION_WORKDIR`, `MSPACE_SOURCE_SESSION_ID`, `MSPACE_SOURCE_COMMIT_SHA`, `MSPACE_SESSION_CONTEXT`, and `MSPACE_SESSION_ARTIFACT_DIR`.
- Tailwind CSS 4 scans monorepo UI packages through `@source` entries in `apps/desktop/src/renderer/src/globals.css`.
- shadcn/ui semantic tokens are mapped to the Notion-like mspace palette through `@theme inline` in `apps/desktop/src/renderer/src/globals.css`.
- Vite resolves shadcn aliases through `apps/desktop/electron.vite.config.ts`: `@mspace/ui/components`, `@mspace/ui/lib`, and `@mspace/ui`.

## Working Rules

- Keep Inbox and Issue objects as first-class product objects.
- Keep Inbox read state per-user and event-based. Do not add new product behavior that depends on a global issue-level unread boolean.
- Keep task lists as inline child issue views, not Markdown checkbox state. Agents should create tasks through `POST /api/issues/{issueID}/tasks`, update task status through `PUT /api/issues/{taskID}`, and remove obsolete tasks through `DELETE /api/issues/{issueID}/tasks/{taskID}` when the local API base URL and `MSPACE_AGENT_TOKEN` are available.
- Humans do not choose general top-level workflow states from the Issue sidebar. The sidebar status is read-only; human lifecycle actions live inside the Issue Detail comment composer footer. Humans can close a top-level issue as `closed` or `cancelled`, and can reopen a closed/cancelled issue to `changes_requested`; they cannot move a post-open issue back to `open`. Keep the primary close/reopen action visible and hide less common close reasons such as `Close as not planned` behind a compact dropdown. Agents may move their scoped issue to `needs_review`, `ready_for_test`, or `blocked`, and may close child tasks under their assigned issue; every status transition should be recorded as an actor-authored issue timeline comment and displayed as a one-line status event with from/to badges.
- Stopping a queued or running session cancels only that `agent_sessions` row and records a compact, non-editable session event. It must not change the Issue status or treat the whole Issue as discarded. If the DB still says `running` but the current runner has no in-memory canceller, treat it as an orphaned session from a prior runner process: allow Stop to mark it `cancelled`, append an explanatory system log, and update the linked issue test environment out of active progress.
- Keep Inbox review-only. New issue creation belongs in the Issues flow, not in Inbox.
- Keep issue creation minimal: note only. Project routing is inferred from the issue text, type is classified asynchronously after creation, and priority is set manually from Issue Detail.
- Keep type triage asynchronous. Issue creation must not wait for agent classification.
- Do not use keyword matching for issue type classification; use the triage agent and validate its structured output against the fixed type set.
- Keep local development runtime as the MVP default.
- Keep project runbooks as the only user-facing place for project operation knowledge. Humans may edit the Markdown runbook; agents should discover and update it from real sessions. Do not expand Project settings with separate command inputs for install, test, build, image, deploy, or health checks.
- Keep multiplayer identity and collaboration authority in `server/`, not in the desktop renderer or local runner. The runner may keep local execution state, but users, workspaces, membership, GitHub identity, auth sessions, audit, and future GitHub App installation credentials belong to the control plane.
- Treat runner `creator_name`, `creator_avatar_url`, `author_name`, and `author_avatar_url` as transitional display snapshots for the local MVP, not the authoritative user model. Do not expand this into a parallel local account system.
- GitHub OAuth should prove identity only. The product session should be an mspace-issued token, and future GitHub repository automation should use GitHub App installation tokens owned by `server/`, not long-lived personal OAuth tokens stored by desktop/runner.
- For Codex-backed local sessions, prefer `codex app-server --listen stdio://` over `codex exec` so mspace can retain thread, turn, status, and notification state.
- Keep agent specialization as SQLite-managed profile instructions on top of the Codex app-server provider unless a genuinely separate runtime is introduced.
- Keep Kubernetes as the default deployment and test environment.
- Do not rely on Sealos UI APIs as the primary control path.
- Prefer namespace-scoped operations and explicit RBAC.
- Treat `kubectl` as acceptable for agent-run deploy prototypes, but prefer structured Kubernetes APIs for durable product logic. The Issue Resources API already uses Kubernetes `client-go` typed clients, not shelling out or ad hoc HTTP.
- Do not design cluster-wide agent permissions.
- Do not let agents read Secrets by default.
- Every write-capable session must have an audit trail and a cleanup path.
- Do not fork Multica; use it as a structural reference only.
- Keep docs current when product decisions change.
- Keep `apps/website/src/changelog.ts` current when a task ships meaningful product, engineering, documentation, or website progress. Group entries by calendar day and write public-facing bullets that explain what changed without exposing private environment details.
- Keep the desktop product shell quiet and document-first. The public website may use a bolder forensic brand direction, but it must still avoid generic AI-platform claims and stay grounded in issues, sessions, diffs, Kubernetes preview namespaces, evidence, and cleanup.
- Use the shadcn CLI for new shared UI primitives when an equivalent shadcn component exists, then wrap or re-export from `@mspace/ui` as needed.
- Preserve the quiet Notion-like workspace style: document-first, low-contrast paper surfaces, compact rows, restrained icon buttons, and no decorative dashboard or marketing layout.
- Treat `DESIGN.md` as the first reference for UI style, tokens, component rules, and visual guardrails.
- Never execute database write operations unless the user explicitly asks for database modification.
- Do not delete session workdirs or git worktrees unless the user explicitly asks for cleanup through the product or the current conversation.

## Local Commands

```bash
pnpm install
pnpm dev:desktop
pnpm dev:website
pnpm build:website
pnpm preview:website
pnpm run server
```

The desktop app normally starts the runner automatically on `127.0.0.1:7788`.
If an old runner is already listening but does not advertise the expected health capabilities, Electron treats it as stale and replaces it before judging frontend errors.
The desktop app also starts the local server control plane automatically on `127.0.0.1:8787` when no healthy server is already listening.

Debug the runner separately:

```bash
pnpm runner
pnpm dev:desktop
```

Validation:

```bash
pnpm typecheck
pnpm --filter @mspace/desktop build
pnpm test:server
(cd packages/ui && pnpm dlx shadcn@latest info --json)
(cd runner && go test ./...)
(cd runner && go build ./...)
```

## Documentation Map

- `DESIGN.md`: design system reference for visual thesis, tokens, typography, components, layout, and UI guardrails.
- `ROADMAP.md`: milestone priority and acceptance criteria for the next product work.
- `docs/product-value.md`: product value thesis, proof standard, and boundary between Codex wrapper risk and mspace's Kubernetes-backed issue workflow.
- `docs/product.md`: inbox and issue product positioning, users, workflows, MVP, non-goals.
- `docs/control-plane.md`: server/control-plane direction for multiplayer identity, GitHub auth, and runtime-client boundaries.
- `docs/architecture.md`: collaboration layer, runtime layer, permission model, data sketch, risks.
- `docs/integration-guide.md`: local runner API contract, cluster import calls, and issue test environment calls.
- `docs/ia.md`: MVP navigation, screen map, page regions, state model, build sequence.
- `docs/references.md`: notes from Multica and Optio references.
- `docs/runbook.md`: local run, data paths, smoke checks, and troubleshooting.
- `apps/website/README.md`: website scope, local commands, Vercel deployment, visual guardrails, and asset sources.
- `apps/website/TODO.md`: narrow website backlog; keep speculative website tasks here instead of `ROADMAP.md`.

## Current Non-Goals

- Generic AI agent platform.
- Generic DevOps troubleshooting chatbot.
- Agent skill/rule management product.
- Automatic merge pipeline.
- Cluster-wide Kubernetes assistant.
- Sealos API wrapper.
- Direct Multica code inheritance as the product baseline.
- Generated scoped kubeconfig and ServiceAccount lifecycle in the current local MVP.
- Kubernetes-hosted agent runtime in the current local MVP.

## Preferred Vocabulary

Use these terms consistently:

- Workspace: the team boundary for members, issues, agents, and runtime policy.
- Inbox Event: server-side review fact for issue activity; per-user receipts determine unread state.
- Inbox Item: local runner fallback triage row for incoming work.
- Project: a repository plus runtime policy.
- Issue: the durable collaboration document for one unit of work.
- Agent Session: one agent run attached to one issue.
- Issue Test Environment: the per-issue Kubernetes validation record and namespace lifecycle.
- Project Namespace: a long-lived namespace owned by one project.
- Issue Namespace: a namespace created and managed for one issue's test deployment.
- Runtime Provider: the mechanism that starts the agent workspace.
- Scoped Kubeconfig: kubeconfig bound to a session ServiceAccount and namespace policy.

## Product Taste

The UI should feel operational, document-first, and close to Notion's quiet workspace language. Avoid marketing-heavy pages, decorative dashboards, and abstract AI terminology. The first screen should help a developer answer:

- Which issues need attention now?
- Which agent sessions are attached to each issue?
- What is the agent doing now?
- Which runtime and namespace is it operating?
- Where is the environment?
- What branch or PR did it produce?
