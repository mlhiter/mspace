# mspace API Integration Guide

> Status: server-owned local MVP API guide, updated 2026-06-01

This guide covers the current server control-plane API used by the desktop and workers. The control plane normally runs on `http://127.0.0.1:8787`.

The API is not a public cloud contract yet. It is stable enough for the desktop renderer, runtime workers, smoke checks, and small integration scripts.

## Base URL

```bash
export MSPACE_SERVER_BASE="http://127.0.0.1:8787"
curl "$MSPACE_SERVER_BASE/health"
```

The Electron preload exposes the server base URL to the renderer through `window.mspaceDesktop.serverBaseUrl`.

The desktop chooses its server in this order:

1. `MSPACE_SERVER_URL`, locked for the current launch.
2. A saved Team server URL from Electron user data.
3. The local bundled/dev server on `127.0.0.1:8787`.

Before saving a Team server URL, the desktop checks `/health`. Compatible servers must return `ok: true`, `serverProtocol: 1`, and these capabilities set to `true`: `workspaceInboxIssueGrouping`, `teamWorkspaceCreation`, `workspaceInvitations`, `workspaceInvitationPreview`, `workspaceKinds`, `workspaceCollaboration`, `runtimeWorkerRegistration`, and `runtimeTaskQueue`. `capabilities.githubAuth` is optional behavior metadata. GitHub login is shown only when the desktop is using an explicitly configured team server, from either `MSPACE_SERVER_URL` or a saved Team server URL, and that server reports `capabilities.githubAuth: true`. The default local personal server stays local-account-only and starts on account creation.

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

- local password auth, optional GitHub auth, and mspace `msp_...` sessions;
- users, workspaces, members, invitations, and identity;
- projects, project runbooks, issues, child tasks, comments, reactions, labels, Inbox events, and per-user receipts;
- workspace settings, agent profiles, clusters, issue test environments, issue handoffs, failures, review evidence, and source change nodes;
- runtime worker registration, worker heartbeat/capability state, runtime task queue state, task events, task logs, cancellation, and task results.

The desktop owns native shell behavior, local UI state, file pickers, and opening browser auth flows. Workers own execution: repository cache, per-session workdir, Codex app-server lifecycle, command execution, source capture, artifacts, and logs while running. The server never starts Codex and never requires Codex credentials; it only queues Codex-capable runtime tasks and reconciles worker results.

## Auth And Workspace APIs

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Server health and protocol capabilities. |
| `POST` | `/api/auth/password/register` | Create a local username/password identity, default personal workspace, and mspace session. |
| `POST` | `/api/auth/password/login` | Authenticate a local account and issue a mspace session. |
| `GET` | `/api/auth/github/start` | Create OAuth state and return the GitHub authorization URL plus polling path. |
| `GET` | `/api/auth/github/callback` | Complete GitHub OAuth and render a browser success page. |
| `GET` | `/api/auth/github/result` | Poll the single-use state-bound desktop login result. |
| `GET` | `/api/auth/me` | Return the current user, workspaces, auth identity provider/login, and `isServerAdmin` for a bearer token. |
| `GET` | `/api/workspaces` | List the authenticated user's workspaces. |
| `POST` | `/api/workspaces` | Create a team workspace. Server admins only. |
| `PUT` | `/api/workspaces/{workspaceID}` | Update team workspace identity fields: name, mark, and description. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/members` | List workspace members. |
| `POST` | `/api/workspaces/{workspaceID}/invitations` | Create a one-time `msi_...` invitation link. |
| `GET` | `/api/workspaces/{workspaceID}/invitations` | List invitations without raw tokens. |
| `DELETE` | `/api/workspaces/{workspaceID}/invitations/{invitationID}` | Revoke an invitation. |
| `GET` | `/api/workspace-invitations/preview?token=msi_...` | Preview a join link without authentication. |
| `POST` | `/api/workspace-invitations/accept` | Accept an invitation. |

Local password auth is the default path for restricted or offline environments. GitHub OAuth values are optional and only needed when the deployment can reach GitHub.

Create a local account:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/auth/password/register" \
  -H 'Content-Type: application/json' \
  -d '{"login":"local-admin","name":"Local Admin","password":"correct-password"}'
```

