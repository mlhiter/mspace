# mspace Test Module Product And Implementation Plan

> Status: product and implementation plan plus implementation snapshot
> Date: 2026-06-05

## Conclusion

mspace should add a test module, but it should not become a standalone test management platform. The right product shape is project-level test cases and case suggestions, workspace-level test plans and test runs, and issue-backed execution.

The test module owns what to test, how to test it, which version is under test, which environment is used, and what the result was. Actual execution should still go through the existing mspace chain: Issue, Agent Session, Worker, Evidence, and Issue Test Environment. This keeps testing inside the product's current source of truth instead of creating a side channel.

In short:

```text
Projects own cases and case suggestions.
The workspace owns plans and runs.
Issues own execution and collaboration.
Workers and Codex perform the actual tests.
Evidence makes the result inspectable and trustworthy.
```

## New Requirements From The Updated Draft

The updated workflow adds or clarifies these requirements:

- A test plan can select a reusable Environment and freeze that selection into a run snapshot.
- An Environment is not a preview URL. In the current product vocabulary it is only a Kubernetes cluster target or a virtual machine target.
- Workers are not owned by environments. Multiple workers may have the capabilities needed to operate the same environment, and task routing stays in the runtime worker queue.
- Test plans support multiple rounds: full run, failed-case retry, blocked-case retry, incremental test, and self-test.
- Some cases need mock data setup.
- A formal test plan may need plan-level setup steps that run once before cases, such as signing in to a platform, updating a Deployment image, SSHing into a VM, preparing mock data, or verifying a preview URL.
- Test types include functional tests, UI tests, API tests, and deployment tests.
- UI tests need browser/CDP support. Deployment tests may need SSH or commands similar to `sealos run`.
- Test results need screenshots or other evidence.
- Human review remains a checkpoint. A Codex-completed run should not automatically mean a release is accepted, but the current product treats accept/block as audit records until a later release or plan gate consumes them.

## Product Positioning

The test module is not another Jira or TestRail. It is the quality layer of the existing mspace agent workspace.

It solves four concrete problems:

1. Where test cases come from: QA input, rough list import, repository analysis, and project runbook analysis.
2. Whether a case is executable: clear steps, explicit expectations, complete dependencies, and correct ordering.
3. How a version is tested: create a plan for an RC, commit, branch, image, offline package, or preview environment.
4. Why the result can be trusted: Codex returns status, screenshots, logs, commands, resources, failure summaries, retry state, and human review records back to the Issue.

## Non-Goals

The first version should not include:

- A generic test management platform.
- A test execution system that bypasses Issues.
- Server-side Codex execution or Codex credentials in the server.
- Renderer-local test storage or a sidecar-owned test database.
- A cluster-wide operations console.
- Sealos UI APIs as the primary execution path.
- Default agent access to Secrets.
- UI/CDP automation, SSH multi-machine scheduling, or deployment-test orchestration in the first phase.
- A reusable setup template library, dependency DAG, or general workflow orchestrator for the first setup slice.
- Direct writes from Codex-generated cases into the canonical test case library.
- Treating Codex pass results as release approval without a human review checkpoint.

## Core Object Model

### Test Case

A test case is a project-level object.

This is intentional. A useful case depends on project code, the project runbook, runtime environment, branch, and version. Without a Project, a case can exist only as a draft and cannot enter execution.

Recommended fields:

- workspace
- project
- title
- type: currently supports `functional`, `ui`, `api`, and `deployment`
- area or feature
- priority, set manually
- status: `draft`, `needs_review`, `ready`, `archived`
- source: `manual`, `import`, `codex_generated`, `codex_refined`
- preconditions
- test steps
- expected result
- dependent cases
- environment requirements
- tags
- executability signal
- executability findings
- creator and updated time

The executability signal should start as a deterministic rule set rather than an AI judgment. It can still be stored as `qualityScore` internally, but the user-facing meaning is whether a case is runnable enough to trust. Examples:

- missing preconditions lower the score;
- missing expected result lowers the score;
- vague one-line steps lower the score;
- phrases such as "verify it works" or "check whether it is normal" lower the score;
- missing environment requirements lower the score;
- highly similar title or steps compared with existing cases lowers the score.
- cases that delete, update, rename, archive, enable, or disable existing data should say whether the target data is created by the case, prepared by setup, or provided by a dependency; otherwise the case gets a `missing_data_source` finding.

### Test Case Revision

Every manual edit or accepted Codex improvement creates a revision.

This gives the product two important capabilities:

- test knowledge can be rolled back;
- users can compare before and after when Codex refines a case.

