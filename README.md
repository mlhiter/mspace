# mspace

mspace is an Inbox and Issue workspace for software teams that want coding agents to develop locally and validate changes in real Kubernetes test environments instead of abstract sandboxes.

It takes the interaction shape of Multica, where humans and agents collaborate around shared issues and progress updates, and combines it with the deployment and validation shape of Optio, where projects can be exercised against controlled cluster resources. The product direction is narrower than both: each issue can attach an agent session, the current development path is local-first, and the deployed test target is a namespace-scoped Kubernetes environment in a shared cluster.

## Core Idea

AI coding work should live in a shared team workspace, not only in a repository checkout or a terminal transcript. The issue itself should be the durable document: context, discussion, agent progress, runtime evidence, PR output, and environment links all belong in one place.

## Why K8s

The Kubernetes environment is the deployment and test target that makes mspace worth building. Agents should be able to take locally developed changes, deploy them into a scoped namespace, and validate them with real cluster resources, real logs, real events, and real rollout state.

## Current Status

The repository currently contains a runnable local desktop MVP:

- Electron desktop app with Inbox, Projects, Issue Detail, and Session Detail screens.
- Notion-like desktop workspace shell built on real shadcn/ui source components, Radix UI primitives, and lucide-react icons in `@mspace/ui`.
- Go local runner with HTTP APIs, SQLite storage, session logs, and server-sent events.
- Local session execution in git worktrees under `~/.mspace/workdirs/<project-id>/<session-id>`.
- Session workspace inspection for git status, changed files, diff previews, commits, and comparison against the project default branch.
- Project-level Kubernetes context and namespace fields that are passed into session commands.

Kubernetes is currently the configurable validation target. Generated scoped kubeconfigs, ServiceAccounts, namespace allocation, PR capture, and cleanup controls are not implemented in the local MVP yet.

## Requirements

- Node.js and pnpm.
- Go 1.24 or newer.
- Git on `PATH`.
- `kubectl` only when running project deploy or validation commands that inspect Kubernetes.

## Run Locally

Install dependencies:

```bash
pnpm install
```

Start the desktop app:

```bash
pnpm dev:desktop
```

The Electron main process starts the Go runner automatically on `127.0.0.1:7788` if no healthy runner is already listening.

Run the runner separately when debugging API behavior:

```bash
pnpm runner
pnpm dev:desktop
```

Useful environment variables:

| Variable | Used by | Default | Purpose |
| --- | --- | --- | --- |
| `MSPACE_RUNNER_PORT` | Electron main process | `7788` | Port used when desktop starts the local runner. |
| `MSPACE_RUNNER_URL` | Electron preload/renderer | `http://127.0.0.1:7788` | API base URL exposed to the renderer. |
| `MSPACE_PORT` | Go runner | `7788` | Port used by a standalone runner. |

Local data paths:

| Path | Purpose |
| --- | --- |
| `~/.mspace/mspace.db` | SQLite database. |
| `~/.mspace/workdirs/<project-id>/<session-id>` | Git worktree for one agent session. |

## Verify

```bash
pnpm typecheck
pnpm --filter @mspace/desktop build
(cd packages/ui && pnpm dlx shadcn@latest info --json)
(cd runner && go test ./...)
(cd runner && go build ./...)
```

For local runner checks:

```bash
curl http://127.0.0.1:7788/health
```

## First Product Promise

For each project, mspace gives a team a document-style issue workflow where a coding agent can:

- receive work through an inbox and issue flow;
- collaborate through comments, status updates, and blockers;
- run in a local development runtime by default;
- configure a Kubernetes context and namespace for validation;
- deploy or update the project in a test cluster;
- inspect pods, services, ingress, events, and logs;
- keep open the future option of generated scoped kubeconfigs and ServiceAccounts;
- keep open the future option of running the agent runtime inside Kubernetes;
- produce a PR, branch link, and environment evidence.

## Documentation

- [Product Brief](docs/product.md)
- [Architecture Notes](docs/architecture.md)
- [MVP Information Architecture](docs/ia.md)
- [Reference Notes](docs/references.md)
- [Local Runbook](docs/runbook.md)
