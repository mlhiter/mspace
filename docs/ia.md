# mspace MVP Information Architecture

> Status: initial IA draft, created 2026-05-06

## IA Goal

The first version of mspace should feel like a document-first issue workspace with attached agent execution, not like a Kubernetes console with some comments around it.

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
- Projects
- Sessions

Navigation rules:

- Inbox is the default entry screen.
- Issues is the durable knowledge surface.
- Projects is configuration and project-level history.
- Sessions is an operational fallback view, not the primary home.

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

```text
/inbox
/issues
/issues/:issueId
/projects
/projects/:projectId
/sessions
/sessions/:sessionId
```

The MVP does not need more top-level areas than this.

## Inbox

### Purpose

Inbox is where new work arrives and gets shaped into issues.

### List structure

Each row should show:

- title;
- project;
- latest activity time;
- assignee;
- unread state;
- current status;
- whether an agent session exists.

### Primary actions

- create issue;
- assign to human;
- assign to agent;
- mark read or unread;
- archive;
- open issue detail.

### Layout

```text
+--------------------------------------------------------------+
| Filters / Search / Quick Create                             |
+----------------------+---------------------------------------+
| Inbox list           | Optional preview of selected item     |
| - status             | - summary                             |
| - unread             | - recent comments                     |
| - assigned           | - linked issue                        |
+----------------------+---------------------------------------+
```

The preview can be dropped in the earliest implementation if it slows execution.

## Issue Detail

### Purpose

Issue Detail is the main working screen. It should read like a live document with attached execution.

### Core regions

- Header
- Document body
- Activity thread
- Session panel
- Evidence panel

### Header

The header should show:

- title;
- project;
- status;
- assignee;
- subscribers;
- runtime mode summary;
- quick actions.

Primary header actions:

- edit issue;
- add comment;
- start session;
- pause or resume session;
- copy branch or PR link;
- open environment.

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
- agent progress updates;
- status changes;
- blocker notices;
- session lifecycle events.

System events should be visually quieter than human and agent messages.

### Session panel

The session panel should show the currently attached session first:

- provider and model;
- runtime type, with local called out explicitly in the MVP;
- deployment target cluster and namespace when attached;
- current state;
- branch;
- latest log line;
- last updated time.

Secondary actions:

- open full session;
- restart;
- cancel;
- attach a new session.

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

### Layout

```text
+-------------------------------------------------------------------+
| Header: title / status / assignee / start session / env / PR      |
+------------------------------------------+------------------------+
| Document body                            | Session panel          |
| - context                                | - current session      |
| - acceptance criteria                    | - runtime summary      |
| - implementation notes                   | - key actions          |
+------------------------------------------+------------------------+
| Activity thread                          | Evidence panel         |
| - comments                               | - PR                   |
| - progress updates                       | - env                  |
| - blockers                               | - pod status           |
| - system events                          | - logs/events          |
+------------------------------------------+------------------------+
```

The document body and activity thread are the center of gravity. Session and evidence should support them, not compete with them.

## Projects

### Purpose

Projects hold repository and runtime policy, not daily conversation.

### Project List

Each row should show:

- project name;
- default branch;
- default agent provider;
- active issues;
- active sessions;
- namespace policy.

### Project Detail

Project Detail should show:

- repository settings;
- runtime defaults;
- linked issues;
- linked sessions;
- recent environment failures.

The Project view should help operators configure the system without turning it into the primary working surface.

## Sessions

### Purpose

Sessions is the operational list view for people who need to monitor running work across many issues.

### Session List

Each row should show:

- linked issue;
- project;
- agent provider;
- runtime type;
- status;
- branch;
- latest activity time.

### Session Detail

Session Detail should prioritize:

- live terminal stream;
- runtime metadata;
- branch and PR output;
- namespace resources;
- cleanup actions.

This page is for deep execution inspection. It should not replace Issue Detail as the default place to work.

## State Model

The MVP should keep states simple.

Inbox item states:

- new
- triaged
- archived

Issue states:

- open
- in_progress
- blocked
- in_review
- done

Session states:

- queued
- starting
- running
- blocked
- failed
- completed
- canceled

Avoid a large workflow matrix in v1.

## First Screen

The first screen after sign-in should be Inbox, with enough density to triage fast:

- active items near the top;
- unread visible at a glance;
- direct path into the underlying issue;
- direct path to start an agent session.

The first screen should not be a dashboard full of charts.

## MVP Cut Line

Must-have for MVP:

- Inbox list
- Issue detail as the main work surface
- Comments and progress updates
- Start session from issue
- Session panel on issue page
- Evidence panel on issue page
- Project settings and runtime defaults
- Session detail with logs and namespace evidence
- local session startup with cluster and namespace visibility

Can wait until later:

- multi-column kanban views
- advanced issue dependencies
- complex workflow automation
- multiple simultaneous session comparisons
- custom dashboard analytics
- cluster-wide observability

## Design Guidance

The UI should feel operational, quiet, and dense enough for real work.

Design rules:

- prefer readable document layouts over card-heavy marketing composition;
- treat the issue body as a real page with generous writing space;
- keep session and evidence side panels compact and inspectable;
- avoid making Kubernetes details the first visual focus;
- make agent activity legible without turning the screen into a terminal wall.

Avoiding Kubernetes as the first visual focus does not mean hiding it. Cluster, namespace, pod health, and rollout state should remain visible enough that the user always knows which environment the issue is operating in.

## Build Sequence

1. Inbox list and issue creation flow.
2. Issue detail shell with document body and activity thread.
3. Session creation from issue.
4. Session panel and session state updates.
5. Evidence panel with placeholder data shape.
6. Runtime provider wiring.
7. Kubernetes-backed evidence and namespace inspection.
