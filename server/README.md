# mspace Server

`server/` is the mspace control plane. It owns identity, workspaces, membership, auth sessions, workspace product data, project test data, runtime-facing product surfaces, and workspace GitHub App installation state.

The desktop app and runtime workers are clients of this service. They do not own collaboration identity or product truth.

The server is intentionally Agent-runtime-free. It does not install Codex, Claude Code, or Pi, mount their homes, read their credentials, or start their processes. Agent work is expressed as runtime tasks and executed by workers. The server can still own workflow skill metadata and bundle content, so every worker receives the same required skill revision with a task instead of depending on worker-local skill installs.

## Run

Create a local env file in the project root:

```bash
cp .env.example .env.local
```

Then edit `.env.local`. Without `DATABASE_URL`, the server defaults to the local SQLite personal store:

```dotenv
# Optional. Set for Postgres-backed shared development or deployment.
# DATABASE_URL=postgres://mspace:mspace@127.0.0.1:5432/mspace?sslmode=disable
# Optional. Use sqlite for packaged/local personal mode; use postgres for shared deployments.
# MSPACE_STORE=sqlite
# MSPACE_SQLITE_PATH=/path/to/mspace.db
# MSPACE_DATA_DIR=/path/to/mspace-data
# Optional. Only needed for GitHub OAuth sign-in.
# MSPACE_GITHUB_CLIENT_ID=...
# MSPACE_GITHUB_CLIENT_SECRET=...
# MSPACE_GITHUB_REDIRECT_URI=http://127.0.0.1:8787/api/auth/github/callback
# Optional. Server-owned GitHub App automation readiness.
# MSPACE_GITHUB_APP_ID=...
# MSPACE_GITHUB_APP_CLIENT_ID=...
# MSPACE_GITHUB_APP_PRIVATE_KEY="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----"
MSPACE_SERVER_ADMIN_LOGINS=admin,mlhiter
MSPACE_BOOTSTRAP_ADMIN_LOGIN=admin
MSPACE_BOOTSTRAP_ADMIN_PASSWORD=change-me-long-random-password
MSPACE_BOOTSTRAP_ADMIN_NAME=Admin
MSPACE_SERVER_ADDR=127.0.0.1:8787
MSPACE_DEV_POSTGRES_CONTAINER=mspace-postgres-dev
MSPACE_DEV_POSTGRES_VOLUME=mspace-postgres-data
MSPACE_DEV_POSTGRES_IMAGE=postgres:16
```

Start the server:

```bash
pnpm run server
```

The server loads `.env`, `.env.local`, `server/.env`, and `server/.env.local` from the project root. Shell environment variables still take precedence over values from those files.

For local personal runs without Postgres, use the SQLite store. Production and shared development should use Postgres. The in-memory store is for tests only.

When `scripts/run-mspace-codex-dev.sh` needs to auto-start local Docker Postgres, it expects the durable data volume above. It labels the container and volume and rejects an existing `mspace-postgres-dev` container if it points at a different Postgres data volume.

## Auth Shape

Local password auth and GitHub OAuth are both identity providers. The product session is always a server-issued `msp_...` token.

Password auth is the default path for restricted or offline environments:

1. Desktop posts `POST /api/auth/password/register` or `POST /api/auth/password/login`.
2. The server validates the username/password payload, stores only a bcrypt password hash for registrations, ensures a default personal workspace, and issues an mspace session token.
3. Desktop stores the returned `msp_...` token.
4. Desktop clients call mspace APIs with `Authorization: Bearer <msp_...>`.

GitHub OAuth is optional. The server advertises OAuth availability through `/health` with `capabilities.githubAuth: true`, which requires `MSPACE_GITHUB_CLIENT_ID`, `MSPACE_GITHUB_CLIENT_SECRET`, and `MSPACE_GITHUB_REDIRECT_URI` to all be configured. GitHub App repository automation is separate: `/health` reports `capabilities.githubApp: true` only when `MSPACE_GITHUB_APP_ID`, `MSPACE_GITHUB_APP_CLIENT_ID`, and `MSPACE_GITHUB_APP_PRIVATE_KEY` are configured. The desktop also gates the sign-in button by server source: the default local personal server stays local-account-only, while saved Team server URLs and `MSPACE_SERVER_URL` launches can show GitHub when OAuth capability is true.

