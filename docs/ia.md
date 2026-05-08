# mspace MVP Information Architecture

> Status: local MVP implementation snapshot, updated 2026-05-08

## IA Goal

The first version of mspace should feel like a document-first issue workspace with attached agent execution, not like a Kubernetes console with some comments around it. The current visual reference is Notion: quiet navigation, paper-like pages, compact rows, subdued metadata, and focused document surfaces.

The user should be able to answer four questions quickly:

- what needs attention now;
- which issue is the source of truth;
- what the attached agent is doing;
- what runtime evidence exists for the current issue.

The key balance is:

- the issue page is the primary working surface;
- the local session is the primary development surface;
- the Kubernetes environment is the default validation surface behind it.

## Primary Navigation

The first MVP should keep the navigation narrow:

- Inbox
- Issues
- Agents
- Projects

Navigation rules:

- Inbox is the default entry screen.
- Inbox is the unread review surface for issue and session updates.
- Issues is the durable knowledge surface and issue creation home.
- Agents is the managed profile surface for Codex-backed collaborators and mentions.
- Projects is configuration and project-level history.
- Session detail is deep-linked from issues and remains an operational fallback view, not a primary home.

The fact that Inbox is first does not mean Kubernetes is secondary. It means the product starts from work intake, then routes that work into a local development flow plus a Kubernetes-backed validation flow.

## Object Hierarchy

```text
Workspace
  -> Inbox Item
      -> Issue
          -> Agent Session
              -> Runtime
                  -> Evidence
```

The ownership model should be clear:

- Inbox Item is for triage.
- Issue is for durable collaboration.
- Agent Session is for execution.
- Runtime is for environment access.
- Evidence is the result attached back to the issue.

## Screen Map

Current implemented desktop routes:

```text
/inbox
/issues
/issues/:issueId
/agents
/projects
/sessions/:sessionId
```

Planned but not implemented yet:

```text
/projects/:projectId
/sessions
```

The MVP does not need more top-level areas than this. The current sidebar exposes Inbox, Issues, Agents, and Projects, with a quick issue creation link under the search affordance. Session detail remains deep-linked from issue work.

## Visual Language

Current implementation principles:

- left sidebar contains the workspace identity, search affordance, quick issue creation, primary navigation, namespace hints, and local runner state;
- Inbox and Project lists use row-level cards and compact metadata rather than dashboard tiles;
- Issue Detail should read as a live document with session and evidence context attached around it;
- Session Detail can be more operational, but should still preserve the same paper workspace tone;
- shadcn/ui primitives are the base for buttons, cards, inputs, fields, badges, alerts, separators, selects, scroll areas, and textareas;
- lucide-react icons should carry common actions where a familiar symbol is clearer than text.

Avoid hero sections, marketing copy, decorative dashboards, and high-saturation visual effects in the product shell. mspace should feel like a calm operations workspace that happens to run serious Kubernetes-backed agent sessions.

## Inbox

### Purpose

Inbox is where unread issue updates appear for review.

### List structure

Each row should show:

- title;
- project;
- latest activity time;
- assignee;
- unread state;
- current status;
- enough context to jump back into the linked issue.

### Primary actions

- open issue detail.

### Layout

```text
+--------------------------------------------------+
| Inbox list                                        |
| - unread update                                  |
| - project + assignee + timestamp                 |
| - linked issue status                            |
+--------------------------------------------------+
```

Current implementation:

- lists inbox items from the local runner;
- refreshes through `/api/inbox/stream`;
- navigates to issue detail for review and action;
- keeps assignee state visible so agent and human ownership changes are obvious.

## Issues

### Purpose

Issues is the durable list and creation surface for real work.

### List structure

Each row should show:

- title;
- body preview;
- project;
- owner;
- current status;
- unread marker;
- attached session count.
- issue labels.

### Primary actions

- create issue;
- open issue detail.

### Layout

```text
+--------------------------------------------------------------+
| Header / New issue                                           |
+--------------------------------------------------------------+
| Issue list                                                   |
| - title + preview                                            |
| - project                                                    |
| - owner                                                      |
| - state                                                      |
+--------------------------------------------------------------+
```

