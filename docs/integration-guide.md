# mspace Local API Integration Guide

> Status: local MVP API guide, updated 2026-05-13

This guide covers the local runner API and the current server collaboration API used by the desktop. The server API is the product-data path and is served by the control plane, normally on `http://127.0.0.1:8787`; the runner API is the local runtime path and is served by the Go runner, normally on `http://127.0.0.1:7788`.

The API is not a public cloud contract yet. It is a stable enough local MVP contract for the desktop renderer, smoke checks, and small integration scripts.

## Base URL

```bash
export MSPACE_API_BASE="http://127.0.0.1:7788"
curl "$MSPACE_API_BASE/health"
```

The Electron preload exposes the same base URL to the renderer through `window.mspaceDesktop.apiBaseUrl`.

The server control plane normally runs on `http://127.0.0.1:8787` and owns GitHub auth, personal/team workspaces, membership, workspace projects, project runbooks, issues, child issue tasks, comments, reactions, labels, Inbox receipts, invitations, worker registration, and runtime task queues:

```bash
export MSPACE_SERVER_BASE="http://127.0.0.1:8787"
curl "$MSPACE_SERVER_BASE/health"
```

Agent sessions also receive `MSPACE_API_BASE_URL` so they can update issue task state from the prepared worktree when needed.

Normal source-code agent sessions are captured as issue change nodes when they finish with git changes. If the session worktree is already ahead of the project base branch, the runner records the current HEAD commit as the change node instead of creating a duplicate commit. Otherwise it commits the session worktree changes, excludes `.mspace` artifacts, and exposes the commit metadata and diff preview from `GET /api/issues/{issueID}` as `changeNodes`.

Review evidence is exposed from the same issue detail response as `reviewEvidence`. This is not a diff surface: code changes stay in `changeNodes` and the Commits tab. `reviewEvidence` is the durable session snapshot for commands run, tests, build result, deployment result, preview URL, Kubernetes namespace state, agent summary, risks/follow-ups, and cleanup/retain state. The runner persists compact evidence commands in `session_review_evidence.commands_json`; exploratory command output such as file reads and searches stays in `session_logs` for raw debugging.

Branch and pull request handoff is exposed as `handoffs` on Issue Detail. PR handoff is issue-level: one issue should have one current PR, and commit rows are the review/source evidence that feed that PR. A handoff records branch, source session/commit, head commit, commit list, preview URL, evidence summary, PR URL/number/state/title, and any local preflight error. MVP PR creation runs through the local runtime's `git`, `gitleaks`, and `gh` identity; if a PR already exists for the source branch, `gh pr view <branch>` auto-detects it instead of asking the user to paste a URL. Future hosted/team mode should move GitHub automation to a GitHub App installation token owned by the control plane.

Failed work is exposed as `failures` on Issue Detail and Session Detail. Each failure belongs to one session and stores a phase, status, failed command, compact error summary/excerpt, cluster/namespace/resource hints, plus links to deployment and review evidence when available. The UI should treat these rows as continueable collaboration state: users can add a follow-up agent instruction, retry deployment from the same selected source commit, stop an active session, or decide to retain/clean up the namespace.

Agents can improve the snapshot by writing `${MSPACE_SESSION_ARTIFACT_DIR}/review-evidence.json`. Supported fields are `commandsRun`, `tests`, `buildResult`, `deploymentResult`, `agentSummary`, `risks`, and `followUps`. `commandsRun` may be an array of command objects or strings; `tests` may be an array or a map; result fields may be objects or short strings. When the artifact is missing, the runner derives evidence from session logs, test-environment state, and Kubernetes evidence, then keeps only evidence-worthy commands such as test/build/deploy commands, dependency install commands, `git diff --check`, `git commit`, Playwright checks, and issue-status update calls. If an earlier test/build/deploy attempt failed but a later attempt passed, the latest result is treated as authoritative and the failed attempt remains available in `session_logs`.

Minimal review evidence artifact:

```json
{
  "agentSummary": "Implemented the issue and moved it to ready_for_test.",
  "commandsRun": ["pnpm typecheck", "pnpm --filter @mspace/desktop build"],
  "tests": {
    "pnpm typecheck": "passed",
    "pnpm --filter @mspace/desktop build": "passed"
  },
  "buildResult": "passed: desktop build completed",
  "deploymentResult": {
    "status": "not_reported",
    "summary": "Deployment result was not reported."
  },
  "risks": [],
  "followUps": []
}
```

