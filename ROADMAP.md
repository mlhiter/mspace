# mspace Roadmap

> Status: milestone roadmap, updated 2026-07-17

## Purpose

This roadmap defines priority and sequence. It is not a task dump, issue tracker, or product spec.

Product truth lives in `docs/product.md`, product value thesis lives in `docs/product-value.md`, architecture truth lives in `docs/architecture.md`, interface structure lives in `docs/ia.md`, and visual rules live in `DESIGN.md`. This file answers one question: what should mspace do next?

## Current Focus

Build the first usable product loop:

```text
Issue intake
  -> Optional Project attachment
  -> Server-owned Agent Session
  -> Inbox review updates
  -> Worker worktree and evidence review
  -> Branch / PR handoff record or manual issue test deployment
  -> Issue namespace preview URL and validation evidence
  -> Branch / PR and cleanup
```

The roadmap intentionally keeps fixed worker workflow first. Kubernetes is the manually triggered test target for the MVP, not the first development runtime.

The server control-plane slice now owns local password auth, optional GitHub sign-in, mspace auth sessions, users, workspaces, membership, workspace projects, runbooks, issues, child tasks, comments, reactions, labels, Inbox receipts, workspace settings, the fixed Agent catalog contract, Skills, Environments, Kubernetes cluster compatibility records, issue test environments, PR handoffs, runtime registration, runtime tasks, worker logs, and runtime results. Agent definitions are code-owned Codex, Claude Code, and Pi engines rather than persisted workspace profiles. Team/shared workspace data uses server Postgres; packaged personal desktop mode can use the same server contract on a local SQLite store.

The local MVP now has first versions of commit-backed deploy source selection, issue-level branch / PR handoff records, structured review evidence, continueable failure evidence, opt-in automatic test deploy after captured source commits, automatic personal worker startup/credential renewal, and bilingual desktop UI support for English and Simplified Chinese. The next proof point is a real dogfood issue that exercises those surfaces together instead of treating each as a separate feature.

The Tests surface is now part of the local MVP loop: project cases, case suggestions, plans, lightweight plan-level setup, runs, modal create/import with preview-before-confirm, Excel `.xlsx` import, case revisions, and issue-backed test run execution are implemented. The next test-module proof point is dogfooding a real plan through setup-produced `test-setup-result.json`, worker-produced `test-result.json`, failed-item retry, lightweight human review records, and follow-up case suggestions rather than expanding into CDP, SSH scheduling, templates, or multi-worker orchestration first.

## Approved Execution Plan

The product should reach the Multica-like workflow in two stages.

### Stage 1: Usable Server-Owned Worker Loop

Target: 5-8 focused development days.

Goal:

- Create an issue, attach or import a project when execution is needed, assign it to an agent, watch status updates, then record branch / PR handoff state or manually run an issue-scoped Kubernetes test deployment.

Build in order:

- Project import: support existing local folders for personal desktop workspaces, auto-detect GitHub remote metadata, and support direct GitHub repository URLs for team workspaces where workers clone source into their own repo cache.
- Issue creation: keep creation in the Issues surface, use a document-style note without a project selector, allow workspace-level issues before the repository is known, and attach a project later when agent execution, PR handoff, or test deployment is needed.
- Issue task lists: treat task rows as child issues, convert creation-time Markdown checklist lines into child rows, and let humans or agents update task status from the parent issue.
- Agent mention flow: expose only `@codex`, `@claude`, and `@pi`; save the Issue comment and create a server-owned Session with the selected `agentEngine` and exact Worker capability.
- Inbox realtime updates: move issue/session status changes into the Inbox review feed without relying on slow manual refreshes.
- Worker Agent context: send Issue body, comments, project metadata, branch, and relevant Environment context into the selected engine adapter while keeping worktree, Skills, artifacts, and source capture in shared Worker Core.
- Progress comments: turn meaningful session lifecycle and status updates into issue activity, not just terminal logs.
- Issue labels and session stop controls: keep type triage asynchronous, keep priority manual, and allow a human to interrupt queued or running work.
- Manual test deployment: let the user select a saved Kubernetes Environment and optional exposure overrides before queueing a deploy/test agent turn.
- Issue namespace lifecycle: each issue can reserve one test namespace; the deploy/test agent creates it, deploys resources, mspace validates the preview URL, and writes the result back.
- Branch and PR output: expose issue-level handoff recording from captured source evidence, with GitHub App-backed PR creation left as a server-owned executor step.
- Cleanup controls: let the user retain or clean session worktrees now, and record retain/cleanup decisions for issue test namespaces.

