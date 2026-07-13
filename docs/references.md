# Reference Notes

> Status: public reference scan and implementation notes, updated 2026-05-20

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

What not to copy directly:

- local daemon as the main runtime model;
- broad managed-agent platform positioning;
- generic skill marketplace compounding as the main product wedge.

mspace interpretation:

Multica proves the interaction model. mspace should borrow the teammate/task/status feel, then anchor execution in Kubernetes namespaces.

Current implementation note:

The current codebase is independent. It borrows the Inbox, Issue, comments, sessions, and agent-as-collaborator shape without forking Multica or depending on Multica runtime code.

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

## Core Differentiation

mspace is not only a task board and not only a coding-agent runner.

Its wedge is:

```text
coding agent + project repository + scoped Kubernetes namespace + real test deployment
```

The user should open mspace because they want an agent to work in the same kind of test environment a senior engineer would use manually.
