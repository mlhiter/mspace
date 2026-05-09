# mspace Local API Integration Guide

> Status: local MVP API guide, updated 2026-05-09

This guide is for local tools or future desktop integrations that need to call the mspace runner directly. The API is local-first and currently served by the Go runner, normally on `http://127.0.0.1:7788`.

The API is not a public cloud contract yet. It is a stable enough local MVP contract for the desktop renderer, smoke checks, and small integration scripts.

## Base URL

```bash
export MSPACE_API_BASE="http://127.0.0.1:7788"
curl "$MSPACE_API_BASE/health"
```

The Electron preload exposes the same base URL to the renderer through `window.mspaceDesktop.apiBaseUrl`.

Agent sessions also receive `MSPACE_API_BASE_URL` so they can update issue task state from the prepared worktree when needed.

## Issue Task APIs

Task lists are stored as child issues. Markdown checklist lines submitted in `POST /api/issues` are converted into child issue tasks and removed from the parent body.

| Method | Path | Purpose |
| --- | --- | --- |
| `PUT` | `/api/issues/{issueID}` | Update an issue or task title, body, or status. |
| `POST` | `/api/issues/{issueID}/tasks` | Create a child issue task under a parent issue. |

Create a task:

```bash
curl -X POST "$MSPACE_API_BASE/api/issues/<issue-id>/tasks" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Add regression coverage"}'
```

Mark a task complete:

```bash
curl -X PUT "$MSPACE_API_BASE/api/issues/<task-id>" \
  -H 'Content-Type: application/json' \
  -d '{"status":"completed"}'
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
  -d '{"clusterId":"<cluster-id>","exposureMode":"nodeport","nodeHost":"test-node.example.com"}'
```

Queue an Ingress deploy/test turn:

```bash
curl -X POST "$MSPACE_API_BASE/api/issues/<issue-id>/test-deploy" \
  -H 'Content-Type: application/json' \
  -d '{"clusterId":"<cluster-id>","exposureMode":"ingress","previewDomain":"preview.example.com","ingressClass":"nginx"}'
```

The deploy/test session receives the selected kubeconfig, context, issue namespace, registry prefix, exposure mode, and preview routing values through environment variables. If the agent writes `$MSPACE_SESSION_ARTIFACT_DIR/test-environment.json` with `previewUrl`, the runner copies that URL back to the issue test environment.

## Error Notes

- `405 Method Not Allowed` from `GET /api/clusters/discover-defaults` usually means the desktop is connected to an older runner already listening on the configured port. Restart the runner so the current route table is loaded.
- `kubectl is not available on PATH` means discovery or import cannot inspect kubeconfig contexts.
- An `unreachable` imported cluster means kubeconfig parsing worked, but the read-only cluster `/version` check failed. The cluster remains editable.