1. Desktop starts GitHub login through `GET /api/auth/github/start`.
2. Desktop opens the returned `authorizeUrl` in the browser.
3. GitHub redirects to `GET /api/auth/github/callback`.
4. The server validates OAuth state, exchanges the code with GitHub using the server-side client secret, upserts the mspace user, ensures a default personal workspace, issues an mspace session token, and stores a short-lived single-use login result for that OAuth state.
5. The callback renders a success page. It does not return raw auth JSON.
6. Desktop polls `GET /api/auth/github/result?state=...` and stores the returned `msp_...` token.
7. Desktop clients call mspace APIs with `Authorization: Bearer <msp_...>`.

GitHub tokens are not the product session. They are used only to prove GitHub identity. Local password registration does not verify email ownership, so it must not merge identities by email. Durable team repository automation should later use GitHub App installation tokens minted by this service from workspace installation state; outside the explicitly personal Codex PR handoff path, workers should not depend on local `gh` identity.

Password and GitHub auth responses, plus `GET /api/auth/me`, include `identity.provider` and `identity.login`. UI clients should display account type from that explicit provider. Do not treat an empty `user.email` as a GitHub connection fallback; password accounts intentionally keep canonical email blank. `PUT /api/auth/me` may update the current user's display `name` and `avatarUrl`, but it must not update the auth provider, auth login, password credential, or GitHub identity link.

Only server admins can create team workspaces. `MSPACE_SERVER_ADMIN_LOGINS` lists local password logins or GitHub logins with that right. Emails are not used for admin matching because local password registration does not verify email ownership. `MSPACE_BOOTSTRAP_ADMIN_LOGIN` plus `MSPACE_BOOTSTRAP_ADMIN_PASSWORD` creates the first local admin account on startup if it does not already exist; the server does not reset the password for an existing account. If `MSPACE_BOOTSTRAP_TEAM_WORKSPACE_NAME` and `MSPACE_BOOTSTRAP_RUNTIME_TOKEN` are also set, startup creates or finds that admin-owned team workspace and registers the supplied `msw_...` token for the fixed worker. Other registered users still get a personal workspace and can join a team workspace through an owner/admin invitation.

