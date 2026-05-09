<!-- prettier-ignore -->
<div align="center">

<img src="./apps/desktop/assets/brand/mspace-icon.png" alt="mspace logo" width="96" />

# mspace

**A local-first desktop workspace for coding agents, issue review, and Kubernetes validation evidence.**

![Status](https://img.shields.io/badge/status-local%20MVP-2d2926?style=flat-square)
![TypeScript](https://img.shields.io/badge/TypeScript-5.9-3178c6?style=flat-square&logo=typescript&logoColor=white)
![React](https://img.shields.io/badge/React-19-149eca?style=flat-square&logo=react&logoColor=white)
![Electron](https://img.shields.io/badge/Electron-39-47848f?style=flat-square&logo=electron&logoColor=white)
![Go](https://img.shields.io/badge/Go-1.24+-00add8?style=flat-square&logo=go&logoColor=white)
![Kubernetes](https://img.shields.io/badge/Kubernetes-validation%20target-326ce5?style=flat-square&logo=kubernetes&logoColor=white)

[Overview](#overview) • [Screenshots](#screenshots) • [Features](#features) • [Architecture](#architecture) • [Quick Start](#quick-start) • [Verification](#verification) • [Docs](#docs)

</div>

## Overview

mspace is a review Inbox and Issue workspace for software teams that want coding agents to work in real repositories and validate changes in namespace-scoped Kubernetes test environments.

The interaction model is closer to a shared engineering document than a terminal transcript: each issue keeps the problem statement, inline child-issue tasks, comments, agent session, branch state, logs, deployment evidence, preview URL, and cleanup decision in one place.

> [!NOTE]
> mspace is currently a runnable local desktop MVP. Agent execution is local-first through Codex app-server, while Kubernetes is a manually triggered issue test target.

## Screenshots

![mspace issues list](./docs/images/mspace-issues-list.png)

![mspace issue detail](./docs/images/mspace-issue-detail.png)

## Why mspace

- **Issues are the durable workspace.** Agent turns, child tasks, progress, blockers, session evidence, and review comments stay attached to the issue.
- **Local development stays fast.** Sessions run in prepared git worktrees under `~/.mspace/workdirs`.
- **Validation is real.** Test deployments use a saved cluster config and issue-scoped namespace to create Kubernetes preview environments.
- **Evidence is reviewable.** Branch status, diffs, logs, namespace state, preview URLs, and deployment output are visible without reconstructing context from a terminal.

## Features

- Electron desktop app with Inbox, Issues, Agents, Clusters, Projects, Issue Detail, and Session Detail screens.
- Notion-like paper workspace UI built with React 19, Tailwind CSS 4, Radix UI, lucide-react, and real shadcn/ui source components in `@mspace/ui`.
- Go local runner with HTTP APIs, SQLite storage, server-sent events, session logs, git-aware project import, and Codex app-server integration.
- Project import from a local folder or GitHub repository URL, including GitHub remote metadata detection when available.
- Managed agent profiles stored in SQLite, seeded with internal `@triage` plus user-facing `@codex`, `@bugfix`, and `@design`.
- Agent mentions from issue comments, with turn queueing, profile instructions, status updates, and issue timeline updates.
- Markdown-backed TipTap writing surfaces for issue creation and human issue comments, including checklist input that becomes child tasks.
- Per-session git worktrees, workspace inspection, changed file lists, diff previews, commits, and comparison against the project default branch.
- Type and priority labels, child issue task lists with create/toggle/delete controls, asynchronous type triage, unread Inbox updates, running-session stop controls, and manual worktree cleanup after completion or cancellation.
- Reusable cluster configs imported from kubeconfig files, with read-only reachability checks, image registry prefix, preview routing defaults, and optional Kubernetes context.
- Project default cluster selection.
- Manual issue test deployment that queues an agent turn to create the namespace, build and push images, deploy resources, expose a preview, and record evidence.

> [!IMPORTANT]
> Generated scoped kubeconfigs, ServiceAccounts, automatic PR capture, and Kubernetes-hosted agent runtime are future work. The MVP trusts the kubeconfig path configured on the selected cluster.

## Architecture

mspace separates collaboration, execution, and validation:

![mspace overview](./docs/images/mspace-overview.svg)

| Layer | What it owns | Current implementation |
| --- | --- | --- |
| Desktop workspace | Inbox, issues, comments, projects, agents, sessions, evidence review | Electron, React, TanStack Router, React Query, shared `@mspace/ui` |
| Local runner | API, SQLite state, SSE streams, worktree preparation, Codex session lifecycle | Go, chi, SQLite, `codex app-server --listen stdio://` |
| Agent runtime | One issue-bound turn in an isolated working directory | Local git worktree under `~/.mspace/workdirs/<project-id>/<session-id>` |
| Validation target | Build, deploy, inspect, preview, and cleanup issue test environments | Namespace-scoped Kubernetes workflow triggered from Issue Detail |

The desktop process starts the Go runner automatically on `127.0.0.1:7788` when no healthy runner is already available.

## Quick Start

### Requirements

- Node.js and pnpm.
- Go 1.24 or newer.
- Git on `PATH`.
- Codex CLI on `PATH` for `codex app-server --listen stdio://`.
- `kubectl` only when running deployment or validation flows that inspect Kubernetes.

### Run the desktop app

```bash
pnpm install
pnpm dev:desktop
```

Run the runner separately when debugging API behavior:

```bash
pnpm runner
pnpm dev:desktop
```

### First workflow

1. Add a project from a local folder or GitHub repository URL.
2. Add a reusable test cluster in the Clusters tab, or use the first-run prompt to choose which discovered `~/.kube` kubeconfig files to import.
3. Select that cluster as the project default when needed.
4. Create an issue in the Issues tab with a document-style note; include the project or repository name when multiple projects exist.
5. Use checklist rows such as `- [ ] Add tests` when the issue needs child tasks.
6. Mention an enabled agent profile, such as `@codex`, in a rich issue comment.
7. Review session status, logs, branch state, and diffs from Issue Detail or Session Detail.
8. Trigger the manual test deployment action from Issue Detail and keep the preview URL and evidence on the issue.

## Configuration

Runtime variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `MSPACE_RUNNER_PORT` | `7788` | Port used when the desktop starts the local runner. |
| `MSPACE_RUNNER_URL` | `http://127.0.0.1:7788` | API base URL exposed to the renderer. |
| `MSPACE_RUNNER_START_TIMEOUT_MS` | `60000` | Startup health-check timeout for the runner. |
| `MSPACE_PORT` | `7788` | Port used by a standalone runner. |

Local data paths:

| Path | Purpose |
| --- | --- |
| `~/.mspace/mspace.db` | SQLite database. |
| `~/.mspace/repos/<owner>/<repo>` | Cached clone path for GitHub-imported projects. |
| `~/.mspace/workdirs/<project-id>/<session-id>` | Git worktree for one agent session. |
| `~/.mspace/workdirs/_contexts/<session-id>.md` | Session context markdown included in Codex prompts. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session` | Session artifact directory. |
| `~/.mspace/workdirs/<project-id>/<session-id>/.mspace/session/test-environment.json` | Optional agent-written deployment result; `previewUrl` is copied back to the issue test environment. |

## Verification

```bash
pnpm typecheck
pnpm build:desktop
(cd packages/ui && pnpm dlx shadcn@latest info --json)
(cd runner && go test ./...)
(cd runner && go build ./...)
```

Runner health check:

```bash
curl http://127.0.0.1:7788/health
```

> [!TIP]
> The shadcn/ui source files live under `packages/ui/src/components/ui`. If UI imports fail, check the root `components.json`, `packages/ui/components.json`, and the desktop Vite aliases for `@mspace/ui/components` and `@mspace/ui/lib`.

## Project Layout

```text
apps/desktop/        Electron desktop shell and renderer entrypoint
packages/core/       Shared API client and TypeScript types
packages/ui/         Shared UI primitives and shadcn/ui source components
packages/views/      Product routes for Inbox, Issues, Agents, Projects, Sessions
runner/              Go local runner, SQLite migrations, Codex app-server bridge
docs/                Product, architecture, IA, references, runbook, and images
```

## Docs

- [Product Brief](./docs/product.md)
- [Architecture Notes](./docs/architecture.md)
- [Local API Integration Guide](./docs/integration-guide.md)
- [MVP Information Architecture](./docs/ia.md)
- [Local Runbook](./docs/runbook.md)
- [Reference Notes](./docs/references.md)
- [Design System](./DESIGN.md)
- [Roadmap](./ROADMAP.md)
