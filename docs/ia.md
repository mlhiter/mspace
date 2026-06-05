# mspace MVP Information Architecture

> Status: local MVP implementation snapshot, updated 2026-06-04

## IA Goal

The first version of mspace should feel like a document-first issue workspace with attached agent execution, not like a Kubernetes console with some comments around it. The current visual reference is Notion: quiet navigation, paper-like pages, compact rows, subdued metadata, and focused document surfaces.

The user should be able to answer four questions quickly:

- what needs attention now;
- which issue is the source of truth;
- what the attached agent is doing;
- what runtime evidence exists for the current issue.

The key balance is:

- the issue page is the primary working surface;
- the registered worker session is the primary development surface;
- the selected Environment is manually triggered from the issue after agent work is ready to preview, with Kubernetes namespace deployment as the current issue deploy path.

## Primary Navigation

The first MVP should keep the navigation narrow:

- Inbox
- Issues
- Tests
- Agents
- Environments
- Projects

Navigation rules:

- Inbox is the default entry screen.
- Inbox is the unread review surface for issue and session updates.
- Issues is the durable knowledge surface and issue creation home.
- Tests is the workspace quality surface for project-level cases/suggestions and workspace-level plans/runs. It sits after Issues because test execution still routes through issue-backed worker sessions.
- Agents is the managed profile surface for Codex-backed collaborators and mentions.
- Environments is reusable target access: Kubernetes kubeconfig import, reachability status, registry/exposure defaults, and virtual machine SSH target metadata with password/private-key login validation.
- Projects is configuration and project-level history.
- Workspace Settings is accessed from the workspace identity menu instead of the main rail, because it controls automation, membership, and runtime worker policy for the current workspace rather than daily issue work.
- Language switching also lives in the workspace identity menu, because English/Simplified Chinese is a global desktop preference rather than a route-specific action.
- Session detail is deep-linked from issues and remains an operational fallback view, not a primary home.

The fact that Inbox is first does not mean Environments are secondary. It means the product starts from work intake, then routes that work into a worker-backed development flow plus selected-environment validation.

## Object Hierarchy

```text
Workspace
  -> Inbox Item
      -> Issue
          -> Agent Session
              -> Runtime
              -> Environment
                  -> Evidence
  -> Project
      -> Test Case
  -> Test Plan
      -> Test Run
```

The ownership model should be clear:

- Inbox Item is for triage.
- Issue is for durable collaboration.
- Agent Session is for execution.
- Runtime is the worker execution surface.
- Environment is the target being operated, currently Kubernetes or virtual machine.
- Evidence is the result attached back to the issue.
- Test Case is project-level quality knowledge; Test Plan and Test Run are workspace-level orchestration and acceptance records that preserve per-case project identity.

## Screen Map

Current implemented desktop routes:

```text
/inbox
/issues
/issues/:issueId
/issues/:issueId/commits/:commitSha
/issues/:issueId/evidence/history
/issues/:issueId/evidence/snapshots
/tests
/tests/cases/:caseId
/tests/plans/:planId
/tests/runs/:runId
/agents
/environments
/clusters
/projects
/settings
/invite/:token
/sessions/:sessionId
```

Planned but not implemented yet:

```text
/projects/:projectId
/sessions
```

The current sidebar exposes Inbox, Issues, Tests, Agents, Environments, and Projects, with a global search / Command+K palette for issues and projects plus a quick issue creation link. `/clusters` remains a compatibility route for existing Kubernetes records, but product navigation should use `/environments`. The workspace menu owns workspace switching, team workspace creation, language switching, and the entry into Workspace Settings. Workspace Settings owns workspace automation, team-only access controls, and runtime worker/queue controls for the selected workspace. The invite route is a deep-link entry that resolves the target team server, shows safe invitation context, handles login or registration, accepts the invite, and lands in the invited workspace. Session detail remains deep-linked from issue work.

## Visual Language

Current implementation principles:

- left sidebar contains the workspace identity/account menu, global search / Command+K palette for issues and projects, quick issue creation, primary navigation, active issue work, and runtime worker state;
- Inbox and Project lists use row-level cards and compact metadata rather than dashboard tiles;
- Issue Detail should read as a live document with session and evidence context attached around it;
- Session Detail can be more operational, but should still preserve the same paper workspace tone;
- shadcn/ui primitives are the base for buttons, cards, inputs, fields, badges, alerts, separators, selects, scroll areas, and textareas;
- lucide-react icons should carry common actions where a familiar symbol is clearer than text.
- visible shell and main workflow copy should use the shared English/Simplified Chinese locale resources; technical identifiers, logs, user-authored content, branch names, commit hashes, Kubernetes names, and runtime protocol values should remain literal.

Avoid hero sections, marketing copy, decorative dashboards, and high-saturation visual effects in the product shell. mspace should feel like a calm operations workspace that happens to run serious Kubernetes-backed agent sessions.

## Inbox

### Purpose

Inbox is where unread issue updates appear for review.

### List structure

Each row should show:

- title;
- project, or `No project` for workspace-level issues that have not been attached yet;
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

- lists signed-in workspace Inbox items from server issue-event receipts;
- polls server receipts for the current workspace;
- shows a sidebar count badge when unread Inbox items exist;
- navigates to issue detail for review and action;
- keeps assignee state visible so agent and human ownership changes are obvious.

## Issues

### Purpose

Issues is the durable list and creation surface for real work.

The default Issues list should show human-owned collaboration work, not internal test automation carriers. Issues created only to support test case optimization, test case generation, or Test Run execution remain directly reachable for audit, logs, evidence, and session detail, but their primary discovery surface is Tests.

### List structure

Each row should show:

- title;
- body preview;
- project, or `No project` for workspace-level issues that have not been attached yet;
- owner avatar and display name;
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
- uses a TipTap-backed Markdown document editor for the issue note, including task-list input and pasted or dropped image attachments rendered as thumbnails;
- does not show a project selector during issue creation; the issue may start as a workspace-level document without a project, and a single existing project can be auto-attached when unambiguous;
- keeps the creation modal minimal: issue note only, with project attachment handled from Issue Detail when the repository becomes necessary;
- supports search, status/type/priority filters, and sorting by updated time, created time, priority, or type;
- shows unread state, stored owner avatar/name, owner type, linked session count, child task count, and labels inline.

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
- project, or `No project` when the issue is still workspace-level;
- status;
- assignee;
- latest activity summary.

Primary action should stay in the reply composer, not in a large runtime control panel. To ask an agent, the user writes a normal issue comment and mentions an enabled agent from the Agents module.

When an issue has no attached project, Issue Detail should stay readable and commentable but block agent execution, PR handoff, test environments, and project runbook access until a project is attached from the Project sidebar section.

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

Status changes are timeline events, not long comments. Render them in one line as the actor changing status from one readable badge to another. Do not expose stored raw values such as `in_progress` or `ready_for_test` in the user-facing event text.

### Reply composer

The composer is the main interaction control:

- Markdown comments stay on the issue through the same TipTap-backed document editor used for issue creation, including image upload, paste, drop, and thumbnail previews;
- comment reactions stay as lightweight metadata on comments and should not rewrite the Markdown body or agent prompt history;
- the latest human-authored comment can be edited inline only while it is still unconsumed by an agent session, including adding a supported agent mention and then saving that edit to queue the turn;
- supported agent mentions first verify a matching active Codex worker, then save the trigger comment and queue a worker-backed Codex app-server turn with the selected managed profile;
- unsupported agent mentions should be visible but not queued;
- when an agent is already working, a second agent turn should be disabled until the current turn finishes or is stopped.
- issue lifecycle actions live in the composer footer with the comment submit controls. Show the primary close or reopen action directly, hide less common close reasons such as `Close as not planned` behind a compact dropdown, and do not repeat the current issue status inside the composer.

The UI can provide lightweight mention assistance, but it should not feel like a separate command console.

### Agent turn summary

Agent turns should appear inline in the timeline and show the currently attached session first:

- provider and model;
- runtime mode and selected worker, with personal/team or Kubernetes-hosted fixed worker called out explicitly;
- deployment target Environment and namespace when attached;
- current state;
- branch;
- latest agent summary;
- last updated time.

Secondary actions:

- open full session;
- expand execution details.

### Evidence tab

The Evidence tab should summarize the current review packet, not act as a second logs or Kubernetes resources page.

The default Evidence view should show:

- code-change handoff through the Commits tab;
- session branch and source commit identity;
- the current review packet verdict and status signals;
- agent summary;
- compact command evidence for the current packet;
- risks and follow-ups;
- source and capture facts.

It should answer "can I trust this review packet?" without forcing the user through raw logs. It should not repeat the Resources tab, duplicate the current packet validation rows, or expand historical failures in the right rail.

Historical or wide information has its own pages:

- `Previous attempts` opens `/issues/:issueId/evidence/history` for older review evidence, interruptions, failures, and blockers.
- `Kubernetes evidence` opens `/issues/:issueId/evidence/snapshots` for deployment evidence snapshots, resource tables, and events.
- Live namespace inspection stays in the Resources tab.

In the default test path, the current review packet should be grounded in the issue namespace, source commit, preview URL check, build/deploy/test result, and command evidence rather than generic agent logs alone.

Current implementation:

- shows issue body first, then a timeline of human comments, Codex turns, and failure/deploy attention events;
- supports rich Markdown comments and managed agent mentions from the same reply box;
- reads enabled mention suggestions from the Agents module instead of a frontend constant;
- checks worker liveness before saving an agent trigger comment; personal desktop mode may auto-start the local worker, while team workspaces require a connected team worker;
- saves the trigger comment before queuing the worker-backed Codex session, so accepted turns are visible in the issue history;
- sends the mention-stripped comment as the current turn request, ahead of the original issue context;
- shows Type and Priority controls in the quiet metadata sidebar, with a `Classifying...` state while a worker-backed `issue_type_triage` task assigns type;
- keeps the quiet metadata sidebar on Overview only, while Commits, Sessions, and Evidence use the full page width for review-heavy content;
- exposes a Stop action for queued or running sessions, cancelling only that session and rendering the stop as a compact, non-editable event while leaving the issue status unchanged;
- streams session logs and status while a session is running, but keeps debug output collapsed by default;
- renders a compact failure callout with the last meaningful runtime error when a session fails;
- renders structured failure records as continueable timeline and Evidence entries, including failed command, error summary, namespace/resource hints, and Continue / Retry deploy / Stop affordances when applicable;
- exposes manual test deployment controls in the metadata sidebar: deploy test env, cleanup namespace, and retain namespace;
- shows selected Environment, issue test namespace state, cleanup state, exposure mode, and preview URL when available;
- automatically checks preview status in the background when Issue Detail opens or refreshes an existing test environment, updating only the Test environment sidebar state and `Checked` time instead of exposing a separate Probe button or adding timeline evidence;
- exposes a Resources tab for the issue's current Kubernetes test namespace, refreshed on tab entry or manual refresh, with Environment/context/lifecycle/exposure/cleanup/preview metadata plus Pods, Services, Deployments, Ingresses, and Events;
- separates source review, live resources, and review evidence: Commits shows code changes and diffs, Resources shows live namespace objects, and Evidence shows the current review packet with command evidence, agent summary, risks/follow-ups, source facts, plus links to full-width previous-attempt and Kubernetes-snapshot history pages;
- shows issue-level branch / PR handoff state on the Commits tab and sidebar, with actions to record one handoff for the selected source branch and refresh the server-owned handoff record; GitHub App-backed PR creation/refresh remains a later executor step;
- keeps raw command trails collapsed in session logs, with exploratory commands excluded from persisted review evidence;
- shows a compact Project runbook entry in the Workflow sidebar; clicking it opens a read-only TipTap runbook modal;
- renders the issue creator, human comments, system comments, Codex-backed agent turns, and actor-authored status changes with their current display names and avatar sources;
- renders comment reactions inline with quiet reaction chips and a compact icon picker;
- renders status changes as compact one-line events with `from` and `to` badges instead of showing the full stored status-change comment body;
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
The two-column layout is the Overview shape. Review-heavy tabs such as Commits, Sessions, and Evidence hide the metadata sidebar and expand the main column inside the same page frame.