Case Detail should make that comparison visible in the list itself. The first revision should show it is the initial version plus compact facts such as type, status, source, steps, and expected result. Later revisions should compare against the previous revision snapshot and show which fields changed, with before/after values for title, type, area, priority, status, source, preconditions, steps, expected result, environment requirements, tags, and executability score. Do not render revision history as only `#version - title`.

### Test Case Proposal

Codex does not directly modify canonical cases. It returns proposals.

In the user-facing UI, label this surface as `Case suggestions` / `用例建议` rather than `Proposals` / `提案`. The product meaning is not a separate planning object. It is a review queue for suggested changes to the case library.

Proposal types include:

- create a new case;
- rewrite a title;
- add preconditions;
- split or clarify steps;
- add expected results;
- mark dependencies;
- mark duplicates;
- suggest archiving;
- suggest missing coverage.

The user accepts or dismisses these suggestions in the Case Suggestions review surface.

### Environment And Run Snapshot

An environment is a key part of whether a plan can run. It should not be represented as a URL, and preview URLs should stay as outputs of a deploy/run.

Use two layers:

```text
Environment
  -> long-lived workspace target, kind=kubernetes|virtual_machine

Test Run Environment Snapshot
  -> frozen selection and resolved fields for one test run
```

The Environment can include:

- for Kubernetes: cluster id, kubeconfig path, context, registry prefix, exposure defaults, preview domain, ingress class, and NodePort host;
- for virtual machines: SSH host, port, user, credential configuration state, workdir, service hints, labels, and readiness from server-owned password/private-key SSH login validation.

The run snapshot should freeze:

- environment id and kind;
- resolved Kubernetes or VM fields;
- branch;
- commit;
- image;
- offline package or version URL;
- environment record update time;
- linked Issue Test Environment when the run creates or reuses one;
- preview URL only when it is produced by an issue deploy or test run output.

In the first phase, mspace keeps Kubernetes clusters as compatibility records behind the Environment API and adds virtual machine environments as SSH target records with server-owned password/private-key credentials. They must pass SSH login validation before they can be `ready`, and rechecks use the saved credential unless a replacement is supplied. It should not introduce heavy environment orchestration yet. Test Plan and Test Run store `environment_id`, `environment_kind`, and `environment_snapshot`, while the existing free-text `environment` field remains human notes for the agent.

### Test Plan

A test plan answers: which version are we testing, which cases are included, which environment is used, and which round is this?

A test plan is a workspace-level orchestration object. It may include ready cases from multiple projects in the same workspace. The plan can still keep a primary project for compatibility and default filtering, but each included case keeps its own project identity.

Recommended fields:

- workspace
- primary project, for compatibility/default filtering
- title, for example `rc4 functional test plan`
- target type: branch, commit, source session, image, offline package, version URL, or preview URL
- target value
- linked Environment
- optional setup steps that run once before case execution
- selected cases, each with project and case id
- case order and dependencies
- current round
- status: `draft`, `ready`, `running`, `needs_acceptance`, `completed`, `archived`
- creator

### Test Run

A test run is the durable execution record. In the normal product flow it starts from a workspace test plan, including quick plans created from selected ready cases. Compatibility/debug ad hoc runs may still exist at the API layer, and failed/blocked retry or later incremental scopes reuse the same run model.

A test run is also workspace-level. Its run items preserve the project/case identity used for execution, artifacts, and result reconciliation. When a run covers multiple projects, mspace groups queued items by project and creates separate execution Issues/agent sessions per project batch; one agent session should not span multiple repositories or projects.

Typical rounds:

- Round 1: full run;
- Round 2: failed and blocked cases only;
- Round 3: incremental cases affected by the current commit;
- Self-test: a developer or Codex runs a smaller high-risk set before formal testing.

A Test Run stores:

- workspace;
- primary project, for compatibility/default filtering;
- source: `ad_hoc`, `plan`, `retry`, or `incremental`;
- round number;
- linked Test Plan when the source is plan-based;
- linked parent Issue;
- frozen setup steps, setup status, setup Issue, setup Session, setup result, and setup-derived run context when the source plan has setup;
- linked environment snapshot;
- status: `queued`, `setup_running`, `setup_failed`, `running`, `needs_acceptance`, `accepted`, `blocked`, or `cancelled`;
- passed, failed, blocked, and skipped counts;
- pass rate;
- start and completion time;
- human review status.

### Test Run Item

A Test Run Item is one case result inside one run.

It stores:

- linked Project;
- linked Test Case;
- linked execution Issue;
- linked Agent Session;
- status: `queued`, `running`, `passed`, `failed`, `blocked`, `skipped`, or `cancelled`;
- actual result;
- failure summary;
- screenshot, log, command, or resource evidence;
- whether human review is required.

## Core Product Workflows

