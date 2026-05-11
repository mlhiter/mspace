# mspace Product Brief

> Status: local MVP implementation snapshot, updated 2026-05-11

## One-Line Definition

mspace is a review Inbox and Issue workspace for coding agents: a place where teams coordinate work in document-style issues, develop changes in a local-first agent session, and validate those changes in a real, namespace-scoped Kubernetes test environment.

## Current MVP State

The repository now has a runnable local desktop MVP:

- create projects from a local folder picker or GitHub repository URL and manage settings later;
- create and manage issues in the Issues tab;
- jump to issues and projects from the sidebar global search or `Command+K` palette;
- sign in with GitHub through the local server control plane and show the current user/workspace state in the sidebar;
- write checklist-style task lists during issue creation and have those rows converted into inline child issues on the parent Issue page, where they can be toggled or deleted;
- classify new issues asynchronously with a background triage agent that assigns one Conventional Commit type label;
- label priority manually from Issue Detail, and scan/filter labels from the Issues list;
- manage Codex-backed agents from the Agents route, including mention, description, enabled state, and role instructions;
- use Inbox as a review feed for unread issue and session updates;
- open document-style issue detail pages with Markdown-backed rich comments, image attachment thumbnails, lightweight reactions, and linked sessions;
- mention an enabled agent from issue detail and start local app-server agent sessions with the matching managed profile;
- edit the latest unconsumed human comment, then stop and retry a session when a bad prompt has already been consumed;
- stop a queued or running session from Issue Detail or Session Detail without cancelling the whole issue;
- run sessions in git worktrees under `~/.mspace/workdirs/<project-id>/<session-id>`;
- clean a completed or cancelled session worktree from Session Detail while preserving the issue timeline, logs, evidence, and session metadata;
- cache imported GitHub repositories under `~/.mspace/repos/<owner>/<repo>`;
- show a project runbook in Projects, open it from the Issue Detail sidebar as a read-only TipTap modal, and update it either from direct Markdown edits or from successful agent session artifacts;
- store session metadata, logs, comments, comment reactions, issues, projects, evidence, local creator/author display snapshots, and issue image attachment blobs in SQLite under `~/.mspace/mspace.db`;
- inspect session worktree status, changed files, diff previews, commits, and comparison against the project default branch;
- manage reusable test cluster configs from the Clusters route, including first-run `~/.kube` discovery, selectable kubeconfig import, read-only reachability check, image registry prefix, and preview exposure defaults;
- choose a default cluster per project and select a cluster when manually deploying an issue test environment;
- manually trigger an issue-scoped test deployment where the agent creates the namespace, builds and pushes images, deploys resources, and returns a preview URL;
- record issue test namespace state, cleanup/retain state, deploy session, cleanup session, and preview URL.
- use a Notion-like desktop workspace shell with real shadcn/ui primitives, Radix base components, lucide-react icons, Material Icon Theme file icons, and low-contrast document surfaces.

Kubernetes is the deployment and test environment, not the required development runtime for the first version. The current development runtime is local. Running the agent runtime inside Kubernetes remains a later option once the local workflow is stable.

## Why This Exists

The current high-leverage developer workflow is no longer just "ask Codex to edit code." A strong team workflow looks more like this:

1. Start from a real project and a real issue.
2. Keep the problem statement, discussion, and decisions in one durable page.
3. Attach an agent session to the issue.
4. Let the agent modify code in a local development runtime.
5. Manually trigger PR generation or a test deployment when the local agent result is ready.
6. For test deployment, let the agent create an issue namespace in the shared test cluster, deploy the app, and return a probed preview URL.
7. Review the PR or preview URL together with logs, events, resources, and runtime evidence.

This is already how advanced users work manually: Codex or Claude Code edits the code locally, the developer keeps notes in chat or docs, and then gives the agent access to a test cluster through `kubectl`. mspace turns that fragmented workflow into a repeatable team product with the issue as the center of gravity, local development as the first step, and Kubernetes as the validation environment.

## Target Users

- Platform teams that already run shared Kubernetes test clusters.
- Engineering teams that want a shared Inbox for human and agent work.
- Engineering teams that want agents to validate changes in realistic environments.
- Teams using Codex, Claude Code, Cursor, Kimi, OpenCode, or similar coding agents.
- Sealos-like teams where application runtime, namespace isolation, DevBox, registry, and deployment workflows already live on Kubernetes.

