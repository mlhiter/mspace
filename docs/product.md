# mspace Product Brief

> Status: local MVP implementation snapshot, updated 2026-07-29

## One-Line Definition

mspace is a review Inbox and Issue workspace for coding agents: a place where teams coordinate work in document-style issues, run changes through registered runtime workers, and validate those changes against selected Environments. Environments are either Kubernetes clusters or virtual machines; preview URLs are deployment outputs, not environment records.

## Current MVP State

The repository now has a runnable local desktop MVP:

- create personal projects from a local folder picker or GitHub repository URL, create team projects from a GitHub repository URL that connected workers can clone, and manage settings later;
- create and manage issues in the Issues tab;
- automatically start a read-only first analysis session for project-backed issues when a Codex worker is ready, including issues that receive their project after creation, using the server-managed `think` workflow skill instead of requiring an immediate manual `@codex` comment;
- jump to issues and projects from the sidebar global search or `Command+K` palette;
- sign in with a local username/password account, or optional GitHub OAuth when available, through the server control plane, show the current user/workspace state in the sidebar, and edit the user's display profile from Account Settings;
- store signed-in workspace projects, project runbooks, project test cases, test case suggestions, test plans, test runs, issues, child issue tasks, comments, reactions, labels, and Inbox receipts in the server store: Postgres for team/shared deployments, local SQLite for packaged personal desktop mode;
- write checklist-style task lists during issue creation and have those rows converted into inline child issues on the parent Issue page, where they can be toggled or deleted;
- classify new issues asynchronously by queueing `issue_type_triage` runtime tasks that are claimed by Codex-capable workers and reconciled into one Conventional Commit type label by the server;
- let updated triage workers return a rewritten title in the same background turn, while the server applies it only when the user has not changed the immediate plain-text draft;
- label priority manually from Issue Detail, and scan/filter labels from the Issues list;
- view fixed Codex, Claude Code, and Pi readiness for This Mac, server-derived claimable Worker coverage, and per-engine status across connected Workers from the Agents route, while managing Workflow Skills separately;
- use Inbox as a review feed for unread issue and session updates;
- open document-style issue detail pages with Markdown-backed rich comments, image attachment thumbnails, lightweight reactions, and linked sessions;
- mention `@codex`, `@claude`, or `@pi` from Issue Detail only when a Worker exposes the exact engine capability, then queue a server-owned `agent_session` for the corresponding adapter;
- edit the latest unconsumed human comment, then stop and retry a session when a bad prompt has already been consumed;
- stop a queued or running Session from Issue Detail or Session Detail without cancelling the whole Issue; queued work stops immediately, while claimed/running source work waits for the Worker to report whether the reusable copy is clean, dirty, or `recovery_required`;
- run human source Sessions serially in one reusable Issue working copy, run Agent Session automation in detached per-Session workdirs, and keep non-Agent Workflow tasks outside the mutable Issue copy;
- preserve completed or cancelled Worker Session metadata, logs, source evidence, and supported artifact metadata or transferred evidence in Server-owned records while Worker-local artifact files remain runtime storage;
- cache imported GitHub repositories inside worker-managed repository roots;
- show a project runbook in Projects, open it from the Issue Detail sidebar as a read-only TipTap modal, and update it either from direct Markdown edits or from successful agent session artifacts;
- manage project-level test cases and case suggestions plus workspace-level test plans and issue-backed test runs that start from plans in the Tests route, including modal create/import flows, preview-before-confirm Markdown/text/CSV/Excel `.xlsx` import, field-level case revision summaries, readiness scoring, retry for failed run items, and lightweight human review records for run outcomes;
- keep signed-in workspace product and runtime state in the server store, including sessions, logs, evidence, environments, Kubernetes cluster compatibility records, issue test environments, handoffs, and execution metadata;
- keep the server control plane free of Agent runtime dependencies: no Codex, Claude Code, or Pi CLI, credentials, or in-process client in the server;
- keep workflow skills server-owned and worker-materialized per task, including built-in skills and workspace custom skill revisions, so workers can use the same pinned revision without depending on local global skill installs;
- inspect the Issue source working-copy state, stable branch, changed files, Session Commit evidence, diff previews, and comparison against the project default branch;
- manage workspace automation policy, keeping source commit capture always on while recording branch / PR handoff state from captured source commits;
- optionally queue an automatic issue test-environment deployment after a successful source session captures a commit, using the same source commit and deploy/test path as manual deployment;
- manage reusable Environments from the Environments route: Kubernetes environments can be imported from kubeconfig files, and virtual machine environments store SSH target metadata plus server-owned SSH credentials;
- choose a default Environment per project and select a Kubernetes environment when manually deploying an issue test environment;
- manually trigger an issue-scoped Kubernetes test deployment where the agent creates the namespace, builds and pushes images, deploys resources, and returns a preview URL;
- record issue test namespace state, cleanup/retain state, deploy session, cleanup session, and preview URL.
- inspect the current issue namespace from a Resources tab with Pods, Services and NodePort mappings, Deployments, Ingresses, and recent Events.
- review structured session evidence from Issue Detail, with code changes in Commits, live namespace objects in Resources, and the current review packet plus command evidence in Evidence.
- record issue-level branch / PR handoff state from the selected source change, including commit list, preview URL, evidence summary, PR URL/title/state, and server-owned executor errors.
- show failed sessions and failed deploy/preview/cleanup checks as concise error states, with continue, retry deploy, stop, retain, or cleanup choices from Issue Detail while keeping raw failure metadata internal.
- use a Notion-like desktop workspace shell with real shadcn/ui primitives, Radix base components, lucide-react icons, Material Icon Theme file icons, and low-contrast document surfaces.