### 1. Import Or Create Test Cases

The user enters `Tests -> Cases`, selects a project, then can:

- import a rough Markdown or text file;
- import CSV;
- import Excel `.xlsx` workbooks with the same column contract as CSV;
- create a case manually;
- generate suggestions from the repository and runbook.

The system does three things:

1. parses rough input through a read-only import preview;
2. reports how many cases will import, how many rows will be skipped, how source columns map to system fields, and whether required fields are missing;
3. lets the user confirm before writing cases;
4. converts confirmed input into structured draft cases and computes executability signals.

Output:

```text
Rough case list
  -> structured drafts
  -> executability evaluation
  -> human review and adjustment
  -> ready test case library
```

### 2. Refine Test Cases With Codex

The user selects draft or low-executability cases and clicks `Optimize`.

mspace should not call Codex from the server. It should create a normal issue-backed Agent Session:

```text
Tests page
  -> create or reuse an "optimize test cases" Issue
  -> attach selected cases, project runbook, and code context to the session context
  -> server creates an agent_session runtime task
  -> worker starts codex app-server
  -> Codex writes test-case-proposals.json
  -> server validates proposals
  -> human accepts or dismisses them in Case Suggestions review
```

This keeps the optimization auditable:

- the Codex run has an Issue;
- logs, failures, and context are inspectable;
- the server remains Codex-free;
- humans control canonical test knowledge.

Optimization and generation Issues are audit carriers for the Tests surface. They should stay directly reachable from their session or suggestion context, but they should not appear in the default Issues list.

### 3. Generate Test Cases From A Repository

The user can select a Project and ask Codex to analyze:

- project runbook;
- source directories;
- routes, pages, or APIs;
- existing Issues;
- existing cases;
- changed files from a commit or source session.

Codex returns proposed cases, not direct writes.

The first version should support two entry points:

- generate baseline test cases for the whole project;
- generate incremental cases for the current change.

### 4. Create A Test Plan

The user selects `ready` cases and creates a test plan.

The plan needs:

- title;
- target type: branch, commit, source session, image, offline package, version URL, or preview URL;
- environment: selected Environment or an existing Issue Test Environment snapshot;
- optional setup steps that run once before case execution;
- selected ready cases in explicit execution order;
- execution scope: full, failed retry, blocked retry, incremental, or custom;
- whether parallel execution is allowed;
- whether human review is required.

The create/edit plan UI should stay focused on fields that change execution behavior: title, target type, Environment, setup steps, and selected ready cases. Do not expose a standalone description box or target-value input in the primary modal unless a later workflow makes those values actionable for the user. Existing stored description or target value data may still be displayed when present for historical plans and runs.

The selected-case list is ordered. Users can move selected cases up or down in the create/edit plan form, the Plan Detail page shows that order, and each run freezes the order onto its run items. This is intentionally a linear order, not a dependency graph or workflow scheduler.

Plan-level setup is intentionally simple in the current slice. It is free-text operational guidance stored on the plan, not a reusable template or graph engine. The text can describe browser, Kubernetes, VM, SSH, Sealos, mock-data, or deployment preparation in the user's own words. When the plan starts a run, mspace freezes those setup steps onto the run so later edits do not rewrite historical execution context.

Examples:

```text
1. Confirm the test environment.
2. SSH to the staging VM and update the target service image.
3. Log in to Sealos with the test account, open Object Storage, and verify the app page loads.
4. Write test-setup-result.json with status passed and outputs.previewUrl.
```

### 5. Enter Issue-Backed Execution

When a test run starts, mspace creates one parent Issue for the run and execution Issues for batches.

These Issues preserve the normal Issue-backed audit trail, but the Test Run remains the user-facing list and detail entry. The default Issues list should hide test run parent Issues and execution batch Issues; manually created `type:test` Issues remain ordinary user work and should still be listed.

Run items preserve the plan's frozen case order with `sortOrder`. Execution Issues receive cases in that order inside each batch, and Run Detail renders the same ordering so users can compare expected sequence and result sequence without relying on creation timestamps.

By default, mspace should not create one Issue per case. That would flood the issue list during large regression runs. Suggested batching:

- execute dependent cases in dependency order;
- group cases by feature area;
- isolate high-risk cases in their own Issue;
- keep each batch small enough for one Codex session to execute and report clearly.

Example:

```text
Test Plan: rc4 functional test
  -> Run 1
      -> Parent Issue: Run rc4 functional test
          -> Child Issue: Login and workspace entry
          -> Child Issue: Issue creation and Codex assignment
          -> Child Issue: Test environment deploy and preview validation
```

Each execution Issue starts a Codex Agent Session through the existing `POST /issues/{issueID}/sessions` path.

