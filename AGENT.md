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
- UI system: shadcn/ui source components in `packages/ui/src/components/ui`, Radix UI primitives, lucide-react icons, and shared exports through `@mspace/ui`.
- Workspace tooling: pnpm workspaces and Turbo.
- Runner: Go, chi, SQLite through `modernc.org/sqlite`.
- The Electron main process auto-starts the local runner with `go run .` unless `GET /health` is already healthy.
- Sidebar navigation currently exposes Inbox, Issues, Agents, Clusters, and Projects, plus a quick link that opens issue creation from the left rail and an Active work block for recent issue/session/test-environment activity.
- Inbox is a review-only unread feed; issue creation and management live in the Issues route.
- SQLite database path: `~/.mspace/mspace.db`.
- Imported GitHub repositories are cloned or reused under `~/.mspace/repos/<owner>/<repo>`.
- Session worktree root: `~/.mspace/workdirs/<project-id>/<session-id>`.
- Session context markdown is written to `~/.mspace/workdirs/_contexts/<session-id>.md`.
- The runner stores the real worktree path in `agent_sessions.workdir`.
- Codex sessions start `codex app-server --listen stdio://` in the prepared worktree instead of shelling out through `codex exec`.
- The runner sends `initialize`, `thread/start`, and `turn/start` over newline-delimited JSON-RPC on stdio, then maps app-server notifications into `session_logs`.
- The runner persists `agent_profile`, `codex_thread_id`, `codex_turn_id`, `agent_status`, `artifact_dir`, `cleanup_status`, and `cleaned_at` on `agent_sessions`.
- Agent definitions live in `agent_profiles`; defaults are seeded for `@codex`, `@bugfix`, and `@design`, but the Issue composer and runner resolve profiles from SQLite instead of hardcoded frontend constants.
- `POST /api/sessions/{sessionID}/cleanup` removes retained, non-active local session worktrees after validating the path stays under `~/.mspace/workdirs`; session logs, comments, evidence, and metadata remain in SQLite.
- Session branches default to `mspace/<issue-short-id>/<session-short-id>`.
- Project creation supports either a desktop folder picker for local repositories or a GitHub repository URL that is cloned into the local cache.
- Local project imports auto-detect remote metadata when a git remote exists and persist `source_type`, `remote_url`, `git_provider`, `git_owner`, and `git_repo`.
- Inbox invalidation is driven by `GET /api/inbox/stream`, and the issue detail screen starts Codex by saving an agent-mention comment before calling `POST /api/issues/{issueID}/assign-agent`.
- Supported Codex-backed agents are managed from the Agents route. They share the app-server runtime and differ by stored `agent_profile` prompt instructions.
- Issue labels are issue-local records in `issue_labels` and should stay lightweight until a global label taxonomy is truly needed.
- Reusable cluster configs live in `clusters`; they store kubeconfig path, optional context, registry prefix, exposure defaults, readiness status, and last check time.
- The Clusters route imports kubeconfig files through the desktop file picker. On first empty-state entry, it discovers regular files under `~/.kube`, lists candidates and contexts, and lets the user choose which files to import.
- Kubeconfig import creates one cluster per context and marks it `ready` or `unreachable` after a read-only `kubectl get --raw=/version` check. Unreachable clusters remain editable.
- Projects store `default_cluster_id`; issue test deployments can override cluster and exposure mode per run.
- Each issue can have one `issue_test_environments` record with cluster id, issue namespace, namespace state, cleanup state, preview URL, deploy/cleanup session ids, registry, kubeconfig, context, exposure mode, domain, ingress class, and NodePort host.
- Deploy/test sessions receive resolved Kubernetes and preview values through `KUBECONFIG`, `MSPACE_KUBECONFIG`, `MSPACE_CLUSTER_ID`, `MSPACE_KUBE_CONTEXT`, `MSPACE_KUBE_NAMESPACE`, `MSPACE_TEST_NAMESPACE`, `MSPACE_IMAGE_REGISTRY_PREFIX`, `MSPACE_EXPOSURE_MODE`, `MSPACE_PREVIEW_DOMAIN`, `MSPACE_INGRESS_CLASS`, and `MSPACE_NODE_HOST`.
- Sessions also receive `MSPACE_ISSUE_ID`, `MSPACE_SESSION_ID`, `MSPACE_AGENT_PROFILE`, `MSPACE_SESSION_BRANCH`, `MSPACE_SESSION_WORKDIR`, `MSPACE_SESSION_CONTEXT`, and `MSPACE_SESSION_ARTIFACT_DIR`.
- Tailwind CSS 4 scans monorepo UI packages through `@source` entries in `apps/desktop/src/renderer/src/globals.css`.
- shadcn/ui semantic tokens are mapped to the Notion-like mspace palette through `@theme inline` in `apps/desktop/src/renderer/src/globals.css`.
- Vite resolves shadcn aliases through `apps/desktop/electron.vite.config.ts`: `@mspace/ui/components`, `@mspace/ui/lib`, and `@mspace/ui`.

