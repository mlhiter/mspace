# mspace Roadmap

> Status: milestone roadmap, updated 2026-05-08

## Purpose

This roadmap defines priority and sequence. It is not a task dump, issue tracker, or product spec.

Product truth lives in `docs/product.md`, architecture truth lives in `docs/architecture.md`, interface structure lives in `docs/ia.md`, and visual rules live in `DESIGN.md`. This file answers one question: what should mspace do next?

## Current Focus

Build the first usable product loop:

```text
Project
  -> Issue
  -> Local Agent Session
  -> Inbox review updates
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
- Issue creation: keep creation in the Issues surface, allow lightweight prompts, and allow project inference when the user leaves project empty.
- Agent mention flow: let a user manage agent profiles, write an issue comment with an enabled agent mention, save that comment, and create the local session from the current turn request and selected profile.
- Inbox realtime updates: move issue/session status changes into the Inbox review feed without relying on slow manual refreshes.
- Local agent context: send issue body, comments, project metadata, branch, kube context, and namespace into the Codex app-server turn prompt.
- Progress comments: turn meaningful session lifecycle and status updates into issue activity, not just terminal logs.
- Issue labels and session stop controls: keep issue triage lightweight and allow a human to interrupt queued or running work.
- Kubernetes validation evidence: capture Pods, Deployments, Services, Ingress, Events, logs, rollout state, and environment URLs after deploy or validation commands run.
- Branch and PR output: detect the session branch, capture PR URLs when `gh` or provider credentials are available, and keep fallback commands when PR creation is not configured.
- Cleanup controls: let the user retain or clean session worktrees now, and later clean session namespaces.

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
- Inbox, Issues, Projects, Issue Detail, and Session Detail routes exist.
- shadcn/ui primitives are installed under `packages/ui/src/components/ui`.
- `DESIGN.md` defines the Notion-style black-and-white product surface.
- Local Go runner, SQLite, session logs, and git worktree execution exist.

## Milestone 1: Local Issue Workflow

Status: current priority.

Goal:

- Make mspace useful for managing one local project issue from intake to local agent session review.

Build:

- Keep Inbox focused on review updates and unread agent activity.
- Polish Issue creation, project inference, and list flow.
- Polish Project creation and settings flow.
- Polish Issue Detail as the durable working document.
- Make starting a local agent session through an agent mention in the issue composer feel obvious.
- Support lightweight labels for issue triage.
- Let users stop queued or running sessions without opening a debug surface.
- Make session logs and status updates easy to follow from Issue Detail.
- Make session summary draft generation easy to review and post back.

Acceptance:

- A user can create a project from a local folder or GitHub repository URL and adjust runtime settings later.
- A user can create an issue from the Issues surface or sidebar quick action and return to it later.
- A user can manage agent profiles, mention an enabled agent in an issue comment, and start a local session from that current turn request.
- A user can label an issue and stop an active session from Issue Detail.
- The session runs in its own worktree.
- Inbox reflects issue and session updates without a manual refresh loop.
- Logs stream while the session runs.
- The issue clearly shows linked sessions, status, comments, current-turn agent output, and summary output.

## Milestone 2: Evidence-Centered Session Review

Goal:

- Turn a session from raw terminal output into reviewable work evidence.

Build:

- Improve branch state display.
- Improve changed files and diff previews.
- Improve commit and base-branch comparison display.
- Preserve command/tool history and runtime metadata, including Codex thread and turn ids.
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
- Add session retain and cleanup controls for local worktrees.
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
- review Inbox updates;
- start session;
- understand current agent work;
- review code changes;
- review Kubernetes validation;
- preserve branch, PR, or cleanup evidence.