## API Slice

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Server health. |
| `POST` | `/api/auth/password/register` | Create a local username/password identity, default personal workspace, and mspace session. |
| `POST` | `/api/auth/password/login` | Authenticate a local account and issue a mspace session. |
| `GET` | `/api/auth/github/start` | Create OAuth state and return the GitHub authorization URL plus polling path. |
| `GET` | `/api/auth/github/callback` | Complete GitHub OAuth, link identity, create an mspace session, and render the browser success page. |
| `GET` | `/api/auth/github/result` | Poll the state-bound login result from the desktop app. Returns `202` while pending and consumes the result once ready. |
| `GET` | `/api/auth/me` | Return the current mspace user, workspaces, auth identity provider/login, and admin status for a bearer token. |
| `PUT` | `/api/auth/me` | Update the current user's display name and avatar URL while keeping auth identity fields read-only. |
| `GET` | `/api/workspaces` | List the authenticated user's workspaces. |
| `POST` | `/api/workspaces` | Create an explicit team workspace for the authenticated user. |
| `PUT` | `/api/workspaces/{workspaceID}` | Update team workspace identity fields: name, mark, and description. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/members` | List workspace members with role and display identity. |
| `POST` | `/api/workspaces/{workspaceID}/invitations` | Create a one-time `msi_...` workspace invitation link. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/invitations` | List recent workspace invitations without raw tokens. Owner/admin only. |
| `DELETE` | `/api/workspaces/{workspaceID}/invitations/{invitationID}` | Revoke a workspace invitation. Owner/admin only. |
| `GET` | `/api/workspace-invitations/preview?token=msi_...` | Preview a join link without authentication. Returns workspace name, role, inviter display fields, expiry, and status only. |
| `POST` | `/api/workspace-invitations/accept` | Accept a workspace invitation as the current authenticated user. |
| `GET` | `/api/workspaces/{workspaceID}/inbox` | List unread workspace issue-event receipts for the authenticated user. |
| `POST` | `/api/workspaces/{workspaceID}/issue-events` | Append a reviewable issue event and create per-user receipts. |
| `POST` | `/api/workspaces/{workspaceID}/issue-events/{eventID}/read` | Mark one event receipt read for the authenticated user. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/read-through` | Mark unread receipts for one issue read through an optional event boundary. |
| `GET` | `/api/workspaces/{workspaceID}/projects` | List workspace projects. |
| `POST` | `/api/workspaces/{workspaceID}/projects` | Create a workspace project. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}` | Update a workspace project. |
| `DELETE` | `/api/workspaces/{workspaceID}/projects/{projectID}` | Delete a project when no issues reference it. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/runbook` | Read the workspace project runbook. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}/runbook` | Replace the workspace project runbook and record a revision. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases` | List project test cases as `{ cases, total, limit, offset }`; supports `status`, `q`, `limit`, and `offset`, and hides archived cases unless `status=archived`. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import/preview` | Parse Markdown, text, CSV, or Excel `.xlsx` cases without writing them, returning import counts, skipped rows, missing field counts, quality finding counts, and sample rows. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import` | Import Markdown, text, CSV, or Excel `.xlsx` cases. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/optimize` | Queue an issue-backed Codex case refinement session. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/generate` | Queue an issue-backed Codex case generation session. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases` | Create a project test case. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/delete` | Archive multiple project test cases by id. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}` | Read one project test case. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}` | Update one project test case and record a revision. |
| `DELETE` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}` | Archive one project test case and record a revision. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}/revisions` | List project test case revisions. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals` | List Codex case suggestions. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals/{proposalID}/apply` | Accept a case suggestion. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals/{proposalID}/reject` | Dismiss a case suggestion. |
| `GET` | `/api/workspaces/{workspaceID}/test-plans` | List workspace test plans. |
| `POST` | `/api/workspaces/{workspaceID}/test-plans` | Create a workspace test plan from ready project cases. |
| `GET` | `/api/workspaces/{workspaceID}/test-plans/{planID}` | Read one workspace test plan. |
| `PUT` | `/api/workspaces/{workspaceID}/test-plans/{planID}` | Update one workspace test plan. |
| `POST` | `/api/workspaces/{workspaceID}/test-plans/{planID}/runs` | Start an issue-backed test run. |
| `GET` | `/api/workspaces/{workspaceID}/test-runs` | List workspace test runs from plans, compatibility ad hoc runs, retries, or incremental scopes. |
| `POST` | `/api/workspaces/{workspaceID}/test-runs` | Compatibility/debug API for starting an ad hoc issue-backed run from selected ready test cases. Normal product clients should create a test plan and call `/api/workspaces/{workspaceID}/test-plans/{planID}/runs`. |
| `GET` | `/api/workspaces/{workspaceID}/test-runs/{runID}` | Read one workspace test run. |
| `GET` | `/api/workspaces/{workspaceID}/test-runs/{runID}/artifacts` | List artifacts for one workspace test run. |
| `POST` | `/api/workspaces/{workspaceID}/test-runs/{runID}/retry` | Retry blocked or failed test run items. |
| `POST` | `/api/workspaces/{workspaceID}/test-runs/{runID}/accept` | Record that a user reviewed the test run result. |
| `POST` | `/api/workspaces/{workspaceID}/test-runs/{runID}/block` | Record that a test run needs follow-up with a human note. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-plans` | Compatibility-filter workspace plans by project. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-plans` | Compatibility-create a workspace plan using the URL project as default case project. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}` | Compatibility-read a workspace plan that includes the project. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}` | Compatibility-update a workspace plan using the URL project as default case project. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}/runs` | Compatibility-start a workspace run from a plan that includes the project. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-runs` | Compatibility-filter workspace runs by project. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-runs` | Compatibility-start an ad hoc workspace run using the URL project as default case project. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}` | Compatibility-read a workspace run that includes the project. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/artifacts` | Compatibility-list artifacts for a workspace run filtered to the project. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/retry` | Compatibility-retry blocked or failed run items when the run includes the project. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/accept` | Compatibility-accept a workspace run that includes the project. |
| `POST` | `/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/block` | Compatibility-block a workspace run that includes the project. |
| `GET` | `/api/workspaces/{workspaceID}/workspace/settings` | Read workspace automation settings. |
| `PUT` | `/api/workspaces/{workspaceID}/workspace/settings` | Update workspace automation settings. |
| `GET` | `/api/workspaces/{workspaceID}/github-app` | Read server-owned GitHub App installation status for the workspace. |
| `GET` | `/api/workspaces/{workspaceID}/skills` | List built-in and workspace custom workflow skill metadata. |
| `POST` | `/api/workspaces/{workspaceID}/skills` | Create a workspace custom workflow skill with `SKILL.md` files. |
| `GET` | `/api/workspaces/{workspaceID}/skills/{skillID}` | Read owner/admin skill detail and files for management. |
| `PUT` | `/api/workspaces/{workspaceID}/skills/{skillID}` | Update a custom skill revision or built-in workspace invocation settings. |
| `DELETE` | `/api/workspaces/{workspaceID}/skills/{skillID}` | Delete a workspace custom skill. Built-ins cannot be deleted. |
| `POST` | `/api/workspaces/{workspaceID}/skills/{skillID}/duplicate` | Copy a built-in or custom skill into a new workspace custom skill. |
| `GET` | `/api/workspaces/{workspaceID}/agents` | List the fixed Codex, Claude Code, and Pi Agent engine catalog. |
| `GET` | `/api/workspaces/{workspaceID}/environments` | List Kubernetes and virtual machine Environments. Kubernetes rows are projected from cluster compatibility records. |
| `POST` | `/api/workspaces/{workspaceID}/environments` | Create an Environment with `kind:"kubernetes"` or `kind:"virtual_machine"`. Kubernetes create checks kubeconfig reachability; VM create requires `sshAuth` password/private-key material, saves it server-side, and derives status from SSH login validation. |
| `PUT` | `/api/workspaces/{workspaceID}/environments/{environmentID}` | Update an Environment. Kubernetes update refreshes kubeconfig reachability; VM update uses the saved SSH credential unless new `sshAuth` is provided to replace it. |
| `POST` | `/api/workspaces/{workspaceID}/environments/{environmentID}/check` | Refresh Environment reachability. Kubernetes delegates to the kubeconfig check; VM uses the saved SSH credential by default or replaces it when new `sshAuth` is provided. |
| `DELETE` | `/api/workspaces/{workspaceID}/environments/{environmentID}` | Delete an unused Environment. |
| `GET` | `/api/workspaces/{workspaceID}/clusters` | Compatibility API for Kubernetes cluster configs. Prefer `/environments` in product integrations. |
| `POST` | `/api/workspaces/{workspaceID}/clusters` | Compatibility API for creating a Kubernetes cluster config. |
| `PUT` | `/api/workspaces/{workspaceID}/clusters/{clusterID}` | Compatibility API for updating a Kubernetes cluster config. |
| `POST` | `/api/workspaces/{workspaceID}/clusters/{clusterID}/check` | Compatibility API for refreshing Kubernetes Environment reachability. |
| `DELETE` | `/api/workspaces/{workspaceID}/clusters/{clusterID}` | Compatibility API for deleting an unused Kubernetes cluster config. |
| `GET` | `/api/workspaces/{workspaceID}/clusters/discover-defaults` | Discover kubeconfig candidates under `~/.kube`. |
| `POST` | `/api/workspaces/{workspaceID}/clusters/import` | Import selected kubeconfig files into workspace clusters. |
| `GET` | `/api/workspaces/{workspaceID}/issue-label-definitions` | List issue type and priority label definitions. |
| `GET` | `/api/workspaces/{workspaceID}/issues` | List top-level workspace issues. |
| `POST` | `/api/workspaces/{workspaceID}/issues` | Create a workspace issue. New clients sending a plain-text draft `title` also send `titleSource: "plain_text"`; omission supports older Markdown-derived drafts. Creation returns without waiting for refinement; after any project analysis is queued, the existing `issue_type_triage` task receives the captured draft and updated workers can return a rewritten title with the type result. The server conditionally applies only a sanitized plain title while preserving older type-only worker compatibility. `projectId` is optional; issues can remain projectless until execution is needed. |
| `GET` | `/api/workspaces/{workspaceID}/issues/{issueID}` | Load Issue detail with optional project, Issue working-copy state, child tasks, labels, comments, and Sessions. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}` | Update issue project attachment, plain-text title, body, or workflow status. A title-only request may include `expectedTitle`; the stores update only the title when it still matches, otherwise they return the current Issue unchanged. Attaching a project to a projectless top-level issue also tries the automatic read-only `issue_analysis` path when a matching Codex worker is online. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/tasks` | Create a child issue task. |
| `DELETE` | `/api/workspaces/{workspaceID}/issues/{issueID}/tasks/{taskID}` | Delete a child issue task under the parent. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/labels` | Replace selected issue label keys. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments` | Add a Markdown human comment. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}` | Edit the current user's eligible human comment. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}` | Add the current user's reaction to a comment. |
| `DELETE` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}` | Remove the current user's reaction from a comment. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/sessions` | Atomically reserve the Issue's source working copy and queue a human `agent_session` after a supported Agent mention, attached project, and claimable Worker with the selected engine capability and `issueWorkingCopyV1`. Branch/source/execution/workdir fields are Server-owned and rejected from clients. |
| `GET` | `/api/workspaces/{workspaceID}/sessions/{sessionID}` | Load session detail derived from the runtime task and worker logs. |
| `POST` | `/api/workspaces/{workspaceID}/sessions/{sessionID}/cancel` | Request cancellation for the session's runtime task. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-deploy` | Queue a server-owned test deployment session for an issue. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/cleanup` | Queue test namespace cleanup for an issue. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/retain` | Retain the issue test namespace for debugging. |
| `GET` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/resources` | List namespace-scoped Kubernetes resources for the issue test environment. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/probe` | Refresh preview reachability state for the issue test environment. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/create-pr` | Queue a personal Codex PR handoff session with the server-provided `pr-creator` skill for captured source evidence. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/{handoffID}/refresh` | Refresh the server-owned issue handoff record from GitHub when the recorded PR URL is reachable. Personal Codex handoffs still create PRs through the `pull_request_handoff` session path. |
| `POST` | `/api/workspaces/{workspaceID}/worker-installations` | Create a short-lived worker host install command. Owner/admin only. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | Create a short-lived worker registration credential. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-registration-tokens` | List worker registration credential metadata without raw token values. Owner/admin only. |
| `DELETE` | `/api/workspaces/{workspaceID}/runtime-registration-tokens/{tokenID}` | Revoke a worker registration credential. Owner/admin only. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-workers` | List registered runtime workers and their latest heartbeat state. |
| `GET` | `/api/workspaces/{workspaceID}/runtime/availability` | Return readiness for a runtime mode and required capabilities. Optional `issueId` also applies source writer, recovery, and Worker storage-affinity rules. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks` | Workspace owner/admin API for unbound `protocol_smoke` or `noop` diagnostics. Raw tasks cannot bind Issue/Session/Project records or include Agent, Skill, automation, workdir, environment, or other server-owned control fields. Product and system tasks must use their dedicated server-owned flows. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks?limit=10&offset=0` | List runtime tasks for the workspace with pagination metadata and status counts. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/events` | List audit events for one runtime task. |
| `GET` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/logs` | List worker-appended logs for one runtime task. |
| `POST` | `/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/cancel` | Request cancellation for a queued, claimed, or running runtime task. |
| `POST` | `/api/runtime/workers/register` | Register or refresh a worker using `Authorization: Bearer msw_...`. |
| `POST` | `/api/runtime/workers/{workerID}/heartbeat` | Update worker liveness, status, load, and optional capability metadata. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/claim` | Claim the next queued task that matches Worker mode, required capabilities, and optional Issue storage affinity. First Issue-working-copy claim binds its storage id. |
| `GET` | `/api/runtime/workers/{workerID}/tasks/{taskID}` | Let the claiming worker inspect its task status while executing. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/{taskID}/logs` | Append a log line to a claimed/running worker-owned task. |
| `POST` | `/api/runtime/workers/{workerID}/tasks/{taskID}/status` | Move a claimed task to `running`, `completed`, `failed`, or `cancelled`. |