Sign in with that account:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/auth/password/login" \
  -H 'Content-Type: application/json' \
  -d '{"login":"local-admin","password":"correct-password"}'
```

Both endpoints return the normal auth shape:

```json
{
  "token": "msp_...",
  "expiresAt": "2026-05-20T12:00:00Z",
  "user": { "id": "...", "name": "Local Admin" },
  "workspaces": [{ "id": "...", "kind": "personal", "role": "owner", "icon": "", "description": "" }],
  "identity": { "provider": "password", "login": "local-admin" },
  "isServerAdmin": true
}
```

`identity.provider` is the source of truth for account-type display. Password users return `provider: "password"` and a local login; GitHub OAuth users return `provider: "github"` and a GitHub login. Do not infer GitHub connection from `user.email`: local password accounts intentionally keep canonical `user.email` blank because password email is not verified.

Usernames are normalized to lowercase and must use letters, numbers, dots, underscores, or hyphens. Passwords must be 8 to 1024 characters. Duplicate registration returns `409`, and invalid login/password returns `401` without distinguishing missing, wrong, or disabled accounts.

Open registration intentionally creates only a personal workspace. Server admin status is matched by configured auth login, not display name or email, because local password email is not verified. Configure `MSPACE_SERVER_ADMIN_LOGINS` with local password logins or GitHub logins allowed to create team workspaces. For deployed environments, `MSPACE_BOOTSTRAP_ADMIN_LOGIN` and `MSPACE_BOOTSTRAP_ADMIN_PASSWORD` can create the first local admin account during server startup; the server leaves an existing account password unchanged.

Only server admins can call `POST /api/workspaces` to create a team workspace. Other registered users can use their personal workspace and personal runner, then join a team workspace only after a team owner/admin creates an `msi_...` invitation.

Create a one-time team join link:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/invitations" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"role":"member","expiresInHours":72}'
```

The response includes the raw `msi_...` token for the creator session and a desktop link shaped like:

```text
mspace://invite/<token>?server=<team-server-url>
```

Workspace Settings should present the link as the user-facing artifact and keep ids, token prefixes, and join codes out of normal UI. The `server` query value lets Electron switch to the invited team server before previewing or accepting the invitation.

Preview the invitation before authentication:

```bash
curl "$MSPACE_SERVER_BASE/api/workspace-invitations/preview?token=msi_..."
```

Preview responses intentionally contain only safe information:

```json
{
  "workspaceName": "Admin Team",
  "role": "member",
  "invitedByName": "Admin",
  "invitedByAvatarUrl": "",
  "invitedByLogin": "admin",
  "expiresAt": "2026-06-04T12:00:00Z",
  "status": "pending"
}
```

They must not include workspace members, internal user ids, invitation ids, token prefixes, or other debug metadata. Preview can return `pending`, `accepted`, `revoked`, or `expired`. A preview `404` means the token is unknown to the server being queried or the backend does not expose the preview route; check the deep-link server value and deployed backend version first.

Accept the invitation after password registration, password login, or GitHub OAuth returns a normal `msp_...` session:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspace-invitations/accept" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"token":"msi_..."}'
```

Successful acceptance returns the joined workspace. Desktop clients should select that workspace immediately and continue into the app instead of showing a second invitation confirmation after the user already saw the signed-out preview.

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
| `POST` | `/api/workspaces/{workspaceID}/issues/suggest-title` | Suggest a title from issue body text using deterministic server fallback only. |
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

When a new issue has no explicit type label, the server marks type triage as pending and queues a worker-backed `issue_type_triage` runtime task. That task requires a worker with `{"codex":true}` capabilities in the workspace runtime mode. The worker returns a JSON result such as `{"type":"fix","confidence":0.86,"reason":"..."}`; the server validates the type against the fixed Conventional Commit set before applying the `type:*` label. Priority remains manual and is not classified by the worker.

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
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/sessions` | Queue an `agent_session` runtime task after a supported agent mention, attached project, and active matching Codex worker. |
| `GET` | `/api/workspaces/{workspaceID}/sessions/{sessionID}` | Load session detail derived from the runtime task and worker logs. |
| `POST` | `/api/workspaces/{workspaceID}/sessions/{sessionID}/cancel` | Request cancellation for the session's runtime task. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-deploy` | Queue a test deployment session. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/cleanup` | Queue namespace cleanup. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/retain` | Retain the namespace for debugging. |
| `GET` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/resources` | List Pods, Services, Deployments, Ingresses, and Events from the fixed issue namespace. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/probe` | Refresh preview reachability state without creating new evidence rows. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/create-pr` | Store or update the issue PR handoff from selected source evidence. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/{handoffID}/refresh` | Refresh the issue handoff record. |

