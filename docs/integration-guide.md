# mspace API Integration Guide

> Status: server-owned local MVP API guide, updated 2026-07-17

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

Before saving a Team server URL, the desktop checks `/health`. Compatible servers must return `ok: true`, `serverProtocol: 2`, and these capabilities set to `true`: `workspaceInboxIssueGrouping`, `teamWorkspaceCreation`, `workspaceInvitations`, `workspaceInvitationPreview`, `workspaceKinds`, `workspaceCollaboration`, `runtimeWorkerRegistration`, `runtimeAvailability`, and `runtimeTaskQueue`. Protocol 2 is the fixed Agent Engine contract; a protocol-1 Desktop or Server is rejected during health checking instead of entering a mixed-version state that can misroute Agent Sessions. `capabilities.githubAuth` and `capabilities.githubApp` are optional behavior metadata. GitHub login is shown only when the desktop is using an explicitly configured team server, from either `MSPACE_SERVER_URL` or a saved Team server URL, and that server reports `capabilities.githubAuth: true`. GitHub App automation status is shown separately for team workspaces and must not be inferred from GitHub OAuth. The default local personal server stays local-account-only and starts on account creation.

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
- projects, project runbooks, project test cases, test case revisions, test case suggestions, test plans, test runs, issues, child tasks, comments, reactions, labels, Inbox events, and per-user receipts;
- workspace settings, the fixed Agent catalog contract, built-in and workspace custom Workflow Skill catalog/revisions/settings, Environments, Kubernetes cluster compatibility records, issue test environments, issue handoffs, failures, review evidence, and source change nodes;
- runtime worker registration, worker heartbeat/capability state, runtime task queue state, task events, task logs, cancellation, and task results.

The desktop owns native shell behavior, local UI state, file pickers, and opening browser auth flows. Workers own execution: repository cache, per-Session workdir, Codex/Claude Code/Pi adapters, source capture, artifacts, and logs. The server never starts Agent CLIs or requires their credentials; it freezes `agentEngine`, queues one exact capability, attaches server-owned Skill bundles, and reconciles Worker results.

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
| `PUT` | `/api/auth/me` | Update the current user's display name and avatar URL without changing auth provider/login. |
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

Update the current account's display profile:

```bash
curl -X PUT "$MSPACE_SERVER_BASE/api/auth/me" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Local Admin","avatarUrl":"https://example.test/avatar.png"}'
```

This updates only `user.name` and `user.avatarUrl`. The returned `identity.provider` and `identity.login` stay read-only and continue to drive account-type display, admin matching, and audit-sensitive identity.

Usernames are normalized to lowercase and must use letters, numbers, dots, underscores, or hyphens. Passwords must be 8 to 1024 characters. Duplicate registration returns `409`, and invalid login/password returns `401` without distinguishing missing, wrong, or disabled accounts.

Open registration intentionally creates only a personal workspace. Server admin status is matched by configured auth login, not display name or email, because local password email is not verified. Configure `MSPACE_SERVER_ADMIN_LOGINS` with local password logins or GitHub logins allowed to create team workspaces. For deployed environments, `MSPACE_BOOTSTRAP_ADMIN_LOGIN` and `MSPACE_BOOTSTRAP_ADMIN_PASSWORD` can create the first local admin account during server startup; the server leaves an existing account password unchanged.

