# mspace Runbook

> Status: server-owned local MVP operations guide, updated 2026-07-29

## Local Data

The product and runtime state for signed-in workspaces lives in the server store. Team/customer/shared deployments use Postgres through `DATABASE_URL`; packaged personal desktop mode uses the same server APIs with local SQLite.

| Path | Purpose |
| --- | --- |
| `<Electron userData>/mspace.db` | Packaged personal desktop SQLite store used by the bundled local server. |
| `<Electron userData>/server-config.json` | Saved Team server URL for this device. `MSPACE_SERVER_URL` overrides it for the launch. |
| `<Electron userData>/worker/host-identity.json` | Anonymous stable `msh_...` id used to distinguish this Mac's personal Workers. Created with owner-only permissions. |
| `<Electron userData>/worker/tokens/<workspace-id>.token` | Short-lived personal Worker credential file managed by Electron. Agent environments do not inherit it, but same-user filesystem access remains a residual risk. |
| `<worker-root>/.mspace/storage-id` | Owner-only opaque `msws_...` Worker storage identity used for Issue working-copy affinity. It is not a credential and contains no filesystem path. |
| `~/.mspace/codex-worker-home` | Host-side Codex home copy used by the Docker Codex worker. |
| `/var/lib/mspace-worker/repos/<cache-key>` | Repository cache inside Docker-backed workers. |
| `/var/lib/mspace-worker/workdirs/<project-id>/<issue-id>` | Reusable source worktree for one Issue. Human source Sessions serialize on its stable Server-owned branch and the directory stays bound to one Worker storage identity. |
| `/var/lib/mspace-worker/workdirs/<project-id>/<session-id>` | Detached worktree for analysis, deploy, Tests, cleanup, and explicit source-Commit Agent Sessions. Non-Agent tasks such as import mapping use their own temporary execution path. |
| `<worker-root>/artifacts/<project-id>/<issue-id>/<session-id>` | Isolated artifacts for one human source Session, outside the reusable Issue worktree so they cannot dirty source state. |
| `<detached-worktree>/.mspace/session` | Artifact directory for a detached automation Session. |
| `<artifact-dir>/skills/` | Session-scoped server-provided workflow skill bundles. Workers recreate this directory from task payloads and set `MSPACE_SESSION_SKILLS_DIR`. |
| `<artifact-dir>/skills/manifest.json` | Manifest of materialized skill bundle names, revisions, hashes, and files for the current session. |
| `<artifact-dir>/test-environment.json` | Optional deploy/test artifact with preview values. |
| `<artifact-dir>/review-evidence.json` | Optional review artifact for commands, tests, build/deploy result, summary, risks, and follow-ups. |
| `<artifact-dir>/pull-request.json` | Required PR metadata artifact for Codex PR handoff sessions after creating or finding a GitHub pull request. |
| `<artifact-dir>/test-case-proposals.json` | Optional Codex case suggestion artifact reconciled into project Case suggestions. |
| `<artifact-dir>/test-setup-result.json` | Required completion checkpoint for Tests setup automation in plan-based runs that have setup steps. A passing setup stores `setupResult`, copies `outputs` into the run context, and only then starts case execution sessions. A failed, cancelled, or missing setup artifact marks the run `setup_failed` and leaves run items queued. User-facing setup summaries should follow the run's stored `resultLocale`. |
| `<artifact-dir>/test-result.json` | Required completion checkpoint for Tests execution automation, reconciled into test run items and run review state. Prefer `{"runId":"...","items":[...]}`; the worker also accepts a top-level array of result items when each item carries `runId`. If evidence references screenshot files inside the artifact directory, the worker waits briefly for those files to become readable, embeds small image data URLs for transfer, and only then treats the result artifact as complete. The server persists supported screenshots as `test_artifacts` and rewrites evidence to authenticated artifact refs. User-facing `actualResult` and `failureSummary` text should follow the run's stored `resultLocale`. |
| `<artifact-dir>/branch-name.json` | Optional branch proposal only for a detached Agent Session whose server-owned payload enables source capture. Issue working-copy Sessions and detached automation without source capture ignore it. |
| `<artifact-dir>/project-runbook.md` | Optional agent-learned project runbook artifact. |

For Tests setup and execution sessions, `test-setup-result.json` and `test-result.json` are runtime completion checkpoints. If a Codex turn writes the matching artifact but the app-server turn never reports final completion, the worker should still complete the runtime task from the artifact. If the Codex turn completes without the matching artifact, the worker should fail the runtime task with a missing-artifact error so the run cannot appear successful without evidence. If a `test-result.json` references local screenshot paths, the completion checkpoint is not ready until those screenshots can be read and embedded for server persistence. A restarted Worker may reclaim its own stale `running` task only when it also has the same persistent `MSPACE_WORKER_WORK_ROOT` or Docker volume; matching the Worker name without the original storage cannot recover the existing detached Session workdir or its artifacts.

The server store records users, local password credentials, GitHub identities, workspaces, memberships, OAuth state/results, mspace auth sessions, projects, runbooks, Tests state, issues, Issue working copies, comments, reactions, labels, Inbox receipts, workspace Skills and revisions, Environments, issue test environments, handoffs, GitHub App state, Agent Sessions, runtime credentials, Workers, tasks, events, and logs. The fixed Codex/Claude Code/Pi Agent catalog is code-owned and is not stored in Postgres or SQLite. Migration `030_fixed_agent_engines.sql` drops the obsolete `agent_profiles` table, migration `031_runtime_worker_agent_engine_diagnostics.sql` adds the sanitized Worker diagnostic snapshot, and migration `032_issue_working_copies.sql` adds Issue source state, Worker storage identity, task affinity, and cancellation requests. Let the Server run these in its normal migration sequence; do not apply them manually to production as part of local verification.

Docker-backed workers store target project source under the worker root volume, not in the host checkout. The dry-run worker defaults to the `mspace-worker-dev-root` Docker volume, and the Codex-capable worker defaults to `mspace-worker-codex-dev-root`; both mount that volume at `/var/lib/mspace-worker`.

For local Codex/Electron development, `scripts/run-mspace-codex-dev.sh` auto-starts Docker Postgres only for local `DATABASE_URL` hosts. The expected durable database is container `mspace-postgres-dev` with named volume `mspace-postgres-data` and image `postgres:16`.

Check that Docker Postgres is using the expected volume:

```bash
docker inspect mspace-postgres-dev --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}'
docker exec mspace-postgres-dev psql -U mspace -d mspace -Atc "select count(*) from projects; select count(*) from issues;"
```

## Start The App

Install dependencies:

```bash
pnpm install
```

Start desktop:

```bash
pnpm dev:desktop
```

Electron uses `MSPACE_SERVER_URL` first, then a saved Team server URL, then the local bundled/dev server. It starts the local server control plane automatically only for the default personal path if `GET /health` is not already healthy.

Run the server separately for auth or control-plane debugging:

```bash
cp .env.example .env.local
# set DATABASE_URL only when testing Postgres; GitHub OAuth values are optional
pnpm run server
```

For personal desktop workspaces, the app starts and keeps alive one generic host-local Worker as soon as auth and workspace selection are ready. Electron main owns the bundled process, discovers installed Agent executables without running them, persists an anonymous host id, and serializes concurrent ensure requests. Renderer readiness calls the server with the requested engine capability and asks Electron main to idempotently ensure it; team workspaces never auto-start a Worker. The Worker uses the active desktop server URL, a short-lived registration credential, `MSPACE_WORKER_MODE=personal`, and each installed Agent CLI's local configuration. It resolves probe executables once at startup, runs bounded readiness probes with a stripped environment, and heartbeats sanitized results without command output or paths. Set `MSPACE_AUTO_PERSONAL_WORKER=0` to disable this behavior while debugging.

Ordinary personal-workspace startup does not launch Chrome. Browser-backed Tests remain Codex system Workflows: when the server requires `codex`, `browser`, and `chrome_cdp`, Electron prepares CDP and starts a separate `-browser` companion Worker while leaving the generic base Worker alive. The processes use different names, capability snapshots, and execution roots; the companion stores its repo cache/workdirs under `<Electron userData>/worker/browser-companion`. If no CDP endpoint can be reached, browser-required preflight fails and leaves the base Worker untouched.

For team or self-hosted worker runtime hosts, connect a worker from Workspace Settings:

1. Sign in with a local account or configured GitHub OAuth, then select the target workspace.
2. Open Workspace Settings.
3. In Runtime, click `Install worker`.
4. Copy the generated install command.
5. Run it on the server, VM, DevBox, or other Docker-capable host that should execute mspace agent work.

mspace creates a short-lived internal worker bootstrap credential and embeds it in the install command once. The worker host must have Docker and a usable Codex home with `auth.json` and `config.toml`. The command starts the Codex-capable worker container, and Workspace Settings refreshes the Workers list after registration.