Workspace settings currently include:

```json
{
  "autoCreateDraftPr": false,
  "autoDeployTestEnvironment": false
}
```

`autoDeployTestEnvironment` is opt-in. When it is `true`, the server queues a deploy/test session after a completed non-dry-run source session captures a commit and the issue has an attached project, resolvable deploy settings, no active issue session, and a matching online Codex worker. The queued deploy task uses the same `agent_session` and `issue_test_environments` contracts as a manual test deploy, with automation marker `auto_test_deploy`.

## Server Agent Sessions

Issue Detail starts a worker turn only after a worker preflight:

1. Refresh `GET /api/workspaces/{workspaceID}/runtime-workers` and look for a worker in the selected workspace and runtime mode with `codex:true`, `status:"online"`, and a fresh heartbeat.
2. In personal desktop mode, ask Electron to ensure the host-local personal worker, then wait briefly for it to heartbeat. Team workspaces do not auto-start a worker; the user must connect a matching team worker.
3. Write the human comment through `POST /api/workspaces/{workspaceID}/issues/{issueID}/comments`.
4. Call `POST /api/workspaces/{workspaceID}/issues/{issueID}/sessions` with the comment id as `triggerCommentId`.

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

The server validates that the issue has an attached project, that `runtimeMode` matches the workspace kind, and that a matching active Codex worker exists before it creates `agent_sessions` or `runtime_tasks`. If no worker is online, the server returns HTTP `409` with `{"error":"no active codex worker"}`. Clients should keep the preflight before the trigger comment so unsupported `@codex` turns do not leave a human comment waiting for a worker that cannot claim the task.

When accepted, the server snapshots issue/project/runbook/comment/child issue/label context into the runtime task payload and returns the server `AgentSession`. The worker prepares its own repo cache and workdir, appends logs to `runtime_task_logs`, and reports Codex thread/turn ids plus source branch and commit metadata in `runtime_tasks.result`. Server Issue Detail includes matching sessions by mapping `runtime_tasks` with `kind="agent_session"` back into its `sessions` field, and the Commits tab derives change nodes from task results.

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

The server first creates the `agent_session` runtime task with Kubernetes and source metadata. After queueing succeeds, it stores or updates `issue_test_environments` with the deployment session id and `deploying` state. The worker performs the deploy/test turn and can write `test-environment.json` in its artifact directory to report preview values. Automatic test deploys follow this same path and pin `sourceSessionId` / `sourceCommitSha` to the completed source session that triggered them.

Inspect live namespace resources:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/issues/<issue-id>/test-environment/resources"
```

## Runtime Worker APIs

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/install/worker` | Return the self-host worker install script used by generated install commands. |
| `POST` | `/api/workspaces/{workspaceID}/worker-installations` | Create a short-lived worker environment install command. Owner/admin only. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | Create a short-lived worker registration token. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | List worker registration token metadata without raw token values. |
| `DELETE` | `/api/workspaces/{workspaceID}/runtime-registration-tokens/{tokenID}` | Revoke a worker registration token. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-workers` | List registered runtime workers and heartbeat state. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks` | Queue a runtime task manually for API-level smoke/debug tooling. Product UI flows normally create tasks through issue triage, agent session, or test deploy routes. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks?limit=10&offset=0` | List runtime tasks with pagination metadata and status counts. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/events` | List audit events for one runtime task. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/logs` | List worker-appended logs for one runtime task. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/cancel` | Request cancellation for a queued, claimed, or running task. |
| `POST` | `/api/runtime/workers/register` | Register or refresh a worker using `Authorization: Bearer msw_...`. |
| `POST` | `/api/runtime/workers/{workerID}/heartbeat` | Update worker liveness, status, load, and optional capability metadata. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/claim` | Claim the next queued task that matches the worker mode and required capabilities. |
| `GET` | `/api/runtime/workers/{workerID}/tasks/{taskID}` | Let the claiming worker inspect task state while executing. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/{taskID}/logs` | Append a log line to a claimed/running task. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/{taskID}/status` | Move a claimed task to `running`, `completed`, `failed`, or `cancelled`. |

Create a worker install command:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/worker-installations" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"build-vm-worker","expiresInHours":1}'
```