## Workspace Inbox Model

The workspace Inbox is event-based. `issue_events` stores the append-only review fact, `issue_event_receipts` stores each recipient user's unread/read/archive state, and `issue_watchers` stores the issue-level recipient set. Opening or polling an issue must not clear unread state; clients should call the read-through endpoint after the user intentionally reviews an Inbox row.

Personal workspaces are the default result of password registration or GitHub sign-in. Personal and team workspaces both store projects, runbooks, issues, comments, reactions, labels, Inbox receipts, Skills, environments, Kubernetes cluster compatibility records, issue test environments, PR handoffs, Agent Sessions, runtime tasks, Worker logs, and runtime results in the server store. Agent definitions are not persisted workspace records: `GET /agents` returns the fixed code-owned Codex, Claude Code, and Pi catalog. Team/shared deployments use Postgres; packaged personal desktop mode can use local SQLite. Runtime worker registration and task APIs are available to both personal and team workspaces, but the runtime mode must match the workspace kind: personal workspaces use personal workers, while team workspaces use team workers. Team workspaces additionally unlock invitations and shared membership.

Issue `project_id` is optional in the control plane. A user can capture a workspace-level issue before the repository is known, comment on it, and attach a project later through `PUT /api/workspaces/{workspaceID}/issues/{issueID}` with `projectId`. If a create request omits `projectId` and the workspace has exactly one project, the server auto-attaches it; zero or multiple projects leave the issue unassigned. Agent execution, PR handoff, Tests, and issue test environments require an attached project.