Kubernetes remains the default deployment and issue test environment, but it is not the only Environment type. The current development runtime is a registered fixed worker, and workers are separate from Environments: a worker claims tasks from the server queue, then operates the selected Kubernetes or virtual machine target through the access mechanism carried by that environment. Running the agent runtime inside Kubernetes remains a later option once the Server Worker loop is stable.

## Why This Exists

The current high-leverage developer workflow is no longer just "ask Codex to edit code." A strong team workflow looks more like this:

1. Start from a real project and a real issue.
2. Keep the problem statement, discussion, and decisions in one durable page.
3. Attach an agent session to the issue.
4. Let the agent modify code in a worker-prepared development runtime.
5. Record the source branch/PR handoff, then trigger a test deployment when the worker result is ready.
6. For Kubernetes test deployment, let the agent create an issue namespace in the selected environment, deploy the app, and return a preview URL that mspace checks and records.
7. Review the PR or preview URL together with logs, events, resources, and runtime evidence.

This is already how advanced users work manually: Codex or Claude Code edits the code in a real checkout, the developer keeps notes in chat or docs, and then gives the agent access to a test cluster through `kubectl` or to a VM through SSH. mspace turns that fragmented workflow into a repeatable team product with the issue as the center of gravity, worker execution as the development step, and the selected Environment as the validation target.

## Target Users

- Platform teams that already run shared Kubernetes test clusters or VM-based deployment test hosts.
- Engineering teams that want a shared Inbox for human and agent work.
- Engineering teams that want agents to validate changes in realistic environments.
- Teams using Codex, Claude Code, Cursor, Kimi, OpenCode, or similar coding agents.
- Sealos-like teams where application runtime, namespace isolation, DevBox, registry, and deployment workflows already live on Kubernetes.

## Product Position

mspace is not a general agent platform. It is a collaborative issue workspace for coding agents that are expected to validate work against explicit team-owned Environments, with Kubernetes as the default issue deployment path and virtual machines for higher-level deployment tests.

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
  -> Reusable Issue source working copy
  -> Inbox review updates
  -> Branch / PR handoff or manual test deploy
  -> Selected Environment
  -> Issue test namespace for Kubernetes deploys
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
- default Environment;
- project runbook as mspace-owned Markdown with revision history, editable from Project settings and visible from the Issue Detail sidebar;
- allowed Kubernetes resource scope.

An Environment records reusable deployment access. Current Environment kinds are only `kubernetes` and `virtual_machine`; a preview URL is a deployment result attached to an issue, run, or evidence packet.

A Kubernetes Environment is projected from the existing `clusters` compatibility records and stores:

- kubeconfig path and optional Kubernetes context;
- image registry prefix;
- default exposure mode;
- optional preview domain and ingress class;
- optional NodePort host.

A virtual machine Environment stores SSH-oriented target metadata:

- SSH host, port, and user;
- SSH credential configuration state, with raw secret material stored server-side and never returned to clients;
- optional working directory and service hint;
- optional labels for future worker routing or runbook matching.

Creating a virtual machine Environment requires a password or private key so mspace can verify SSH login before the target is treated as usable. Editing or rechecking uses the saved credential unless the user supplies a new password/private key to replace it. Only a successful SSH check marks the VM `ready`; failed checks keep the record for later repair as `unreachable`, and missing auth material is rejected instead of creating a fake-ready environment when no saved credential exists.

### Runtime and Environment Stance

The development runtime and the validation environment should be treated as separate concepts.

The intended order is:

- fixed Server Worker runtime for day-to-day development in the MVP;
- Kubernetes environment for deployment and validation;
- Kubernetes-hosted runtimes later when the Server Worker flow is solid.