Run a worker manually only when debugging or recovering an external worker or terminal-only setup:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
pnpm worker
```

The Worker registers with the Server, sends heartbeat state and its opaque storage id, claims matching runtime tasks, completes `protocol_smoke` / `noop` tasks, and keeps system Workflows such as `issue_type_triage` on Codex. User `agent_session` tasks select the exact `codex`, `claudeCode`, or `pi` capability. Shared Worker Core prepares the repository cache plus either the reusable Issue source worktree or a detached automation workdir under `MSPACE_WORKER_WORK_ROOT`, materializes server-owned Skills, handles artifacts and source capture, and assembles the common result; the Codex, Claude Code, and Pi adapters own their CLI protocols, cancellation, terminal evidence, and opaque refs. If an `agent_session` payload carries `requiredSkills`, the Worker verifies file paths and hashes, writes the Skill bundle under `<artifact-dir>/skills/`, and injects `MSPACE_SESSION_SKILLS_DIR` plus `MSPACE_SESSION_SKILL_MANIFEST` without changing global engine configuration. Agent subprocesses do not inherit Worker registration or control-plane credentials. Workspace Settings shows these tasks as Issue-linked operational rows and keeps protocol payloads, results, events, and logs in expandable details.

For a source Session, verify the Working Copy state before retrying: `dirty` means the owning Worker preserved uncommitted changes and a follow-up Session will continue there; `recovery_required` means the Server will not queue another writer until the underlying worktree/branch/head problem is repaired. `working_copy_storage_unavailable` means no online Worker currently owns the bound `msws_...` storage. Never bypass these states by changing task affinity or recreating the branch on another Worker. A queued cancellation clears the writer reservation immediately; a claimed/running cancellation stays requested until the owning Worker stops the Agent and reports the final working-copy envelope.

Existing pre-V1 test Issues have no working-copy row. Their next human source Session starts a fresh V1 branch from the project default ref; old per-Session branches and workdirs remain historical test artifacts and are not reconciled into the new source line.

Codex configuration and authentication belong to worker runtimes. The server control plane queues work and applies runtime results, but it does not install Codex or mount Codex credentials.

Issue creation stores an immediate plain-text draft title and opens the Issue without waiting for final refinement. Current clients mark the draft with `titleSource="plain_text"`; a missing source is the compatibility path for older Markdown-derived drafts. After any project `issue_analysis` is queued, the create request synchronously persists the existing priority-0 `issue_type_triage` task with the exact draft in `expectedTitle`, then returns without waiting for worker execution. Issue list/detail reads do not retry or create triage tasks. An already-active compatibility task with an empty draft is upgraded atomically under the store lock instead of creating a duplicate. Updated workers return `{"title":"...","type":"fix","confidence":...,"reason":"..."}` from the same Codex turn; older workers may return the type-only shape and remain compatible. The server parses the title as Markdown defensively, reduces it to at most 72 plain-text characters, and applies it only if the stored title still equals `expectedTitle`; a human edit, title-less result, or failed triage leaves the draft intact. Type-label reconciliation remains independent, so a manual type selection while the task runs does not suppress a valid title CAS. When debugging a draft that never changes, verify the create response succeeded, the triage task payload contains the exact `expectedTitle`, the worker version/result shape, task status/logs, and the current stored title. The deterministic `/issues/suggest-title` route remains a compatibility fallback and is not the normal renderer write-back path. Project-backed issues can also queue one read-only `agent_session` automation marked `issue_analysis` before triage starts when a matching Codex worker is already online; priority 10 keeps that analysis ahead of priority-0 triage. That task receives the server-owned `think` skill bundle, runs with a read-only sandbox and source capture disabled, and produces first-pass analysis without requiring a manual `@codex` comment. If no project or worker is available, issue creation still succeeds and analysis is skipped.

During a mixed-version server rollout, an older server binary does not participate in the new per-Issue advisory lock and can still enqueue a duplicate triage task. Title compare-and-set and type reconciliation protect Issue data, but they do not prevent the duplicate Codex turn. Avoid overlapping old/new server instances when duplicate automatic turns are unacceptable; a future database-level active-task uniqueness constraint would be required to remove this rollout-only gap.

## Tests Workflow

Use Tests after at least one project exists. Cases and Case suggestions are project-level. Plans and Runs are workspace-level and can include ready cases from multiple projects. Personal projects can use a local folder or GitHub URL. Team projects must use a GitHub URL so the team worker can clone source into its own repo cache.

Recommended smoke order:

1. Open Tests and pick a project.
2. Verify the Cases list shows compact operational columns: title, type, status, priority, executability, latest result, and updated time.
3. Create one case from the modal and verify the case detail page opens.
4. Import Markdown/text cases and confirm the modal previews the parsed count, importable count, skipped rows, missing field counts, and sample cases before the user confirms import.
5. Import CSV or Excel `.xlsx` cases with canonical `title`, `type`, `area`, `priority`, `preconditions`, `steps`, `expected_result`, `environment_requirements`, and `tags` headers. The preview should show source-column mappings before confirmation, rows without `title` should appear in the skipped list, and `type` should use `functional`, `ui`, `api`, or `deployment`.
6. Import a CSV or Excel file with localized or product-specific headers such as `用例ID`, `用例名称`, `所属模块`, `测试类别`, `步骤描述`, and `执行结果`. The first preview should surface unmatched columns; use AI column matching to queue a worker task, review the returned confidence/reasons, then confirm only after the refreshed preview maps business categories to `tags` and historical execution state to `latest_result`.
7. Edit a case and verify revisions show newest first, with non-initial revisions showing changed fields and before/after values rather than only the case title.
8. Exercise Optimize or Generate with no connected worker; the UI should surface the worker/session blocker rather than silently claiming success.
9. Create a formal workspace plan with setup steps, such as confirming the target Environment, updating a Deployment image, SSHing into a VM, or logging into Sealos before opening Object Storage. A plan may include ready cases from multiple projects; starting it should preserve each run item's project id, group execution by project, create one setup Issue/Session when setup is configured, and keep run items queued while the run is `setup_running`.
10. Complete the setup session with `${MSPACE_SESSION_ARTIFACT_DIR}/test-setup-result.json`. `status:"passed"` should move the run to `running`, expose setup result/run context on Run Detail, and then create execution sessions. `status:"failed"`, a failed worker task, cancellation, or a missing artifact should move the run to `setup_failed` without starting case sessions. Run Detail should show the setup `failureSummary` and failed setup step directly in the setup panel, with the raw setup result still available for debugging.
11. Connect a Codex-capable worker, then verify `test-case-proposals.json` becomes Case suggestions and `test-result.json` updates run items. The Cases list should show the latest final run item status, and user-facing failure text should match the run's `resultLocale` / current UI language. Cross-project runs should create separate execution Issues/agent sessions per project batch so one session never spans multiple repositories. Case Detail should expose `Details`, `Run history`, and `Revisions` tabs, with Run history showing all runs for that case. Case Detail / Run Detail should expose structured evidence such as authenticated screenshot thumbnails, assertions, network summaries, DOM snapshots, raw evidence, and setup result/run context when a plan setup ran.
12. Retry failed or blocked run items when the result needs another pass. Optionally record the run as reviewed or needing follow-up; a Codex-completed run is not release acceptance until a later release gate consumes that review state.

The current case library accepts `functional`, `ui`, `api`, and `deployment` as case types. Browser-backed execution batches require an online worker whose capabilities include `codex`, `browser`, and `chrome_cdp`; this includes `ui` cases plus functional cases that explicitly mention browser/CDP/screenshot/frontend URL, Sealos Desktop/platform entry, app icons, access-key flows, or S3 service parameter modals. Otherwise the run or retry is rejected before creating execution Issues. Browser-backed result artifacts should save screenshots under `${MSPACE_SESSION_ARTIFACT_DIR}/screenshots/` and reference them with `evidence.screenshotPaths` in `test-result.json` so the worker/server can persist authenticated screenshot artifacts. Specialized API harnessing, deployment orchestration, and multi-worker scheduling are still later execution work; the first loop keeps execution behind Issues, Agent Sessions, Workers, and Evidence.

Dry-run Docker worker:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
scripts/run-server-worker-dev.sh
```

Codex-capable Docker worker:

```bash
export MSPACE_RUNTIME_TOKEN="msw_..."
scripts/run-server-worker-codex-dev.sh
```

Worker-issued Codex sessions should not start or keep development servers running by default. Prefer non-interactive validation such as lint, tests, typecheck, build, or short internal probes. If a temporary server is needed, stop it before the session finishes and do not present container-local `localhost` or `127.0.0.1` as a user-facing preview.

Cancellation is cooperative. A queued task becomes `cancelled` immediately. Stopping a claimed or running Issue-working-copy Session sets `cancelRequested` and leaves the task claimed/running with its writer reservation intact. The Worker polls that flag, interrupts the selected Agent, inspects the final source state, and then acknowledges terminal `cancelled` with a working-copy envelope; a late `completed` result must not override the cancellation request.

## Workspace Automation

Workspace Settings has two automation switches:

- Source commit capture is always on and records source changes as issue change nodes.
- `autoDeployTestEnvironment` is opt-in and queues a deploy/test session after a successful source session captures a commit.

Automatic `issue_analysis` is not a Workspace Settings toggle in this version. It is a conservative product default for project-backed issues with an active Codex worker, whether the project is present at issue creation or attached later. The session is read-only planning and should not edit files, commit, deploy, or create PRs. The worker does not capture source for this automation, and the server ignores source/test/deploy/review artifacts if they appear in the result.

Automatic test deploy uses the same `agent_session` path as a manual test deploy. It is intentionally conservative: the triggering task must be completed, non-dry-run, and not itself a deploy/test task; it must have a source commit and no source error; the issue must have an attached project; Kubernetes Environment and deploy settings must resolve; no other agent session can be active for the issue; and a matching online Codex worker must exist. If no worker is connected or deploy settings cannot be resolved, the server adds a compact system comment explaining why the deploy was skipped.

## Kubernetes Customer Deployment

The first customer deployment path is a Kubernetes-hosted fixed Server Worker, not a per-session Kubernetes Runtime Provider. Use:

```bash
deploy/scripts/build-images.sh
helm upgrade --install mspace deploy/helm/mspace -n mspace-system -f /tmp/mspace-values.yaml
```

The image script defaults to `linux/amd64`, injects the root version, full checkout HEAD SHA, and one UTC RFC3339 build time into the Server binary and OCI labels, and injects the authoritative version into the Worker binary. It validates and prints `SERVER_DIGEST` and `WORKER_DIGEST` after a push. Prefer those values as `server.image.digest` and `worker.image.digest`; when a digest is empty, the chart uses the configured tag or `Chart.appVersion`. Desktop release packaging uses the same identity for every Server slice and the same version for every Worker slice. The release validate build uses those ldflags too and fails unless the checked-out tag is exactly `v<root package.json version>`, the checkout is clean, and all packaging tools come from that tagged tree. Manual `workflow_dispatch` reruns never overlay current-branch scripts onto an old tag; create a patched point release when the old tree lacks the complete identity contract.

Before a container build, the script requires `BUILD_VERSION` to match the root package version, `BUILD_COMMIT_SHA` to match checkout `HEAD`, and every file that can affect the Server or Worker image to be clean and committed. After a push, verify the registry index has exactly one non-attestation runtime manifest and it is `linux/amd64`, the OCI `revision` equals that commit, the deployed Pod `imageID` equals the pushed digest, and the live `/health.commitSha` equals the same commit. BuildKit provenance attestations may appear as separate `unknown/unknown` manifests and must not be counted as runnable platforms. A generic readiness or preview status is not provenance evidence.

Before local desktop packaging, `apps/desktop/scripts/prepare-release.mjs` applies the same version/SHA checks and rejects dirty desktop, shared-package, Server, or Worker inputs before it deletes or regenerates bundled binaries. A mismatch error means the caller supplied stale identity variables; a dirty-input error means the source must be committed or intentionally moved to a clean release checkout before retrying.

### Verify deployed build provenance

Set the values from the selected source commit, pushed Server image receipt, target release, and live endpoint, then run the checks from an authenticated operator shell with Docker, `jq`, `kubectl`, and `curl` available:

```bash
SOURCE_COMMIT='<full-lowercase-source-commit-sha>'
SERVER_IMAGE='<registry>/<repository>/mspace-server'
SERVER_DIGEST='sha256:<pushed-server-manifest-digest>'
KUBECONFIG='<path-to-kubeconfig>'
KUBE_CONTEXT='<context-name>'
NAMESPACE='<namespace>'
SERVER_SELECTOR='app.kubernetes.io/instance=<release-name>,app.kubernetes.io/component=server'
SERVER_CONTAINER='server'
HEALTH_URL='https://<server-host>/health'
EXPECTED_SERVER_PROTOCOL='<server-protocol-number>'
IMAGE_REF="${SERVER_IMAGE}@${SERVER_DIGEST}"

MANIFEST_PLATFORMS="$(docker buildx imagetools inspect --raw "${IMAGE_REF}" | jq -r \
  '[.manifests[]
    | select((.annotations // {})["vnd.docker.reference.type"] != "attestation-manifest")
    | .platform
    | "\(.os)/\(.architecture)\(if .variant then "/" + .variant else "" end)"]
   | sort | join(",")')"
test "${MANIFEST_PLATFORMS}" = 'linux/amd64'

docker pull --platform linux/amd64 "${IMAGE_REF}" >/dev/null
OCI_REVISION="$(docker image inspect "${IMAGE_REF}" \
  --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}')"
test "${OCI_REVISION}" = "${SOURCE_COMMIT}"

DEPLOYED_DIGESTS="$(kubectl --kubeconfig "${KUBECONFIG}" --context "${KUBE_CONTEXT}" \
  -n "${NAMESPACE}" get pods -l "${SERVER_SELECTOR}" -o json | jq -r \
  --arg container "${SERVER_CONTAINER}" \
  '[.items[].status.containerStatuses[]?
    | select(.name == $container)
    | .imageID
    | sub("^.*@"; "")]
   | unique | sort | join(",")')"
test "${DEPLOYED_DIGESTS}" = "${SERVER_DIGEST}"

HEALTH_BODY="$(mktemp)"
trap 'rm -f "${HEALTH_BODY}"' EXIT
HTTP_STATUS="$(curl --request GET --silent --show-error --output "${HEALTH_BODY}" \
  --write-out '%{http_code}' "${HEALTH_URL}")"
test "${HTTP_STATUS}" = '200'
jq -e --arg commit "${SOURCE_COMMIT}" \
  --argjson protocol "${EXPECTED_SERVER_PROTOCOL}" \
  '.ok == true
   and .service == "mspace-server"
   and .serverProtocol == $protocol
   and .commitSha == $commit' "${HEALTH_BODY}" >/dev/null

HEALTH_COMMIT="$(jq -r '.commitSha' "${HEALTH_BODY}")"
printf 'commit: %s = %s = %s\n' "${SOURCE_COMMIT}" "${OCI_REVISION}" "${HEALTH_COMMIT}"
printf 'digest: %s = %s\n' "${SERVER_DIGEST}" "${DEPLOYED_DIGESTS}"
```

The required receipt is `SOURCE_COMMIT = OCI_REVISION = /health.commitSha`, `SERVER_DIGEST = every deployed Server Pod imageID digest`, exactly one non-attestation runtime manifest whose platform is `linux/amd64`, and `/health` HTTP `200` with `ok=true`, `service=mspace-server`, and the expected `serverProtocol`.

See `docs/kubernetes-deployment.md` for the customer install shape. The default fixed-worker path now sets `bootstrap.teamWorkspace.enabled=true` so Helm installs the server and worker together: the chart creates or preserves one `MSPACE_RUNTIME_TOKEN`, server startup registers it against the admin-owned default team workspace, and the worker registers with that same token. Before installing, create `mspace-codex-home` with a worker-scoped `auth.json` and `deploy/codex/worker-config.toml` or an untracked private-provider variant.

## Website

Production site:

- [mspace-website-blue.vercel.app](https://mspace-website-blue.vercel.app)

Local development:

```bash
pnpm dev:website
pnpm build:website
pnpm preview:website
```

The public changelog is a static website view, not product runtime state. Update `apps/website/src/changelog.ts` whenever a task ships meaningful product, engineering, documentation, or website progress.

The website Download view links to packaged desktop installers on GitHub Releases. Keep those links pointed at a release that actually carries desktop assets; as verified on 2026-07-02, `v0.2.0-rc.1` has macOS, Windows, and Linux packages, while `v0.1.0` does not. Do not present candidate artifacts as stable customer downloads until release signing and notarization are ready.

## Desktop Localization

Desktop localization is centralized in `packages/i18n` and currently supports English (`en`) and Simplified Chinese (`zh-CN`). The selected locale is stored in `localStorage["mspace.locale"]` and mirrored to `<html lang="...">`.

Visible product copy in `apps/desktop`, `packages/ui`, or `packages/views` should use `packages/i18n/src/index.ts`. Leave logs, user-authored Markdown, branch names, commit hashes, Kubernetes object names, storage values, and runtime protocol fields literal unless a separate product decision says otherwise.

## Desktop Team Server Selection

Personal desktop mode starts with the local bundled server, so local users usually do not need to think about a server URL. The default local sign-in opens on account creation and hides GitHub sign-in, even when a local dev server is configured with OAuth variables. The sign-in screen has a collapsed Team server entry for customer or team deployments such as `https://mspace.example.com`; opening it lets the app check `/health`, save the URL in Electron user data, and reuse it on later launches.

`MSPACE_SERVER_URL` remains the launch-time override. If it is set, it takes precedence over the saved UI value and the Team server entry opens in a locked state for that launch.

Use the same override when testing GitHub OAuth against the local server:

```bash
MSPACE_SERVER_URL=http://127.0.0.1:8787 pnpm dev:desktop
```

That explicit source makes the desktop use the configured-team-server sign-in path, so GitHub can appear when `/health` reports `capabilities.githubAuth: true`.

## Workspace Invitations

Team workspace owners and admins create one-time join links from Workspace Settings. The normal link format is:

```text
mspace://invite/<token>?server=<team-server-url>
```

The `server` value is operationally important: when the link is opened from Feishu, WeChat, a browser, or another app, Electron switches to that team server before calling the preview or accept APIs. If the recipient is not signed in, the invite route shows only safe preview data, lets them sign in or create a local account, accepts the invitation after authentication, and opens the invited team workspace directly.

Unauthenticated preview smoke check:

```bash
curl "$MSPACE_SERVER_BASE/api/workspace-invitations/preview?token=msi_..."
```

If preview returns `404`, check these before changing UI code:

- the link's `server` query points at the deployed team server that created the invite;
- the backend image is updated and `/health` reports `workspaceInvitationPreview: true`;
- the token has the expected `msi_...` shape and was created on the same server;
- the desktop did not fall back to the default local personal server before handling the link.

If preview succeeds but returns `accepted`, `revoked`, or `expired`, the route is healthy and the invitation is no longer usable. Create a new join link from Workspace Settings instead.

## Environment Variables

| Variable | Used by | Default | Purpose |
| --- | --- | --- | --- |
| `MSPACE_SERVER_ADDR` | Server and Electron main process | `127.0.0.1:8787` | Address used when the server control plane listens or is started by desktop. |
| `MSPACE_SERVER_URL` | Electron main/preload/renderer | `http://127.0.0.1:8787` | Launch-time server control-plane override. Takes precedence over the saved Team server setting. |
| `MSPACE_SERVER_START_TIMEOUT_MS` | Electron main process | `30000` | How long the desktop waits for the server health check before startup fails. |
| `MSPACE_STORE` | Server | inferred | Storage mode. Uses `postgres` when `DATABASE_URL` is set, otherwise `sqlite` for local personal mode. |
| `MSPACE_SQLITE_PATH` | Server and packaged desktop | app/user config path | SQLite file path for local personal mode. |
| `MSPACE_DATA_DIR` | Server | none | Optional directory used to derive the default SQLite path. |
| `DATABASE_URL` | Server | none | Postgres connection string for control-plane storage. |
| `MSPACE_GITHUB_CLIENT_ID` | Server | none | Optional GitHub OAuth App client id. |
| `MSPACE_GITHUB_CLIENT_SECRET` | Server | none | Optional GitHub OAuth App client secret; keep it server-side only. |
| `MSPACE_GITHUB_REDIRECT_URI` | Server | none | Optional OAuth callback URL, usually `http://127.0.0.1:8787/api/auth/github/callback` locally. |
| `MSPACE_GITHUB_APP_ID` | Server | none | Optional GitHub App id. Enables `capabilities.githubApp` only when the client id and private key are also configured. |
| `MSPACE_GITHUB_APP_CLIENT_ID` | Server | none | Optional GitHub App client id for server-owned repository automation setup. |
| `MSPACE_GITHUB_APP_PRIVATE_KEY` | Server | none | Optional GitHub App private key; keep it server-side only. The current implementation records installation status but does not mint installation tokens yet. |
| `MSPACE_SERVER_ADMIN_LOGINS` | Server | empty | Comma-separated local password logins or GitHub logins allowed to create team workspaces. If empty, no signed-in user can create a team workspace unless listed through bootstrap admin settings. |
| `MSPACE_BOOTSTRAP_ADMIN_LOGIN` | Server | empty | Optional local password login to create on server startup and treat as a server admin. |
| `MSPACE_BOOTSTRAP_ADMIN_PASSWORD` | Server | empty | Required with `MSPACE_BOOTSTRAP_ADMIN_LOGIN`; used only when the bootstrap account does not already exist. |
| `MSPACE_BOOTSTRAP_ADMIN_NAME` | Server | login | Optional display name for the bootstrap admin account. |
| `MSPACE_BOOTSTRAP_ADMIN_EMAIL` | Server | empty | Optional display email stored on the bootstrap identity. It is not used for admin matching. |
| `MSPACE_BOOTSTRAP_TEAM_WORKSPACE_NAME` | Server | empty | Optional team workspace name to create or find during server startup. Used by Helm fixed-worker bootstrap. |
| `MSPACE_BOOTSTRAP_RUNTIME_TOKEN` | Server | empty | Optional `msw_...` token to register against the bootstrap team workspace. Must be set with `MSPACE_BOOTSTRAP_TEAM_WORKSPACE_NAME`. |
| `MSPACE_BOOTSTRAP_RUNTIME_TOKEN_NAME` | Server | `Helm fixed worker` | Metadata name for the bootstrap runtime registration token. |
| `MSPACE_BOOTSTRAP_RUNTIME_TOKEN_TTL_HOURS` | Server | `2160` | Runtime token TTL in hours for the bootstrap fixed worker. The server and chart cap this at 90 days. |
| `MSPACE_RUNTIME_TOKEN` | Worker | none | Internal runtime bootstrap credential with `msw_` prefix. |
| `MSPACE_WORKER_NAME` | Worker | host-derived | Stable worker name shown in Workspace Settings. |
| `MSPACE_WORKER_MODE` | Worker | `team` | Runtime mode reported to the server; `team` or `personal`. |
| `MSPACE_WORKER_IMAGE` | Docker worker install command | `crpi-7jr40k6elhldekqp.cn-hangzhou.personal.cr.aliyuncs.com/mlhiter/mspace-worker-codex:dev` | Docker image used by generated self-host worker install commands. |
| `MSPACE_WORKER_CONTAINER` | Docker worker install command | `${MSPACE_WORKER_NAME}` | Docker container name used by generated self-host worker install commands. |
| `MSPACE_WORKER_CAPABILITIES` | Worker | worker-dependent | JSON object used by server-side capability matching. The plain worker default is `{"protocolSmoke":true,"codex":false,"dryRun":true}`; generated self-host install commands default to `{"protocolSmoke":true,"codex":true,"docker":true,"kubectl":true,"buildkit":false,"dryRun":false}`. |
| `MSPACE_WORKER_LABELS` | Worker | worker-dependent | JSON object for placement or inventory labels. Generated self-host install commands default to `{"provider":"self-host","environment":"docker"}`. |
| `MSPACE_WORKER_POLL_INTERVAL` | Worker | `5s` | How often the worker polls the runtime task queue. |
| `MSPACE_WORKER_HEARTBEAT_INTERVAL` | Worker | `10s` | How often the worker reports liveness and load. |
| `MSPACE_WORKER_WORK_ROOT` | Worker | `/tmp/mspace-worker` or `/var/lib/mspace-worker` in Docker | Root directory for repository caches, the stable Worker storage identity, reusable Issue worktrees, detached automation workdirs, and artifacts. |
| `MSPACE_WORKER_VOLUME` | Docker worker scripts | `mspace-worker-${MSPACE_WORKER_NAME}` in install commands | Docker volume mounted at `/var/lib/mspace-worker`. Dev helper scripts may use their own fixed dev volume names. |
| `MSPACE_WORKER_CODEX_HOME_SOURCE` | Docker Codex worker script | `${CODEX_HOME:-~/.codex}` | Source Codex home copied into a dedicated worker Codex home before container startup. |
| `MSPACE_WORKER_CODEX_HOME_DIR` | Docker Codex worker script | `~/.mspace/codex-worker-home` | Host directory mounted into the Codex-capable dev worker as `CODEX_HOME`. |
| `MSPACE_WORKER_CODEX_CLI_VERSION` | Docker Codex worker script | `0.130.0` | `@openai/codex` npm version installed into `worker/Dockerfile.codex-dev`. |
| `MSPACE_AUTO_PERSONAL_WORKER` | Electron main process | enabled | Set to `0` to prevent the desktop from auto-starting a host-local personal worker before agent mentions. |
| `MSPACE_CHROME_CDP_URL` | Electron main process / Worker session | auto-detected for browser-required personal workers | Optional reachable Chrome DevTools Protocol base URL. Browser-required action preflight uses this URL for the browser companion Worker and advertises UI test capability only if `/json/version` is reachable; if it is not reachable, managed CDP fallback is attempted. |
| `MSPACE_CHROME_EXECUTABLE` | Electron main process | platform default Chrome/Chromium paths | Optional browser executable path used when Electron needs to start a dedicated personal-worker CDP browser. |
| `MSPACE_ELECTRON_EXECUTABLE` | Electron main process | dev Electron binary when available | Optional Electron executable path used as the managed Chromium CDP fallback when local Chrome cannot expose CDP. |

Environment, project, and issue test environment fields are passed into sessions as:

| Variable | Source |
| --- | --- |
| `MSPACE_CLUSTER_ID` | Selected Kubernetes issue test environment cluster id when present. |
| `MSPACE_KUBE_CONTEXT` | Selected issue test environment `kube_context`. |
| `KUBECONFIG` / `MSPACE_KUBECONFIG` | Selected issue test environment `kubeconfig_path`. |
| `MSPACE_KUBE_NAMESPACE` | Issue test namespace. |
| `MSPACE_TEST_NAMESPACE` | Issue test namespace. |
| `MSPACE_IMAGE_REGISTRY_PREFIX` | Selected Kubernetes environment or issue test environment image registry prefix. |
| `MSPACE_EXPOSURE_MODE` | Selected issue test environment exposure mode. |
| `MSPACE_PREVIEW_DOMAIN` | Optional issue preview domain. |
| `MSPACE_INGRESS_CLASS` | Optional issue ingress class. |
| `MSPACE_NODE_HOST` | Optional issue NodePort host. |

Session metadata is passed into the selected Agent process environment as:

| Variable | Source |
| --- | --- |
| `MSPACE_API_BASE_URL` | Server control-plane API base URL. |
| `MSPACE_ISSUE_ID` | Current issue id. |
| `MSPACE_SESSION_ID` | Current session id. |
| `MSPACE_AGENT_TOKEN` | Scoped bearer token for agent writes. |
| `MSPACE_AGENT_ENGINE` | Selected fixed engine id: `codex`, `claude_code`, or `pi`. |
| `MSPACE_SESSION_BRANCH` | Server-owned stable Issue branch for source Sessions, or the detached task branch when applicable. |
| `MSPACE_SESSION_WORKDIR` | Prepared agent working directory. For local project subdirectories, this is the subdirectory inside the worker-created git worktree. |
| `MSPACE_SOURCE_SESSION_ID` | Selected source session for a deploy/test continuation, when present. |
| `MSPACE_SOURCE_COMMIT_SHA` | Selected source commit for a deploy/test continuation, when present. |
| `MSPACE_SESSION_CONTEXT` | Markdown context file written by the worker. |
| `MSPACE_SESSION_ARTIFACT_DIR` | Session artifact directory. Source Sessions use the external Worker artifact root; detached tasks retain their per-worktree artifact layout. |
| `MSPACE_REPOSITORY_URL` | Resolved repository URL or local git root used by the worker cache. |
| `MSPACE_PROJECT_REPOSITORY_PATH` | Original configured local project path, when it differs from the resolved git root. |
| `MSPACE_PROJECT_SUBDIR` | Project path relative to the git root, when a local project points inside a repository. |

## Smoke Checks

Server health:

```bash
curl http://127.0.0.1:8787/health
```

GitHub auth start endpoint:

```bash
curl -i http://127.0.0.1:8787/api/auth/github/start
```

Workspace Inbox:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/inbox
```

Manual runtime worker credential for external worker debugging:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-registration-tokens" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"local worker","expiresInHours":12}'
```

Queue a protocol task through the API debug path:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"protocol_smoke","runtimeMode":"team","requiredCapabilities":{"protocolSmoke":true},"payload":{"source":"curl"}}'
```

Inspect task events and logs:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks/<task-id>/events"

curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks/<task-id>/logs"
```

