# mspace API Integration Guide

> Status: server-owned local MVP API guide, updated 2026-05-19

This guide covers the current server control-plane API used by the desktop and workers. The control plane normally runs on `http://127.0.0.1:8787`.

The API is not a public cloud contract yet. It is stable enough for the desktop renderer, runtime workers, smoke checks, and small integration scripts.

## Base URL

```bash
export MSPACE_SERVER_BASE="http://127.0.0.1:8787"
curl "$MSPACE_SERVER_BASE/health"
```

The Electron preload exposes the server base URL to the renderer through `window.mspaceDesktop.serverBaseUrl`.

Workspace endpoints require:

```text
Authorization: Bearer <msp-token>
```

Runtime worker endpoints require:

```text
Authorization: Bearer <msw-token>
```

## Ownership Boundary

The server control plane owns:

- GitHub auth and mspace `msp_...` sessions;
- users, workspaces, members, invitations, and identity;
- projects, project runbooks, issues, child tasks, comments, reactions, labels, Inbox events, and per-user receipts;
- workspace settings, agent profiles, clusters, issue test environments, issue handoffs, failures, review evidence, and source change nodes;
- runtime worker registration, worker heartbeat/capability state, runtime task queue state, task events, task logs, cancellation, and task results.

The desktop owns native shell behavior, local UI state, file pickers, and opening browser auth flows. Workers own execution: repository cache, per-session workdir, Codex app-server lifecycle, command execution, source capture, artifacts, and logs while running.

## Auth And Workspace APIs

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Server health and protocol capabilities. |
| `GET` | `/api/auth/github/start` | Create OAuth state and return the GitHub authorization URL plus polling path. |
| `GET` | `/api/auth/github/callback` | Complete GitHub OAuth and render a browser success page. |
| `GET` | `/api/auth/github/result` | Poll the single-use state-bound desktop login result. |
| `GET` | `/api/auth/me` | Return the current user and workspaces for a bearer token. |
| `GET` | `/api/workspaces` | List the authenticated user's workspaces. |
| `POST` | `/api/workspaces` | Create a team workspace. |
| `GET` | `/api/workspaces/{workspaceID}/members` | List workspace members. |
| `POST` | `/api/workspaces/{workspaceID}/invitations` | Create a one-time `msi_...` invitation link. |
| `GET` | `/api/workspaces/{workspaceID}/invitations` | List invitations without raw tokens. |
| `DELETE` | `/api/workspaces/{workspaceID}/invitations/{invitationID}` | Revoke an invitation. |
| `POST` | `/api/workspace-invitations/accept` | Accept an invitation. |

## Product APIs

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/workspaces/{workspaceID}/inbox` | List unread workspace issue-event receipts. |
| `POST` | `/api/workspaces/{workspaceID}/issue-events` | Append a reviewable issue event. |
| `POST` | `/api/workspaces/{workspaceID}/issue-events/{eventID}/read` | Mark one receipt read. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/read-through` | Mark unread receipts for one issue read through an optional event boundary. |
| `GET` | `/api/workspaces/{workspaceID}/projects` | List projects in the selected workspace. |
| `POST` | `/api/workspaces/{workspaceID}/projects` | Create a project. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}` | Update project settings. |
| `DELETE` | `/api/workspaces/{workspaceID}/projects/{projectID}` | Delete a project when no issues reference it. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/runbook` | Read the project runbook. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}/runbook` | Replace the project runbook and record a revision. |
| `GET` | `/api/workspaces/{workspaceID}/issue-label-definitions` | List Type and Priority label options. |
| `GET` | `/api/workspaces/{workspaceID}/issues` | List top-level issues. |
| `POST` | `/api/workspaces/{workspaceID}/issues` | Create a workspace issue. |
| `POST` | `/api/workspaces/{workspaceID}/issues/suggest-title` | Suggest a title from issue body text. |
| `GET` | `/api/workspaces/{workspaceID}/issues/{issueID}` | Load issue detail. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}` | Update issue project attachment, title, body, or workflow status. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/tasks` | Create a child issue task. |
| `DELETE` | `/api/workspaces/{workspaceID}/issues/{issueID}/tasks/{taskID}` | Delete a child issue task under the parent. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/labels` | Replace selected label keys. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments` | Add a Markdown human comment. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}` | Edit the current user's eligible human comment. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}` | Add the current user's reaction to a comment. |
| `DELETE` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}` | Remove the current user's reaction from a comment. |

Create a workspace issue before the repository is known:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/issues" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"body":"Capture a workspace-level issue before the repo is known\n\n- [ ] Clarify the target repository"}'
```

When `projectId` is omitted, the server leaves the issue unassigned if the workspace has zero projects or more than one possible project. If the workspace has exactly one project, the server auto-attaches that project. The issue can be reviewed and commented on without a project, but agent execution, PR handoff, project runbook access, and issue test environments require attaching a project first.

Attach an existing project later:

```bash
curl -X PUT "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/issues/<issue-id>" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"projectId":"<project-id>"}'
```

