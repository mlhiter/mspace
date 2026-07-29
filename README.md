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

mspace is a review Inbox and Issue workspace for software teams that want coding agents to work in real repositories and validate changes against explicit team-owned Environments. Current Environments are Kubernetes clusters and virtual machines; issue deploys use Kubernetes namespaces in this MVP.

The interaction model is closer to a shared engineering document than a terminal transcript: each issue keeps the problem statement, child tasks, comments, agent sessions, source branch state, runtime logs, deployment evidence, preview URL, and cleanup decision in one place.

> [!NOTE]
> mspace is a runnable local desktop MVP with a server-owned control plane. Local username/password auth works in restricted or offline environments, and GitHub OAuth remains an optional external identity provider. Team/shared deployments store product data, runtime task state, Environment records, Kubernetes compatibility configs, issue test environment records, Agent Sessions, GitHub App installation state, PR handoffs, worker logs, and results in server Postgres; packaged personal desktop mode runs the same control plane on a local server-owned SQLite store. Agent definitions are a fixed code-owned catalog, not persisted workspace profiles.

## Screenshots

The README intentionally shows only a representative pair. Current and task-specific captures live in `docs/images/`; article embeds should use uploaded cloud image URLs instead of local repository paths.

![mspace issues list](./docs/images/mspace-issues-list-current.png)

![mspace issue detail](./docs/images/mspace-issue-detail-overview-current.png)

## Website