## Workspace Runtime Checks

List the fixed read-only Agent catalog:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/agents
```

List environments:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/environments
```

List Kubernetes cluster compatibility records:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/clusters
```

Discover kubeconfigs:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/clusters/discover-defaults
```

Refresh Environment readiness after editing, importing, or changing credentials:

```bash
curl -X POST -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/environments/<environment-id>/check
```

For virtual machine Environments, the check endpoint uses the saved SSH credential by default. Include `sshAuth` only when you want to replace the saved credential and recheck in one request:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/environments/<environment-id>/check" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"sshAuth":{"method":"password","password":"<password-for-this-ssh-user>"}}'
```

The older Kubernetes compatibility endpoint remains available for cluster records:

```bash
curl -X POST -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/clusters/<cluster-id>/check
```

List namespace resources for an issue test environment:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/issues/<issue-id>/test-environment/resources
```

Create a test case:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/projects/<project-id>/test-cases" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Local password login succeeds","type":"functional","status":"ready","steps":[{"action":"Submit valid credentials"}],"expectedResult":"The workspace opens.","environmentRequirements":"Personal desktop server is running."}'
```

Preview Markdown cases:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/projects/<project-id>/test-cases/import/preview" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  -d '{"format":"markdown","content":"- Invalid password shows an error\n- Invite link opens the workspace"}'
```