Project creation is workspace-kind aware. Personal workspaces may use `sourceType:"local"` with a local repository path or `sourceType:"github"` with a repository URL. Team workspaces must use `sourceType:"github"` so external team workers can clone source into their own repository cache; the server rejects team projects that try to store a user's desktop-local path.

## Test Module Model

Test cases, revisions, and suggestions are server-owned project data. Test plans, runs, and run items are server-owned workspace data that preserve per-case/per-item project identity. Team/shared deployments persist them through Postgres migrations, while packaged personal desktop mode persists them through the server-owned SQLite snapshot store.

The case list is paginated with `limit` and `offset`. Default case browsing excludes `archived` cases, and user-facing deletion is implemented as an archive status transition so revisions, plan membership, run items, artifacts, and proposal links remain auditable. Clients can inspect archived cases with `status=archived`.

Case import supports `markdown`, `text`, `csv`, and `xlsx`. Clients should call `/test-cases/import/preview` first, show how many cases will be imported, skipped rows, missing field counts, quality findings, source-column mappings, and samples, then call `/test-cases/import` only after the user confirms. Markdown/text import treats each non-empty line as one case. CSV and `.xlsx` share the canonical `title`, `type`, `area`, `priority`, `preconditions`, `steps`, `expected_result`, `environment_requirements`, and `tags` header contract. Unknown or product-specific source headers should be sent to `/test-cases/import/mapping-task`, which queues a Codex worker runtime task that returns mapping suggestions; clients then pass the confirmed `columnMappings` back to preview and import. Business category columns should map to `tags`, not the fixed system `type`, unless the source has an actual mspace case type column. Historical execution-state columns may map to `latest_result` for preview context. Import is capped at 1,000 cases per request. Text-like content is capped at 2 MB; Excel content is base64-encoded in the JSON request and the decoded workbook is capped at 2 MB. The server reads the first non-empty sheet, skips rows without a title, validates types, and opens workbooks with explicit unzip limits.