If the plan has setup steps, the run enters `setup_running` first and creates a single setup child Issue with automation marker `test_run_setup`. Case execution Issues are not created yet. The setup session must write `${MSPACE_SESSION_ARTIFACT_DIR}/test-setup-result.json`. Only a completed setup task with a passing setup artifact moves the run to `running` and starts the normal case batches. Failed, cancelled, missing-artifact, or artifact-level failed setup marks the run `setup_failed` and leaves every run item queued.

A user-initiated stop is different from a failed setup. Stopping a run while it is `queued`, `setup_running`, or `running` marks the run `cancelled`, marks every queued/running item `cancelled`, and requests cancellation for the linked setup/execution runtime tasks. Late `test-setup-result.json` or `test-result.json` artifacts from interrupted workers must not revive the run or overwrite cancelled items.

### 6. Codex Performs The Actual Test

For the first functional-testing phase, Codex can use:

- project code;
- project runbook;
- test cases;
- Environment snapshot;
- setup-derived run context;
- preview URL;
- Kubernetes namespace information;
- required commands.

After execution, Codex returns structured results:

- passed cases;
- failed cases;
- blocked cases;
- actual result;
- failure summary;
- command evidence;
- screenshots or resource evidence;
- risks and follow-ups.

These results update Test Run Items while remaining inspectable from Issue Evidence and Session Detail.

### 7. Human Review

When a run finishes, it should move to `needs_acceptance`. It should not automatically become release-approved.

In the current product, retry is the primary recovery action because it requeues failed or blocked run items. Accept/block decisions are review records for audit and later reporting; they do not yet drive Issue status, release gating, or canonical case knowledge.

The reviewer needs to see:

- pass rate;
- failed and blocked cases;
- Issue and Evidence links for each failure;
- screenshots or logs;
- whether retry is needed;
- whether a defect Issue should be created;
- whether the run was reviewed and whether follow-up is needed.

Human actions:

- rerun failed items;
- record the run as reviewed;
- record follow-up needed;
- create a defect Issue;
- archive the run;
- start the next round.

## Test Type Roadmap

### Functional Tests: Phase 1

Functional tests come first because they can reuse existing mspace primitives:

- Project;
- Issue;
- Agent Session;
- Worker;
- Issue Test Environment;
- Evidence;
- Resources tab.

The first phase should not try to make Codex behave like a full human browser tester. It should first prove this loop:

```text
case library
  -> case refinement
  -> test plan
  -> issue-backed execution
  -> structured result
  -> human review
```

### UI Tests: Later Phase

UI tests require workers with `browser` or `chrome_cdp` capability.

Future model:

```text
Test Run Item
  -> requires browser capability
  -> server routes to browser-capable workers
  -> worker connects to the configured browser environment
  -> worker returns screenshots, DOM state, and failed step details
```

Screenshots must not be stored in renderer-local storage or exposed as worker-local paths. The worker may use session artifacts as the transfer boundary, but reconciled screenshots are persisted by the server as `test_artifacts` and referenced from run item evidence. The product UI should show user-facing evidence such as screenshots, DOM state, network summaries, assertions, and URLs; raw browser protocol details stay inside raw evidence for debugging.

### API Tests: Later Phase

API tests need explicit:

- API base URL;
- authentication method;
- test data;
- assertions;
- cleanup logic.

They can probably arrive before full UI testing, but still after the functional test loop works end to end.

### Deployment Tests: Later Phase

Deployment tests may require:

- SSH;
- `sealos run`;
- offline packages;
- image synchronization;
- multi-machine state;
- cluster component upgrades;
- rollback and cleanup.

This has a higher risk profile and needs strict permission boundaries. It should not be part of the first version.

## Parallelism And Multi-Environment Scheduling

The updated draft says:

> One plan is associated with an Environment snapshot. Workers and browser services are separate executors that may be routed to that Environment by capability, and each worker can test a different case when concurrency rules allow it.

This direction is right, but it should be implemented in layers.

### Layer 1: Capability Routing

Workers report capabilities through heartbeat:

- `codex`
- `kubectl`
- `browser`
- `chrome_cdp`
- `api_test`
- `ssh`
- `sealos_run`

Each Test Run Item declares the capabilities it needs. The server routes the task only to matching workers.

### Layer 2: Concurrency Limits

Test Plan, Environment policy, or later scheduler policy defines:

- max Codex concurrency;
- max browser concurrency;
- whether parallel write operations are allowed in the same environment;
- whether dependency failure should stop downstream cases.

### Layer 3: Environment Isolation

The first version can run one Test Run against one shared test environment.

Later versions can support:

- one environment per execution Issue;
- one namespace per high-risk case;
- one isolated browser per worker;
- multi-machine coordination.