Helm fixed-worker installs may also set `MSPACE_BOOTSTRAP_TEAM_WORKSPACE_NAME` and `MSPACE_BOOTSTRAP_RUNTIME_TOKEN` during server startup. When both are present, the server creates or finds the named team workspace owned by the bootstrap admin and registers the supplied `msw_...` token for that workspace. The Helm chart uses this to make one install create the admin-owned team workspace and connect the fixed worker without asking an operator to copy a runtime token from the UI.

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
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases` | List project test cases as `{ cases, total, limit, offset }`; supports `status`, `q`, `limit`, and `offset`, and hides archived cases unless `status=archived`. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import/preview` | Parse Markdown, text, CSV, or Excel `.xlsx` cases without writing them. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import` | Import Markdown, text, CSV, or Excel `.xlsx` cases. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/optimize` | Queue an issue-backed Codex session that returns case suggestions. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/generate` | Queue an issue-backed Codex session that proposes baseline cases. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases` | Create one test case. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/delete` | Archive multiple test cases by id and record revisions. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}` | Read one test case. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}` | Update one test case and record a revision. |
| `DELETE` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}` | Archive one test case and record a revision. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}/revisions` | List case revisions newest first. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals` | List Codex case suggestions. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals/{proposalID}/apply` | Accept a case suggestion and optionally write a review note. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals/{proposalID}/reject` | Dismiss a case suggestion and optionally write a review note. |
| `GET` | `/api/workspaces/{workspaceID}/test-plans` | List workspace test plans. |
| `POST` | `/api/workspaces/{workspaceID}/test-plans` | Create a workspace test plan from ready project cases. |
| `GET` | `/api/workspaces/{workspaceID}/test-plans/{planID}` | Read a workspace test plan with selected cases. |
| `PUT` | `/api/workspaces/{workspaceID}/test-plans/{planID}` | Update workspace plan metadata and selected cases. |
| `POST` | `/api/workspaces/{workspaceID}/test-plans/{planID}/runs` | Start an issue-backed test run from a workspace plan. |
| `GET` | `/api/workspaces/{workspaceID}/test-runs` | List workspace test runs. |
| `POST` | `/api/workspaces/{workspaceID}/test-runs` | Compatibility/debug-only ad hoc run from selected ready project cases. Normal clients should create a test plan and start `/api/workspaces/{workspaceID}/test-plans/{planID}/runs`. |
| `GET` | `/api/workspaces/{workspaceID}/test-runs/{runID}` | Read a workspace test run with run items. |
| `GET` | `/api/workspaces/{workspaceID}/test-runs/{runID}/artifacts` | List artifacts for one workspace test run. |
| `POST` | `/api/workspaces/{workspaceID}/test-runs/{runID}/retry` | Retry failed or blocked run items. |
| `POST` | `/api/workspaces/{workspaceID}/test-runs/{runID}/cancel` | Stop a queued, setup-running, or running test run and cancel linked runtime tasks. |
| `POST` | `/api/workspaces/{workspaceID}/test-runs/{runID}/accept` | Record that a user reviewed the run result. |
| `POST` | `/api/workspaces/{workspaceID}/test-runs/{runID}/block` | Record that the run needs follow-up with a human note. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-plans` | Compatibility-filter workspace plans by project. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-plans` | Compatibility-create a workspace plan using the URL project as default case project. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}` | Compatibility-read a workspace plan that includes the project. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}` | Compatibility-update a workspace plan using the URL project as default case project. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}/runs` | Compatibility-start a workspace run from a plan that includes the project. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-runs` | Compatibility-filter workspace runs by project. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}` | Compatibility-read a workspace run that includes the project. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/cancel` | Compatibility-stop a workspace run that includes the project. |
| `GET` | `/api/workspaces/{workspaceID}/issue-label-definitions` | List Type and Priority label options. |
| `GET` | `/api/workspaces/{workspaceID}/issues` | List top-level issues. |
| `POST` | `/api/workspaces/{workspaceID}/issues` | Create a workspace issue. New clients that send a plain-text draft `title` must also send `titleSource: "plain_text"`. |
| `POST` | `/api/workspaces/{workspaceID}/issues/suggest-title` | Suggest a title from issue body text using deterministic server fallback only. |
| `GET` | `/api/workspaces/{workspaceID}/issues/{issueID}` | Load issue detail. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}` | Update issue project attachment, title, body, or workflow status. A title-only request may include `expectedTitle` for an atomic compare-and-set. |
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

Issue and child issue title fields are plain text even when the source note uses Markdown. New clients should send `titleSource: "plain_text"` with an explicit draft title; omitting the field is reserved for older clients whose draft was copied from the Markdown body. Normal clients do not wait for or write the final title. The create path passes the captured draft as `expectedTitle` to the existing `issue_type_triage` task; list/get endpoints do not enqueue tasks. If a compatibility task is already active without the draft, the server atomically upgrades that task rather than dropping title refinement or starting a duplicate turn. Updated workers return a rewritten title alongside type classification, and the server conditionally applies the sanitized plain result. Older type-only worker results remain valid.

External integrations that implement their own title refinement may still use the title-only conditional update API:

```bash
curl -X PUT "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/issues/<issue-id>" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Final plain title","expectedTitle":"Original draft title"}'
```

`expectedTitle` is accepted only with `title`. The server updates only the title when the stored title still equals `expectedTitle`; otherwise it returns the current Issue unchanged. Worker-backed triage enforces the same compare-and-set directly in Memory/Postgres, independently from type-label reconciliation.

When a new issue has no explicit type label, the server marks type triage as pending and queues a worker-backed `issue_type_triage` runtime task. That task requires a worker with `{"codex":true}` capabilities in the workspace runtime mode. Updated workers return `{"title":"Concise plain title","type":"fix","confidence":0.86,"reason":"..."}`; older workers may omit `title`. The server validates the type against the fixed Conventional Commit set before applying the `type:*` label and independently applies the title only while it still matches the captured draft. Priority remains manual and is not classified by the worker.

Attach an existing project later:

```bash
curl -X PUT "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/issues/<issue-id>" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"projectId":"<project-id>"}'
```

## Test Module APIs

Test cases, revisions, and Case suggestions are project-scoped. Test plans and test runs are workspace-scoped and can include ready cases from multiple projects. Each selected plan case and run item keeps its project id so execution, artifacts, and result reconciliation can still route through the correct repository. A personal project may point at a local folder or a GitHub repository URL. A team project must use `sourceType:"github"` and a GitHub URL because team workers clone source into their own repository cache and cannot read a user's desktop-local path.

Create a case:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/projects/<project-id>/test-cases" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "title":"Invite link opens the team workspace",
    "type":"ui",
    "area":"team access",
    "priority":"p1",
    "status":"ready",
    "preconditions":"A valid mspace invite link exists.",
    "steps":[
      {"action":"Open the invite link"},
      {"action":"Sign in with a local account","expected":"The app accepts the invite"}
    ],
    "expectedResult":"The invited workspace opens.",
    "environmentRequirements":"Desktop is connected to the team server.",
    "tags":["invite","smoke"]
  }'
```