Valid case types are `functional`, `ui`, `api`, and `deployment`. The current product can classify those cases and plan against them. Execution batches that include `ui` cases, or functional cases with explicit browser/platform/session UI signals such as Sealos Desktop app entry, access-key flows, S3 service parameter modals, screenshots, or frontend URL assertions, require an online worker with `{"codex":true,"browser":true,"chrome_cdp":true}`; run start and retry fail before creating execution Issues when no matching worker is active. Test Plan and Run detail responses expose the server-computed union as `requiredCapabilities`; clients must use that map for worker preflight instead of reimplementing browser-signal classification. Browser-backed result artifacts should save screenshots under `${MSPACE_SESSION_ARTIFACT_DIR}/screenshots/` and reference them from each item with `evidence.screenshotPaths` in `test-result.json` so the worker/server can persist authenticated screenshot artifacts. Specialized API harnessing, deployment orchestration, and multi-worker scheduling remain future execution capabilities behind the same Issue and Worker runtime path.

Codex-generated or Codex-refined cases never write directly into canonical test cases. Workers return `test-case-proposals.json`; the server stores validated proposals as Case suggestions; humans apply or reject them. Normal test runs start from a workspace test plan so the selected cases, target, environment, setup, and execution order are explicit before execution. The server stores plan case order in `test_plan_cases.sort_order` and freezes it onto `test_run_items.sort_order` for each run, so result rows stay aligned with the planned sequence. Plan runs may include free-text setup steps; in that case the server freezes the setup text onto the run, creates one setup Issue/Session first, waits for `test-setup-result.json`, stores `setupResult`, copies setup `outputs` into `runContext`, and only starts case execution after the setup task completed with `status:"passed"`. Failed, cancelled, or missing setup stops the run as `setup_failed` without starting case sessions. The ad hoc selected-case run endpoint remains as a compatibility/debug API, but it is not the normal product path. Execution workers then return `test-result.json`, the server reconciles run items, persists supported screenshot evidence as `test_artifacts`, and a user may record an accept or follow-up-needed review decision for the run result. Deterministic case quality checks flag cases that mutate or remove existing data without declaring whether that data is created by the case, prepared by setup, or provided by a dependency.

