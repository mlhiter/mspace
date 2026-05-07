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
