# Kubernetes Deployment Runbook

> Status: first customer deployment path for a Kubernetes-hosted fixed Server Worker.

This runbook deploys mspace into a customer Kubernetes cluster. It intentionally uses the current server/worker protocol instead of introducing a per-session Kubernetes Runtime Provider.

## Deployment Shape

```text
desktop client
  -> https://mspace.example.com
  -> mspace-server Deployment
  -> Postgres

self-hosted mspace worker
  -> claims runtime_tasks from server
  -> stores repo cache and session workdirs on its worker volume
  -> runs codex app-server
  -> uses buildctl and kubectl for issue test deployments

BuildKit StatefulSet
  -> builds linux/amd64 images for deploy/test turns
```

## Prerequisites

- Kubernetes cluster with a default StorageClass or explicit storage classes in values.
- Ingress controller if exposing the server through Ingress.
- Registry push access for mspace images and project test images.
- Optional GitHub OAuth App with callback URL `https://<host>/api/auth/github/callback`. Restricted or offline deployments can use local username/password auth without GitHub.
- At least one server admin login for creating team workspaces. Ordinary registered users only receive a personal workspace until an admin invites them to a team workspace.
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
  --from-file=auth.json=/path/to/worker-codex-home/auth.json \
  --from-file=config.toml=deploy/codex/worker-config.toml
```

`config.toml` is required for API-based Codex auth/configuration. Use the repository-owned `deploy/codex/worker-config.toml` for Kubernetes workers instead of copying a full laptop `~/.codex/config.toml`; local project paths, MCP servers, desktop plugins, notify hooks, and shell environment settings should stay out of the cluster. Do not deploy the worker with only `auth.json`; the chart fails rendering when `worker.enabled=true` without a configured Codex home Secret and both key names.

If the worker must use a private model provider, copy `deploy/codex/worker-config.toml` to a local untracked file, append the private `[model_providers.*]` block and matching `model_provider`, then create the Secret from that local file. Do not commit private provider URLs or credentials.

Customer kubeconfig:

```bash
kubectl -n mspace-system create secret generic mspace-customer-kubeconfig \
  --from-file=config=/path/to/customer.kubeconfig
```

If the image registry is private, create an image pull secret and reference it from `imagePullSecrets`.

## Install Server And Fixed Worker

Use the example values as a starting point:

```bash
cp deploy/helm/mspace/examples/customer-values.yaml /tmp/mspace-values.yaml
```

Set:

- optional `secrets.githubClientId`
- optional `secrets.githubClientSecret`
- optional `secrets.githubRedirectUri`
- `secrets.serverAdminLogins` as a comma-separated list of local password logins or GitHub logins that may create team workspaces
- `secrets.bootstrapAdminLogin` and `secrets.bootstrapAdminPassword` for the default local admin account
- `bootstrap.teamWorkspace.enabled=true` and `bootstrap.teamWorkspace.name` for the default customer team workspace owned by the bootstrap admin
- `server.image.tag`
- `worker.image.tag`
- `server.ingress.hosts[0].host`
- `codexHome.existingSecret=mspace-codex-home`
- optional storage classes

With `bootstrap.teamWorkspace.enabled=true`, Helm creates one `msw_...` runtime token in the release Secret, the server registers that token against the default team workspace during bootstrap, and the fixed worker StatefulSet uses the same token to register back to the server. This keeps the server Codex-free: Helm only passes mspace runtime registration data to the server, while Codex auth/config stays in the worker-mounted `mspace-codex-home` Secret.

Keep `buildkit.enabled=false` for the first worker registration unless the customer cluster accepts rootless BuildKit's `Unconfined` seccomp profile.

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

Sign in with the bootstrap local admin account. The customer team workspace already exists and is owned by that admin. Ordinary self-registered accounts can use only their personal workspace and local personal runner until invited into this team workspace.

For a self-hosted worker on a customer server, VM, DevBox, or Docker-capable host, open Workspace Settings, choose `Install worker`, copy the generated install command, and run it on that worker host. The command embeds a short-lived workspace bootstrap credential once and starts the Docker-backed Codex worker. The raw `msw_...` token API remains available for recovery/debugging, but it is not the normal customer setup path.

For custom/recovery Helm-managed fixed Worker StatefulSet installs, you may still create a runtime registration token through the API or an internal admin/debug flow and put it in a Kubernetes Secret instead of using `bootstrap.teamWorkspace.enabled`:

```bash
kubectl -n mspace-system create secret generic mspace-runtime-token \
  --from-literal=MSPACE_RUNTIME_TOKEN='msw_...'
```

## Custom Runtime Token Path

If you use the custom `mspace-runtime-token` Secret path above, set these values:

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

## Optional BuildKit

Keep BuildKit off unless the customer cluster accepts rootless BuildKit's `Unconfined` seccomp profile:

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

Upgrade if you changed values after the first install:

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

The worker should appear online in Workspace Settings. The same page also shows active worker credentials separately from expired or replaced credential history.

## Protocol Smoke

Workspace Settings lets operators inspect issue-linked runtime tasks, expand events/logs/payloads, and cancel queued/running tasks, but the normal UI does not expose a generic queue-task form. For API-level smoke:

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

## Configure Kubernetes Environments

In mspace Environments, create or import a Kubernetes environment that points at the in-cluster mounted kubeconfig path:

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
5. Trigger test deployment with the mounted Kubernetes environment.
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
- The default Helm path bootstraps exactly one admin-owned team workspace and fixed worker token for the release. Additional workspaces still need their own external worker install command or explicit runtime token path.
- The mounted kubeconfig is the current deployment credential boundary.
- Codex credentials/config are mounted only into the worker. The server image and server Deployment are Codex-free.
- Server-owned GitHub App PR execution is not part of this deployment package.
- The worker must not report container-local `localhost` URLs as user-facing previews.