Current implementation:

- lists issues across the workspace;
- opens create issue in a modal from the page header or the sidebar quick action;
- allows the user to leave project empty and let the runner resolve it when possible;
- shows unread state, owner type, and linked session count inline.
- shows issue labels inline when present.

## Issue Detail

### Purpose

Issue Detail is the main working screen. It should read like a live document with attached execution.

### Core regions

- Header
- Document body
- Activity thread
- Reply composer
- Quiet metadata sidebar
- Collapsed execution details

### Header

The header should show:

- title;
- project;
- status;
- assignee;
- latest activity summary.

Primary action should stay in the reply composer, not in a large runner control panel. To ask an agent, the user writes a normal issue comment and mentions an enabled agent from the Agents module.

### Document body

The document body should hold:

- problem statement;
- acceptance criteria;
- implementation notes;
- links and references.

This area should feel like a durable page, not a tiny description field.

### Activity thread

The activity thread should mix:

- human comments;
- agent turns;
- status changes;
- blocker notices;
- session lifecycle events.

System events should be visually quieter than human and agent messages. Raw thread ids, turn ids, worktree paths, logs, and artifact paths should stay collapsed behind execution details unless the user is debugging.

### Reply composer

The composer is the main interaction control:

- plain text comments stay on the issue;
- supported agent mentions save the comment and start a Codex app-server turn with the selected managed profile;
- unsupported agent mentions should be visible but not queued;
- when an agent is already working, a second agent turn should be disabled until the current turn finishes or is stopped.

The UI can provide lightweight mention assistance, but it should not feel like a separate command console.

### Agent turn summary

Agent turns should appear inline in the timeline and show the currently attached session first:

- provider and model;
- runtime type, with local called out explicitly in the MVP;
- deployment target cluster and namespace when attached;
- current state;
- branch;
- latest agent summary;
- last updated time.

Secondary actions:

- open full session;
- expand execution details.

### Evidence panel

The evidence panel should summarize:

- PR link;
- branch link;
- environment URL;
- cluster and namespace summary;
- pod health;
- deployment status;
- recent events;
- recent logs.

It should answer "did this actually run" without forcing the user into a separate ops view.

In the default path, the answer should be grounded in Kubernetes deployment and test evidence rather than generic agent logs alone.

Current implementation:

- shows issue body first, then a timeline of human comments, Codex turns, and evidence;
- supports plain comments and managed agent mentions from the same reply box;
- reads enabled mention suggestions from the Agents module instead of a frontend constant;
- saves the comment before queuing the Codex app-server session, so the current turn is visible in the issue history;
- sends the mention-stripped comment as the current turn request, ahead of the original issue context;
- shows issue labels in the quiet metadata sidebar and lets users edit them inline;
- exposes a Stop action for queued or running sessions;
- streams session logs and status while a session is running, but keeps debug output collapsed by default;
- links into full session detail for deep inspection.

### Layout

```text
+-------------------------------------------------------------------+
| Header: title / project / status                                  |
+------------------------------------------+------------------------+
| Document body                            | Quiet metadata         |
| - context                                | - status / owner       |
| - acceptance criteria                    | - project / namespace  |
| - implementation notes                   | - latest session       |
+------------------------------------------+------------------------+
| Timeline                                 | Collapsed details      |
| - human comments                         | - commands             |
| - agent turns                            | - thread / turn ids    |
| - evidence                               | - logs / artifacts     |
+------------------------------------------+------------------------+
| Reply composer: comment or agent current turn                     |
+-------------------------------------------------------------------+
```

The document body and activity thread are the center of gravity. Session and evidence should support them, not compete with them.

## Projects

### Purpose

Projects hold repository and runtime policy, not daily conversation.

### Project List

Each row should show:

- project name;
- default branch;
- local repository path;
- active issues;
- active sessions;
- Kubernetes context and namespace.

### Project Detail

Project Detail should show:

- repository settings;
- runtime defaults;
- linked issues;
- linked sessions;
- recent environment failures.