## Server Workspace Collaboration APIs

When a user is signed into a selected workspace, the desktop reads and writes Projects, Issues, Issue Detail comments/tasks/labels, Inbox receipts, and Project runbooks through the server base URL. Personal workspaces still bind to the user's local runner and machine environment for execution, but their product data lives in server Postgres.

The current transition boundary is explicit:

- server Postgres is truth for signed-in workspace projects, runbooks, issues, child tasks, comments, reactions, labels, and Inbox receipts;
- local runner SQLite is still truth for local execution state, worktrees, sessions, logs, evidence, handoffs, clusters, issue test environments, image attachments, and runtime metadata;
- server-owned issues currently guard agent-session starts and attachment uploads until the runtime bridge can operate directly on PG-backed issue ids.

Server workspace endpoints require `Authorization: Bearer <msp-token>`:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/workspaces/{workspaceID}/projects` | List projects in the selected workspace. |
| `POST` | `/api/workspaces/{workspaceID}/projects` | Create a project in the selected workspace. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}` | Update project settings. |
| `DELETE` | `/api/workspaces/{workspaceID}/projects/{projectID}` | Delete a project when no issues reference it. |
| `GET` | `/api/workspaces/{workspaceID}/projects/{projectID}/runbook` | Read the workspace project runbook. |
| `PUT` | `/api/workspaces/{workspaceID}/projects/{projectID}/runbook` | Replace the workspace project runbook and record a revision. |
| `GET` | `/api/workspaces/{workspaceID}/issue-label-definitions` | List Type and Priority label options. |
| `GET` | `/api/workspaces/{workspaceID}/issues` | List top-level issues in the selected workspace. |
| `POST` | `/api/workspaces/{workspaceID}/issues` | Create an issue from `title`, `body` or `prompt`, optional `projectId`, optional labels, and optional child task drafts. |
| `GET` | `/api/workspaces/{workspaceID}/issues/{issueID}` | Load issue detail with project, child issues, labels, comments, and reaction summaries. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}` | Update issue title, body, or workflow status. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/tasks` | Create a child issue task. |
| `DELETE` | `/api/workspaces/{workspaceID}/issues/{issueID}/tasks/{taskID}` | Delete a child issue task under the parent. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/labels` | Replace an issue's selected label keys. |
| `POST` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments` | Add a Markdown human comment. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}` | Edit the current user's human comment. |
| `PUT` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}` | Add the current user's reaction to a comment. |
| `DELETE` | `/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}` | Remove the current user's reaction from a comment. |

Create a server-backed project and issue:

```bash
project_json="$(curl -sS -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/projects" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"mspace","sourceType":"local","repoPath":"/Users/mlhiter/personal-projects/mspace","defaultBranch":"main"}')"
project_id="$(printf '%s' "$project_json" | jq -r '.id')"

curl -X POST "$MSPACE_SERVER_BASE/api/workspaces/<workspace-id>/issues" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d "{\"projectId\":\"$project_id\",\"body\":\"Move workspace issue data to server PG\\n\\n- [ ] Keep runner workdirs local\",\"labelKeys\":[\"type:feat\",\"priority:p1\"]}"
```

When `projectId` is omitted, the server infers the best matching existing project from the title, body, and task text. If no project exists, issue creation returns `400 Bad Request`.

## Legacy Runner Issue Writing APIs

These endpoints remain for the current runtime bridge, local attachments, and older test data. The desktop product surfaces should use the server workspace endpoints above for both personal and team workspaces.

The desktop uses a rich TipTap editor for issue creation, human comments, project runbook editing, and read-only Issue Detail runbook viewing, but the runner API stores Markdown text. Image uploads are stored as attachment records and inserted into Markdown as stable `/api/attachments/<id>` image URLs, so future storage backends can change without rewriting issue bodies. Issue write APIs require a bearer token:

- human requests use the mspace session token from GitHub sign-in, verified by the control plane through `GET /api/auth/me`;
- agent requests use the scoped `MSPACE_AGENT_TOKEN` injected into the session environment.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/attachments` | Upload one image as multipart form field `file`; returns a stable attachment URL. |
| `GET` | `/api/attachments/{attachmentID}` | Read an uploaded image attachment by id. |
| `GET` | `/api/projects/{projectID}/runbook` | Read the current Markdown project runbook and metadata. |
| `PUT` | `/api/projects/{projectID}/runbook` | Replace the current Markdown project runbook from the Projects UI. |
| `POST` | `/api/issues` | Create an issue from `title`, `body` or `prompt`, optional `projectId`, optional labels, and optional child task drafts. |
| `POST` | `/api/issues/{issueID}/comments` | Add a Markdown human comment. |
| `PUT` | `/api/issues/{issueID}/comments/{commentID}` | Edit the latest human comment before it has triggered an agent session. |
| `PUT` | `/api/issues/{issueID}/comments/{commentID}/reactions/{reaction}` | Add the current human user's reaction to a comment. |
| `DELETE` | `/api/issues/{issueID}/comments/{commentID}/reactions/{reaction}` | Remove the current human user's reaction from a comment. |

When `projectId` is omitted, the runner infers the best matching existing project from the title, body, and task text. If no project exists, issue creation returns `400 Bad Request`.

Comment reactions are stored separately from comment Markdown so they do not change agent prompt history or comment edit eligibility. Supported reaction keys are `thumbs_up`, `thumbs_down`, `laugh`, `hooray`, `confused`, `heart`, `rocket`, and `eyes`; `GET /api/issues/{issueID}` returns per-comment reaction counts plus `reactedByMe` for the authenticated viewer.

The desktop API client attaches `Authorization: Bearer <msp-token>` and injects the current display identity from `localStorage["mspace.authIdentity"]` into local runner writes. External local integrations may pass the same display snapshots directly:

- `creatorName` and `creatorAvatarUrl` on `POST /api/issues`;
- `authorName` and `authorAvatarUrl` on `POST /api/issues/{issueID}/comments`.

These fields are for local UI rendering only. They are not authentication, authorization, or the durable account model; authoritative users and workspaces belong to the server control plane. The runner stores the verified control-plane user id on new human comments as `author_user_id` and uses it to authorize comment edits.

Agent-triggering sessions store the comment id that created the turn. An eligible unconsumed comment edit may add a supported agent mention, then call `POST /api/issues/{issueID}/assign-agent` with that same comment id as `triggerCommentId`. Once a comment has been used as `trigger_comment_id`, it cannot be edited; stop that session and add a corrected comment instead.

Image upload constraints:

- accepted content types are PNG, JPEG, GIF, and WebP;
- maximum image size is 10 MB;
- upload responses include `id`, `url`, `filename`, `contentType`, `sizeBytes`, and `storageBackend`;
- pass referenced attachment ids as `attachmentIds` on `POST /api/issues` or `POST /api/issues/{issueID}/comments` so the runner binds them to the issue/comment in the same transaction.

Project runbooks are stored by mspace, not committed into the target repository by default. Codex sessions receive the current runbook as advisory context and may update it by writing Markdown to `${MSPACE_SESSION_ARTIFACT_DIR}/project-runbook.md`; the runner imports that artifact after a successful session and records a revision.

Upload and bind an image to a comment:

```bash
attachment_json="$(curl -sS -X POST "$MSPACE_API_BASE/api/attachments" \
  -H "Authorization: Bearer <msp-token>" \
  -F "file=@/path/to/screenshot.png")"
attachment_id="$(printf '%s' "$attachment_json" | jq -r '.id')"
attachment_url="$(printf '%s' "$attachment_json" | jq -r '.url')"

curl -X POST "$MSPACE_API_BASE/api/issues/<issue-id>/comments" \
  -H "Authorization: Bearer <msp-token>" \
  -H "Content-Type: application/json" \
  -d "{\"body\":\"Screenshot attached.\\n\\n![screenshot]($attachment_url)\",\"attachmentIds\":[\"$attachment_id\"]}"