## Working Rules

- Keep Inbox and Issue objects as first-class product objects.
- Keep Inbox review-only. New issue creation belongs in the Issues flow, not in Inbox.
- Keep local development runtime as the MVP default.
- For Codex-backed local sessions, prefer `codex app-server --listen stdio://` over `codex exec` so mspace can retain thread, turn, status, and notification state.
- Keep agent specialization as SQLite-managed profile instructions on top of the Codex app-server provider unless a genuinely separate runtime is introduced.
- Keep Kubernetes as the default deployment and test environment.
- Do not rely on Sealos UI APIs as the primary control path.
- Prefer namespace-scoped operations and explicit RBAC.
- Treat `kubectl` as acceptable for prototypes, but prefer structured Kubernetes APIs for durable product logic.
- Do not design cluster-wide agent permissions.
- Do not let agents read Secrets by default.
- Every write-capable session must have an audit trail and a cleanup path.
- Do not fork Multica; use it as a structural reference only.
- Keep docs current when product decisions change.
- Use the shadcn CLI for new shared UI primitives when an equivalent shadcn component exists, then wrap or re-export from `@mspace/ui` as needed.
- Preserve the quiet Notion-like workspace style: document-first, low-contrast paper surfaces, compact rows, restrained icon buttons, and no decorative dashboard or marketing layout.
- Treat `DESIGN.md` as the first reference for UI style, tokens, component rules, and visual guardrails.
- Never execute database write operations unless the user explicitly asks for database modification.
- Do not delete session workdirs or git worktrees unless the user explicitly asks for cleanup through the product or the current conversation.

## Local Commands

```bash
pnpm install
pnpm dev:desktop
```

The desktop app normally starts the runner automatically on `127.0.0.1:7788`.
If a healthy old runner is already listening, Electron reuses it. After API route changes, restart that runner before judging frontend errors.

Debug the runner separately:

```bash
pnpm runner
pnpm dev:desktop
```

Validation:

```bash
pnpm typecheck
pnpm --filter @mspace/desktop build
(cd packages/ui && pnpm dlx shadcn@latest info --json)
(cd runner && go test ./...)
(cd runner && go build ./...)
```

## Documentation Map

- `DESIGN.md`: design system reference for visual thesis, tokens, typography, components, layout, and UI guardrails.
- `ROADMAP.md`: milestone priority and acceptance criteria for the next product work.
- `docs/product.md`: inbox and issue product positioning, users, workflows, MVP, non-goals.
- `docs/architecture.md`: collaboration layer, runtime layer, permission model, data sketch, risks.
- `docs/integration-guide.md`: local runner API contract, cluster import calls, and issue test environment calls.
- `docs/ia.md`: MVP navigation, screen map, page regions, state model, build sequence.
- `docs/references.md`: notes from Multica and Optio references.
- `docs/runbook.md`: local run, data paths, smoke checks, and troubleshooting.

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
- Inbox Item: triage unit for incoming work.
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
