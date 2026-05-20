# Kubernetes Deployment Runbook

> Status: first customer deployment path for a Kubernetes-hosted fixed Server Worker.

This runbook deploys mspace into a customer Kubernetes cluster. It intentionally uses the current server/worker protocol instead of introducing a per-session Kubernetes Runtime Provider.

## Deployment Shape

```text
desktop client
  -> https://mspace.example.com
  -> mspace-server Deployment
  -> Postgres

mspace-worker StatefulSet
  -> claims runtime_tasks from server
  -> stores repo cache and session workdirs on PVC
  -> runs codex app-server
  -> uses buildctl and kubectl for issue test deployments

BuildKit StatefulSet
  -> builds linux/amd64 images for deploy/test turns
```

## Prerequisites

- Kubernetes cluster with a default StorageClass or explicit storage classes in values.
- Ingress controller if exposing the server through Ingress.
- Registry push access for mspace images and project test images.
- GitHub OAuth App with callback URL `https://<host>/api/auth/github/callback`.
- Codex auth JSON and config TOML for the worker. The server control plane does not run Codex.
- A kubeconfig whose permissions are limited to the customer test cluster scope.
- Helm 3.
- Docker Buildx on the release machine.

## Build And Push Images

Production images default to `linux/amd64`:

```bash
export REGISTRY=crpi-7jr40k6elhldekqp.cn-hangzhou.personal.cr.aliyuncs.com/mlhiter
export TAG=k8s-$(date +%Y%m%d%H%M)-$(git rev-parse --short HEAD)
deploy/scripts/build-images.sh
```

The script prints:

```text
SERVER_IMAGE=<registry>/mspace-server:<tag>
WORKER_IMAGE=<registry>/mspace-worker-codex:<tag>
```

Use the same tag in Helm values.

## Prepare Secrets

Create the namespace first if the chart is not creating it:

```bash
kubectl create namespace mspace-system
kubectl label namespace mspace-system \
  pod-security.kubernetes.io/enforce=baseline \
  pod-security.kubernetes.io/audit=restricted \
  pod-security.kubernetes.io/warn=restricted \
  --overwrite
```

Worker Codex home:

```bash
kubectl -n mspace-system create secret generic mspace-codex-home \
  --from-file=auth.json="${CODEX_HOME:-$HOME/.codex}/auth.json" \
  --from-file=config.toml="${CODEX_HOME:-$HOME/.codex}/config.toml"
```

`config.toml` is required for API-based Codex auth/configuration. Do not deploy the worker with only `auth.json`; the chart fails rendering when `worker.enabled=true` without a configured Codex home Secret and both key names.

Customer kubeconfig:

```bash
kubectl -n mspace-system create secret generic mspace-customer-kubeconfig \
  --from-file=config=/path/to/customer.kubeconfig
```

If the image registry is private, create an image pull secret and reference it from `imagePullSecrets`.

## Install Server First

Use the example values as a starting point:

```bash
cp deploy/helm/mspace/examples/customer-values.yaml /tmp/mspace-values.yaml
```

Set:

- `secrets.githubClientId`
- `secrets.githubClientSecret`
- `secrets.githubRedirectUri`
- `server.image.tag`
- `server.ingress.hosts[0].host`
- optional storage classes

Keep `worker.enabled=false` and `buildkit.enabled=false` for the first install because runtime tokens are workspace-scoped and must be created after sign-in. `codexHome.*` may already be set in the values file, but it is only mounted by the worker StatefulSet.

Install:

```bash
helm upgrade --install mspace deploy/helm/mspace \
  -n mspace-system \
  -f /tmp/mspace-values.yaml
```

Verify:

```bash
kubectl -n mspace-system rollout status deployment/mspace-server
kubectl -n mspace-system port-forward svc/mspace 8787:8787
curl http://127.0.0.1:8787/health
```

Open the desktop app with:

```bash
MSPACE_SERVER_URL=https://mspace.example.com pnpm dev:desktop
```

Sign in, create or select the customer workspace, then create a runtime registration token from Workspace Settings. Copy the raw `msw_...` token once.

Create a Secret for that token. Do not put the token directly in Helm values:

```bash
kubectl -n mspace-system create secret generic mspace-runtime-token \
  --from-literal=MSPACE_RUNTIME_TOKEN='msw_...'
```

## Enable Worker

Set these values:

```yaml
secrets:
  runtimeTokenExistingSecret: mspace-runtime-token
  # Optional if the Secret key is not MSPACE_RUNTIME_TOKEN.
  runtimeTokenExistingSecretKey: MSPACE_RUNTIME_TOKEN

codexHome:
  existingSecret: mspace-codex-home
  authKey: auth.json
  configKey: config.toml

worker:
  enabled: true
```

Keep BuildKit off for the first worker registration unless the customer cluster accepts rootless BuildKit's `Unconfined` seccomp profile:

```yaml
buildkit:
  enabled: false
```

If the customer cluster allows that policy tradeoff, enable BuildKit in the same upgrade and relax the mspace namespace enforcement:

```yaml
namespace:
  podSecurity:
    enforce: privileged

buildkit:
  enabled: true
```

Upgrade:

```bash
helm upgrade mspace deploy/helm/mspace \
  -n mspace-system \
  -f /tmp/mspace-values.yaml
```

Verify:

```bash
kubectl -n mspace-system rollout status statefulset/mspace-worker
kubectl -n mspace-system logs statefulset/mspace-worker -f
```

If BuildKit is enabled, also verify it:

```bash
kubectl -n mspace-system rollout status statefulset/mspace-buildkit
```

The worker should appear online in Workspace Settings.

## Protocol Smoke

From Workspace Settings, queue or inspect runtime tasks. For API-level smoke:

```bash
curl -X POST "https://mspace.example.com/api/workspaces/<workspace-id>/runtime-tasks" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"protocol_smoke","runtimeMode":"team","requiredCapabilities":{"protocolSmoke":true},"payload":{"source":"k8s-smoke"}}'
```

Then inspect task events and logs from Workspace Settings or:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "https://mspace.example.com/api/workspaces/<workspace-id>/runtime-tasks/<task-id>/events"

curl -H "Authorization: Bearer <msp-token>" \
  "https://mspace.example.com/api/workspaces/<workspace-id>/runtime-tasks/<task-id>/logs"
```

## Configure Cluster Records

In mspace Clusters, create a cluster record that points at the in-cluster mounted kubeconfig path:

```text
/etc/mspace/kubeconfigs/customer.kubeconfig
```

Set:

- image registry prefix for issue test images;
- exposure mode `nodeport` or `ingress`;
- optional preview domain and ingress class.

## First Customer Flow

1. Create an issue.
2. Attach a project.
3. Mention `@codex` to produce a source change.
4. Review the captured source commit in Commits.
5. Trigger test deployment with the mounted cluster record.
6. Verify the issue namespace, preview URL, Resources tab, Evidence tab, and cleanup/retain behavior.

## Rollback

Before upgrades, back up Postgres:

```bash
kubectl -n mspace-system exec statefulset/mspace-postgres -- \
  pg_dump -U mspace -d mspace > mspace-backup.sql
```

Rollback the chart:

```bash
helm -n mspace-system history mspace
helm -n mspace-system rollback mspace <revision>
```

## Current Limits

- Runtime execution is a fixed Kubernetes-hosted worker, not one Pod per session.
- Worker token creation is two-stage because tokens are workspace-scoped.
- The mounted kubeconfig is the current deployment credential boundary.
- Codex credentials/config are mounted only into the worker. The server image and server Deployment are Codex-free.
- Server-owned GitHub App PR execution is not part of this deployment package.
- The worker must not report container-local `localhost` URLs as user-facing previews.