### Stage 2: Team Runtime Providers

Target: 1-2 additional weeks after Stage 1 is usable.

Goal:

- Make Team Runtime production-usable first as a fixed Server Worker, then add Kubernetes-hosted execution as a second backend behind the same runtime task protocol.

Build in order:

- Harden Server Worker repo/workspace provisioning so the worker can clone or reuse a repository, prepare its own workdir, run the selected fixed Agent engine, and return artifacts without desktop filesystem coupling.
- Harden source-change and artifact adoption from remote workers back into the issue/session model.
- Runtime provider labels and capabilities for routing tasks to fixed Server Workers or future Kubernetes providers.
- Kubernetes namespace allocator with labels, TTL, ResourceQuota, LimitRange, and project/session ownership metadata.
- Scoped ServiceAccount, Role, RoleBinding, and kubeconfig generation for each Kubernetes runtime session.
- Kubernetes Pod/Job runtime for isolated one-shot agent sessions, using the same `runtime_tasks`, log, cancellation, session/evidence, and PR handoff protocol as Server Worker.
- Optional Deployment, Service, and Ingress runtime mode when an interactive runtime URL is required.
- Namespace and runtime cleanup lifecycle.

Decision:

- Stage 1 remains first. Kubernetes-hosted agent runtime starts only after Server Worker can complete the team execution loop without desktop filesystem coupling. Kubernetes is a backend upgrade, not a separate issue/session product.

## Milestone 0: Product Surface Baseline

Status: mostly complete.

Goal:

- Establish the local desktop MVP, core navigation, design system, and shared UI foundation.

Acceptance:

- Electron desktop shell exists.
- Inbox, Issues, Tests, Projects, Issue Detail, Test Case Detail, Test Plan Detail, Test Run Detail, and Session Detail routes exist.
- shadcn/ui primitives are installed under `packages/ui/src/components/ui`.
- `DESIGN.md` defines the Notion-style black-and-white product surface.
- Server control plane, runtime worker registration, task logs, and worker-managed git worktree execution exist.

## Milestone 1: Local Issue Workflow

Status: current reliability and usability priority.

Goal:

- Make mspace useful for managing one project issue from intake to worker-backed agent session review.

Build:

- Keep Inbox focused on review updates and unread agent activity.
- Polish Issue creation, project attachment/inference, and list flow.
- Polish Project creation and settings flow.
- Polish Issue Detail as the durable working document.
- Make starting an agent session through an agent mention in the issue composer feel obvious.
- Support type and priority labels for issue triage, with agent-based type classification and manual priority selection.
- Let users stop queued or running sessions without opening a debug surface.
- Make session logs and status updates easy to follow from Issue Detail.
- Make session summary draft generation easy to review and post back.

Acceptance:

- A user can create an issue from the Issues surface or sidebar quick action and return to it later, even before the repository is known.
- A user can create a personal project from a local folder or GitHub repository URL, create a team project from a GitHub repository URL, attach it to the issue when execution is needed, and adjust runtime settings later.
- A user can import or create project test cases, review case suggestions, assemble a plan with optional setup steps, and start an issue-backed test run.
- A user can create and check off issue tasks without duplicating state between Markdown checkboxes and child issue rows.
- A user can mention Codex, Claude Code, or Pi in an Issue comment and start a Worker-backed Session routed only to that engine capability.
- A user can label an issue and stop an active session from Issue Detail.
- The session runs in its own worktree.
- Inbox reflects issue and session updates without a manual refresh loop.
- Logs stream while the session runs.
- The issue clearly shows linked sessions, status, comments, current-turn agent output, and summary output.

## Milestone 2: Evidence-Centered Session Review