The product wedge is not "agent runs in Kubernetes" on day one. The wedge is "after worker-backed agent work, the user can ask the agent to prove the change in a real selected Environment and return evidence the team can review." Manual Kubernetes deployment stays the default review control; workspace owners/admins can opt into automatic test deployment after source sessions when the project, Kubernetes environment, registry, and worker prerequisites are already ready. VM execution is a later provider-specific path and should not pretend to support namespace Resources until that provider exists.

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
- workspace-level issue creation before a project is known;
- automatic project inference from the issue text when a single clear project exists, without a project selector in the creation flow;
- assignments to people or agents;
- durable list browsing and reopening later.

An Issue should hold:

- title and durable problem statement;
- optional project link, required before agent execution, PR handoff, or test environments;
- comments, lightweight reactions, and progress updates;
- assignee and subscriber list;
- linked branch, PR, and environment evidence;
- one stable source branch, canonical head, and explicit Worker-storage/recovery state after source execution begins;
- concise failure states when a session, deployment, preview check, interruption, or cleanup needs attention;
- inline task rows backed by child issues, not Markdown checkbox state;
- one or more attached agent sessions;
- one issue test environment record with namespace, preview URL, deploy status, and cleanup decision.

### Session Creation

A user creates a Session by writing an Issue comment that mentions one fixed Agent: `@codex`, `@claude`, or `@pi`. In the MVP path, mspace saves the comment first, queues a server-owned runtime task, and then:

- uses a registered runtime worker that claims server tasks;
- reserves and prepares the Issue's reusable source worktree, or an isolated detached worktree for Agent Session automation;
- freezes `agentEngine` as `codex`, `claude_code`, or `pi` and requires the exact `codex`, `claudeCode`, or `pi` Worker capability;
- starts the selected engine adapter inside the Worker-prepared worktree while shared Worker Core retains Skill materialization, artifact handling, source capture, and cancellation;
- streams agent messages, command execution items, status changes, and diagnostics;
- passes the selected Environment plus environment-specific Kubernetes context, issue namespace, image registry, and exposure settings into the app-server process and turn prompt for deploy/test sessions.

Before the trigger comment is written, mspace resolves the Issue's project attachment and asks the Server for availability using the Issue id and mentioned Agent capability. Personal desktop workspaces proactively keep one generic host-local Worker ready and discover installed Agent CLIs without launching them; team workspaces require a connected team Worker. The Server enforces the same engine, project, runtime-mode, working-copy writer, recovery, and Worker-storage affinity checks and returns a visible conflict instead of creating an unclaimable Session.

The first human source Session creates one stable `mspace/<issue-id>` branch and reusable Issue worktree. Later human Sessions serialize on that same source line and continue from its canonical head; Session logs, artifacts, engine references, Commit evidence, failures, and cancellation remain separate audit records. The Working Copy stays bound to the opaque storage identity of the Worker that owns its Git objects until durable source transfer exists. Once a V1 working copy is initialized, a missing worktree, wrong branch/head, or untrusted terminal result becomes an explicit recovery state instead of silently rebuilding from the project default branch. Existing pre-V1 test Issues initialize a fresh copy rather than importing legacy Session branches.

Server-owned Agent Session Workflows and explicit source-Commit tasks are detached. Issue analysis, deploy, Tests, and cleanup keep their own per-Session workdir and cannot acquire or move the Issue source branch. Type triage and import mapping are separate runtime task kinds that execute independently outside the Issue working copy.

Agent, Skill, Workflow, Worker, and Environment are separate product concepts. Agents are fixed execution engines. Skills are server-managed versioned instruction bundles. Workflows are mspace-owned automations; issue analysis, triage, Tests, import mapping, deploy, and cleanup remain Codex-backed in this release. Workers execute tasks, and Environments are operated targets.

Scoped namespace, ServiceAccount, and kubeconfig generation are target behavior, not implemented behavior in the current local MVP.

### Runtime Operation

Inside the session, the agent can:

- read the issue context and comment history;
- treat the triggering agent mention comment as the highest-priority current turn request;
- run tests and local commands;
- modify code in the worker-prepared runtime;
- deploy the project into the namespace;
- use `kubectl` against the scoped namespace;
- inspect Pod status, Events, Services, Ingress, logs, and rollout state;
- update the task status and report blockers.

### Completion

A completed session should leave:

- issue comments or progress updates;
- PR or branch link;
- environment URL when available;
- compact command evidence for the current review packet plus raw session logs for debugging;
- test, build, deployment, risk, follow-up, and cleanup evidence;
- runtime evidence such as pod status and logs;
- reusable Issue working-copy state after source execution, plus Issue namespace retain/cleanup decisions; detached Session workdirs remain Worker-managed runtime storage rather than a user-facing cleanup choice.

## MVP Scope

The first version should prove one thing:

