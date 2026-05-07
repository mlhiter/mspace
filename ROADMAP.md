# mspace Roadmap

> Status: milestone roadmap, updated 2026-05-07

## Purpose

This roadmap defines priority and sequence. It is not a task dump, issue tracker, or product spec.

Product truth lives in `docs/product.md`, architecture truth lives in `docs/architecture.md`, interface structure lives in `docs/ia.md`, and visual rules live in `DESIGN.md`. This file answers one question: what should mspace do next?

## Current Focus

Build the first usable product loop:

```text
Project
  -> Inbox / Issue
  -> Local Agent Session
  -> Worktree and evidence review
  -> Kubernetes validation evidence
  -> Branch / PR and cleanup
```

The roadmap intentionally keeps local workflow first. Kubernetes is the validation target for the MVP, not the first development runtime.

## Approved Execution Plan

The product should reach the Multica-like workflow in two stages.

### Stage 1: Usable Local Agent Loop

Target: 5-8 focused development days.

Goal:

- Create or import a project, create an issue, assign it to an agent, watch status updates, and finish with branch/PR output plus Kubernetes validation evidence.

Build in order:

- Project import: support existing local folders, auto-detect GitHub remote metadata, and support direct GitHub repository URLs cloned into the mspace data directory.
- Agent assignment: make an issue assignable to a human or agent and create the local session from that assignment.
- Inbox realtime updates: move issue/session status changes into the Inbox without relying on slow manual refreshes.
- Local agent context: inject issue body, comments, project metadata, branch, kube context, and namespace into the session command.
- Progress comments: turn meaningful session lifecycle and status updates into issue activity, not just terminal logs.
- Kubernetes validation evidence: capture Pods, Deployments, Services, Ingress, Events, logs, rollout state, and environment URLs after deploy or validation commands run.
- Branch and PR output: detect the session branch, capture PR URLs when `gh` or provider credentials are available, and keep fallback commands when PR creation is not configured.
- Cleanup controls: let the user retain or delete session worktrees and later session namespaces.

### Stage 2: Kubernetes-Hosted Agent Runtime

Target: 1-2 additional weeks after Stage 1 is usable.

Goal:

- Allocate cluster resources for an agent session, deploy the agent/runtime into the assigned namespace, and return runtime and environment access URLs.

Build in order:

- Namespace allocator with labels, TTL, ResourceQuota, LimitRange, and project/session ownership metadata.
- Scoped ServiceAccount, Role, RoleBinding, and kubeconfig generation for each session.
- Kubernetes Job runtime for one-shot agent sessions.
- Optional Deployment, Service, and Ingress runtime mode when an interactive runtime URL is required.
- Runtime URL and project environment URL extraction back into Issue Detail.
- Namespace and runtime cleanup lifecycle.

Decision:

- Stage 1 remains first. Kubernetes-hosted agent runtime starts only after the local issue-to-evidence loop can complete one real internal project without manual reconstruction.

## Milestone 0: Product Surface Baseline

Status: mostly complete.

Goal:

- Establish the local desktop MVP, core navigation, design system, and shared UI foundation.

Acceptance:

- Electron desktop shell exists.
- Inbox, Projects, Issue Detail, and Session Detail routes exist.
- shadcn/ui primitives are installed under `packages/ui/src/components/ui`.
- `DESIGN.md` defines the Notion-style black-and-white product surface.
- Local Go runner, SQLite, session logs, and git worktree execution exist.

## Milestone 1: Local Issue Workflow

Status: current priority.

Goal:

- Make mspace useful for managing one local project issue from intake to local agent session review.

Build:

- Polish Inbox issue creation and triage.
- Polish Project creation and edit flow.
- Polish Issue Detail as the durable working document.
- Make starting a local agent session from an issue feel obvious.
- Make session logs and status updates easy to follow from Issue Detail.
- Make session summary draft generation easy to review and post back.

Acceptance:

- A user can create a project with repo path, default branch, commands, kube context, and namespace.
- A user can create an issue from Inbox and return to it later.
- A user can start a local session from the issue.
- The session runs in its own worktree.
- Logs stream while the session runs.
- The issue clearly shows linked sessions, status, comments, and summary output.

## Milestone 2: Evidence-Centered Session Review

Goal:

- Turn a session from raw terminal output into reviewable work evidence.

Build:

- Improve branch state display.
- Improve changed files and diff previews.
- Improve commit and base-branch comparison display.
- Preserve command history and runtime metadata.
- Attach generated session evidence back to the issue.
- Make evidence panels readable without opening a separate operations console.

Acceptance:

- A user can open a session and understand what changed.
- A user can compare the session branch against the project default branch.
- A user can see commits, changed files, diff previews, logs, and evidence summary in one review path.
- Issue Detail remains the primary place to understand the work.

## Milestone 3: Kubernetes Validation Evidence

Goal:

- Move Kubernetes from configuration fields to visible validation proof.

Build:

- Capture structured output from deploy and validation commands.
- Capture namespace snapshot data after validation.
- Show cluster, context, namespace, and environment URL clearly.
- Show Pods, Services, Ingress, Events, logs, and rollout state in a compact evidence view.
- Attach Kubernetes evidence to the issue and session.

Acceptance:

- A user can configure a project namespace and validation commands.
- A session can run validation commands against the configured namespace.
- The UI shows whether the project actually deployed or failed.
- Kubernetes evidence is stored and reviewable after the session exits.
- The user does not need a separate terminal just to answer "did this run?"

## Milestone 4: Branch / PR And Cleanup Loop

Goal:

- Complete the first end-to-end loop from issue to branch or PR, evidence, and cleanup decision.

Build:

- Capture branch output clearly.
- Capture PR URL when available.
- Attach branch or PR output back to Issue Detail.
- Add session retain and cleanup controls.
- Add project-level expectations for retained workdirs and evidence.
- Run one real internal project through the full flow.

Acceptance:

- A user can start from an issue and finish with a branch or PR link.
- The issue holds the branch or PR, validation evidence, session logs, and summary.
- A user can decide whether to retain or clean up the session worktree.
- One real project can complete the workflow without manual reconstruction.

## Later

Do not pull these into the MVP path until Milestone 1 through Milestone 4 are usable:

- scoped kubeconfig generation;
- ServiceAccount and RoleBinding lifecycle;
- namespace per session;
- Kubernetes-hosted agent runtime;
- multi-user workspace and account system;
- automatic merge pipeline;
- generic agent rules or skill management product.

## Priority Rule

When choosing between two next tasks, prefer the one that makes the issue-to-evidence loop more usable.

If a task does not improve one of these paths, defer it:

- create issue;
- start session;
- understand current agent work;
- review code changes;
- review Kubernetes validation;
- preserve branch, PR, or cleanup evidence.