## Product Position

mspace is not a general agent platform. It is a collaborative issue workspace for coding agents that are expected to validate work in Kubernetes-backed test environments.

| Product | Primary Shape | mspace Difference |
| --- | --- | --- |
| Multica | Human + agent task collaboration workspace | mspace keeps the inbox, issue, and teammate interaction idea, then adds attachable runtimes and environment evidence. |
| Optio | Ticket-to-PR coding agent workflow on Kubernetes | mspace borrows the K8s execution model but treats the issue page, not the automation pipeline, as the product center. |
| Generic AI IDE | Local coding assistant | mspace assumes the work is shared, reviewable, and often tied to a real test environment rather than a single user's local flow. |
| DevOps agent | Chat-driven cluster operation | mspace starts from project issues and coding work, not cluster-wide troubleshooting. |

## Core Workflow

```text
Project
  -> Issue
  -> Agent Session
  -> Local code change
  -> Inbox review updates
  -> Manual PR or manual test deploy
  -> Issue test namespace
  -> Preview URL
  -> Comments and progress updates
  -> Inspect logs/events/resources
  -> PR + environment evidence
```

### Project Setup

A project records:

- source type;
- local repository path;
- remote URL and detected Git provider metadata;
- default branch;
- default test cluster;
- project runbook as mspace-owned Markdown with revision history, editable from Project settings and visible from the Issue Detail sidebar;
- allowed Kubernetes resource scope.

A cluster records reusable deployment access:

- kubeconfig path and optional Kubernetes context;
- image registry prefix;
- default exposure mode;
- optional preview domain and ingress class;
- optional NodePort host.

### Runtime and Environment Stance

The development runtime and the validation environment should be treated as separate concepts.

The intended order is:

- local runtime for day-to-day development in the MVP;
- Kubernetes environment for deployment and validation;
- remote or Kubernetes-hosted runtimes later when the local-first flow is solid.

The product wedge is not "agent runs in Kubernetes" on day one. The wedge is "after local agent work, the user can manually ask the agent to prove the change in a real issue-scoped Kubernetes environment and return a URL the team can open."

### Inbox and Issue Flow

Work should start as Issues, with Inbox reflecting unread updates from those issues rather than acting as the issue database itself.

The Inbox should support:

- per-user unread/read/archive state;
- status and assignee changes;
- quick jump back into the linked issue;
- progress updates from attached agent sessions.

For the team-ready model, unread is not an issue property. Reviewable changes are written as issue events, and each user receives their own receipt. Opening or polling an issue must not clear unread state; read state changes only through explicit Inbox read-through actions.

The Issues surface should support:

- human-created issues;
- automatic project inference from the issue text, without a project selector in the creation flow;
- assignments to people or agents;
- durable list browsing and reopening later.

An Issue should hold:

- title and durable problem statement;
- project link;
- comments, lightweight reactions, and progress updates;
- assignee and subscriber list;
- linked branch, PR, and environment evidence;
- inline task rows backed by child issues, not Markdown checkbox state;
- one or more attached agent sessions;
- one issue test environment record with namespace, preview URL, deploy status, and cleanup decision.

### Session Creation

A user creates a session by writing an issue comment that mentions an enabled agent profile. In the MVP path, mspace saves the comment first, starts a local-first session, and then:

- uses the desktop-managed local runner;
- prepares a git worktree for the repository;
- starts `codex app-server --listen stdio://` inside that worktree for Codex-backed sessions;
- stores the selected profile in `agent_sessions.agent_profile` and injects the profile instructions from `agent_profiles` into the Codex prompt;
- streams agent messages, command execution items, status changes, and diagnostics;
- passes the selected cluster, Kubernetes context, issue namespace, image registry, and exposure settings into the app-server process and turn prompt.

Scoped namespace, ServiceAccount, and kubeconfig generation are target behavior, not implemented behavior in the current local MVP.

### Runtime Operation

Inside the session, the agent can:

- read the issue context and comment history;
- treat the triggering agent mention comment as the highest-priority current turn request;
- run tests and local commands;
- modify code in the local runtime;
- deploy the project into the namespace;
- use `kubectl` against the scoped namespace;
- inspect Pod status, Events, Services, Ingress, logs, and rollout state;
- update the task status and report blockers.

### Completion

A completed session should leave:

