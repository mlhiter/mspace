<!-- prettier-ignore -->
<div align="center">

<img src="./apps/desktop/assets/brand/mspace-icon.png" alt="mspace logo" width="96" />

# mspace

**A desktop workspace plus server control plane for coding agents, issue review, and Kubernetes validation evidence.**

![Status](https://img.shields.io/badge/status-local%20MVP-2d2926?style=flat-square)
![TypeScript](https://img.shields.io/badge/TypeScript-5.9-3178c6?style=flat-square&logo=typescript&logoColor=white)
![React](https://img.shields.io/badge/React-19-149eca?style=flat-square&logo=react&logoColor=white)
![Electron](https://img.shields.io/badge/Electron-39-47848f?style=flat-square&logo=electron&logoColor=white)
![Go](https://img.shields.io/badge/Go-1.24+-00add8?style=flat-square&logo=go&logoColor=white)
![Kubernetes](https://img.shields.io/badge/Kubernetes-validation%20target-326ce5?style=flat-square&logo=kubernetes&logoColor=white)

[Overview](#overview) - [Screenshots](#screenshots) - [Website](#website) - [Features](#features) - [Architecture](#architecture) - [Quick Start](#quick-start) - [Verification](#verification) - [Docs](#docs)

</div>

## Overview

mspace is a review Inbox and Issue workspace for software teams that want coding agents to work in real repositories and validate changes in namespace-scoped Kubernetes test environments.

The interaction model is closer to a shared engineering document than a terminal transcript: each issue keeps the problem statement, child tasks, comments, agent sessions, source branch state, runtime logs, deployment evidence, preview URL, and cleanup decision in one place.

> [!NOTE]
> mspace is a runnable local desktop MVP with a server-owned control plane. Local username/password auth works in restricted or offline environments, and GitHub OAuth remains an optional external identity provider. Signed-in personal and team workspaces store product data, runtime task state, test environment records, cluster configs, agent profiles, PR handoffs, worker logs, and results in server Postgres. Runtime workers claim tasks from the server queue and prepare their own repo cache/workdir.

## Screenshots

The README intentionally shows only a representative pair. Current and task-specific captures live in `docs/images/`; article embeds should use uploaded cloud image URLs instead of local repository paths.

![mspace issues list](./docs/images/mspace-issues-list-current.png)

![mspace issue detail](./docs/images/mspace-issue-detail-overview-current.png)

## Website

Public site: [mspace-website-blue.vercel.app](https://mspace-website-blue.vercel.app)

The website is a Vite/React/Tailwind brand surface in `apps/website`. It is intentionally bolder than the desktop product shell, but it stays anchored to the product story: issue workspace, Codex-backed sessions, source diffs, Kubernetes namespace previews, review evidence, and cleanup decisions. It also exposes a static `Changelog` navigation view backed by `apps/website/src/changelog.ts`.

```bash
pnpm dev:website
pnpm build:website
pnpm preview:website
```

Production deployment uses the root `vercel.json`:

- install: `pnpm install --frozen-lockfile`
- build: `pnpm --filter @mspace/website build`
- output: `apps/website/dist`

## Features

- Electron desktop app with Inbox, Issues, Agents, Clusters, Projects, Workspace Settings, Issue Detail, and Session Detail screens.
- Go server control plane with local password auth, optional GitHub OAuth, mspace session tokens, personal/team workspaces, workspace membership, invitations, Inbox receipts, projects, runbooks, issues, comments, reactions, labels, runtime worker registration, agent profiles, clusters, test environments, PR handoffs, runtime tasks, worker logs, and runtime results.
- Runtime worker daemon in `worker/` that registers with `msw_...`, heartbeats, claims matching server tasks, prepares its own repo cache/workdir, runs `codex app-server --listen stdio://`, streams logs, captures source metadata, and reports task results.
- Codex execution belongs to runtime workers. The server image does not install Codex or mount Codex credentials.
- Workspace Settings for team access, worker tokens, worker liveness, task events, task logs, and workspace automation.
- Notion-like paper workspace UI built with React 19, Tailwind CSS 4, Radix UI, lucide-react, Material Icon Theme file icons, and shadcn/ui source components in `@mspace/ui`.
- Bilingual desktop UI support for English and Simplified Chinese through `@mspace/i18n`.
- Document-style issue creation and comments with TipTap Markdown editing, inline child issues from checklist rows, image rendering for stable attachment URLs, and lightweight comment reactions.
- Agent mentions from issue comments, with server-side session records, runtime task queueing, profile instructions, trigger-comment tracking, worker logs, status updates, and issue timeline updates.
- Per-session worker-managed git worktrees, changed file lists, diff previews, commits, and comparison against the project default branch.
- Reusable cluster configs imported from kubeconfig files, with read-only reachability checks, image registry prefix, preview routing defaults, and optional Kubernetes context.
- Manual issue test deployment that queues an agent turn to create the namespace, build and push images, deploy resources, expose a preview, and update the issue test environment record.
- Issue Resources tab for the current test namespace, showing Pods, Services and NodePort mappings, Deployments, Ingresses, and recent Events without accepting cross-namespace input.
- Issue Evidence tab for the current review packet, with full-width pages for previous attempts and Kubernetes snapshot history.
- Issue-level branch / PR handoff records that keep one current PR with source branch, source commit, head commit, commit list, preview URL, evidence summary, local preflight errors, and refreshable PR state.
- Structured failure evidence for failed sessions, deploy reconciliation, preview checks, interruption, and cleanup failures.

> [!IMPORTANT]
> Generated scoped kubeconfigs, ServiceAccounts, server-owned GitHub App PR execution, and Kubernetes-hosted agent runtime are future work. The current MVP uses stored kubeconfig paths for test environments and fixed workers for execution.

## Architecture

mspace separates collaboration, execution, and validation:

![mspace overview](./docs/images/mspace-overview.svg)

| Layer | What it owns | Current implementation |
| --- | --- | --- |
| Control plane | Users, workspaces, product data, membership, local password credentials, GitHub identity, mspace auth sessions, agents, clusters, test environments, PR handoffs, agent sessions, runtime task/log/result state, future GitHub App installations | Go server in `server/`, chi, PostgreSQL through `pgx` |
| Desktop workspace | Inbox, issues, comments, projects, agents, sessions, evidence review, language preference | Electron, React, TanStack Router, React Query, shared `@mspace/ui` and `@mspace/i18n` |
| Runtime worker | Personal or team-owned fixed machine, VM, DevBox, or Docker dev worker that claims server tasks | Go daemon in `worker/`, registered with `msw_...`, worker-managed repo cache and workdir |
| Agent runtime | One issue-bound turn in an isolated working directory | Worker-managed git workdir under the selected runtime mode |
| Validation target | Build, deploy, inspect, preview, and cleanup issue test environments | Namespace-scoped Kubernetes workflow triggered from Issue Detail |

The desktop process starts the server control plane automatically on `127.0.0.1:8787` when no compatible server is already healthy. Execution happens through registered workers, not through a desktop-owned local product store.

## Quick Start

### Requirements

- Node.js and pnpm.
- Go 1.24 or newer.
- Git on `PATH`.
- Codex CLI on `PATH` for real Codex worker sessions.
- `kubectl` only when running deployment or validation flows that inspect Kubernetes.
- PostgreSQL through `DATABASE_URL`; the dev helper can start local Docker Postgres.

### Run the desktop app

```bash
pnpm install
pnpm dev:desktop
```

Run the server separately only when debugging server behavior:

```bash
cp .env.example .env.local
# edit .env.local with DATABASE_URL; GitHub OAuth values are optional
pnpm run server
```

Start a worker from Workspace Settings for the normal local dogfood flow, or run one manually:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
pnpm worker
```

For Docker-backed worker testing:

```bash
scripts/run-server-worker-dev.sh
scripts/run-server-worker-codex-dev.sh
```

For customer Kubernetes deployment, use the Helm chart and runbook under `deploy/helm/mspace` and `docs/kubernetes-deployment.md`.

### First workflow

1. Sign in with a local account, or use GitHub OAuth when it is configured, then select the personal or team workspace.
2. Create an issue in the Issues tab with a document-style note.
3. Attach or create a project before agent execution, PR handoff, project runbook access, or test environments.
4. Create/import a cluster config in Clusters if the issue needs a Kubernetes test environment.
5. Create a worker token from Workspace Settings and start a matching personal or team worker. Self-registered users stay in personal workspaces until a team owner/admin invites them; only server admins can create team workspaces.
6. Mention an enabled agent profile, such as `@codex`, in an issue comment.
7. Review session status, logs, branch state, and diffs from Issue Detail or Session Detail.
8. Use Commits for source review and PR handoff.
9. Trigger a manual test deployment from Issue Detail when ready.
10. Use Resources and Evidence to inspect namespace state, preview status, command evidence, failures, and cleanup decisions.

## Configuration

Runtime variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `MSPACE_SERVER_ADDR` | `127.0.0.1:8787` | Address used by the server control plane. |
| `MSPACE_SERVER_URL` | `http://127.0.0.1:8787` | Server control-plane URL exposed to the desktop renderer. |
| `MSPACE_SERVER_START_TIMEOUT_MS` | `30000` | Startup health-check timeout for the server when launched by Electron. |
| `DATABASE_URL` | none | Postgres connection string for the server control plane. |
| `MSPACE_DEV_POSTGRES_CONTAINER` | `mspace-postgres-dev` | Local Codex dev helper container name for auto-started Docker Postgres. |
| `MSPACE_DEV_POSTGRES_VOLUME` | `mspace-postgres-data` | Durable named Docker volume for local control-plane Postgres data. |
| `MSPACE_DEV_POSTGRES_IMAGE` | `postgres:16` | Docker image used when the local Codex dev helper creates Postgres. |
| `MSPACE_GITHUB_CLIENT_ID` | none | Optional GitHub OAuth client ID used by the server. |
| `MSPACE_GITHUB_CLIENT_SECRET` | none | Optional GitHub OAuth client secret; belongs on the server only. |
| `MSPACE_GITHUB_REDIRECT_URI` | none | Optional GitHub OAuth callback URL for the server. |
| `MSPACE_SERVER_ADMIN_LOGINS` | none | Comma-separated local password logins or GitHub logins allowed to create team workspaces. |
| `MSPACE_BOOTSTRAP_ADMIN_LOGIN` | none | Optional local password login created on server startup and treated as a server admin. |
| `MSPACE_BOOTSTRAP_ADMIN_PASSWORD` | none | Required with `MSPACE_BOOTSTRAP_ADMIN_LOGIN`; the server does not reset an existing account password. |
| `MSPACE_BOOTSTRAP_ADMIN_NAME` | login | Optional display name for the bootstrap admin account. |
| `MSPACE_BOOTSTRAP_ADMIN_EMAIL` | none | Optional bootstrap identity email; not used for admin matching. |
| `MSPACE_RUNTIME_TOKEN` | none | `msw_...` runtime worker registration token used by `pnpm worker`. |
| `MSPACE_WORKER_CAPABILITIES` | `{"protocolSmoke":true,"codex":false,"dryRun":true}` | Worker capability JSON used by server-side task matching. |
| `MSPACE_WORKER_VOLUME` | script-specific | Docker volume mounted at `/var/lib/mspace-worker` for worker-managed repo caches, session worktrees, and artifacts. |
| `MSPACE_WORKER_WORK_ROOT` | `/var/lib/mspace-worker` in Docker | Runtime worker root for `repos/<cache-key>` and `workdirs/<project-id>/<session-id>`. |
| `MSPACE_WORKER_CODEX_HOME_DIR` | `~/.mspace/codex-worker-home` | Host Codex home copy mounted by the Docker Codex dev worker. |
| `MSPACE_WORKER_CODEX_CLI_VERSION` | `0.130.0` | Codex CLI version installed by the Docker Codex dev worker image. |

Local data paths:

| Path | Purpose |
| --- | --- |
| `~/.mspace/codex-worker-home` | Host-side Codex home copy for Docker Codex workers. |
| `/var/lib/mspace-worker/repos/<cache-key>` | Repository cache inside Docker-backed workers. |
| `/var/lib/mspace-worker/workdirs/<project-id>/<session-id>` | Per-session worker workdir inside Docker-backed workers. |
| `<worker-root>/workdirs/<project-id>/<session-id>/.mspace/session` | Session artifact directory. |
| `<artifact-dir>/test-environment.json` | Optional agent-written deployment result. |
| `<artifact-dir>/review-evidence.json` | Optional agent-written review snapshot. |
| `<artifact-dir>/branch-name.json` | Optional agent-written source branch proposal such as `{ "branch": "fix/pr-source-branch-selection" }`. |
| `<artifact-dir>/project-runbook.md` | Optional agent-written project runbook update. |

## Verification

```bash
pnpm typecheck
pnpm build:website
pnpm build:desktop
pnpm test:server
(cd packages/ui && pnpm dlx shadcn@latest info --json)
(cd worker && go test ./...)
(cd worker && go build ./...)
```

Health checks:

```bash
curl http://127.0.0.1:8787/health
```

> [!TIP]
> The shadcn/ui source files live under `packages/ui/src/components/ui`. If UI imports fail, check the root `components.json`, `packages/ui/components.json`, and the desktop Vite aliases for `@mspace/ui/components` and `@mspace/ui/lib`.

## Project Layout

```text
apps/desktop/        Electron desktop shell and renderer entrypoint
apps/website/        Public Vite/React brand site and changelog for the issue-to-evidence story
packages/core/       Shared API client and TypeScript types
packages/i18n/       Shared English and Simplified Chinese desktop localization
packages/ui/         Shared UI primitives and shadcn/ui source components
packages/views/      Product routes for Inbox, Issues, Agents, Projects, Sessions
server/              Go control plane for identity, workspaces, auth sessions, product state, runtime state
worker/              Go runtime worker for claiming and executing server tasks
docs/                Product, value thesis, architecture, IA, references, runbook, and images
```

## Docs

- [Product Brief](./docs/product.md)
- [Product Value Thesis](./docs/product-value.md)
- [Control Plane Direction](./docs/control-plane.md)
- [Architecture Notes](./docs/architecture.md)
- [API Integration Guide](./docs/integration-guide.md)
- [Kubernetes Deployment Runbook](./docs/kubernetes-deployment.md)
- [MVP Information Architecture](./docs/ia.md)
- [Runbook](./docs/runbook.md)
- [Reference Notes](./docs/references.md)
- [Design System](./DESIGN.md)
- [Website App Notes](./apps/website/README.md)
- [Roadmap](./ROADMAP.md)