The preview is read-only. It returns `parsedCount`, `importableCount`, `skippedCount`, `missingFieldCounts`, `qualityFindingCounts`, `columnMappings`, `importableCaseSamples`, and `skippedSamples`. The current request limits are 1,000 importable cases, 2 MB for Markdown/text/CSV content, and a 2 MB decoded workbook for `.xlsx`.

Import `.xlsx` cases by base64-encoding the workbook bytes:

```bash
python3 - <<'PY'
import base64, json, pathlib
path = pathlib.Path("cases.xlsx")
print(json.dumps({"format": "xlsx", "fileName": path.name, "content": base64.b64encode(path.read_bytes()).decode()}))
PY
```

Then preview that JSON body:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/projects/<project-id>/test-cases/import/preview" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  --data-binary @/tmp/test-cases-import.json
```

After the preview looks right, send the same JSON body to:

```bash
curl -X POST "http://127.0.0.1:8787/api/workspaces/<workspace-id>/projects/<project-id>/test-cases/import" \
  -H "Authorization: Bearer <msp-token>" \
  -H 'Content-Type: application/json' \
  --data-binary @/tmp/test-cases-import.json
```

## Validation

```bash
pnpm typecheck
pnpm --filter @mspace/desktop build
pnpm --filter @mspace/website build
pnpm dist:desktop:mac
pnpm dist:desktop:win
pnpm dist:desktop:linux
pnpm test:server
(cd packages/ui && pnpm dlx shadcn@latest info --json)
(cd worker && go test ./...)
(cd worker && go build ./...)
```

## Troubleshooting

### Codex desktop run fails before Electron starts

`scripts/run-mspace-codex-dev.sh` writes the active run to `~/.mspace/logs/codex-run-latest.log`. If the log stops around `electron-vite dev` with `Error: Electron uninstall`, or `pnpm --filter @mspace/desktop exec electron --version` reports `Electron failed to install correctly`, check whether pnpm skipped native install scripts:

```bash
pnpm ignored-builds
pnpm --filter @mspace/desktop exec electron --version
```

The root `pnpm-workspace.yaml` must allow install scripts for `electron`, `electron-winstaller`, and `esbuild` through `allowBuilds`. After that, reinstall with the lockfile:

```bash
pnpm install --frozen-lockfile
```

If the current `node_modules` was already installed while those scripts were ignored, rebuild pending packages or reinstall dependencies before retrying the Codex dev runner.

### GitHub sign-in does not complete

Local password sign-in does not require GitHub. Use GitHub only when the server has OAuth values and the environment can reach GitHub.

Check server env first:

```bash
curl -i http://127.0.0.1:8787/api/auth/github/start
```

Required variables for this GitHub OAuth path: `MSPACE_GITHUB_CLIENT_ID`, `MSPACE_GITHUB_CLIENT_SECRET`, and `MSPACE_GITHUB_REDIRECT_URI`. The desktop shows GitHub login only for an explicitly configured team server when `/health` reports `capabilities.githubAuth: true`. If the button is hidden while debugging local OAuth, launch desktop with `MSPACE_SERVER_URL=http://127.0.0.1:8787 pnpm dev:desktop` instead of relying on the default local personal server source.

### Workspace looks empty

Check that the desktop is pointing at the expected server. In packaged personal mode, inspect the Electron user-data SQLite path; in Postgres development, inspect the Docker volume:

```bash
curl http://127.0.0.1:8787/health
find "$HOME/Library/Application Support" -maxdepth 3 -name mspace.db 2>/dev/null
docker inspect mspace-postgres-dev --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}'
docker exec mspace-postgres-dev psql -U mspace -d mspace -Atc "select count(*) from workspaces; select count(*) from projects; select count(*) from issues;"
```

### Worker does not claim tasks

Agent mentions are rejected before queueing when no active Worker exposes the selected engine capability. Check readiness with `codex`, `claudeCode`, or `pi`, then inspect Worker/task details when the reason code points at liveness or capability drift:

```bash
curl -G http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime/availability \
  -H "Authorization: Bearer <msp-token>" \
  --data-urlencode 'runtimeMode=personal' \
  --data-urlencode 'requiredCapabilities={"codex":true}' \
  --data-urlencode 'issueId=<issue-id>'

curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-workers
```