Do not reuse this Overview shape for object list/detail browsing. Lists such as Tests cases, plans, runs, projects, sessions, or future object collections should open row details on dedicated pages. A persistent left-list/right-detail pane is too cramped for mspace's document-first workspace and should not be introduced as a general pattern.

## Projects

### Purpose

Projects hold repository and runtime policy, not daily conversation.

### Project List

Each row should show:

- project name;
- default Environment or local fallback;
- runbook status;
- local repository path;
- active issues;
- active sessions;
- repository metadata;
- an icon-only settings action.

### Project Settings

Project Settings should show:

- mspace-owned runbook, edited as Markdown in the document editor;
- runbook status and learned source metadata;
- project name;
- repository settings;
- default Environment/runtime defaults;
- guarded delete action.

The Project view should help operators configure the system without turning it into the primary working surface.

Current implementation:

- lists projects;
- creates personal projects in a modal from either a local folder picker or a GitHub repository URL;
- creates team projects from GitHub repository URLs only, because team workers clone source into their own repo cache and cannot read a user's desktop-local folder path;
- auto-detects GitHub metadata for repositories when a remote URL exists;
- opens Project settings as a full page, not a modal;
- edits project name, default Environment, and the mspace-owned Markdown runbook from that page;
- exposes the project runbook from Issue Detail as a read-only TipTap modal so users can inspect runbook knowledge without leaving the issue;
- only allows deletion before issues or sessions exist;
- stores runbook history in `project_runbooks` and `project_runbook_revisions`, plus the default Environment id. The current database keeps the Kubernetes compatibility field as `default_cluster_id` and exposes it as the product-facing default Environment.

## Workspace Settings

### Purpose

Workspace Settings owns workspace automation, team access, and runtime worker policy for the current workspace.

### Current implementation

- opens from the workspace identity menu, not from the main navigation rail;
- lets team workspace owners/admins edit the current team workspace name, mark, and description from the identity section;
- keeps source commit capture always on for issue review and deploy continuation;
- shows team workspace creation only to server admins, while ordinary registered users stay in personal workspaces until invited;
- lets team owners/admins create and revoke one-time join links, with the copy action beside the link and no email field, join code, invitation id, or token debug text in the normal UI;
- routes signed-out invite recipients through a safe preview plus login or registration, then accepts the invite and opens the invited workspace without showing another confirmation screen;
- connects worker runtime hosts through owner/admin-only one-time install commands, keeps runtime mode fixed to the current workspace kind, and leaves raw runtime credential creation to API/debug paths;
- separates active worker credentials from expired or replaced credential history, with desktop personal-worker credentials labeled as automatic;
- shows runtime tasks as issue-linked operational rows with Task, Issue, Status, Worker, Updated, and Action columns;
- paginates runtime tasks so Workspace Settings stays bounded when the queue history grows;
- links issue-backed runtime tasks to the issue page, and agent-session tasks to the matching Issue Detail session when a session exists;
- keeps raw task kind, required capabilities, payload, result, events, and logs in expandable task details;
- does not expose a generic queue-task button in the normal UI, since product flows create issue triage, agent-session, and deploy/test tasks;
- exposes handoff automation policy while source commit capture stays always on;
- explains that GitHub App-backed PR automation is a server-owned executor step that is still future work.

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
- keeps changed-file lists file-focused by hiding directory-only placeholder entries and using IDE-style file type icons for visible file rows;
- generates an issue-summary draft from the session and can post it back to the issue.

## State Model

The MVP should keep states simple.

Inbox item states:

- unread
- read
- archived

Issue states:

- open
- needs_review
- changes_requested
- ready_for_test
- blocked
- cancelled (closed as not planned)
- closed

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
- Comments, reactions, and progress updates
- Manage Agents and queue worker-backed Codex sessions from enabled agent-profile issue comments
- Issue labels
- Stop queued or running sessions
- Agent turns inline on the issue timeline
- Evidence tab plus Test environment sidebar state, without health-check noise in the issue timeline
- Project settings, workspace automation policy, and runtime defaults
- Tests case library, case suggestions, workspace plans, and issue-backed test runs
- Worker session startup with git worktree isolation
- Manual cleanup for retained worker session workdirs
- Session detail with logs and workspace evidence
- worker session startup with Environment and namespace visibility
- manual issue test deployment with issue namespace lifecycle state
- narrow Resources tab for the current issue namespace

Can wait until later:

- multi-column kanban views
- advanced issue dependencies
- complex workflow automation
- multiple simultaneous session comparisons
- custom dashboard analytics
- cluster-wide or VM-wide observability
- generated scoped kubeconfig and ServiceAccount lifecycle
- full Kubernetes namespace resource browser beyond the current issue-scoped Pods, Services, Deployments, Ingresses, and Events view

## Design Guidance

The UI should feel operational, quiet, and dense enough for real work.

Design rules:

- prefer readable document layouts over card-heavy marketing composition;
- treat the issue body as a real page with generous writing space;
- route object details to dedicated pages instead of side-by-side list/detail panes;
- keep session and evidence details compact and inspectable;
- avoid making Kubernetes details the first visual focus;
- make agent activity legible without turning the screen into a terminal wall.

Avoiding Kubernetes as the first visual focus does not mean hiding it. Kubeconfig, issue namespace, exposure mode, preview URL, pod health, and rollout state should remain visible enough that the user always knows which environment the issue is operating in.

## Build Sequence

Implemented as of 2026-06-03:

1. Inbox review list, Issues list, and issue creation flow.
2. Issue detail shell with document body and activity thread.
3. Project create, settings, guarded delete, and repository validation.
4. Managed Agents route plus dynamic mention flow from issue comments.
5. Inline agent turn summaries and live session state updates.
6. Session detail with logs, workspace snapshot, branch comparison, and issue summary draft.
7. Server control plane, runtime worker registration, and git worktree isolation.
8. Tailwind CSS 4 monorepo source detection for desktop UI packages.
9. Issue labels, stop controls for active sessions, and manual worktree cleanup.
10. Environments route with desktop file picker import for Kubernetes kubeconfigs, first-run `~/.kube` discovery, context listing, reachability status, registry/preview exposure defaults, and virtual machine SSH metadata.
11. Issue test environment records plus manual deploy/cleanup/retain actions.
12. Server-backed mspace sign-in: default local personal mode starts on account creation and hides GitHub, while explicitly configured team servers can offer login plus optional GitHub OAuth when `/health` reports `capabilities.githubAuth: true`.
13. Sidebar global search and Command+K palette for issues and projects.
14. Commits/Evidence split on Issue Detail, with structured `session_review_evidence` snapshots and compact evidence-command persistence.
15. Issue-level branch / PR handoff records, including local PR creation, existing PR auto-detection by source branch, status refresh, source commits, preview URL, and evidence summary.
16. Structured `session_failures` records that surface failed sessions, deploy-time preview verification failures, agent interruption, and cleanup failures as continueable Issue Detail timeline and Evidence entries.
17. Preview status refreshes that update Test environment state and `Checked` time without adding healthy snapshot cards to the Overview timeline.
18. Issue Resources tab for the fixed test namespace, using live Kubernetes resource reads without exposing cross-namespace browsing.
19. Tests route with project-level Cases and Case suggestions, workspace-level Plans and Runs, dedicated detail pages, modal case create/import flows, preview-before-confirm Excel `.xlsx` and text-like file import, and issue-backed test run execution.

Next build steps:

1. Dogfood one real issue through source change, PR handoff, test namespace, preview URL, failure recovery if needed, and cleanup/retain.
2. Harden Kubernetes resource parsing and failure evidence quality.
3. Scoped kubeconfig or ServiceAccount generation.
4. Standalone Sessions list view.