**A team can create an issue, start a worker-backed agent session, and watch it modify, deploy, inspect, and validate the project in Kubernetes with all evidence preserved on the issue.**

MVP features:

- Inbox review list, Issues list, and issue detail;
- project list with create, settings, and guarded delete;
- Tests route with project cases, case suggestions, workspace plans, workspace runs, and dedicated detail pages;
- issue comments and assignee field;
- type and priority labels, with worker-backed asynchronous type triage and manual priority selection from Issue Detail;
- choose Codex, Claude Code, or Pi from the fixed Agent catalog and create a Worker-backed Session from its Issue mention;
- edit the latest human comment before it has triggered a session;
- cancel queued or running Sessions while preserving whether the Issue working copy is clean, dirty, or requires recovery before another source turn;
- fixed Server Worker development runtime;
- one reusable source worktree and stable branch per Issue, with serialized Sessions, storage affinity, dirty/recovery state, and detached automation isolation;
- reusable Environments route for Kubernetes kubeconfig discovery/import, registry/exposure defaults, and virtual machine SSH metadata;
- project default Environment selection, currently backed by the Kubernetes compatibility default for issue deploys;
- opt-in automatic test deploy after captured source commits;
- personal desktop PR creation from the captured local worktree through the signed-in Mac user's local `gh`, with the resulting PR URL recorded back into the issue handoff;
- read-only workspace GitHub App installation status for team branch/PR automation readiness;
- terminal/progress stream;
- Issue working-copy inspection and Session evidence review;
- stable Issue branch comparison against the project default branch;
- issue-summary draft generation from session output;
- inline task lists backed by child issues, with status toggles, task creation, and task deletion from Issue Detail.

Still outside the current implemented MVP:

- generated scoped kubeconfig;
- ServiceAccount and RoleBinding lifecycle;
- namespace per session;
- full Kubernetes resource browser;
- server-owned GitHub App token minting, branch publishing, and team PR automation;
- automated namespace cleanup policy beyond the current manual cleanup/retain decision.

The product architecture uses the Server control plane as the product and runtime truth for every signed-in workspace. Users, local password credentials, workspaces, membership, optional GitHub identity, auth sessions, projects, runbooks, issues, Issue working-copy state, comments, reactions, labels, Inbox receipts, Skills, Environments, Kubernetes cluster compatibility records, issue test environments, handoffs, audit, runtime tasks, Worker logs, and GitHub App installation state live in the Server. The physical Git worktree and objects stay Worker-owned. The fixed Agent catalog lives in code and is returned read-only; it is not workspace data.

Display name/avatar fields are snapshots for rendering only. They should not become a second account system; shared issue ownership, comments, and permissions belong behind the control plane.

## Explicit Non-Goals

- No automatic merge.
- No generalized DevOps troubleshooting assistant.
- No Sealos API dependency as the primary control path.
- No cluster-wide write permissions for agents.
- No custom Agent profiles, prompt personas, arbitrary executable definitions, model marketplace, or Agent credential storage in the control plane.
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
- the UI explains which worker runtime is executing and which Environment, namespace, or VM target the task can operate.

But the runtime should feel closer to Optio:

- work should run through a registered worker first, then validate against the selected Environment, with Kubernetes as the default deployment target;
- isolation is expressed through namespaces, ServiceAccounts, Roles, and quotas;
- each issue/session can later gain its own long-lived or temporary Kubernetes-hosted runtime when the product grows into that model;
- self-hosting on a team's own Kubernetes cluster or VM fleet is a first-class deployment model.

The current implementation borrows Optio's Git worktree isolation through one Worker-managed source worktree per Issue plus detached automation workdirs, and leaves Kubernetes-hosted runtime as future work.

The 2026-05-07 desktop UI direction borrows Notion's quiet document workspace feel: a left sidebar, paper-like pages, compact rows, inline metadata, and subdued panels. This is a product style reference, not a dependency on Notion behavior or branding.

## Key Product Bet

The product assumes that serious coding agents need both durable collaboration context and real deployment feedback. If agents only edit files and open PRs, existing agent dashboards are enough. If teams only want a document tool, existing issue trackers are enough. mspace becomes valuable when the issue itself is also the launch point for a real deployment/test Environment and its evidence.

## First Usability Test

Use one internal project and one Kubernetes test environment.

The test is successful if a developer can:

1. create a project in mspace;
2. create an issue from the Issues flow or sidebar quick action;
3. start a worker-backed session with Codex from that issue;
4. let Codex operate only the assigned repository and scoped test namespace;
5. deploy or inspect the project through `kubectl`;
6. open a PR or leave a branch with runtime evidence attached to the issue;
7. continue the Issue source working copy across Sessions and choose whether to retain or clean the Issue test namespace from mspace.
