# mspace Local API Integration Guide

> Status: local MVP API guide, updated 2026-05-10

This guide is for local tools or future desktop integrations that need to call the mspace runner directly. The API is local-first and currently served by the Go runner, normally on `http://127.0.0.1:7788`.

The API is not a public cloud contract yet. It is a stable enough local MVP contract for the desktop renderer, smoke checks, and small integration scripts.

## Base URL

```bash
export MSPACE_API_BASE="http://127.0.0.1:7788"
curl "$MSPACE_API_BASE/health"
```

The Electron preload exposes the same base URL to the renderer through `window.mspaceDesktop.apiBaseUrl`.

Agent sessions also receive `MSPACE_API_BASE_URL` so they can update issue task state from the prepared worktree when needed.

Normal source-code agent sessions are captured as issue change nodes when they finish with git changes. The runner commits the session worktree changes, excludes `.mspace` artifacts, and exposes the commit metadata and diff preview from `GET /api/issues/{issueID}` as `changeNodes`.

Review evidence is exposed from the same issue detail response as `reviewEvidence`. This is not a diff surface: code changes stay in `changeNodes` and the Commits tab. `reviewEvidence` is the durable session snapshot for commands run, tests, build result, deployment result, preview URL, Kubernetes namespace state, agent summary, risks/follow-ups, and cleanup/retain state. Agents can improve the snapshot by writing `${MSPACE_SESSION_ARTIFACT_DIR}/review-evidence.json` with `commandsRun`, `tests`, `buildResult`, `deploymentResult`, `agentSummary`, `risks`, and `followUps`; the runner falls back to session logs, test-environment state, and Kubernetes evidence when the artifact is missing.

## Issue Writing APIs

The desktop uses a rich TipTap editor for issue creation and human comments, but the runner API stores Markdown text. Issue write APIs require a bearer token:

- human requests use the mspace session token from GitHub sign-in, verified by the control plane through `GET /api/auth/me`;
- agent requests use the scoped `MSPACE_AGENT_TOKEN` injected into the session environment.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/issues` | Create an issue from `title`, `body` or `prompt`, optional `projectId`, optional labels, and optional child task drafts. |
| `POST` | `/api/issues/{issueID}/comments` | Add a Markdown human comment. |

When `projectId` is omitted, the runner infers the best matching existing project from the title, body, and task text. If no project exists, issue creation returns `400 Bad Request`.

The desktop API client attaches `Authorization: Bearer <msp-token>` and injects the current display identity from `localStorage["mspace.authIdentity"]` into local runner writes. External local integrations may pass the same display snapshots directly:

- `creatorName` and `creatorAvatarUrl` on `POST /api/issues`;
- `authorName` and `authorAvatarUrl` on `POST /api/issues/{issueID}/comments`.

These fields are for local UI rendering only. They are not authentication, authorization, or the durable account model; authoritative users and workspaces belong to the server control plane.

## Inbox APIs

The local runner still exposes a fallback unread Inbox, while signed-in team Inbox state belongs to the server control plane. External local tools should treat runner unread as local-only and use the control-plane APIs for team read state.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/control-plane/session` | Give the runner the current server base URL, token, and workspace id so it can report reviewable issue events. |
| `GET` | `/api/inbox` | List local fallback unread inbox items. |
| `POST` | `/api/inbox/issues/{issueID}/read` | Mark one local fallback inbox item read. |
| `GET` | `/api/inbox/stream` | Stream local fallback inbox invalidation events. |

Configure the runner to report reviewable events to the control plane:

```bash
curl -X POST "$MSPACE_API_BASE/api/control-plane/session" \
  -H 'Content-Type: application/json' \
  -d '{"serverBaseUrl":"http://127.0.0.1:8787","token":"<msp-token>","workspaceId":"<workspace-id>"}'
```

The team Inbox endpoints live on the server base URL and require `Authorization: Bearer <msp-token>`:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/inbox"

curl -X POST \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/issues/<issue-id>/read-through" \
  -d '{"throughEventId":"<event-id>"}'
```

Create an issue with a creator display snapshot:

```bash
curl -X POST "$MSPACE_API_BASE/api/issues" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"body":"Investigate login avatar rendering","creatorName":"mlhiter","creatorAvatarUrl":"https://avatars.githubusercontent.com/u/<github-id>?v=4"}'
```

Add a comment with an author display snapshot:

```bash
curl -X POST "$MSPACE_API_BASE/api/issues/<issue-id>/comments" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"body":"Avatar fallback is fixed locally.","authorName":"mlhiter","authorAvatarUrl":"https://avatars.githubusercontent.com/u/<github-id>?v=4"}'
```

## Issue Task APIs

Task lists are stored as child issues. Markdown checklist lines submitted in `POST /api/issues` are converted into child issue tasks and removed from the parent body.

Issue status values are explicit workflow states: `open`, `in_progress`, `needs_review`, `changes_requested`, `ready_for_test`, `test_in_progress`, `test_passed`, `test_failed`, `blocked`, `failed`, `cancelled`, and `closed`. Legacy inputs such as `completed`, `review`, and `testing` are normalized for compatibility. Only human requests may set a top-level issue to `closed`; scoped agent tokens may set review/test/progress/failure states and may close child tasks under their assigned issue.

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

Issue test environments are manually triggered. They are not created automatically when a normal local agent session finishes.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/issues/{issueID}/test-deploy` | Queue a deploy/test agent turn for the issue namespace. |
| `POST` | `/api/issues/{issueID}/test-environment/retain` | Record that the namespace should be retained. |
| `POST` | `/api/issues/{issueID}/test-environment/cleanup` | Queue a cleanup agent turn for the issue namespace. |

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

The deploy/test session receives the selected source commit, source session, kubeconfig, context, issue namespace, registry prefix, exposure mode, and preview routing values through environment variables. The runner prepares the deploy worktree at the selected commit so the agent deploys the selected change node instead of implicitly using the latest session. If the agent writes `$MSPACE_SESSION_ARTIFACT_DIR/test-environment.json` with `previewUrl`, the runner copies that URL back to the issue test environment.

## Error Notes

- `405 Method Not Allowed` from `GET /api/clusters/discover-defaults` usually means the desktop is connected to an older runner already listening on the configured port. Restart the runner so the current route table is loaded.
- `kubectl is not available on PATH` means discovery or import cannot inspect kubeconfig contexts.
- An `unreachable` imported cluster means kubeconfig parsing worked, but the read-only cluster `/version` check failed. The cluster remains editable.