```

## Inbox APIs

The local runner still exposes legacy unread Inbox endpoints, while signed-in workspace Inbox state belongs to the server control plane. External local tools should treat runner unread as local-only and use the control-plane APIs for product read state.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/control-plane/session` | Give the runner the current server base URL, token, and workspace id so it can report reviewable issue events. |
| `GET` | `/api/inbox` | List legacy local unread inbox items. |
| `POST` | `/api/inbox/issues/{issueID}/read` | Mark one legacy local inbox item read. |
| `GET` | `/api/inbox/stream` | Stream legacy local inbox invalidation events. |

Configure the runner to report reviewable events to the control plane:

```bash
curl -X POST "$MSPACE_API_BASE/api/control-plane/session" \
  -H 'Content-Type: application/json' \
  -d '{"serverBaseUrl":"http://127.0.0.1:8787","token":"<msp-token>","workspaceId":"<workspace-id>"}'
```

Personal workspaces are the default after GitHub sign-in. Invitations, worker registration tokens, registered workers, and runtime tasks require an explicit team workspace:

```bash
curl -X POST "$MSPACE_SERVER_BASE/api/workspaces" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Engineering team","kind":"team"}'
```

The workspace Inbox endpoints live on the server base URL and require `Authorization: Bearer <msp-token>`:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/inbox"

curl -X POST \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/issues/<issue-id>/read-through" \
  -d '{"throughEventId":"<event-id>"}'
```

Create and accept a team workspace invitation:

```bash
invite_json="$(curl -sS -X POST "$MSPACE_SERVER_BASE/api/workspaces/<team-workspace-id>/invitations" \
  -H "Authorization: Bearer <owner-or-admin-msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"email":"teammate@example.com","role":"member","expiresInHours":168}')"
invite_token="$(printf '%s' "$invite_json" | jq -r '.token')"

curl -X POST "$MSPACE_SERVER_BASE/api/workspace-invitations/accept" \
  -H "Authorization: Bearer <teammate-msp-token>" \
  -H 'Content-Type: application/json' \
  -d "{\"token\":\"$invite_token\"}"
```

The raw `msi_...` invite token is returned only when the invitation is created. Listing invitations returns metadata and a token prefix only.

Legacy runner issue creation with a display snapshot:

```bash
curl -X POST "$MSPACE_API_BASE/api/issues" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"body":"Investigate login avatar rendering","creatorName":"mlhiter","creatorAvatarUrl":"https://avatars.githubusercontent.com/u/<github-id>?v=4"}'
```

Legacy runner comment creation with a display snapshot:

```bash
curl -X POST "$MSPACE_API_BASE/api/issues/<issue-id>/comments" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"body":"Avatar fallback is fixed locally.","authorName":"mlhiter","authorAvatarUrl":"https://avatars.githubusercontent.com/u/<github-id>?v=4"}'
```

## Issue Task APIs

Task lists are stored as child issues. Markdown checklist lines submitted in `POST /api/issues` are converted into child issue tasks and removed from the parent body.

Issue status values are explicit handoff states: `open`, `needs_review`, `changes_requested`, `ready_for_test`, `blocked`, `cancelled`, and `closed`. `cancelled` means the issue was closed as not planned; it is not a session-stop result. Legacy inputs such as `queued`, `running`, `in_progress`, `completed`, `review`, `testing`, and `test_in_progress` are normalized into the durable issue states. Existing local test-data outcome statuses such as `test_passed`, `test_failed`, and `failed` are reset to `open` during the close-reason schema upgrade. Human requests may close top-level issues as `closed` or `cancelled`, and may reopen a closed/cancelled issue to `changes_requested`; humans cannot move a post-open issue back to `open` or manually set agent handoff states. Scoped agent tokens may set `needs_review`, `ready_for_test`, or `blocked`, and may close child tasks under their assigned issue. Cancelling a queued or running session cancels that session, records a compact non-editable timeline event, and leaves the issue status unchanged. If a persisted `running` session no longer has an in-memory runner handle after a desktop or runner restart, the cancel API still accepts Stop and marks the orphaned session `cancelled` while moving any linked test environment out of active progress.

Every accepted status transition is appended to the parent issue timeline as a comment authored by the actor that made the change. Human token transitions use the signed-in mspace/GitHub user returned by control-plane `GET /api/auth/me`; agent token transitions use the scoped session actor. The stored comment body includes the raw transition sentence plus an optional reason for compatibility, while the desktop renders status-transition comments as a compact one-line event with readable status badges.

| Method | Path | Purpose |
| --- | --- | --- |
| `PUT` | `/api/issues/{issueID}` | Update an issue or task title, body, or status. |
| `POST` | `/api/issues/{issueID}/tasks` | Create a child issue task under a parent issue. |
| `DELETE` | `/api/issues/{issueID}/tasks/{taskID}` | Delete a child issue task from its parent issue. |

Create a task:

```bash
curl -X POST "$MSPACE_API_BASE/api/issues/<issue-id>/tasks" \
  -H "Authorization: Bearer ${MSPACE_AGENT_TOKEN:-<msp-token>}" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Add regression coverage"}'