## Workspace Invitations

Workspace Settings exposes the first UI-testable collaboration loop. Owners and admins create one-time join links, copy a `mspace://invite/<token>?server=<team-server-url>` deep link, inspect pending/accepted/revoked invitations, and revoke unused invitations. A teammate can open the deep link in the desktop app; if the app is pointed at a different server, it switches to the invited team server before showing the safe preview. If they are not signed in, the app previews only safe invite metadata, lets them create or sign into a local mspace account, then accepts the invite automatically and switches into the team workspace. Invitations are not email-bound because local password accounts do not have verified canonical email addresses.

## Runtime Worker Registry

Agent-session creation is guarded by project attachment, Worker liveness, and Issue working-copy state. The Server checks that the Issue has a project, that the requested `runtimeMode` matches the workspace kind, that no mutable writer is active, and that an online Worker with the selected engine capability, `issueWorkingCopyV1`, a fresh heartbeat, and any required storage affinity can claim the task. UI clients should resolve project attachment and preflight `/runtime/availability?issueId=<issue-id>` before saving a trigger comment, using `reasonCode` and `canAutoStart` instead of reimplementing scheduler rules. Desktop personal mode can ask Electron main to auto-start the host-local personal Worker, while team workspaces require an explicitly connected team Worker.

Normal team worker setup should use the Workspace Settings worker install action, backed by `POST /api/workspaces/{workspaceID}/worker-installations`. The response contains a one-time install command that embeds a short-lived bootstrap credential and starts the Docker-backed worker on the target host. The raw `msw_...` registration credential endpoints remain for Electron's automatic personal worker lifecycle and API-level recovery/debugging, but they are no longer the main product setup path.

Runtime registration credentials use the `msw_` prefix and are returned only once. The server stores a hash and prefix, then workers use the credential to register, heartbeat, claim eligible tasks, and report task status.