Valid case types are `functional`, `ui`, `api`, and `deployment`. Status values are `draft`, `needs_review`, `ready`, and `archived`. Priority is optional and can be empty or `p0`, `p1`, `p2`, or `p3`.

List cases with server-backed pagination:

```bash
curl "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/projects/<project-id>/test-cases?limit=50&offset=0" \
  -H "Authorization: Bearer <msp-token>"
```

The response shape is:

```json
{
  "cases": [],
  "total": 0,
  "limit": 50,
  "offset": 0
}
```

Default listing excludes archived cases. Use `status=archived` to inspect archived cases. User-facing deletion archives cases so revisions, plan membership, run items, artifacts, and proposal links remain auditable.

Archive selected cases:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/projects/<project-id>/test-cases/delete" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"caseIds":["<case-id-1>","<case-id-2>"]}'
```

Archive one case:

```bash
curl -X DELETE "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/projects/<project-id>/test-cases/<case-id>" \
  -H "Authorization: Bearer <msp-token>"
```

The import parser also accepts common type aliases such as `functional_test`, `ui_test`, `api_test`, `deployment_test`, `功能测试`, `UI 测试`, `接口测试`, and `部署测试`, then stores the normalized fixed value.

Preview cases before import:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/projects/<project-id>/test-cases/import/preview" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"format":"markdown","content":"- Invalid password shows an error\n- User can reset password from the login page"}'
```

The preview response is read-only and returns `parsedCount`, `importableCount`, `skippedCount`, `missingFieldCounts`, `qualityFindingCounts`, `columnMappings`, `importableCaseSamples`, and `skippedSamples`. Clients should show those counts, source-column mappings, and samples, then call `/test-cases/import` with the same body only after the user confirms.

`format` can be `markdown`, `text`, `csv`, or `xlsx`. Markdown and text imports treat each non-empty line as one case. CSV and `.xlsx` imports use the same header contract:

```text
title,type,area,priority,preconditions,steps,expected_result,environment_requirements,tags
```

For unknown, localized, or product-specific headers, call `POST /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import/mapping-task` with the same `format`, `content`, and `fileName`. The server queues a `test_case_import_mapping` runtime task that requires a Codex-capable worker. The worker receives only headers and a small sample, returns mapping suggestions with confidence and reasons, and does not persist or import anything. After the user accepts the suggestions, pass them as `columnMappings` to `/test-cases/import/preview` and finally to `/test-cases/import`. Business classification columns such as `测试类别` should map to `tags`, not the fixed mspace `type`, unless the source explicitly provides a system type column. Historical execution-state columns can map to `latest_result` for preview context.

For `.xlsx`, send the workbook bytes as base64 in `content` and set `format:"xlsx"`. Import is capped at 1,000 cases per request. Text-like content is capped at 2 MB, and decoded `.xlsx` workbooks are capped at 2 MB before the server opens them with explicit workbook unzip limits. The server reads the first non-empty worksheet, skips rows without `title`, validates `type`, and records skipped rows in the preview and final import responses.

```json
{
  "format": "xlsx",
  "fileName": "cases.xlsx",
  "content": "<base64 workbook bytes>"
}
```