Do not start with one environment per case. That would explode implementation and resource cost before the core loop is proven.

## Incremental Testing And Self-Test

Incremental testing should not be a vague AI guess. Start with rules.

Inputs:

- source session;
- branch;
- commit;
- changed files;
- PR handoff;
- project runbook;
- test case area, tags, and dependencies.

Outputs:

- affected cases;
- recommended self-test cases;
- mandatory smoke cases;
- skipped cases with reasons.

The first version can use manual selection plus simple tag matching. Codex can suggest the set, but the user confirms the final scope.

## UI Plan

Add `Tests` to the main navigation after `Issues`.

The page should keep the quiet mspace workspace style. It should not become a dashboard-heavy test management console.

Tests should not use a left-list/right-detail layout. The Cases, Plans, and Runs tabs are list surfaces. Selecting a case, plan, or run opens a dedicated detail page so the content has enough room for editing, evidence, and review.

### Tests / Cases

Purpose: manage project test cases.

Main actions:

- filter by Project;
- import;
- create;
- archive selected cases;
- batch optimize;
- generate from project;
- filter by executability;
- filter by status;
- filter by type, area, and priority;
- add to test plan.

List columns:

- title;
- type;
- status;
- priority;
- executability;
- latest run result;
- updated time.

The compact Cases list should currently show `title`, `type`, `status`, `priority`, `executability`, `latest run result`, and `updated time`; `area` can remain secondary metadata under the title instead of consuming its own column.

The Cases list must stay server-paginated because a project can carry many cases. The list API returns `{ cases, total, limit, offset }`, defaults to hiding `archived` cases, and treats user-facing deletion as archiving. Archived cases remain available through the archived status filter so run history, revisions, plan references, and evidence are not destroyed.

### Case Detail

Open from a case row into a dedicated detail page and display the case as a document:

- title;
- preconditions;
- steps;
- expected result;
- dependencies;
- historical runs;
- revisions with field-level change summaries;
- related Issues.

The create and edit UI should express backend constraints as user-facing runnable-case guidance. Show readiness checks, examples, and field hints for title, preconditions, steps, expected result, and environment requirements; do not ask users to memorize storage enums such as `manual`, `import`, or `codex_generated`.

### Case Suggestions Review

Show Codex proposals as diffs:

- new cases;
- modified cases;
- duplicates;
- archive suggestions;
- missing coverage.

Actions:

- accept selected;
- reject all;
- edit manually, then accept.

The tab label should be `Case suggestions` / `用例建议`, with action copy such as `Accept suggestion` and `Dismiss suggestion`, so users understand they are approving changes to test cases.

### Tests / Plans

Purpose: define a test scope for a version, branch, or environment.

Plan rows open into dedicated plan detail pages. The list page should not keep a selected-plan detail pane beside the plan list.

Show:

- plan title;
- target under test, showing the target value only when one exists;
- linked environment;
- case count;
- current round;
- status;
- latest pass rate;
- human review status.

### Tests / Runs

Purpose: inspect each test run's progress and result.

Run rows open into dedicated run detail pages. The list page should not render run results beside the run list.

Show:

- current status;
- pass rate;
- failed, blocked, and skipped counts;
- execution Issues;
- Agent Sessions;
- Evidence;
- environment;
- screenshots/logs;
- failed-item retry and human review actions.

### Issue Detail Integration

Issues created by a Test Run should show a compact test context block:

- test plan;
- round;
- cases included in this Issue;
- target under test;
- test environment;
- current result;
- link back to the Test Run.

Do not turn Issue Detail into the test management console. It should remain the execution, collaboration, and evidence surface.

## Technical Implementation

### Storage Boundary

All test module data must live in `server/`, with Postgres migrations. Personal desktop mode can continue to persist through the existing server-owned SQLite snapshot store.

Do not add renderer-local test storage.

### Recommended New Tables

Phase 1:

- `test_cases`
- `test_case_revisions`
- `test_case_proposals`

Phase 2:

- `test_environment_templates`, or first extend the existing cluster/project setting relationship;
- `test_plans`
- `test_plan_cases`
- `test_runs`
- `test_run_items`
- `test_artifacts`

Later:

- `test_run_environment_snapshots`
- `test_worker_capability_requirements`

### Recommended New APIs

Phase 1:

```text
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-cases
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import/preview
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-cases
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/delete
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}
PUT    /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}
DELETE /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/optimize
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/generate
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals/{proposalID}/apply
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals/{proposalID}/reject
```

Phase 2 primary workspace routes:

```text
GET    /api/workspaces/{workspaceID}/test-plans
POST   /api/workspaces/{workspaceID}/test-plans
GET    /api/workspaces/{workspaceID}/test-plans/{planID}
PUT    /api/workspaces/{workspaceID}/test-plans/{planID}
POST   /api/workspaces/{workspaceID}/test-plans/{planID}/runs
GET    /api/workspaces/{workspaceID}/test-runs
POST   /api/workspaces/{workspaceID}/test-runs
GET    /api/workspaces/{workspaceID}/test-runs/{runID}
GET    /api/workspaces/{workspaceID}/test-runs/{runID}/artifacts
POST   /api/workspaces/{workspaceID}/test-runs/{runID}/retry
POST   /api/workspaces/{workspaceID}/test-runs/{runID}/cancel
POST   /api/workspaces/{workspaceID}/test-runs/{runID}/accept
POST   /api/workspaces/{workspaceID}/test-runs/{runID}/block
```

Project-scoped plan/run routes remain as compatibility filters for clients that are still anchored on one project:

```text
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-plans
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-plans
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}
PUT    /api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}/runs
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-runs
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-runs
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/retry
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/cancel
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/accept
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/block
```

Execution still uses the existing session route:

```text
POST /api/workspaces/{workspaceID}/issues/{issueID}/sessions
```

### Artifact Contracts

Codex case optimization writes:

```text
test-case-proposals.json
```

Codex test execution writes:

```text
test-result.json
```

Preferred shape:

```json
{
  "runId": "test-run-...",
  "items": [
    {
      "caseId": "test-case-...",
      "status": "passed",
      "actualResult": "...",
      "evidence": {}
    }
  ]
}
```

The worker also accepts a top-level array for Codex-authored single-run artifacts when each item includes `runId`; it normalizes that array into the object shape before returning `result.testResult`.

Each result item should use a real Test Run Item case id. Codex should not emit synthetic ids such as `batch`, `all`, or `summary`; if a global blocker stops a batch, it should write one final item per affected case. The server still treats a legacy batch-level blocked/failed item as a recovery signal for the active, non-final run items owned by that same agent session, so a completed worker task cannot leave those items stuck in `running`.

Codex plan setup writes:

```text
test-setup-result.json
```

Shape:

```json
{
  "runId": "test-run-...",
  "status": "passed",
  "summary": "Preview is ready.",
  "failureSummary": "",
  "outputs": {
    "previewUrl": "https://preview.example.test",
    "image": "registry.example/app:rc4",
    "namespace": "mspace-rc4"
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

The server stores the whole setup result on the run and copies the `outputs` object into `runContext`. Later execution Issues include that context in their prompt, so a setup step can hand off concrete facts such as `previewUrl`, `image`, `namespace`, or `sshTarget` without adding a new product object. `status:"failed"`, a failed setup task, cancellation, or a missing setup artifact stops the run before any case session starts.

When a UI test writes screenshot paths inside `evidence`, the worker waits briefly for those referenced files to become readable under the session artifact directory, then embeds small screenshot files as `evidence.screenshotImages[]` data URLs before completing the runtime task from `test-result.json`. The same readiness rule applies when a restarted worker recovers an existing session workdir. The server extracts supported image data into `test_artifacts`, removes embedded image payloads from run item evidence, and writes artifact refs back into `evidence.artifacts` and `evidence.screenshotImages`. Case Detail and Run Detail render those refs as authenticated thumbnails with an in-app preview.

Future UI testing can write:

```text
screenshots/
browser-trace.json
```

Future deployment testing can write:

```text
deployment-test-result.json
```

The server must validate artifacts:

- case IDs must belong to the current project;
- statuses must be from the allowed set;
- proposal count must be bounded;
- unknown test types must be rejected;
- cross-workspace and cross-project references must be rejected.

## Implementation Plan

Each phase must be independently mergeable and usable.

### Phase 1: Test Case Library

Goal: let users manage test cases first.

Scope:

- add `Tests` navigation;
- case list;
- case detail as a dedicated page, not a side pane;
- Markdown, text, CSV, and Excel `.xlsx` file import with read-only preview before confirmation;
- manual create and edit;
- case executability signal;
- case revisions;
- English and Simplified Chinese i18n;
- server API and Postgres migration;
- support `functional`, `ui`, `api`, and `deployment` as case types while keeping specialized execution harnesses for later phases.

This phase does not depend on Codex or workers.

Acceptance:

- user can select a Project;
- user can preview and then import a rough test list;
- user can import `.xlsx` workbooks whose first non-empty sheet uses `title`, `type`, `area`, `priority`, `preconditions`, `steps`, `expected_result`, `environment_requirements`, and `tags` headers;
- preview shows importable count, skipped count, missing field counts, quality findings, sample cases, and skipped-row samples before any write;
- system creates structured draft cases;
- executability and missing fields are visible;
- user can edit a case;
- revision history exists and shows what changed between revisions, not only the case title.

### Phase 2: Codex Case Generation And Refinement

Goal: let Codex improve cases while humans approve the final writes.

Scope:

- optimize selected cases;
- generate baseline test cases from a project;
- create traceable optimization Issues;
- run Codex through the existing worker path;
- parse `test-case-proposals.json`;
- Case Suggestions review page;
- accept/reject proposals;
- write accepted changes as case revisions.

Acceptance:

- Codex cannot directly modify canonical cases;
- optimization work is visible in Issue and Session;
- invalid proposal structure does not contaminate the case library;
- only accepted proposals update canonical cases.

### Phase 3: Test Plans And Functional Test Runs

Goal: turn cases into executable plans and assign Codex through Issues.

Scope:

- test plan list and dedicated detail page;
- target-type selection: branch, commit, source session, image, offline package, version URL, or preview URL;
- environment selection;
- optional plan-level setup steps that run once before case execution;
- create Test Run;
- create parent Issue and execution Issues;
- start Codex sessions for execution Issues;
- keep test automation Issues out of the default Issues list while preserving direct audit links;
- parse `test-result.json`;
- update Test Run Items;
- persist screenshot evidence as server-owned artifacts;
- show pass rate, failed, blocked, and skipped items;
- show per-case run history from Case Detail;
- retry failed or blocked items;
- support lightweight human review records.

Acceptance:

- user can create a plan such as `rc4 functional test plan`;
- user can select ready functional, UI, API, or deployment cases;
- user can start a test run;
- plan runs with setup start in `setup_running`, create a setup Issue/Session, and keep items queued until setup passes;
- setup success stores `setupResult` plus `runContext` and then creates execution Issues;
- setup failure or cancellation marks the run `setup_failed` without starting case sessions;
- user can stop a queued/setup-running/running test run; linked runtime tasks are cancelled, run items become `cancelled`, and late artifacts do not reopen the run;
- parent Issue and execution Issues are created;
- default Issues browsing stays focused on human work and does not show those test automation Issues;
- Codex sessions run through the normal worker path;
- results are written back to Test Run Items;
- failed items can be retried or turned into defect Issues;
- human review status is saved.

### Phase 4: Environment Scheduling And Parallel Execution

Goal: let one selected Environment host one plan while multiple decoupled workers execute in parallel under explicit concurrency rules.

Scope:

- reusable Environment policy;
- test run Environment snapshot;
- worker capability routing;
- concurrency limits;
- dependency ordering;
- write-operation mutex for shared environments;
- incremental test selection;
- self-test rounds.

Acceptance:

- a plan can bind to a reusable Environment;
- a run freezes commit/image/environment information;
- multiple Codex workers can claim different execution Issues in parallel;
- dependent cases do not run out of order;
- environment state is traceable.

### Phase 5: UI/API/Deployment Test Expansion

Goal: add more test types without changing the core model.

Scope:

- UI tests: Chrome CDP, screenshots, browser trace;
- API tests: requests, assertions, auth, mock data;
- deployment tests: SSH, `sealos run`, offline package, component upgrade;
- multi-machine scheduling;
- richer trace/log/resource artifact storage beyond screenshots.

Acceptance:

- different test types route to workers by capability;
- UI tests preserve screenshots;
- Case Detail shows a run history tab for all runs of that case;
- deployment tests have audit, rollback, and cleanup paths;
- multi-machine failures identify the machine, step, and environment involved.

## Risks And Constraints

### Risk 1: Issue Volume Explosion

One Issue per case would be too noisy for large regression plans.

Mitigation:

- create execution Issues by area or batch by default;
- isolate only high-risk cases as separate Issues;
- keep per-case state in Test Run Items.

### Risk 2: Codex Results May Be Wrong

Codex may incorrectly mark a case as passed or failed.

Mitigation:

- require structured `test-result.json`;
- keep logs, commands, screenshots, and resource evidence;
- require human review for important runs;
- failed items can be retried or turned into defect Issues.

### Risk 3: Environment Drift

The environment may change during or after a run, making results hard to reproduce.

Mitigation:

- freeze an environment snapshot at run start;
- store cluster, namespace, commit, image, package URL, and preview URL;
- record important environment operations through Issues and Sessions.

### Risk 4: Parallel Tests Interfere With Each Other

Multiple Codex workers testing the same environment can corrupt shared state.

Mitigation:

- avoid strong parallelism in the first version;
- add environment-level concurrency limits later;
- add mutex or dependency controls for write-heavy cases;
- include mock data setup and cleanup in the case or environment definition.

### Risk 5: Scope Creep

UI, API, deployment, and multi-machine testing together would make the first version too large.

Mitigation:

- keep one shared case -> plan -> Issue -> Codex -> evidence -> human review loop;
- support functional, UI, API, and deployment as case types without forking the workflow;
- defer specialized UI/CDP, API harness, and deployment orchestration until the shared loop is stable.

## Recommended MVP

The smallest useful version has three parts:

1. Case library: import, create, edit, and score cases.
2. Case refinement: Codex generates proposals, humans accept them.
3. Functional test plan: select cases, create a run, generate Issues, execute through Codex, collect results, retry failures if needed, and record human review.
4. Lightweight plan setup: for formal plans only, run one setup session before case execution and pass setup outputs into the later case prompts.

This MVP covers the main workflow from the source draft:

```text
rough case list
  -> structured by system
  -> refined by Codex
  -> reviewed by human
  -> final case list
  -> test plan
  -> plan-level setup when needed
  -> assigned to Codex
  -> result and screenshot/evidence
  -> human review
