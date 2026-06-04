# mspace Product Value Thesis

> Status: value thesis from product discussion, updated 2026-06-04

## Core Judgment

mspace is worth building if it proves one workflow:

```text
Issue
  -> Agent session
  -> Code changes
  -> Review evidence
  -> Selected Environment
  -> Issue-scoped Kubernetes test environment when applicable
  -> Preview URL
  -> Branch or PR
  -> Cleanup or retain decision
```

The product should not try to prove that coding agents can edit code. Codex, Claude Code, Cursor, and similar tools already do that. mspace should prove that agent work can become a reviewable issue lifecycle that a team can understand, validate, hand off, and close.

The working thesis is:

> mspace does not make agents better at writing code. It makes agent output easier for a team to verify, inherit, and finish.

## Why This Is Not Just a Codex Wrapper

Codex can already handle much of the raw work:

- inspect a repository;
- edit files;
- run commands;
- debug failures;
- build an image;
- deploy to a test Environment when the user provides access;
- summarize what changed.

That makes a thin wrapper around Codex a weak product. A thin wrapper would add little more than a different UI for a workflow that already works from the terminal.

mspace earns its place when it owns the state that sits around the agent run:

- the durable issue that started the work;
- the comments, decisions, and follow-up tasks around that issue;
- the agent session attached to the issue;
- the worktree, branch, commits, and diff;
- the selected Environment, plus issue namespace for Kubernetes deploys;
- the preview URL the team can open;
- the commands, test results, deployment resources, logs, events, and risks;
- the cleanup or retain decision after review.

This state is difficult to reconstruct from terminal history, chat logs, and ad hoc deployment commands. mspace should make that state visible from the issue page.

## Product Position

mspace should be positioned as an Environment-backed issue workspace for coding agents, with Kubernetes as the default validation target and VM targets for higher-level deployment tests.

It should not be framed as a generic agent platform, a generic task manager, a Sealos API wrapper, or a replacement for Codex. Its strongest wedge is narrower:

```text
coding agent
  + project repository
  + durable issue
  + selected Kubernetes or VM environment
  + isolated Kubernetes namespace when applicable
  + preview URL
  + runtime evidence
```

Multica and OpenAI Symphony are useful references, but mspace should not copy their broad positioning. mspace should start from a workflow that already exists in advanced engineering teams: an agent changes code in a real checkout, a developer deploys the result into a real test environment, and the team reviews the behavior with evidence.

## Target Users

mspace is strongest for teams that already have three habits:

- they use coding agents for real project work;
- they rely on shared Kubernetes or VM-based test environments;
- they need more than a local diff before they trust a change.

This includes platform teams, infra-heavy product teams, and Sealos-like teams where repositories, runtime environments, registries, namespaces, preview URLs, and cleanup policies already matter.

mspace is weaker for solo developers whose work ends at local tests and a commit. For those users, Codex plus a terminal may be enough.

## The Value Unit

The value unit is not an agent task. It is a complete issue work record:

```text
Issue
+ Agent session
+ Worktree or branch
+ selected Environment
+ Kubernetes namespace when applicable
+ Preview URL
+ Review evidence
+ Cleanup decision
```

Each part matters:

- Without the issue, mspace becomes an agent runner.
- Without the agent session, mspace becomes a project tracker.
- Without the selected environment, namespace or VM target, and preview/evidence output, mspace becomes a Codex wrapper with notes.
- Without review evidence, mspace forces users back into logs and terminals.
- Without cleanup state, mspace leaves test environments as operational debt.

The issue page should let a teammate answer these questions without asking the original developer:

- What was the problem?
- What did the agent change?
- Which commands and tests ran?
- Which Environment was used, and where is the deployed test output?
- What Kubernetes resources were created?
- Did mspace confirm the preview URL opened?
- What risks or follow-ups remain?
- Should the team continue, merge, redeploy, retain, or clean up?

## Kubernetes As The Differentiator

Kubernetes is not decorative infrastructure in mspace. It is the validation boundary.

The user can already give Codex a kubeconfig and ask it to deploy. mspace should turn that manual habit into a product loop:

1. The user selects or inherits a project Environment. In the current issue deploy path, that Environment must be Kubernetes.
2. mspace reserves an issue namespace.
3. The agent builds and pushes the required image.
4. The agent deploys the changed project into the namespace.
5. The agent exposes the workload through NodePort or Ingress.
6. mspace checks the preview URL.
7. mspace records the result and evidence on the issue.

This gives mspace a sharper value than generic agent management. It lets the team review behavior in an environment closer to production than a local process.

## Agent-Discovered Project Runbook