Optimize or generate cases queues an issue-backed Codex session and stores returned `test-case-proposals.json` items as Case suggestions. Applying a suggestion is the only path that mutates canonical cases from Codex output.

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/projects/<project-id>/test-case-proposals/<proposal-id>/apply" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"note":"Looks executable after the added environment requirement."}'
```

Workspace test plans select ready project cases and start issue-backed runs. Test Plan and Run detail responses include `requiredCapabilities`, the server-computed union required by their execution cases; clients should pass this map to runtime availability and personal-worker preflight instead of duplicating browser-signal rules. A plan can include `setupSteps`, a free-text plan-level setup block that runs once before case execution. Setup uses the normal issue-backed agent-session path and worker artifact channel: workers write `test-setup-result.json`; the server stores the setup result, copies `outputs` into `runContext`, and starts case execution only when the setup task completed with `status:"passed"`. Failed, cancelled, or missing setup marks the run `setup_failed` and leaves items queued. Execution workers then report results through `test-result.json`; the server reconciles run items, persists supported screenshot evidence as test artifacts, and rewrites run item evidence to authenticated artifact refs. A human can call `cancel` while a run is `queued`, `setup_running`, or `running`; cancellation marks the run and non-final items `cancelled`, cancels linked setup/execution runtime tasks with the supplied reason, and ignores late setup/result artifacts so the stopped run is not revived. A human can call `accept` or `block` to record a review decision, but retry remains the primary follow-up action for failed or blocked items until a later release or plan gate consumes run review state. Run start and retry inputs may include `resultLocale:"en"|"zh-CN"`; the server stores it on the run and instructs setup/execution sessions to write user-facing `summary`, `actualResult`, and `failureSummary` text in that language. When a run spans projects, mspace groups queued items by project and creates separate execution Issues/agent sessions per project batch; one agent session should not span multiple repositories.

Stop a run:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/test-runs/<run-id>/cancel" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"reason":"Stopped from Tests."}'
```

Create a test plan pinned to an Environment:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/test-plans" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "title":"Release smoke",
    "setupSteps":"1. Confirm the staging Environment.\n2. Update the target deployment image.\n3. Verify the preview URL is reachable and write test-setup-result.json.",
    "cases":[
      {"projectId":"<project-id>","caseId":"<case-id-1>"},
      {"projectId":"<another-project-id>","caseId":"<case-id-2>"}
    ],
    "environmentId":"<environment-id>",
    "environment":"Run against the selected staging target"
  }'
```

Start the plan in the current UI language:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/test-plans/<plan-id>/runs" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "runtimeMode":"personal",
    "resultLocale":"zh-CN"
  }'
```

The free-text `environment` value remains human notes for the agent. The structured environment binding is `environmentId`; when a plan or run is created, the server resolves that Environment and freezes `environmentKind` plus `environmentSnapshot` so later environment edits do not rewrite historical run context.

Setup artifact shape:

```json
{
  "runId": "<run-id>",
  "status": "passed",
  "summary": "Preview is ready.",
  "failureSummary": "",
  "outputs": {
    "previewUrl": "https://preview.example.test",
    "image": "registry.example/app:rc4"
  },
  "evidence": {},
  "steps": [
    {
      "title": "Update deployment image",
      "status": "passed",
      "command": "kubectl set image deployment/app app=registry.example/app:rc4",
      "summary": "Deployment rolled out."
    }
  ]
}
```

Use `status:"failed"` plus `failureSummary` when setup cannot safely complete. The server treats failed or cancelled setup tasks as failed even if a stale artifact claims `passed`.