```

Mark a task closed:

```bash
curl -X PUT "$MSPACE_API_BASE/api/issues/<task-id>" \
  -H "Authorization: Bearer ${MSPACE_AGENT_TOKEN:-<msp-token>}" \
  -H 'Content-Type: application/json' \
  -d '{"status":"closed"}'
```

Delete a task:

```bash
curl -X DELETE "$MSPACE_API_BASE/api/issues/<issue-id>/tasks/<task-id>" \
  -H "Authorization: Bearer ${MSPACE_AGENT_TOKEN:-<msp-token>}"
```

## Workspace Settings APIs

Workspace settings are local MVP runtime policy. Source commit capture is always on. `autoCreateDraftPr` controls whether the runner tries to create or refresh an issue-level draft PR after a source commit is captured.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/workspace/settings` | Read local workspace automation policy. |
| `PUT` | `/api/workspace/settings` | Update local workspace automation policy. |

```bash
curl -X PUT "$MSPACE_API_BASE/api/workspace/settings" \
  -H 'Content-Type: application/json' \
  -d '{"autoCreateDraftPr":true}'
```

## Issue Label APIs

Issue labels are constrained by the built-in label definitions. The current dimensions are `type` and `priority`. Type uses Conventional Commit names and is normally assigned asynchronously by the internal triage agent after issue creation. Priority is manual and should be set from Issue Detail.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/issue-label-definitions` | List Type and Priority options. |
| `PUT` | `/api/issues/{issueID}/labels` | Replace an issue's selected label keys. |

List available label options:

```bash
curl "$MSPACE_API_BASE/api/issue-label-definitions"
```

Set Type and Priority:

```bash
curl -X PUT "$MSPACE_API_BASE/api/issues/<issue-id>/labels" \
  -H 'Content-Type: application/json' \
  -d '{"labelKeys":["type:fix","priority:p1"]}'
```

Clear Priority while keeping Type:

```bash
curl -X PUT "$MSPACE_API_BASE/api/issues/<issue-id>/labels" \
  -H 'Content-Type: application/json' \
  -d '{"labelKeys":["type:fix"]}'
```

## Cluster APIs

Clusters are reusable test-cluster access records. They store kubeconfig path, optional context, image registry prefix, exposure defaults, and reachability status.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/clusters` | List reusable cluster configs. |
| `POST` | `/api/clusters` | Create a cluster config manually. |
| `GET` | `/api/clusters/discover-defaults` | List selectable kubeconfig files and contexts under `~/.kube` without importing them. |
| `POST` | `/api/clusters/import` | Import explicitly selected kubeconfig file paths. |
| `POST` | `/api/clusters/import-defaults` | Import all discovered default kubeconfig files. |
| `PUT` | `/api/clusters/{clusterID}` | Update cluster settings. |
| `DELETE` | `/api/clusters/{clusterID}` | Delete an unused cluster config. |

Discover default kubeconfigs:

```bash
curl "$MSPACE_API_BASE/api/clusters/discover-defaults"
```

Import selected kubeconfig files:

```bash
curl -X POST "$MSPACE_API_BASE/api/clusters/import" \
  -H 'Content-Type: application/json' \
  -d '{"paths":["/Users/mlhiter/.kube/70","/Users/mlhiter/.kube/80"]}'
```

Import returns `imported` clusters and `skipped` entries. Each kubeconfig context becomes one cluster. The runner marks imported clusters `ready` or `unreachable` after a read-only `/version` API check.

## Issue Test Environment APIs