The response includes `installCommand`, `runtimeMode`, `workerName`, `credentialPrefix`, and `expiresAt`. Product UI should show the install command and hide the raw bootstrap credential. The target environment needs Docker plus Codex `auth.json` and `config.toml`; after running the command, the worker appears in `/runtime-workers` after its first heartbeat.

Queue a protocol smoke task from API/debug tooling:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/runtime-tasks" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"protocol_smoke","runtimeMode":"team","requiredCapabilities":{"protocolSmoke":true},"payload":{"source":"curl"}}'
```

The server rejects runtime worker registration and runtime task creation when the submitted mode does not match the workspace kind. An install command or token minted in a personal workspace can only register a personal worker, and one minted in a team workspace can only register a team worker. Manual runtime task requests follow the same rule: `runtimeMode:"personal"` for personal workspaces and `runtimeMode:"team"` for team workspaces.

Desktop personal workers use the same token endpoints, but the user normally never sees the raw credential. Electron creates a 12-hour personal worker credential, writes it to an Electron user-data token file, renews it before expiry, and revokes the replaced credential after a short grace period. The worker supports `MSPACE_RUNTIME_TOKEN_FILE` and rereads that file for runtime API calls, so token renewal is designed to be invisible to personal users.

Workspace Settings lists runtime tasks as an operations surface: task purpose, linked Issue title when available, status, worker, update time, and detail/cancel actions. Agent-session task links include `sessionId` so Issue Detail can scroll to the relevant session card. Pure protocol tasks such as `issue_type_triage` may only open the Issue page because they do not have a session card. Raw protocol payloads remain in expanded details instead of the primary row.

The runtime task list endpoint returns `{ tasks, total, limit, offset, statusCounts }`. Use `limit` and `offset` for paged UI lists; the server clamps invalid limits and keeps the result ordered by newest task first. Desktop clients normalize older array responses defensively so a renderer update does not crash while a local or remote server is still restarting onto the paged contract.

Desktop personal worker credentials are named `Desktop personal worker credential` and shown as automatic desktop credentials. Workspace Settings separates active credentials from expired or replaced credential history so background renewal does not look like a pile of duplicate manual credentials.

Runtime task kinds used by the current product path:

| Kind | Producer | Claimed by | Result owner |
| --- | --- | --- | --- |
| `protocol_smoke` | User/API smoke | Any worker with `protocolSmoke:true` | Task result only |
| `noop` | User/API smoke | Any matching worker | Task result only |
| `issue_type_triage` | Server issue creation/update path | Worker with `codex:true` | Server reconciles issue type label |
| `agent_session` | Issue agent mention or test-deploy path | Worker with required runtime capabilities | Server derives session detail, source changes, evidence, and environment state |

## Artifact Contract

Workers may improve the session result by writing JSON or Markdown files under `${MSPACE_SESSION_ARTIFACT_DIR}`:

- `branch-name.json`: proposed source branch, for example `{ "branch": "fix/pr-source-branch-selection" }`.
- `review-evidence.json`: command evidence, tests, build/deploy result, summary, risks, and follow-ups.
- `test-environment.json`: deploy/test result, including `previewUrl` when available.
- `project-runbook.md`: learned project runbook update after a successful session.

Branch names should use Conventional Commit-style prefixes: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `build`, `ci`, `style`, or `revert`.