## Workspace Runtime Surface APIs

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/workspaces/{workspaceID}/workspace/settings` | Read workspace automation settings. |
| `PUT` | `/api/workspaces/{workspaceID}/workspace/settings` | Update workspace automation settings. |
| `GET` | `/api/workspaces/{workspaceID}/agents` | Return the fixed read-only Agent catalog: Codex, Claude Code, and Pi. |
| `GET` | `/api/workspaces/{workspaceID}/skills` | List built-in and workspace custom workflow skill metadata. |
| `POST` | `/api/workspaces/{workspaceID}/skills` | Create a workspace custom workflow skill. |
| `GET` | `/api/workspaces/{workspaceID}/skills/{skillID}` | Read owner/admin skill detail and files for management. |
| `PUT` | `/api/workspaces/{workspaceID}/skills/{skillID}` | Update a custom skill revision or built-in workspace invocation settings. |
| `DELETE` | `/api/workspaces/{workspaceID}/skills/{skillID}` | Delete a workspace custom skill. |
| `POST` | `/api/workspaces/{workspaceID}/skills/{skillID}/duplicate` | Copy a built-in or custom skill into a workspace custom skill. |
| `GET` | `/api/workspaces/{workspaceID}/environments` | List Kubernetes and virtual machine Environments. Kubernetes rows are projected from cluster compatibility records. |
| `POST` | `/api/workspaces/{workspaceID}/environments` | Create an Environment. Use `kind:"kubernetes"` or `kind:"virtual_machine"`. |
| `PUT` | `/api/workspaces/{workspaceID}/environments/{environmentID}` | Update an Environment. |
| `POST` | `/api/workspaces/{workspaceID}/environments/{environmentID}/check` | Refresh Environment reachability. Kubernetes uses the kubeconfig check; VM uses the saved SSH credential or replaces it when new `sshAuth` material is provided. |
| `DELETE` | `/api/workspaces/{workspaceID}/environments/{environmentID}` | Delete an unused Environment. |
| `GET` | `/api/workspaces/{workspaceID}/clusters` | Compatibility API for Kubernetes cluster records. Prefer `/environments` in product integrations. |
| `POST` | `/api/workspaces/{workspaceID}/clusters` | Compatibility API for creating a Kubernetes cluster record. |
| `PUT` | `/api/workspaces/{workspaceID}/clusters/{clusterID}` | Compatibility API for updating a Kubernetes cluster record. |
| `POST` | `/api/workspaces/{workspaceID}/clusters/{clusterID}/check` | Compatibility API for refreshing Kubernetes Environment reachability. |
| `DELETE` | `/api/workspaces/{workspaceID}/clusters/{clusterID}` | Compatibility API for deleting an unused Kubernetes cluster record. |
| `GET` | `/api/workspaces/{workspaceID}/clusters/discover-defaults` | Discover kubeconfig candidates and contexts under `~/.kube`. |
| `POST` | `/api/workspaces/{workspaceID}/clusters/import` | Import selected kubeconfig files. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/sessions` | Queue an `agent_session` after a fixed Agent mention, attached project, and active Worker with the exact engine capability. |
| `GET` | `/api/workspaces/{workspaceID}/sessions/{sessionID}` | Load session detail derived from the runtime task and worker logs. |
| `POST` | `/api/workspaces/{workspaceID}/sessions/{sessionID}/cancel` | Request cancellation for the session's runtime task. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-deploy` | Queue a test deployment session. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/cleanup` | Queue namespace cleanup. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/retain` | Retain the namespace for debugging. |
| `GET` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/resources` | List Pods, Services, Deployments, Ingresses, and Events from the fixed issue namespace. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/probe` | Refresh preview reachability state without creating new evidence rows. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/create-pr` | Store or update the issue source handoff from selected source evidence. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/{handoffID}/refresh` | Refresh the issue handoff record. |
| `GET` | `/api/workspaces/{workspaceID}/github-app` | Read server-owned GitHub App installation status for this workspace. |

`GET /agents` always returns this code-owned shape; write methods on `/agents` are not available:

```json
[
  {"id":"codex","name":"Codex","mention":"@codex","capability":"codex"},
  {"id":"claude_code","name":"Claude Code","mention":"@claude","capability":"claudeCode"},
  {"id":"pi","name":"Pi","mention":"@pi","capability":"pi"}
]
```

Workspace settings currently include:

```json
{
  "autoCreateDraftPr": false,
  "autoDeployTestEnvironment": false
}
```

`autoDeployTestEnvironment` is opt-in. When it is `true`, the server queues a deploy/test session after a completed non-dry-run source session captures a commit and the issue has an attached project, resolvable deploy settings, no active issue session, and a matching online Codex worker. The queued deploy task uses the same `agent_session` and `issue_test_environments` contracts as a manual test deploy, with automation marker `auto_test_deploy`.

Environment records are product-level targets, not workers. A worker registers separately, claims tasks by runtime mode and capabilities, and receives the selected Environment snapshot as task context.

Create a virtual machine Environment:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/environments" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"staging-vm-01",
    "kind":"virtual_machine",
    "virtualMachine":{
      "sshHost":"10.0.8.21",
      "sshPort":22,
      "sshUser":"ubuntu",
      "sshAuthRef":"secret://mspace/staging-vm-01",
      "workdir":"/srv/mspace",
      "serviceHint":"systemd:mspace"
    },
    "sshAuth":{
      "method":"password",
      "password":"<one-time-password-for-validation>"
    }
  }'
```

For private-key validation, send `"sshAuth":{"method":"private_key","privateKey":"<pem-or-openssh-private-key>","passphrase":"<optional-passphrase>"}`. The server stores usable VM SSH credentials for later worker access and does not return raw secret material in the Environment response.

Recheck a saved virtual machine Environment without editing host metadata. Omit `sshAuth` to use the saved credential, or include it to replace the saved credential and recheck in one request:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/environments/<environment-id>/check" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "sshAuth":{
      "method":"password",
      "password":"<password-for-this-ssh-user>"
    }
  }'
```

Create a Kubernetes Environment through the product API:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/environments" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"staging-k8s",
    "kind":"kubernetes",
    "kubernetes":{
      "kubeconfigPath":"/etc/mspace/kubeconfigs/staging.kubeconfig",
      "kubeContext":"staging",
      "imageRegistryPrefix":"registry.example.com/mspace",
      "exposureMode":"nodeport"
    }
  }'