Status: mostly implemented for the server-owned worker path; remaining work is hardening failed-deploy evidence and expanding Kubernetes resource depth beyond the current namespace Resources tab.

Goal:

- Turn a session from raw terminal output into reviewable work evidence.

Build:

- Improve branch state display.
- Improve changed files and diff previews.
- Improve commit and base-branch comparison display.
- Preserve compact command evidence, raw command logs, and runtime metadata, including generic engine session/run refs and historical Codex thread/turn aliases.
- Attach generated session evidence back to the issue.
- Make evidence panels readable without opening a separate operations console.

Acceptance:

- A user can open a session and understand what changed.
- A user can compare the session branch against the project default branch.
- A user can see commits, changed files, diff previews, logs, and evidence summary in one review path.
- A user can understand tests, build/deploy result, preview URL, agent summary, risks, follow-ups, and cleanup/retain state without reading raw logs first.
- Issue Detail remains the primary place to understand the work.

## Milestone 3: Environments And Issue Test Namespace Deployment

Status: mostly implemented for the server-owned worker path; remaining work is deeper Kubernetes evidence parsing and hardening.

Goal:

- Move target access from cluster-only configuration fields to reusable Environments, while keeping Kubernetes issue test deployment manually triggered with a usable preview URL.

Build:

- Store reusable Environments with `kubernetes` and `virtual_machine` kinds. Kubernetes environments project from cluster compatibility records and refresh readiness from kubeconfig checks; VM environments store SSH target metadata and credential references, with `ready` gated by password/private-key SSH login validation.
- Discover regular files under `~/.kube` on first Environments entry, show the candidates and contexts, and let the user choose which kubeconfig files to import.
- Store project default Environment id, currently backed by the Kubernetes compatibility default for issue deploys.
- Store one test environment record per issue: environment id/kind/snapshot, Kubernetes cluster id when applicable, namespace, preview URL, deploy session, cleanup session, namespace state, and cleanup state.
- Show a narrow Resources tab for the current issue namespace: Pods, Services and NodePort mappings, Deployments, Ingresses, and Events.
- Add a manual "Deploy test env" action from Issue Detail, plus opt-in workspace automation for source-session completion.
- Queue an agent deployment turn that creates the issue namespace, builds and pushes images, deploys resources, exposes NodePort by default, uses Ingress when configured, checks the preview URL, and writes preview output back.
- Add manual retain/cleanup namespace decisions from Issue Detail.
- Attach Kubernetes evidence to the issue and session.

Acceptance:

- A user can configure or import kubeconfig path and image registry prefix once in Environments, or record a VM SSH target for future VM-oriented deployment tests.
- A user can trigger a test deployment manually when ready, or explicitly opt a workspace into automatic deploy after successful source sessions.
- The agent creates and manages the issue namespace.
- The UI shows the selected Environment, issue namespace for Kubernetes deploys, namespace state, cleanup state, exposure mode, and preview URL when available.
- The user can refresh current namespace Pods, Services, Deployments, Ingresses, and Events without entering a namespace or leaving the issue.
- Kubernetes evidence is stored and reviewable after the session exits.
- The user does not need a separate terminal just to answer "where can the team test this?"

## Milestone 4: Branch / PR And Cleanup Loop

Status: partially implemented for the server-owned worker path; remaining work is dogfood proof and hardening.

Goal:

- Complete the first end-to-end loop from issue to branch or PR, issue test namespace, preview URL, evidence, and cleanup decision.

Current implementation:

- Captures source commits and semantic source branch evidence through `issue_change_nodes`.
- Captures issue-level branch / PR delivery state through `issue_handoffs`.
- Records branch / PR handoff state from Issue Detail using captured source evidence; GitHub App-backed PR creation/refresh remains a server-owned executor gap.
- Keeps session retain and cleanup controls for worker-managed worktrees.
- Records issue namespace retain/cleanup decisions.
- Records failed sessions and failed environment checks as continueable `session_failures`.

Build next:

- Run one real internal project through the full flow.
- Harden PR body quality, refresh errors, and failed-deploy evidence from that dogfood run.

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
- full multi-user workspace management, membership UI, collaboration sync, and runtime-client authorization;
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