Public site: [mspace-website-blue.vercel.app](https://mspace-website-blue.vercel.app)

The website is a Vite/React/Tailwind brand surface in `apps/website`. It is intentionally bolder than the desktop product shell, but it stays anchored to the product story: issue workspace, worker-backed Agent Sessions, source diffs, Kubernetes namespace previews, review evidence, and cleanup decisions. It also exposes a static `Changelog` navigation view backed by `apps/website/src/changelog.ts` and a `Download` view that links to packaged desktop installers on GitHub Releases.

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

- Electron desktop app with Inbox, Issues, Tests, Agents, Environments, Projects, Account Settings, Workspace Settings, Issue Detail, and Session Detail screens.
- Go server control plane with local password auth, optional GitHub OAuth, explicit auth identity provider/login, editable display profiles, mspace session tokens, personal/team workspaces, workspace membership, one-time team join links with safe signed-out previews, Inbox receipts, projects, runbooks, test cases, test case suggestions, test plans, test runs, issues, comments, reactions, labels, runtime worker registration, a fixed Agent catalog, built-in workflow Skills, environments, Kubernetes cluster compatibility records, test environments, GitHub App installation status, PR handoffs, runtime tasks, worker logs, and runtime results.
- Runtime worker daemon in `worker/` that registers with `msw_...`, heartbeats, claims tasks by exact capability, materializes server-provided Skill bundles, prepares its own repo cache/workdir, and executes Codex, Claude Code, or Pi through engine adapters before shared source/artifact capture.
- Agent execution and credentials belong to runtime workers. The server image does not install Codex, Claude Code, or Pi and does not mount their credentials.
- Agents shows fixed Codex, Claude Code, and Pi readiness on This Mac, server-derived claimable Worker coverage for the current workspace, and a responsive Connected Workers list with liveness and per-engine installation/configuration status. Skills remain a separate tab.
- Workspace Settings for team access, worker host installation, worker liveness, issue-linked runtime tasks, task events, task logs, and workspace automation.
- Notion-like paper workspace UI built with React 19, Tailwind CSS 4, Radix UI, lucide-react, Material Icon Theme file icons, and shadcn/ui source components in `@mspace/ui`.
- Bilingual desktop UI support for English and Simplified Chinese through `@mspace/i18n`.
- Document-style issue creation and comments with TipTap Markdown editing, plain-text Issue and child-task titles derived from formatted notes, non-blocking background title refinement, inline child issues from checklist rows, image rendering for stable attachment URLs, and lightweight comment reactions.
- Fixed Agent mentions from issue comments: `@codex`, `@claude`, and `@pi`, with `/` and `#` workflow Skill tokens, server-side Session records, exact capability routing, trigger-comment tracking, worker logs, status updates, and Issue timeline updates.
- One Server-owned working-copy record per Issue, backed by one reusable Worker-owned source worktree: human Agent Sessions serialize on the stable `mspace/<issue-id>` branch, while changed-file lists, diffs, commits, logs, and engine references remain Session-scoped. Analysis, deploy, and Tests automation stays isolated in detached per-Session worktrees.
- Tests workspace with project-level Cases and Case suggestions plus workspace-level Plans and Runs, dedicated detail pages, modal create/import flows, preview-before-confirm Markdown/text/CSV/Excel `.xlsx` import, readiness scoring, case-detail run history, field-level case revision summaries, and human review before Codex suggestions update canonical cases.
- Issue-backed test run workflow where workspace plans create issue-linked run items; plans can run one setup session first for real preconditions such as updating a Deployment image, SSHing into a VM, logging into a platform, or preparing a preview; workers execute through the existing agent-session path, `test-setup-result.json` and `test-result.json` are reconciled into run state, supported screenshot evidence is persisted as server-owned artifacts, and humans can retry failed items or record a lightweight review decision.
- Reusable environments for Kubernetes clusters and virtual machines. Kubernetes environments can be imported from kubeconfig files with read-only reachability checks, image registry prefix, preview routing defaults, and optional Kubernetes context; virtual machine environments store SSH target metadata plus server-owned SSH credentials for later worker access, while Environment responses expose only credential configuration state and readiness from password/private-key SSH login validation.
- Manual issue test deployment that queues an agent turn to create the namespace, build and push images, deploy resources, expose a preview, and update the issue test environment record.
- Opt-in workspace automation that queues the same test deployment flow after a successful source session captures a commit, when the issue and runtime are ready.
- Issue Resources tab for the current test namespace, showing Pods, Services and NodePort mappings, Deployments, Ingresses, and recent Events without accepting cross-namespace input.
- Issue Evidence tab for the current review packet, with full-width pages for previous attempts and Kubernetes snapshot history.
- Issue-level PR records that keep one current branch/PR delivery artifact with source branch, source commit, head commit, commit list, preview URL, evidence summary, and PR URL/state. Personal mode can queue one active detached Codex PR session per issue that publishes the selected source branch, creates or finds the PR, writes `pull-request.json`, and lets the Server reconcile the result; missing PR artifacts are surfaced as handoff errors. Durable team automation still waits for the server-owned GitHub App executor.
- Structured failure evidence for failed sessions, deploy reconciliation, preview checks, interruption, and cleanup failures.

> [!IMPORTANT]
> Generated scoped kubeconfigs, ServiceAccounts, server-owned GitHub App branch publishing/PR execution, VM deploy providers, and Kubernetes-hosted agent runtime are future work. The current MVP records workspace GitHub App installation state but does not mint installation tokens. Personal PR creation is a Codex handoff session, not the shared/team automation model.

## Architecture

mspace separates collaboration, execution, and validation:

![mspace overview](./docs/images/mspace-overview.svg)

| Layer | What it owns | Current implementation |
| --- | --- | --- |
| Control plane | Users, workspaces, product data, membership, local password credentials, GitHub identity, explicit auth identity display, mspace auth sessions, agents, built-in workflow skills, environments, Kubernetes cluster compatibility records, test environments, GitHub App installation state, PR handoffs, agent sessions, runtime task/log/result state | Go server in `server/`, chi, Postgres for team/shared deployments, local SQLite for packaged personal desktop mode |
| Desktop workspace | Inbox, issues, comments, projects, agents, sessions, evidence review, language preference | Electron, React, TanStack Router, React Query, shared `@mspace/ui` and `@mspace/i18n` |
| Runtime worker | Personal or team-owned fixed machine, VM, DevBox, or Docker dev worker that claims server tasks | Go daemon in `worker/`, registered with `msw_...`, worker-managed repo cache and workdir |
| Agent runtime | A source turn continuing the Issue working copy, or an isolated automation turn | Worker-managed git workdir under the selected runtime mode; human source turns reuse the Issue worktree and stable branch, while automation uses detached per-Session worktrees. Personal local projects may point at a git subdirectory, which the worker resolves to the git root before running the agent from the matching subdirectory |
| Environment / validation target | Reusable Kubernetes or virtual machine targets that decoupled workers can operate; current issue deploys build, deploy, inspect, preview, and cleanup Kubernetes issue test environments | Environment records in the server store; namespace-scoped Kubernetes workflow triggered from Issue Detail |

The desktop process uses `MSPACE_SERVER_URL` first, then a saved Team server URL, then starts the local bundled/dev server on `127.0.0.1:8787` when no configured server is active. Execution happens through registered workers, not through a desktop-owned local product store.

## Quick Start

### Requirements

- Node.js and pnpm.
- Go 1.24 or newer.
- Git on `PATH`.
- At least one supported Agent CLI on `PATH` for real sessions: `codex`, `claude`, or `pi`. Each engine uses its own local authentication/configuration.
- `kubectl` only when running deployment or validation flows that inspect Kubernetes.
- PostgreSQL through `DATABASE_URL` for server/team deployments. Packaged personal desktop mode uses a local SQLite store by default.

### Run the desktop app

```bash
pnpm install
pnpm dev:desktop
```

Run the server separately only when debugging server behavior:

```bash
cp .env.example .env.local
# edit .env.local with DATABASE_URL only when testing Postgres; GitHub OAuth values are optional
pnpm run server
```

Personal desktop workspaces proactively keep one generic host-local worker ready once auth and workspace selection are available. Electron discovers installed `codex`, `claude`, and `pi` commands without launching them and passes those exact capabilities as an execution allowlist. At Worker startup, bounded probes report only a readiness status, reason code, sanitized version, and timestamp: Codex uses `codex login status`, Claude Code uses `claude auth status --json`, and Pi 0.55.1 or newer uses `pi --offline --no-extensions --list-models` from an isolated temporary directory. A Pi installation with no available model reports `needs_setup`; one with a locally available model reports configured but remains unverified because no model request is sent; malformed output from a known supported version fails closed as a probe error. Older or unparseable Pi versions remain unverified and are not probed. Failed readiness can only disable an allowed capability. The desktop creates and renews a short-lived `msw_...` credential, while an anonymous stable host id distinguishes this Mac's primary and browser-companion Workers from Workers on other machines. Ordinary startup does not launch Chrome. Browser-backed Tests remain Codex system Workflows and use a separate browser-capable personal Worker advertising `codex`, `browser`, and `chrome_cdp`, so browser preparation cannot interrupt an existing task.

The personal Worker and Agent CLI still run as the same OS user. The Worker strips inherited registration/control-plane secrets from probe and Agent environments, but broad filesystem access can still expose predictable user-data files. Strong isolation requires a separate OS identity, container, credential broker, or filesystem deny-path sandbox.

Team workspaces normally connect external worker runtime hosts from Workspace Settings with a one-time install command. Run that command on the server, VM, DevBox, or other Docker-capable host that should claim agent tasks; the worker registers itself and appears online after its first heartbeat. Customer Helm installs can also enable `bootstrap.teamWorkspace.enabled=true` to create an admin-owned default team workspace and register the chart-managed fixed worker during server startup. The raw `msw_...` token flow is still available through the API for development and recovery, but it is not the product setup path.

Manual worker debugging:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
pnpm worker
```

For Docker-backed worker testing:

```bash
scripts/run-server-worker-dev.sh
scripts/run-server-worker-codex-dev.sh
```

For customer Kubernetes deployment, use the Helm chart and runbook under `deploy/helm/mspace` and `docs/kubernetes-deployment.md`. The fixed-worker path expects the operator to create the worker Codex home Secret first, using a worker-scoped `auth.json` plus the repository-owned `deploy/codex/worker-config.toml` or an untracked private-provider variant.

Container and desktop release builds use the root `package.json` version, checkout HEAD as a full Git SHA, and one explicit RFC3339 build time. Server binaries receive all three values through Go ldflags; Worker binaries receive the authoritative compile-time version, while Worker image OCI labels carry the shared revision and creation time. `deploy/scripts/build-images.sh` validates and reports pushed digests, and Helm accepts `server.image.digest` and `worker.image.digest` for immutable image references. Container and desktop packaging reject dirty build inputs or version/SHA overrides that do not match the checkout. GitHub release packaging uses only tools present in the tagged tree; an older tag that lacks the current identity contract must be replaced by a patched point release instead of mixing current packaging scripts with old source.

Build desktop packages for internal dogfood:

```bash
pnpm dist:desktop:mac
pnpm dist:desktop:win
pnpm dist:desktop:linux
```

The packaged desktop app includes bundled `mspace-server` and `mspace-worker` binaries. When no remote server is configured, it starts the server in personal mode with a local SQLite store under the app user-data directory; the personal worker is kept alive against the selected personal workspace as platform readiness, and its bootstrap credential is renewed in the background.

### First workflow

1. Sign in with a local account, or use GitHub OAuth when it is configured, then select the personal or team workspace. For team access from an invitation, open the join link; the desktop switches to the invited team server, shows a safe preview, lets you sign in or create an account if needed, accepts the invitation, and opens the invited workspace.
2. Create an issue in the Issues tab with a document-style note. When the issue already has a project, or when a project is attached later, and a matching Codex worker is online, mspace queues an automatic read-only issue analysis session using the server-managed `think` workflow skill, so the next implementation turn starts from a framed plan instead of a cold `@codex` mention.
3. Attach or create a project before agent execution, PR handoff, project runbook access, Tests, or issue test environments. Personal workspaces can use a local git repository root, a folder inside a git repository, or a GitHub URL; team workspaces require a GitHub URL so connected workers can clone the repository.
4. In Tests, import or create project cases. Imports first preview the parsed count, importable count, skipped rows, missing field counts, quality findings, and sample cases before the user confirms. Markdown/text imports use one non-empty line per case; CSV and `.xlsx` workbooks use the same column contract: `title`, `type`, `area`, `priority`, `preconditions`, `steps`, `expected_result`, `environment_requirements`, and `tags`. Valid case types are `functional`, `ui`, `api`, and `deployment`.
5. Create/import an Environment. Import kubeconfigs for Kubernetes targets, or add a virtual machine target with SSH password or private-key validation for SSH-oriented deployment testing.
6. For personal desktop workspaces, let mspace prepare the local worker after sign-in and workspace selection. For team workspaces, connect a worker runtime host from Workspace Settings and run the generated install command on that host. Self-registered users stay in personal workspaces until a team owner/admin invites them; only server admins can create team workspaces.
7. Mention one fixed Agent, `@codex`, `@claude`, or `@pi`, in an Issue comment after the Issue has a project, or let the composer open project attachment first. The selected engine is frozen into `agentEngine` and routed only to a Worker advertising its exact capability. Tests, automatic issue analysis, import mapping, deploy, and cleanup remain Codex-backed system Workflows.
8. Review session status, logs, branch state, and diffs from Issue Detail or Session Detail.
9. Use Commits for source review and PR handoff.
10. Trigger a manual test deployment from Issue Detail when ready, or enable workspace auto-deploy to queue it after successful source sessions.
11. Use Resources and Evidence to inspect namespace state, preview status, command evidence, failures, and cleanup decisions.

## Configuration

Desktop team server selection:

- Personal desktop mode uses the local bundled server by default, so most local users do not need to configure a server URL.
- The default local personal sign-in starts in account-creation mode and hides GitHub sign-in, even if the local dev server has OAuth variables configured.
- The sign-in screen keeps remote control-plane setup behind a collapsed Team server entry for deployed customer or team environments. Explicit team server launches start in login mode.
- Saved server URLs are stored in the Electron user-data profile for this device and reused on the next launch.
- `MSPACE_SERVER_URL` still works as a launch-time override and takes precedence over the saved UI value. When it is set, the Team server entry opens in a locked state for that launch.
- Team invitation links carry the server context in `mspace://invite/<token>?server=<team-server-url>`. When a recipient opens the link, the desktop checks that server before previewing or accepting the invitation, so users do not have to paste a server URL or invitation code manually.
- GitHub sign-in appears only for an explicitly configured team server, either a saved Team server URL or `MSPACE_SERVER_URL`, when that server reports `capabilities.githubAuth: true` from `/health`. For local OAuth debugging against `127.0.0.1:8787`, launch with `MSPACE_SERVER_URL=http://127.0.0.1:8787 pnpm dev:desktop` so the desktop treats the server as configured instead of default local personal mode.

Runtime variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `MSPACE_SERVER_ADDR` | `127.0.0.1:8787` | Address used by the server control plane. |
| `MSPACE_SERVER_URL` | `http://127.0.0.1:8787` | Launch-time server control-plane override for desktop and renderer. Takes precedence over the saved Team server setting. |
| `MSPACE_SERVER_START_TIMEOUT_MS` | `30000` | Startup health-check timeout for the server when launched by Electron. |
| `MSPACE_STORE` | inferred | Server storage mode: `postgres` when `DATABASE_URL` is set, otherwise `sqlite` for local personal mode. |
| `MSPACE_SQLITE_PATH` | app/user config path | SQLite file path for local personal mode. |
| `MSPACE_DATA_DIR` | none | Optional directory used to derive the default SQLite path for the server. |
| `DATABASE_URL` | none | Postgres connection string for the server control plane. |
| `MSPACE_DEV_POSTGRES_CONTAINER` | `mspace-postgres-dev` | Local Codex dev helper container name for auto-started Docker Postgres. |
| `MSPACE_DEV_POSTGRES_VOLUME` | `mspace-postgres-data` | Durable named Docker volume for local control-plane Postgres data. |
| `MSPACE_DEV_POSTGRES_IMAGE` | `postgres:16` | Docker image used when the local Codex dev helper creates Postgres. |
| `MSPACE_GITHUB_CLIENT_ID` | none | Optional GitHub OAuth client ID used by the server. |
| `MSPACE_GITHUB_CLIENT_SECRET` | none | Optional GitHub OAuth client secret; belongs on the server only. |
| `MSPACE_GITHUB_REDIRECT_URI` | none | Optional GitHub OAuth callback URL for the server. |
| `MSPACE_GITHUB_APP_ID` | none | Optional GitHub App ID. Enables `capabilities.githubApp` only with the client ID and private key. |
| `MSPACE_GITHUB_APP_CLIENT_ID` | none | Optional GitHub App client ID for server-owned repository automation setup. |
| `MSPACE_GITHUB_APP_PRIVATE_KEY` | none | Optional GitHub App private key; keep it server-side only. The current slice stores installation status but does not mint tokens yet. |
| `MSPACE_SERVER_ADMIN_LOGINS` | none | Comma-separated local password logins or GitHub logins allowed to create team workspaces. |
| `MSPACE_BOOTSTRAP_ADMIN_LOGIN` | none | Optional local password login created on server startup and treated as a server admin. |
| `MSPACE_BOOTSTRAP_ADMIN_PASSWORD` | none | Required with `MSPACE_BOOTSTRAP_ADMIN_LOGIN`; the server does not reset an existing account password. |
| `MSPACE_BOOTSTRAP_ADMIN_NAME` | login | Optional display name for the bootstrap admin account. |
| `MSPACE_BOOTSTRAP_ADMIN_EMAIL` | none | Optional bootstrap identity email; not used for admin matching. |
| `MSPACE_BOOTSTRAP_TEAM_WORKSPACE_NAME` | none | Optional team workspace name to create during server startup for Helm fixed-worker installs. Requires `MSPACE_BOOTSTRAP_RUNTIME_TOKEN`. |
| `MSPACE_BOOTSTRAP_RUNTIME_TOKEN` | none | Optional pre-created `msw_...` token that the server registers against the bootstrap team workspace. Helm sets this from the release Secret when `bootstrap.teamWorkspace.enabled=true`. |
| `MSPACE_BOOTSTRAP_RUNTIME_TOKEN_NAME` | `Helm fixed worker` | Display name for the bootstrap runtime token metadata. |
| `MSPACE_BOOTSTRAP_RUNTIME_TOKEN_TTL_HOURS` | `2160` | Runtime token TTL in hours for the bootstrap fixed worker, capped at 90 days. |
| `MSPACE_RUNTIME_TOKEN` | none | `msw_...` runtime worker registration token used by `pnpm worker`. |
| `MSPACE_WORKER_NAME` | host-derived | Stable worker name shown in Workspace Settings. |
| `MSPACE_WORKER_MODE` | `team` | Runtime mode reported to the server by install commands and worker scripts; `team` or `personal`. |
| `MSPACE_WORKER_IMAGE` | `crpi-7jr40k6elhldekqp.cn-hangzhou.personal.cr.aliyuncs.com/mlhiter/mspace-worker-codex:dev` | Docker image used by generated self-host worker install commands. |
| `MSPACE_WORKER_CONTAINER` | `MSPACE_WORKER_NAME` | Docker container name used by generated self-host worker install commands. |
| `MSPACE_WORKER_CAPABILITIES` | worker-dependent | Worker capability JSON used by server-side task matching. The plain worker default is `{"protocolSmoke":true,"codex":false,"dryRun":true}`; generated self-host install commands default to Codex/Docker/Kubectl-capable JSON with `dryRun:false`. |
| `MSPACE_WORKER_LABELS` | worker-dependent | Worker placement/inventory labels. Generated self-host install commands default to `{"provider":"self-host","environment":"docker"}`. |
| `MSPACE_WORKER_VOLUME` | `mspace-worker-${MSPACE_WORKER_NAME}` in install commands | Docker volume mounted at `/var/lib/mspace-worker` for worker-managed repo caches, reusable Issue source worktrees, detached automation workdirs, and artifacts. |
| `MSPACE_WORKER_WORK_ROOT` | `/var/lib/mspace-worker` in Docker | Runtime Worker root for `repos/<cache-key>`, reusable `workdirs/<project-id>/<issue-id>`, detached `workdirs/<project-id>/<session-id>`, and source Session artifacts. |
| `MSPACE_WORKER_CODEX_HOME_SOURCE` | `${CODEX_HOME:-~/.codex}` | Source Codex home copied into the generated self-host worker Codex home before container startup. |
| `MSPACE_WORKER_CODEX_HOME_DIR` | `~/.mspace/codex-worker-home` | Host Codex home copy mounted by the Docker Codex dev worker. |
| `MSPACE_WORKER_CODEX_CLI_VERSION` | `0.130.0` | Codex CLI version installed by the Docker Codex dev worker image. |
| `MSPACE_AUTO_PERSONAL_WORKER` | enabled | Set to `0` to disable the desktop's automatic host-local personal worker. |

Local data paths:

| Path | Purpose |
| --- | --- |
| `<Electron userData>/mspace.db` | Packaged personal desktop SQLite store used by the bundled local server. |
| `<Electron userData>/worker/` | Host-local personal worker root for desktop-managed repo caches, workdirs, and artifacts. |
| `<Electron userData>/worker/browser-companion/` | Isolated repo cache, workdirs, and artifacts for the on-demand browser-capable companion Worker. |
| `<Electron userData>/worker/tokens/<workspace-id>.token` | Short-lived personal worker credential file written by Electron and reread by the worker. |
| `<worker-root>/.mspace/storage-id` | Opaque stable `msws_...` identity for one Worker storage root, used for Issue working-copy affinity. |
| `~/.mspace/codex-worker-home` | Host-side Codex home copy for Docker Codex workers. |
| `/var/lib/mspace-worker/repos/<cache-key>` | Repository cache inside Docker-backed workers. |
| `/var/lib/mspace-worker/workdirs/<project-id>/<issue-id>` | Reusable Issue source worktree inside Docker-backed workers. It contains the stable Issue branch and is bound to one Worker storage identity. |
| `/var/lib/mspace-worker/workdirs/<project-id>/<session-id>` | Detached per-Session worktree for analysis, deploy, Tests, and other automation. |
| `<worker-root>/artifacts/<project-id>/<issue-id>/<session-id>` | Isolated artifact directory for one source Session, kept outside the reusable Issue worktree so artifact files cannot dirty source state. |
| `<detached-worktree>/.mspace/session` | Artifact directory for a detached automation Session. |
| `<artifact-dir>/skills/` | Worker-materialized server skill bundles for the current task. |
| `<artifact-dir>/skills/manifest.json` | Per-session manifest of materialized skill names, revisions, directories, bundle hashes, and file hashes. |
| `<artifact-dir>/test-environment.json` | Optional agent-written deployment result. |
| `<artifact-dir>/review-evidence.json` | Optional agent-written review snapshot. |
| `<artifact-dir>/pull-request.json` | Required completion artifact for Codex PR handoff sessions after a PR is created or found. |
| `<artifact-dir>/test-case-proposals.json` | Optional Codex-written test case suggestion artifact reconciled into Case suggestions. |
| `<artifact-dir>/test-setup-result.json` | Required completion checkpoint for Tests setup automation. Passing setup stores `setupResult`, passes `outputs` into the run context, and starts case execution; failed/cancelled/missing setup marks the run `setup_failed`. |
| `<artifact-dir>/test-result.json` | Required completion checkpoint for Tests execution automation, reconciled into run items and review state. Supported screenshot evidence is transferred from the worker artifact directory, persisted as server-owned test artifacts, and shown from Case Detail / Run Detail. |
| `<artifact-dir>/branch-name.json` | Optional branch proposal for detached legacy/source-capture flows. Issue working-copy Sessions ignore it because the Server owns the stable branch. |
| `<artifact-dir>/project-runbook.md` | Optional agent-written project runbook update. |

## Verification

```bash
pnpm typecheck
pnpm build:website
pnpm build:desktop
pnpm dist:desktop:mac
pnpm dist:desktop:win
pnpm dist:desktop:linux
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
apps/website/        Public Vite/React brand site, changelog, and installer download entrypoint
packages/core/       Shared API client and TypeScript types
packages/i18n/       Shared English and Simplified Chinese desktop localization
packages/ui/         Shared UI primitives and shadcn/ui source components
packages/views/      Product routes for Inbox, Issues, Tests, Agents, Projects, Sessions
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
- [Test Module Plan](./docs/test-module-plan.md)
- [Reference Notes](./docs/references.md)
- [Design System](./DESIGN.md)
- [Website App Notes](./apps/website/README.md)
- [Roadmap](./ROADMAP.md)