```

Kubernetes Environments currently use the existing `clusters` storage and remain visible through the `/clusters` compatibility API. Create, update, import, `POST /clusters/{clusterID}/check`, and `POST /environments/{environmentID}/check` all refresh `status` and `lastCheckedAt` from a server-side kubeconfig check: API server discovery plus a lightweight namespace list permission probe. Virtual machine Environments run an SSH login check during create/update and environment-level recheck with either saved or newly supplied password/private-key auth. Only a successful login marks the VM `ready`; network/auth failures save it as `unreachable`, while missing password/private key input is rejected when no saved credential exists. `sshAuthRef` and `sshAuthConfigured` are response metadata only; raw passwords and private keys stay in the server store and are not returned to clients.

## Server Agent Sessions

The desktop shell proactively ensures one generic personal Worker after auth and workspace selection. Electron detects installed `codex`, `claude`, and `pi` executables without launching them and advertises `codex`, `claudeCode`, and `pi` respectively. Issue Detail starts a turn only after engine-specific preflight:

1. Map `@codex`, `@claude`, or `@pi` to `{"codex":true}`, `{"claudeCode":true}`, or `{"pi":true}` and require `state:"ready"` from `GET /api/workspaces/{workspaceID}/runtime/availability`.
2. In personal desktop mode, ask Electron to ensure the host-local personal worker, then wait briefly for the availability response to show a ready worker. Do not skip the Electron ensure step only because the server still has a fresh heartbeat snapshot; that snapshot can survive an app restart for a short window. Team workspaces do not auto-start a worker; the user must connect a matching team worker.
3. Write the human comment through `POST /api/workspaces/{workspaceID}/issues/{issueID}/comments`.
4. Call `POST /api/workspaces/{workspaceID}/issues/{issueID}/sessions` with the comment id as `triggerCommentId`.

Personal workspaces use `runtimeMode: "personal"`; team workspaces use `runtimeMode: "team"`. Both modes share the same server tables, worker claim protocol, task logs, cancellation, and result shape.

```json
{
  "agentEngine": "claude_code",
  "runtimeMode": "team",
  "command": "@claude #think implement the fix",
  "triggerCommentId": "<server-comment-id>",
  "skillSlugs": ["think"]
}
```

Issue comments can reference enabled server-managed workflow skills with `/slug` or `#slug`. Desktop clients should derive `skillSlugs` from the final submitted comment body and send only those slugs. The server accepts built-in or workspace custom skill slugs, de-duplicates them, rejects unknown, disabled, or malformed slugs with HTTP `400`, and rejects client-provided `requiredSkills`, `skills`, `skillBundles`, or skill file content on issue-session creation. Full skill bundles remain server-owned and are included in the worker runtime task payload; workspace user APIs return compact skill references except for owner/admin skill management detail endpoints.

The server validates `agentEngine`, project attachment, workspace/runtime mode, and an active Worker with the exact capability before it creates the runtime task. If no Worker matches, it returns HTTP `409` with `{"error":"no active agent worker"}`. New clients send only `agentEngine`; known legacy `provider`/`agentProfile` inputs map to Codex, while explicit unknown engines fail closed.

When accepted, the server snapshots Issue/project/runbook/comment/child/label context into the runtime task payload and returns `AgentSession`. The Worker prepares its own repo cache/workdir and reports `agentEngine`, `engineSessionRef`, `engineRunRef`, source branch, and commit metadata. Codex also returns legacy thread/turn aliases. Claude Code completion requires a terminal stream-JSON `result`; Pi uses official RPC and requires `agent_end`, sends `abort` on cancellation, and never exposes `sessionFile` paths.

New project-backed issues may also create an automatic `agent_session` with payload `automation:"issue_analysis"` when a matching Codex worker is online. Attaching a project to a projectless top-level issue also tries the same analysis queueing path. That payload is queued before type triage when created with a project, includes `sandbox:"read-only"`, `sourceCapture:false`, and the pinned server-owned `think` skill bundle in `requiredSkills`; workers materialize the skill under the session artifact directory and expose `MSPACE_SESSION_SKILLS_DIR` to Codex. Issue creation and project attachment do not fail when the analysis cannot be queued, and server reconciliation ignores source/test/deploy/review artifacts from this automation.

## Test Environment Flow

Start a test deploy from captured source evidence:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/issues/<issue-id>/test-deploy" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{
    "environmentId":"<kubernetes-environment-id>",
    "sourceSessionId":"<source-session-id>",
    "sourceCommitSha":"<source-commit-sha>",
    "exposureMode":"nodeport"
  }'