The Project view should help operators configure the system without turning it into the primary working surface.

Current implementation:

- lists projects;
- creates projects in a modal from either a local folder picker or a GitHub repository URL;
- auto-detects GitHub metadata for local repositories when a remote exists;
- edits runtime settings in a separate settings modal;
- only allows deletion before issues or sessions exist;
- stores deploy and validation commands plus Kubernetes context and namespace.

## Sessions

### Purpose

Session Detail is the operational drill-down for people who need to inspect one running or completed session.

### Session List

Not implemented yet as a standalone top-level page.

### Session Detail

Session Detail should prioritize:

- live terminal stream;
- runtime metadata;
- branch and worktree state;
- evidence and environment output;
- summary and review handoff.

This page is for deep execution inspection. It should not replace Issue Detail as the default place to work.

Current implementation:

- shows session metadata, agent instructions, Codex thread/turn state, branch, workdir, status, issue, and project;
- exposes the stored agent profile in metadata and generated summaries;
- exposes manual worktree cleanup for retained, non-active sessions;
- shows session-scoped logs and evidence;
- inspects the session worktree with `git status`, changed files, and diff preview;
- compares the session branch against the project default branch through merge-base, ahead/behind count, commits, changed files, and diff preview;
- generates an issue-summary draft from the session and can post it back to the issue.

## State Model

The MVP should keep states simple.

Inbox item states:

- unread
- read

Issue states:

- open
- running
- completed
- failed
- cancelled

Session states:

- queued
- running
- failed
- completed
- cancelled

Avoid a large workflow matrix in v1.

## First Screen

The first screen after sign-in should be Inbox, with enough density to triage fast:

- active items near the top;
- unread visible at a glance;
- direct path into the underlying issue;
- direct path to review and resume agent work.

The first screen should not be a dashboard full of charts.

## MVP Cut Line

Must-have for MVP:

- Inbox review list
- Issues list and issue creation flow
- Issue detail as the main work surface
- Comments and progress updates
- Manage Agents and start Codex from an enabled agent-profile issue comment
- Issue labels
- Stop queued or running sessions
- Agent turns inline on the issue timeline
- Evidence attached to the issue timeline
- Project settings and runtime defaults
- Local session startup with git worktree isolation
- Manual cleanup for retained local session worktrees
- Session detail with logs and workspace evidence
- local session startup with cluster and namespace visibility

Can wait until later:

- multi-column kanban views
- advanced issue dependencies
- complex workflow automation
- multiple simultaneous session comparisons
- custom dashboard analytics
- cluster-wide observability
- generated scoped kubeconfig and ServiceAccount lifecycle
- full Kubernetes namespace resource browser

## Design Guidance

The UI should feel operational, quiet, and dense enough for real work.

Design rules:

- prefer readable document layouts over card-heavy marketing composition;
- treat the issue body as a real page with generous writing space;
- keep session and evidence details compact and inspectable;
- avoid making Kubernetes details the first visual focus;
- make agent activity legible without turning the screen into a terminal wall.

Avoiding Kubernetes as the first visual focus does not mean hiding it. Cluster, namespace, pod health, and rollout state should remain visible enough that the user always knows which environment the issue is operating in.

## Build Sequence

Implemented as of 2026-05-08:

1. Inbox review list, Issues list, and issue creation flow.
2. Issue detail shell with document body and activity thread.
3. Project create, settings, guarded delete, and repository validation.
4. Managed Agents route plus dynamic mention flow from issue comments.
5. Inline agent turn summaries and live session state updates.
6. Session detail with logs, workspace snapshot, branch comparison, and issue summary draft.
7. Local runner process, SQLite storage, and git worktree isolation.
8. Tailwind CSS 4 monorepo source detection for desktop UI packages.
9. Issue labels, stop controls for active sessions, and manual worktree cleanup.

Next build steps:

1. Kubernetes-backed evidence and namespace inspection.
2. Scoped kubeconfig or ServiceAccount generation.
3. Namespace cleanup lifecycle.
4. Standalone Sessions list view.