## Workspace Runtime Surface APIs

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/workspaces/{workspaceID}/workspace/settings` | Read workspace automation settings. |
| `PUT` | `/api/workspaces/{workspaceID}/workspace/settings` | Update workspace automation settings. |
| `GET` | `/api/workspaces/{workspaceID}/agents` | List mentionable agent profiles. |
| `POST` | `/api/workspaces/{workspaceID}/agents` | Create an agent profile. |
| `PUT` | `/api/workspaces/{workspaceID}/agents/{agentID}` | Update an agent profile. |
| `GET` | `/api/workspaces/{workspaceID}/clusters` | List cluster configs. |
| `POST` | `/api/workspaces/{workspaceID}/clusters` | Create a cluster config. |
| `PUT` | `/api/workspaces/{workspaceID}/clusters/{clusterID}` | Update a cluster config. |
| `DELETE` | `/api/workspaces/{workspaceID}/clusters/{clusterID}` | Delete an unused cluster config. |
| `GET` | `/api/workspaces/{workspaceID}/clusters/discover-defaults` | Discover kubeconfig candidates and contexts under `~/.kube`. |
| `POST` | `/api/workspaces/{workspaceID}/clusters/import` | Import selected kubeconfig files. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/sessions` | Queue an `agent_session` runtime task after a supported agent mention. |
| `GET` | `/api/workspaces/{workspaceID}/sessions/{sessionID}` | Load session detail derived from the runtime task and worker logs. |
| `POST` | `/api/workspaces/{workspaceID}/sessions/{sessionID}/cancel` | Request cancellation for the session's runtime task. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-deploy` | Queue a test deployment session. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/cleanup` | Queue namespace cleanup. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/retain` | Retain the namespace for debugging. |
| `GET` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/resources` | List Pods, Services, Deployments, Ingresses, and Events from the fixed issue namespace. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/probe` | Refresh preview reachability state without creating new evidence rows. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/create-pr` | Store or update the issue PR handoff from selected source evidence. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/{handoffID}/refresh` | Refresh the issue handoff record. |

## Server Agent Sessions

Issue Detail starts a worker turn in two steps:

1. Write the human comment through `POST /api/workspaces/{workspaceID}/issues/{issueID}/comments`.
2. Call `POST /api/workspaces/{workspaceID}/issues/{issueID}/sessions` with the comment id as `triggerCommentId`.

Personal workspaces use `runtimeMode: "personal"`; team workspaces use `runtimeMode: "team"`. Both modes share the same server tables, worker claim protocol, task logs, cancellation, and result shape.

```json
{
  "provider": "codex",
  "agentProfile": "codex",
  "runtimeMode": "team",
  "command": "@codex implement the fix",
  "triggerCommentId": "<server-comment-id>"
}
```

The server validates that the issue has an attached project, snapshots issue/project/runbook/comment/child issue/label context into the runtime task payload, and returns the server `AgentSession`. The worker prepares its own repo cache and workdir, appends logs to `runtime_task_logs`, and reports Codex thread/turn ids plus source branch and commit metadata in `runtime_tasks.result`. Server Issue Detail includes matching sessions by mapping `runtime_tasks` with `kind="agent_session"` back into its `sessions` field, and the Commits tab derives change nodes from task results.

## Test Environment Flow

Start a test deploy from captured source evidence:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/issues/<issue-id>/test-deploy" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "clusterId":"<cluster-id>",
    "sourceSessionId":"<source-session-id>",
    "sourceCommitSha":"<source-commit-sha>",
    "exposureMode":"nodeport"
  }'
```

The server first creates the `agent_session` runtime task with Kubernetes and source metadata. After queueing succeeds, it stores or updates `issue_test_environments` with the deployment session id and `deploying` state. The worker performs the deploy/test turn and can write `test-environment.json` in its artifact directory to report preview values.

Inspect live namespace resources:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/issues/<issue-id>/test-environment/resources"
```

## Runtime Worker APIs

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | Create a short-lived worker registration token. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | List worker registration token metadata without raw token values. |
| `DELETE` | `/api/workspaces/{workspaceID}/runtime-registration-tokens/{tokenID}` | Revoke a worker registration token. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-workers` | List registered runtime workers and heartbeat state. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks` | Queue a runtime task manually. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks` | List recent runtime tasks. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/events` | List audit events for one runtime task. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/logs` | List worker-appended logs for one runtime task. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/cancel` | Request cancellation for a queued, claimed, or running task. |
| `POST` | `/api/runtime/workers/register` | Register or refresh a worker using `Authorization: Bearer msw_...`. |
| `POST` | `/api/runtime/workers/{workerID}/heartbeat` | Update worker liveness, status, load, and optional capability metadata. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/claim` | Claim the next queued task that matches the worker mode and required capabilities. |
| `GET` | `/api/runtime/workers/{workerID}/tasks/{taskID}` | Let the claiming worker inspect task state while executing. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/{taskID}/logs` | Append a log line to a claimed/running task. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/{taskID}/status` | Move a claimed task to `running`, `completed`, `failed`, or `cancelled`. |

Queue a protocol smoke task:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/runtime-tasks" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"protocol_smoke","runtimeMode":"team","requiredCapabilities":{"protocolSmoke":true},"payload":{"source":"curl"}}'
```

## Artifact Contract

Workers may improve the session result by writing JSON or Markdown files under `${MSPACE_SESSION_ARTIFACT_DIR}`:

- `branch-name.json`: proposed source branch, for example `{ "branch": "fix/pr-source-branch-selection" }`.
- `review-evidence.json`: command evidence, tests, build/deploy result, summary, risks, and follow-ups.
- `test-environment.json`: deploy/test result, including `previewUrl` when available.
- `project-runbook.md`: learned project runbook update after a successful session.

Branch names should use Conventional Commit-style prefixes: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `build`, `ci`, `style`, or `revert`.