```

`clusterId` is still accepted for compatibility, but new clients should send `environmentId`. The selected Environment must currently be `kind:"kubernetes"` because issue deploy, cleanup, Resources, and preview probing still operate on an issue namespace. If a VM Environment is submitted, the server rejects the deploy request instead of queueing a misleading Kubernetes task.

The server first resolves the Environment, snapshots its id/kind/config, and creates the `agent_session` runtime task with Kubernetes and source metadata. After queueing succeeds, it stores or updates `issue_test_environments` with the Environment snapshot, deployment session id, and `deploying` state. The worker performs the deploy/test turn and can write `test-environment.json` in its artifact directory to report preview values. Automatic test deploys follow this same path and pin `sourceSessionId` / `sourceCommitSha` to the completed source session that triggered them.

Inspect live namespace resources:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/issues/<issue-id>/test-environment/resources"
```

## Runtime Worker APIs

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/install/worker` | Return the self-host worker install script used by generated install commands. |
| `POST` | `/api/workspaces/{workspaceID}/worker-installations` | Create a short-lived worker host install command. Owner/admin only. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | Create a short-lived worker registration token. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | List worker registration token metadata without raw token values. |
| `DELETE` | `/api/workspaces/{workspaceID}/runtime-registration-tokens/{tokenID}` | Revoke a worker registration token. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-workers` | List registered runtime workers, heartbeat state, labels, capabilities, and sanitized Agent-engine diagnostics. |
| `GET` | `/api/workspaces/{workspaceID}/runtime/availability` | Return structured readiness plus server-derived `claimableWorkerCount` for a runtime mode and required capability set. Use this for product action preflight instead of reimplementing heartbeat TTLs or matching in clients. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks` | Owner/admin-only creation of unbound `protocol_smoke` or `noop` diagnostics. Raw payloads cannot bind product records or include Agent, Skill, automation, repository, workdir, environment, or other server-owned fields. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks?limit=10&offset=0` | List runtime tasks with pagination metadata and status counts. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/events` | List audit events for one runtime task. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/logs` | List worker-appended logs for one runtime task. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/cancel` | Request cancellation for a queued, claimed, or running task. |
| `POST` | `/api/runtime/workers/register` | Register or refresh a worker using `Authorization: Bearer msw_...`. |
| `POST` | `/api/runtime/workers/{workerID}/heartbeat` | Update worker liveness, status, load, optional capability metadata, and optional sanitized `agentEngineDiagnostics`. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/claim` | Claim the next queued task that matches the worker mode and required capabilities. |
| `GET` | `/api/runtime/workers/{workerID}/tasks/{taskID}` | Let the claiming worker inspect task state while executing. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/{taskID}/logs` | Append a log line to a claimed/running task. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/{taskID}/status` | Move a claimed task to `running`, `completed`, `failed`, or `cancelled`. |

Check action readiness for a Codex-backed turn:

```bash
curl -G "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/runtime/availability" \
  -H "Authorization: Bearer <msp-token>" \
  --data-urlencode 'runtimeMode=personal' \
  --data-urlencode 'requiredCapabilities={"codex":true}'
```

The response is HTTP `200` even for unavailable states so clients can branch on structured diagnostics:

```json
{
  "workspaceId": "<workspace-id>",
  "runtimeMode": "personal",
  "requiredCapabilities": { "codex": true },
  "state": "unavailable",
  "reasonCode": "no_worker",
  "canQueue": false,
  "claimableWorkerCount": 0,
  "canAutoStart": true,
  "retryAfterMs": 5000,
  "activeWorkerMaxAgeMs": 45000
}
```

`reasonCode` may be `ready`, `no_worker`, `missing_capability`, `stale_heartbeat`, `worker_draining`, `worker_offline`, or `wrong_runtime_mode`. `claimableWorkerCount` is computed with the server's workspace-mode, liveness, heartbeat TTL, load, and exact-capability rules. Clients connected to an older Server that omits the field should show an unknown count rather than recomputing it. Personal desktop clients should ask Electron main to idempotently ensure the host-local worker before trusting a ready heartbeat, then poll availability again when the worker is starting. Team clients must treat unavailable states as a Connect Environment problem.

Current Workers may send the following optional diagnostic object during register and heartbeat:

```json
{
  "agentEngineDiagnostics": {
    "codex": { "status": "ready", "reasonCode": "auth_ok", "version": "codex-cli 1.2.3", "checkedAt": "2026-07-17T08:00:00Z" },
    "claude_code": { "status": "needs_setup", "reasonCode": "auth_required", "version": "claude 2.1.89", "checkedAt": "2026-07-17T08:00:00Z" },
    "pi": { "status": "unverified", "reasonCode": "probe_unsupported", "version": "pi 0.35.0", "checkedAt": "2026-07-17T08:00:00Z" }
  }
}
```

Statuses are `ready`, `needs_setup`, `unverified`, `missing`, or `probe_error`. The Server discards unknown engines and invalid diagnostic fields, rejects an oversized object, and uses a diagnostic only to turn a previously allowed engine capability off. Heartbeats that omit `agentEngineDiagnostics` preserve the stored snapshot; an explicit empty object clears it. Probe output, credentials, executable paths, and Pi session paths are never part of this API.