mspace should not rely on users manually filling out install, test, build, and deploy commands as the main path. Manual forms age quickly and do not match how agent work happens.

The better model is an agent-discovered project runbook:

```text
Agent explores the project
  -> finds how to install dependencies
  -> finds how to run tests
  -> finds how to build images
  -> finds how to deploy and validate previews
  -> mspace stores the successful path
  -> later sessions receive the runbook as context
  -> the agent updates it when the project changes
```

The runbook should preserve agent flexibility while giving future sessions memory. It should store successful commands, deployment patterns, health checks, common failures, and fixes. Humans can inspect and edit the Markdown runbook, but the primary path should be learning from real sessions.

The runbook is mspace product data, not a user-filled command form and not a file mspace silently commits into the target repository. The live source of truth belongs in mspace storage with revision history; a repository doc export can exist later only as an explicit user action.

This is an important product advantage. A single Codex conversation can learn a project for one task. mspace can let the team keep that learning across issues.

## Evidence Should Be A Product Surface

Raw logs are not enough. mspace needs an Evidence surface on the issue.

The Evidence view should gather:

- source session, branch, and source commit identity;
- commands run;
- test results;
- build result;
- image and registry output;
- deployment result;
- Kubernetes resources;
- pod status, events, and logs;
- preview URL and check result;
- agent summary;
- risks and follow-ups;
- cleanup or retain state.

Code changes and diff summary belong in the Commits surface. Evidence should reference the source session/branch/commit, but should not duplicate the code diff.

This view is what turns a session from terminal output into review material. It should help a human decide the next action without replaying the agent run.

## Branch And PR Handoff

The workflow should not end at a preview URL. Engineering teams usually need a branch or PR.

The MVP should keep PR delivery state as a server-owned handoff record, while the product direction moves toward GitHub App integration:

- PR handoff is branch-based: the selected source branch is the issue delivery identity, while captured commits are supporting review evidence for that branch.
- workspace installs the GitHub App;
- the server stores installation state;
- sessions use scoped installation tokens for push and PR actions;
- PRs link back to the issue;
- PR descriptions include preview URL and evidence summary.

An independent agent GitHub account is optional. The more important product requirement is workspace-level permission, audit, and repository-scoped access.

## Failure Is Part Of The Workflow

The product should assume that some sessions fail before they succeed.

Failures should not disappear into raw logs. mspace should preserve enough context to continue:

- the failed command;
- the error summary;
- the affected Kubernetes resource;
- relevant pod logs or events;
- whether the user can retry, continue with a new instruction, stop, retain, or clean up.

This does not mean mspace should interrupt agents at every failure. Codex may fix many failures by itself. It means the issue should still show the path that led to the final result, including failed attempts that affected the review.

## Security Boundary

The current MVP can use an administrator kubeconfig for a test cluster to prove the workflow. That is acceptable for dogfooding.

The team-ready version needs a narrower boundary:

- one issue namespace per test environment;
- scoped ServiceAccount, Role, and RoleBinding;
- temporary kubeconfig for the session;
- no Secret read permission by default;
- audit trail for write-capable actions;
- cleanup that revokes namespace access.

This work should not block the first value proof. It becomes important once the workflow moves from personal use to team adoption.

## Proof Standard

mspace proves its value when one real project can complete this loop without manual reconstruction:

```text
Create issue
  -> start agent session
  -> review code changes
  -> trigger test deployment
  -> open preview URL
  -> inspect evidence
  -> create or record branch / PR
  -> decide cleanup or retain
```

The key question is:

> Can a teammate understand the issue state, open the test environment, inspect the evidence, and decide the next step without returning to the terminal?

If the answer is yes, mspace has independent value. If the answer is no, it remains a heavier way to use Codex.

## Near-Term Product Priorities

The next work should favor dogfooding and hardening the first complete loop over broader platform features. The local MVP now has first versions of commit-backed deploy selection, branch / PR handoff records, structured review evidence, and continueable failure records; the remaining priority is proving those surfaces with real project runs and improving the quality of the captured Kubernetes evidence.

1. Run a real dogfood issue through the full flow.
2. Add or refine the agent-discovered project runbook.
3. Harden the issue Evidence surface with deeper Kubernetes-resource evidence.
4. Dogfood branch / PR handoff until the PR body and refresh behavior are reliable.
5. Dogfood failure recovery until Continue / Retry deploy / Stop / retain / cleanup choices are obvious from Issue Detail.
6. Add scoped Kubernetes credentials after the loop works.

The product should defer multi-agent scheduling, generic skill management, automatic merge pipelines, Kubernetes-hosted agent runtime, and broad DevOps resource browsing until the issue-to-preview-to-evidence loop works for a real internal project.