Issue test deployments, retain decisions, and cleanup turns are manually triggered. Preview status checks are automatic from Issue Detail when a test environment already exists, and can also be called directly for debugging or automation. These checks update the issue test environment state only; they must not append review evidence, deployment evidence, failure records, or top-level issue status/timeline events.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/issues/{issueID}/test-deploy` | Queue a deploy/test agent turn for the issue namespace. |
| `POST` | `/api/issues/{issueID}/test-environment/retain` | Record that the namespace should be retained. |
| `POST` | `/api/issues/{issueID}/test-environment/cleanup` | Queue a cleanup agent turn for the issue namespace. |
| `POST` | `/api/issues/{issueID}/test-environment/probe` | Internal preview status check used by Issue Detail and debugging tools; updates test-environment state only and should not be presented as a primary product action. |
| `GET` | `/api/issues/{issueID}/test-environment/resources` | List live resources from the issue's fixed test namespace. |

Queue a NodePort deploy/test turn:

```bash
curl -X POST "$MSPACE_API_BASE/api/issues/<issue-id>/test-deploy" \
  -H 'Content-Type: application/json' \
  -d '{"clusterId":"<cluster-id>","sourceCommitSha":"<commit-sha>","sourceSessionId":"<session-id>","exposureMode":"nodeport","nodeHost":"test-node.example.com"}'
```

Queue an Ingress deploy/test turn:

```bash
curl -X POST "$MSPACE_API_BASE/api/issues/<issue-id>/test-deploy" \
  -H 'Content-Type: application/json' \
  -d '{"clusterId":"<cluster-id>","sourceCommitSha":"<commit-sha>","sourceSessionId":"<session-id>","exposureMode":"ingress","previewDomain":"preview.example.com","ingressClass":"nginx"}'
```

The deploy/test session receives the selected source commit, source session, kubeconfig, context, issue namespace, registry prefix, exposure mode, and preview routing values through environment variables. The runner prepares the deploy worktree at the selected commit so the agent deploys the selected change node instead of implicitly using the latest session. After deploy, the runner captures Kubernetes evidence, discovers preview candidates, checks the preview URL, and stores the result as `active`, `preview_unverified`, `deploy_failed`, or `deploy_interrupted`. If any completed continuation session writes `$MSPACE_SESSION_ARTIFACT_DIR/test-environment.json` with `previewUrl`, the runner can copy that URL back and adopt the session as the issue's current deploy session.

If the runner process restarts while a deploy or cleanup session is active, the next startup marks the interrupted session `failed` with `agentStatus="interrupted"`. A deploy session linked through `lastDeploySessionId` moves the environment to `namespaceStatus="deploy_interrupted"`; a cleanup session linked through `lastCleanupSessionId` moves it to `namespaceStatus="cleanup_failed"` and `cleanupStatus="cleanup_failed"`.

Fetch the live Resources tab payload:

```bash
curl "$MSPACE_API_BASE/api/issues/<issue-id>/test-environment/resources" \
  -H "Authorization: Bearer <msp-token>"
```

The response includes cluster/context metadata, namespace lifecycle and cleanup state, preview URL, refreshed time, and arrays for `pods`, `services`, `deployments`, `ingresses`, `events`, plus per-section `errors`. The namespace is always derived from `issue_test_environments.namespace`; callers must not pass `?namespace=...`, and the runner rejects namespace overrides with `400 Bad Request`. The first implementation uses Kubernetes `client-go` typed clients and intentionally limits the view to namespaced resources needed for issue debugging. It does not list Secrets, Nodes, or cluster-wide inventory.

## Error Notes

- `404 Not Found` or `405 Method Not Allowed` from newly added runner routes usually means the desktop is connected to an older runner already listening on the configured port. Current desktop startup checks `/health` protocol capabilities and should replace stale local runners automatically; if debugging manually, restart the runner so the current route table is loaded.
- A session that appears `running` in SQLite but Stop returns or previously returned `session is not running` is an orphaned active row from a prior runner process. Current runners reconcile this on startup and the cancel API accepts Stop for the orphaned row; restart the runner if the UI was loaded against older code.
- `kubectl is not available on PATH` means discovery or import cannot inspect kubeconfig contexts.
- An `unreachable` imported cluster means kubeconfig parsing worked, but the read-only cluster `/version` check failed. The cluster remains editable.