- issue comments or progress updates;
- PR or branch link;
- environment URL when available;
- command and tool history;
- runtime evidence such as pod status and logs;
- cleanup state: retained or cleaned for local worktrees, with namespace cleanup as a later lifecycle.

## MVP Scope

The first version should prove one thing:

**A team can create an issue, start a local-first agent session, and watch it modify, deploy, inspect, and validate the project in Kubernetes with all evidence preserved on the issue.**

MVP features:

- Inbox review list, Issues list, and issue detail;
- project list with create, settings, and guarded delete;
- issue comments and assignee field;
- type and priority labels, with asynchronous type triage and manual priority selection from Issue Detail;
- manage agent profiles and create a Codex session from an enabled agent mention in an issue comment;
- edit the latest human comment before it has triggered a session;
- cancel queued or running sessions while keeping the issue retryable;
- local development runtime;
- git worktree isolation per session;
- manual session worktree cleanup controls;
- reusable Clusters route for kubeconfig discovery/import, registry, and exposure defaults;
- project default cluster selection;
- terminal/progress stream;
- session workspace inspection;
- branch comparison against project default branch;
- issue-summary draft generation from session output;
- inline task lists backed by child issues, with status toggles, task creation, and task deletion from Issue Detail.

Still outside the current implemented MVP:

- generated scoped kubeconfig;
- ServiceAccount and RoleBinding lifecycle;
- namespace per session;
- full Kubernetes resource browser;
- PR link capture;
- automated namespace cleanup policy beyond the current manual cleanup/retain decision.

The product architecture is now explicitly moving toward a server control plane for multiplayer collaboration. The local desktop runner remains the execution path, but users, workspaces, membership, GitHub identity, auth sessions, audit, and future GitHub App installation state should live in the server rather than in local-only SQLite.

The current runner `creatorName`/`creatorAvatarUrl` and `authorName`/`authorAvatarUrl` fields are local display snapshots for the MVP. They should not become a second account system; shared issue ownership, comments, and permissions belong behind the control plane.

## Explicit Non-Goals

- No automatic merge.
- No generalized DevOps troubleshooting assistant.
- No Sealos API dependency as the primary control path.
- No cluster-wide write permissions for agents.
- No secret reading by default.
- No multi-agent scheduling before the single-session workflow works.
- No generic AGENTS.md / CLAUDE.md / Cursor rules management product.
- No requirement to fork or inherit Multica code directly.

## Interaction Principles

The interaction should feel closer to Multica than to a terminal-only tool:

- inbox and issue views are first-class, not side panels for runtime jobs;
- agents appear as assignees or collaborators, not as hidden background jobs;
- every issue has status, owner, comments, lightweight reactions, subscribers, and linked sessions;
- every session has logs, blockers, and evidence;
- a human can pause, resume, or cancel a session;
- the UI explains what runtime, namespace, and cluster the agent can operate.

But the runtime should feel closer to Optio:

- work should develop locally first, then validate against Kubernetes by default;
- isolation is expressed through namespaces, ServiceAccounts, Roles, and quotas;
- each issue/session can later gain its own long-lived or temporary Kubernetes-hosted runtime when the product grows into that model;
- self-hosting on a team's own cluster is a first-class deployment model.

The current implementation borrows Optio's git worktree isolation locally and leaves Kubernetes-hosted runtime as future work.

The 2026-05-07 desktop UI direction borrows Notion's quiet document workspace feel: a left sidebar, paper-like pages, compact rows, inline metadata, and subdued panels. This is a product style reference, not a dependency on Notion behavior or branding.

## Key Product Bet

The product assumes that serious coding agents need both durable collaboration context and real deployment feedback. If agents only edit files and open PRs, existing agent dashboards are enough. If teams only want a document tool, existing issue trackers are enough. mspace becomes valuable when the issue itself is also the launch point for a real Kubernetes deployment and test environment.

## First Usability Test

Use one internal project and one test cluster.

The test is successful if a developer can:

1. create a project in mspace;
2. create an issue from the Issues flow or sidebar quick action;
3. start a local-first session with Codex from that issue;
4. let Codex operate only the assigned repository and scoped test namespace;
5. deploy or inspect the project through `kubectl`;
6. open a PR or leave a branch with runtime evidence attached to the issue;
7. retain or clean up the local session worktree and choose whether to retain or clean the issue test namespace from mspace.
