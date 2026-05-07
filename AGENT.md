# AGENT.md

This project is the runnable local MVP workspace for mspace.

## Product Direction

mspace is an Inbox and Issue workspace for coding agents. It lets a team manage work as document-style issues, run development sessions locally in the current phase, and deploy or validate those changes in a real namespace-scoped Kubernetes environment.

The product should stay narrow:

- interaction inspiration: Multica-style inbox, issue, and teammate workflow;
- technical inspiration: Optio-style Kubernetes runtime and git worktree isolation;
- core difference: document-first issue collaboration with attachable real test environments for coding agents.

## Current Implementation

- Desktop app: Electron, electron-vite, React 19, React Router 7, React Query 5, Tailwind CSS 4, TypeScript.
- UI system: shadcn/ui source components in `packages/ui/src/components/ui`, Radix UI primitives, lucide-react icons, and shared exports through `@mspace/ui`.
- Workspace tooling: pnpm workspaces and Turbo.
- Runner: Go, chi, SQLite through `modernc.org/sqlite`.
- The Electron main process auto-starts the local runner with `go run .` unless `GET /health` is already healthy.
- SQLite database path: `~/.mspace/mspace.db`.
- Session worktree root: `~/.mspace/workdirs/<project-id>/<session-id>`.
- The runner stores the real worktree path in `agent_sessions.workdir`.
- Session branches default to `mspace/<issue-short-id>/<session-short-id>`.
- Project Kubernetes context and namespace are passed into sessions as `MSPACE_KUBE_CONTEXT` and `MSPACE_KUBE_NAMESPACE`.
- Tailwind CSS 4 scans monorepo UI packages through `@source` entries in `apps/desktop/src/renderer/src/globals.css`.
- shadcn/ui semantic tokens are mapped to the Notion-like mspace palette through `@theme inline` in `apps/desktop/src/renderer/src/globals.css`.
- Vite resolves shadcn aliases through `apps/desktop/electron.vite.config.ts`: `@mspace/ui/components`, `@mspace/ui/lib`, and `@mspace/ui`.

## Working Rules

- Keep Inbox and Issue objects as first-class product objects.
- Keep local development runtime as the MVP default.
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
- Do not delete session workdirs or git worktrees unless the user explicitly asks for cleanup.

## Local Commands

```bash
pnpm install
pnpm dev:desktop
```

The desktop app normally starts the runner automatically on `127.0.0.1:7788`.

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
- `docs/product.md`: inbox and issue product positioning, users, workflows, MVP, non-goals.
- `docs/architecture.md`: collaboration layer, runtime layer, permission model, data sketch, risks.
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
- Validation Environment: where the changed project is deployed and tested.
- Project Namespace: a long-lived namespace owned by one project.
- Session Namespace: a temporary namespace owned by one agent session.
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