`reasonCode` values such as `no_worker`, `missing_capability`, `stale_heartbeat`, `worker_draining`, `worker_offline`, `wrong_runtime_mode`, `working_copy_busy`, `working_copy_storage_unavailable`, and `working_copy_recovery_required` explain why the product should not queue yet. The latter three are Issue-specific and must not be bypassed by starting a different local Worker. If a task is already queued or running, inspect runtime tasks too:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks?limit=10&offset=0"
```

The task's `runtimeMode` and `requiredCapabilities` must match a ready Worker heartbeat. New `agent_session` tasks are normalized so exactly one engine capability is true; conflicting raw debug payloads are rejected. Missing `agentEngine` is treated as Codex only for legacy payload compatibility.

The Agents page separates these signals. The top rows show This Mac's diagnostic and the Server's `claimableWorkerCount`; the lower matrix shows every current-mode Worker. `ready` means the safe authentication probe passed, `needs_setup` means authentication/configuration is required, `missing` means the executable was not found, `probe_error` means the bounded probe failed, and `unverified` means readiness cannot safely be proven. Pi 0.55.1 or newer uses an offline, extension-disabled model-list probe from an isolated temporary directory: `needs_setup/model_unavailable` means no configured model is available and removes Pi from the Worker's claimable capabilities; `unverified/model_available` is displayed as configured but does not claim that credentials or connectivity were tested. Malformed output from a known supported version reports `probe_error/probe_malformed` and also removes the capability; older and unparseable versions retain `unverified/probe_unsupported` without running the model probe. `unverified/disabled_by_configuration` means an installed engine was excluded from the Worker's execution allowlist. A legacy Worker with no diagnostic remains visible as not reported and may still claim work by its historical capability; upgrade/restart it to get a current diagnostic. A stale heartbeat is a separate Worker-liveness problem.

Probe the same installation manually only after checking the Worker row and without sharing command output that may contain private configuration:

```bash
codex login status
claude auth status --json
command -v pi && pi --version
pi --offline --no-extensions --list-models
```

The Pi probe intentionally disables extension discovery. Models registered only by global or project-local `.pi` extensions can be usable for a Session while remaining undiscoverable from the Agents page; verify those from the target project directory when diagnosing that edge case.

The personal Worker and Agent CLI run under the same OS user. The launch environment excludes Worker/control-plane secrets, but an Agent with broad filesystem access could still scan the Electron user-data credential path. Use a separate OS account, container, credential broker, or filesystem deny-path sandbox where that threat matters.

If a Tests plan includes any `ui` cases or functional cases that explicitly require browser/platform/session UI entry, the server requires a fresh Worker with `{"codex":true,"browser":true,"chrome_cdp":true}`. In personal desktop mode, the start or retry action asks Electron to launch a managed CDP endpoint and a separate browser companion Worker; restarting the desktop or stopping the generic base Worker is not required. If Chrome exists but CDP never becomes reachable, Electron should fall back to its bundled Chromium host in local dev. If no browser-capable Worker appears, provide a reachable `MSPACE_CHROME_CDP_URL`, or set `MSPACE_CHROME_EXECUTABLE` / `MSPACE_ELECTRON_EXECUTABLE` before starting the desktop app.

### Test run stays running after artifacts exist

If a Tests run stays `running` while the worker workdir already contains a matching `test-result.json` or `test-setup-result.json`, inspect the runtime task before changing data by hand:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  "http://127.0.0.1:8787/api/workspaces/<workspace-id>/runtime-tasks?limit=20&offset=0"
```

The fixed path is to restart the same Worker process with the same stable Worker name and the same persistent `MSPACE_WORKER_WORK_ROOT` or Docker volume. The Server allows that Worker to reclaim its own stale `running` task before new queued work, and the Worker should recover the existing Session workdir, attach the artifact, and complete the task through the normal runtime status API. A matching name with a new storage root is not equivalent. Do not edit the SQLite or Postgres run rows directly unless a user explicitly asks for database repair.

### VM Environment stays unreachable

Virtual machine Environments run an SSH login check on create/update/recheck. `ready` only comes from a successful password or private-key login, and raw passwords/private keys are stored server-side for worker access but are never returned in the Environment response. If a VM stays `unreachable`, verify the same tuple from the server host:

```bash
ssh -p <port> <user>@<host>
```

Then retry the VM Environment check. Recheck uses the saved credential by default; include new `sshAuth` only when replacing the saved password/private key. Missing password/private key input is rejected before validation only when the VM has no saved credential; connection and authentication failures keep the record for repair as `unreachable`.

### Personal worker credential expires

The desktop-managed personal worker should renew credentials automatically. Check the Electron log for `[personal-worker] credential renewal failed` or `restart failed` first. If renewal keeps failing, confirm the user is still signed in, the selected server URL is reachable, and the personal workspace id has not changed. As a last local debug step, stop and restart the personal worker by switching server source or relaunching the desktop app; do not create a long-lived manual worker credential for normal personal mode.

### Codex worker fails authentication

For Docker Codex workers, make sure the mounted worker Codex home contains both auth and config:

```bash
ls -la ~/.mspace/codex-worker-home
test -s ~/.mspace/codex-worker-home/auth.json
test -s ~/.mspace/codex-worker-home/config.toml
```

Re-run `scripts/run-server-worker-codex-dev.sh` after refreshing `${CODEX_HOME:-~/.codex}`.

### Claude Code or Pi Session fails before terminal completion

Confirm the command is installed in the same `PATH` passed to the Worker:

```bash
command -v claude
command -v pi
claude --version
pi --version
```

Claude Code must emit a terminal stream-JSON `result`; Pi must emit RPC `agent_end`. Exit code zero without that evidence is a failed task. Pi cancellation sends an RPC `abort` before process termination, and Pi `sessionFile` paths remain local to the Worker. Authentication and model configuration are owned by each CLI installation; do not put those credentials in the mspace server.

### Test environment resources fail to load

Check the server-owned cluster and issue environment:

```bash
curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/clusters

curl -H "Authorization: Bearer <msp-token>" \
  http://127.0.0.1:8787/api/workspaces/<workspace-id>/issues/<issue-id>
```

The issue must have a `testEnvironment` with a namespace, kubeconfig path, and reachable cluster context.
