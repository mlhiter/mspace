# mspace Product Brief

> Status: initial product direction, created 2026-05-06

## One-Line Definition

mspace is an Inbox and Issue workspace for coding agents: a place where teams coordinate work in document-style issues, develop changes in a local-first agent session, and validate those changes in a real, namespace-scoped Kubernetes test environment.

## Why This Exists

The current high-leverage developer workflow is no longer just "ask Codex to edit code." A strong team workflow looks more like this:

1. Start from a real project and a real issue.
2. Keep the problem statement, discussion, and decisions in one durable page.
3. Attach an agent session to the issue.
4. Let the agent modify code in a local development runtime.
5. Give the agent a scoped namespace in the shared test cluster for deployment and validation.
6. Review the PR together with the logs, environment link, and runtime evidence.

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
Inbox
  -> Issue
  -> Agent Session
  -> Local code change
  -> Code change
  -> Deploy to test namespace
  -> Comments and progress updates
  -> Inspect logs/events/resources
  -> PR + environment evidence
```

### Project Setup

A project records:

- repository URL;
- default branch;
- default agent provider;
- test cluster target;
- namespace naming policy;
- bootstrap command;
- deploy command;
- validation command;
- allowed Kubernetes resource scope.

### Runtime and Environment Stance

The development runtime and the validation environment should be treated as separate concepts.

The intended order is:

- local runtime for day-to-day development in the MVP;
- Kubernetes environment for deployment and validation;
- remote or Kubernetes-hosted runtimes later when the local-first flow is solid.

The product wedge is not "agent runs in Kubernetes" on day one. The wedge is "agent can prove the change in a real Kubernetes environment."

### Inbox and Issue Flow

Work should start as Inbox items and Issues, not as raw runtime jobs.

The Inbox should support:

- human-created issues;
- agent-created follow-ups when explicitly allowed;
- assignments to people or agents;
- unread state, subscribers, and status;
- quick triage into issue, project, or archive state.

An Issue should hold:

- title and durable problem statement;
- project link;
- comments and progress updates;
- assignee and subscriber list;
- linked branch, PR, and environment evidence;
- one or more attached agent sessions.

### Session Creation

A user creates a session from an issue. In the MVP path, mspace starts a local-first session and then:

- starts or reuses a local development runtime;
- prepares a workspace with the repository checkout;
- starts the selected coding agent;
- streams terminal output and agent status;
- injects a scoped namespace, ServiceAccount, and kubeconfig for deployment and validation.

### Runtime Operation

Inside the session, the agent can:

- read the issue context and comment history;
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
- command history;
- runtime evidence such as pod status and logs;
- cleanup state: retained, expired, or deleted.

## MVP Scope

The first version should prove one thing:

**A team can create an issue, start a local-first agent session, and watch it modify, deploy, inspect, and validate the project in Kubernetes with all evidence preserved on the issue.**

MVP features:

- inbox list and issue detail;
- project list and project detail;
- issue comments, subscribers, and assignees;
- create agent session from issue;
- local development runtime;
- namespace per project or per session;
- scoped kubeconfig generation;
- terminal/progress stream;
- basic Kubernetes resource view for the attached namespace;
- PR link and environment link capture;
- manual cleanup button.

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
- every issue has status, owner, comments, subscribers, and linked sessions;
- every session has logs, blockers, and evidence;
- a human can pause, resume, or cancel a session;
- the UI explains what runtime, namespace, and cluster the agent can operate.

But the runtime should feel closer to Optio:

- work should develop locally first, then validate against Kubernetes by default;
- isolation is expressed through namespaces, ServiceAccounts, Roles, and quotas;
- each issue/session can later gain its own long-lived or temporary Kubernetes-hosted runtime when the product grows into that model;
- self-hosting on a team's own cluster is a first-class deployment model.

## Key Product Bet

The product assumes that serious coding agents need both durable collaboration context and real deployment feedback. If agents only edit files and open PRs, existing agent dashboards are enough. If teams only want a document tool, existing issue trackers are enough. mspace becomes valuable when the issue itself is also the launch point for a real Kubernetes deployment and test environment.

## First Usability Test

Use one internal project and one test cluster.

The test is successful if a developer can:

1. create a project in mspace;
2. create an issue in the inbox flow;
3. start a local-first session with Codex from that issue;
4. let Codex operate only the assigned repository and scoped test namespace;
5. deploy or inspect the project through `kubectl`;
6. open a PR or leave a branch with runtime evidence attached to the issue;
7. clean up the session namespace without manual cluster surgery.
