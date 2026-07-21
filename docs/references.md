# Reference Notes

> Status: public reference scan and implementation notes, updated 2026-07-21

## Multica

Sources:

- https://multica.ai/docs
- https://github.com/multica-ai/multica

What to borrow:

- human and agent work in the same workspace;
- agents are assigned tasks like teammates;
- agents report progress, blockers, and results;
- dashboard shows agent lifecycle rather than hiding work in terminals;
- provider-neutral stance across Claude Code, Codex, Cursor, Kimi, OpenCode, and similar tools.
- a runtime adapter boundary that normalizes provider events/results while the task system remains provider-neutral;
- explicit runtime capability discovery instead of assuming every installed daemon can execute every Agent.

What not to copy directly:

- local daemon as the main runtime model;
- broad managed-agent platform positioning;
- generic skill marketplace compounding as the main product wedge.
- workspace-defined executable/runtime profiles, provider credentials in the control plane, and a duplicated task/session model per provider;
- treating process exit code zero as success when the Agent protocol did not emit terminal evidence.

mspace interpretation:

Multica proves the interaction model and the value of adapter-based execution. mspace keeps a narrower model: Codex, Claude Code, and Pi are fixed execution Agents; Skills are versioned instruction bundles; mspace system Workflows are separate product automations; Workers execute; Environments are operated targets. The control plane routes exact capabilities but never owns Agent CLIs or credentials.

Current implementation note:

The current codebase is independent. It borrows the Inbox, Issue, comments, Sessions, agent-as-collaborator shape, and engine-adapter boundary without forking Multica or depending on its runtime code. Unlike Multica's broader profile/runtime configuration, mspace intentionally removed persisted Agent Profiles and exposes only `@codex`, `@claude`, and `@pi`.

## Optio

Sources:

- https://optio.host/

What to borrow:

- self-hosted Kubernetes deployment;
- one long-lived Kubernetes pod per repo or similar repository runtime isolation;
- git worktree isolation;
- ticket-to-PR workflow as a future automation path;
- Helm-based installation for teams that run their own clusters.

What not to copy directly:

- fully automated ticket-to-merged-PR as the first product promise;
- assuming the main interaction starts from external issue trackers;
- treating PR merge as the main success metric before environment validation works.

mspace interpretation:

Optio proves the technical runtime shape. mspace should borrow the Kubernetes execution model, but focus first on project namespaces, fixed worker deployment, and realistic test-cluster operation.

Current implementation note:

The current server-owned MVP has implemented worker-managed git workdir isolation for sessions and a Helm-based customer deployment package for a Kubernetes-hosted fixed Server Worker. Per-session Kubernetes Runtime Provider pods/jobs, generated ServiceAccounts, and namespace allocation for runtime execution remain product targets rather than current code.

## Notion

What to borrow:

- document-first workspace feel;
- quiet left sidebar;
- paper-like content surfaces;
- compact rows with inline metadata;
- low-contrast blocks that support reading and scanning.

What not to copy directly:

- Notion branding;
- generic document database positioning;
- marketing-site layout;
- treating the product as a notes app instead of an agent issue workspace.

mspace interpretation:

Notion is the visual and interaction reference for calm document work. mspace should use that tone to make Inbox, Issues, Sessions, and Kubernetes evidence feel readable and steady, while keeping the product centered on coding-agent work and namespace-scoped validation.

## Agent Brand Assets

Sources reviewed 2026-07-21:

- Claude product site: https://claude.ai/
- Claude official SVG favicon asset: https://assets-proxy.anthropic.com/claude-ai/v2/assets/v1/cd02a42d9-Vq_H3mgS.svg
- Pi official logo asset: https://pi.dev/logo-auto.svg

mspace bundles the reviewed marks as `packages/views/src/assets/claude.svg` and `packages/views/src/assets/pi.svg`. Runtime UI resolves them through `packages/views/src/agent-avatar.ts`, so Agent identity remains available offline and does not depend on a remote image request. Source comments stay in the SVG files for file-level provenance.

## Core Differentiation

mspace is not only a task board and not only a coding-agent runner.

Its wedge is:

```text
coding agent + project repository + scoped Kubernetes namespace + real test deployment
```

The user should open mspace because they want an agent to work in the same kind of test environment a senior engineer would use manually.