Create a worker install command:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/worker-installations" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"build-vm-worker","expiresInHours":1}'
```

The response includes `installCommand`, `runtimeMode`, `workerName`, `credentialPrefix`, and `expiresAt`. Product UI should show the install command and hide the raw bootstrap credential. The worker host needs Docker plus Codex `auth.json` and `config.toml`; after running the command, the worker appears in `/runtime-workers` after its first heartbeat.

For Kubernetes-hosted fixed workers managed by the Helm chart, use `bootstrap.teamWorkspace.enabled=true` instead of the UI install command. Helm creates or reuses a release Secret entry named `MSPACE_RUNTIME_TOKEN`, passes it to the server as `MSPACE_BOOTSTRAP_RUNTIME_TOKEN`, and injects only that key into the Worker as its runtime registration credential; the Worker does not receive the Server's database, GitHub, OAuth, or bootstrap credentials. The operator still creates the Worker Codex home Secret separately; that Secret must include `auth.json` plus `config.toml` and is mounted only by the Worker. Agent subprocesses also strip inherited control-plane and Worker-registration variables before the server-owned session environment is appended.

Queue a protocol smoke task from API/debug tooling:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/runtime-tasks" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"protocol_smoke","runtimeMode":"team","requiredCapabilities":{"protocolSmoke":true},"payload":{"source":"curl"}}'
```

The server rejects runtime worker registration and runtime task creation when the submitted mode does not match the workspace kind. An install command or token minted in a personal workspace can only register a personal worker, and one minted in a team workspace can only register a team worker. Manual runtime task requests follow the same rule: `runtimeMode:"personal"` for personal workspaces and `runtimeMode:"team"` for team workspaces.

Desktop personal workers use the same token endpoints, but the user normally never sees the raw credential. Electron creates a 12-hour personal worker credential, writes it to an Electron user-data token file, renews it before expiry, and revokes the replaced credential after a short grace period. The worker supports `MSPACE_RUNTIME_TOKEN_FILE` and rereads that file for runtime API calls, so token renewal is designed to be invisible to personal users. Electron also persists an anonymous `msh_...` host id and labels the base Worker `primary`; browser-required work starts a separately named `browser_companion` with the same host id and isolated execution roots. The renderer identifies This Mac only by exact trusted host-id match. The personal Worker and Agent CLI still share an OS user, so environment filtering does not replace filesystem isolation for the token file.

Workspace Settings lists runtime tasks as an operations surface: task purpose, linked Issue title when available, status, worker, update time, and detail/cancel actions. Agent-session task links include `sessionId` so Issue Detail can scroll to the relevant session card. Pure protocol tasks such as `issue_type_triage` may only open the Issue page because they do not have a session card. Protocol payloads remain in expanded details instead of the primary row, but server-provided skill bundles are redacted to compact references on workspace user APIs; full bundled files are returned only through worker claim/get endpoints.

The runtime task list endpoint returns `{ tasks, total, limit, offset, statusCounts }`. Use `limit` and `offset` for paged UI lists; the server clamps invalid limits and keeps the result ordered by newest task first. Desktop clients normalize older array responses defensively so a renderer update does not crash while a local or remote server is still restarting onto the paged contract.

Desktop personal worker credentials are named `Desktop personal worker credential` and shown as automatic desktop credentials. Workspace Settings separates active credentials from expired or replaced credential history so background renewal does not look like a pile of duplicate manual credentials.

Runtime task kinds used by the current product path:

| Kind | Producer | Claimed by | Result owner |
| --- | --- | --- | --- |
| `protocol_smoke` | User/API smoke | Any worker with `protocolSmoke:true` | Task result only |
| `noop` | User/API smoke | Any matching worker | Task result only |
| `issue_type_triage` | Server issue creation/update path | Worker with `codex:true` | Server conditionally reconciles the generated title and independently applies the issue type label |
| `agent_session` | Issue agent mention or test-deploy path | Worker with required runtime capabilities | Server derives session detail, source changes, evidence, and environment state |

## Artifact Contract

Workers may improve the session result by writing JSON or Markdown files under `${MSPACE_SESSION_ARTIFACT_DIR}`:

- `branch-name.json`: proposed source branch, for example `{ "branch": "fix/pr-source-branch-selection" }`.
- `review-evidence.json`: command evidence, tests, build/deploy result, summary, risks, and follow-ups.
- `test-environment.json`: deploy/test result, including `previewUrl` when available.
- `project-runbook.md`: learned project runbook update after a successful session.

Branch names should use Conventional Commit-style prefixes: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `build`, `ci`, `style`, or `revert`.