Migration 031 adds `runtime_workers.agent_engine_diagnostics`. Workers may report only fixed-engine `status`, `reasonCode`, sanitized `version`, and `checkedAt` fields; the server validates their enums and format, and accepts Pi model reasons only for the canonical `unverified/model_available` and `needs_setup/model_unavailable` tuples. Diagnostics can downgrade an advertised engine capability but cannot enable one. Omitting the heartbeat field preserves the stored snapshot, while explicit `{}` clears it. Availability responses include a server-derived `claimableWorkerCount`; older Workers without diagnostics remain capability-compatible and are displayed as not reported by current clients.

The current queue records workspace task metadata, required capability JSON, payload/result JSON, optional storage affinity, claim ownership, cancellation request state, timestamps, a compact audit event stream, and Worker-appended task logs. Product UI should present these records as Issue-linked runtime tasks with the Issue title and Worker/status context first, while leaving protocol fields in details. Workers stream normalized Agent status and output back through the log endpoint without the Server needing direct network access to the Worker host or any Agent runtime dependency. Queued cancellation is terminal immediately. Claimed/running cancellation stays requested while the claiming Worker interrupts the Agent and reports a final clean, dirty, or recovery-required working-copy envelope.

Runtime task payloads may include `requiredSkills`, which are full server-provided skill bundles for worker consumption. Workspace owners/admins can manage custom skills and built-in enabled state from the Agents route, but issue-session creation still sends only `skillSlugs`; the server resolves enabled built-in or workspace custom skills to pinned bundles before queueing the task. Session responses and workspace-user runtime task APIs expose only compact skill references so UI clients can show the workflow context without receiving every bundled `SKILL.md` body; full files are returned only to owner/admin skill management detail calls and to runtime workers claiming or inspecting their assigned task. The first product use is `issue_analysis`: after a project-backed issue is created, or after a project is attached to a projectless top-level issue, the server queues a higher-priority read-only `agent_session` with `sandbox:"read-only"`, `sourceCapture:false`, and the built-in `think` skill when an active Codex worker is available.

The first Worker-side implementation lives in `../worker`. It uses only the Server HTTP contract: register with `Authorization: Bearer msw_...` plus an opaque persisted `msws_...` storage id, send heartbeat updates, claim matching tasks, inspect its claimed task for cancellation, append logs, and report status. It completes `protocol_smoke` and `noop` tasks, keeps system Workflows such as `issue_type_triage` on Codex, and runs user `agent_session` tasks through Codex, Claude Code, or Pi adapters. Shared Worker Core prepares the repository cache and either a reusable `workdirs/<project>/<issue>` source worktree or a detached `workdirs/<project>/<session>` automation workdir, materializes server-owned Skill bundles, captures source and artifacts, and assembles the common result; each adapter owns its CLI protocol and terminal evidence. For personal local projects whose configured path is inside a Git repository, the Worker resolves the Git root for clone/cache preparation and runs the selected engine from the configured project subdirectory inside the prepared worktree. Docker-backed Workers store target project source under `/var/lib/mspace-worker/repos` and `/var/lib/mspace-worker/workdirs` on the configured Worker volume, not in the host repository checkout. Issue Detail routes Agent turns directly to `POST /api/workspaces/{workspaceID}/issues/{issueID}/sessions`; returned Worker source Commit metadata, changed files, and diff preview are exposed from the runtime task result. Dry-run Worker commits are diagnostic records and should not be used as PR source candidates.

Migration 032 adds `issue_working_copies`, `runtime_workers.storage_id`, `runtime_tasks.storage_affinity_id`, and `runtime_tasks.cancel_requested_at`. Human source Sessions use `executionMode:"issue_working_copy"`; they serialize through `active_session_id`, keep one stable branch, and reconcile terminal state only when Session id and generation match. All server automations and explicit source-Commit Sessions use `executionMode:"detached"` and cannot mutate the Issue working copy. Existing test Issues without a row initialize a fresh V1 working copy from the project default ref on their next human source Session; legacy Session branches are not reconciled.