```

Formal Environment scheduling, multiple Codex/Chrome CDP workers, deployment tests, and multi-machine coordination should come after the first functional loop works.

## File Targets

Implementation will touch more than eight files. That is expected because this feature adds new product objects, server APIs, migrations, frontend routes, and i18n.

Main targets:

- `server/internal/control/migrations`
- `server/internal/control/types.go`
- `server/internal/control/http.go`
- `server/internal/control/postgres_store.go`
- `server/internal/control/memory_store.go`
- `server/internal/control/sqlite_store.go`
- `worker/main.go`
- `packages/core/src/types.ts`
- `packages/core/src/api.ts`
- `packages/views/src`
- `packages/i18n/src/index.ts`
- `packages/ui/src/index.tsx`
- `apps/desktop/src/renderer/src/main.tsx`
- `docs/architecture.md`
- `docs/ia.md`
- `docs/integration-guide.md`
- `ROADMAP.md`
- `apps/website/src/changelog.ts`

## Verification Plan

Automated checks:

```bash
pnpm typecheck
pnpm --filter @mspace/desktop build
pnpm --filter @mspace/website build
pnpm test:server
(cd worker && go test ./...)
(cd worker && go build ./...)
git diff --check
```

Phase 1 manual acceptance:

1. Create or select a Project.
2. Import a rough test case list from Markdown, CSV, or an `.xlsx` workbook.
3. Confirm structured draft cases are created.
4. Confirm executability and missing-field findings are reasonable.
5. Edit one case.
6. Confirm revision history exists and summarizes the changed fields.

Phase 2 manual acceptance:

1. Select a few low-executability cases.
2. Start Codex optimization.
3. Confirm an Issue and Agent Session are created.
4. Confirm Case Suggestions review shows before/after differences.
5. Accept one proposal and reject another.
6. Confirm only accepted proposals update canonical cases.

Phase 3 manual acceptance:

1. Create an `rc4 functional test plan`.
2. Select ready cases.
3. Select target and environment.
4. Start Run 1.
5. Confirm parent Issue and execution Issues are created.
6. Confirm Codex sessions use the normal worker path.
7. Confirm results write back to Test Run Items.
8. Confirm failed items can be retried or turned into defect Issues.
9. Confirm human review status can be saved.

## Design Summary

**Not building**: the first version does not include a generic test platform, UI/CDP/SSH/deployment orchestration, server-side Codex, execution outside Issues, or direct Codex writes into canonical cases.

**Building**: a Tests module where projects own imported, created, and refined test cases; the workspace owns test plans and runs that may span multiple projects; and execution assigns project-grouped case batches to Codex through Issues while storing results, evidence, failures, screenshots, and lightweight human review state.

**Approach**: add server-owned objects for project-level test cases plus workspace-level plans and runs. Reuse the existing Issue + Agent Session + Worker + Evidence path for execution. Codex only produces proposal/result artifacts; the server validates them, and humans approve canonical state changes.

**Key decisions**:

- Cases belong to Projects because execution depends on code, runbooks, environments, and versions.
- Case refinement must go through Case Suggestions review so AI output does not pollute the canonical library.
- Test Run Items store per-case result state; Issues store collaboration and evidence.
- Support functional, UI, API, and deployment as case types, while keeping specialized execution harnesses behind the shared Issue/Worker loop.
- Formal Environment scheduling and parallel execution are important, but they belong in Phase 4 so the first version stays shippable.

**Most fragile assumption**: the existing worker-backed `agent_session` path is stable enough to support case refinement and functional test execution. If this assumption fails, ship only the Phase 1 case library first and delay Phases 2 and 3 until the worker/session loop is stable.

After approval, implementation can start with: `implement this plan`.
