# mspace Test Module Product And Implementation Plan

> Status: product and implementation plan draft  
> Date: 2026-06-01  

## Conclusion

mspace should add a test module, but it should not become a standalone test management platform. The right product shape is a project-level test case library, test plans, test runs, and issue-backed execution.

The test module owns what to test, how to test it, which version is under test, which environment is used, and what the result was. Actual execution should still go through the existing mspace chain: Issue, Agent Session, Worker, Evidence, and Issue Test Environment. This keeps testing inside the product's current source of truth instead of creating a side channel.

In short:

```text
The test module owns cases and plans.
Issues own execution and collaboration.
Workers and Codex perform the actual tests.
Evidence makes the result inspectable and trustworthy.
```

## New Requirements From The Updated Draft

The updated workflow adds or clarifies these requirements:

- A test plan can be associated with a formal versioned test cluster environment.
- A test environment is more than a URL. It can include machine details, login method, cluster, component versions, branch, image, offline package, or commit.
- One test cluster can connect to multiple Codex workers and multiple Chrome CDP services so different cases can run in parallel.
- Test plans support multiple rounds: full run, failed-case retry, blocked-case retry, incremental test, and self-test.
- Some cases need mock data setup.
- Test types include functional tests, UI tests, API tests, and deployment tests.
- UI tests need browser/CDP support. Deployment tests may need SSH or commands similar to `sealos run`.
- Test results need screenshots or other evidence.
- Human acceptance is required. A Codex-completed run should not automatically mean a release is accepted.

## Product Positioning

The test module is not another Jira or TestRail. It is the quality layer of the existing mspace agent workspace.

It solves four concrete problems:

1. Where test cases come from: QA input, rough list import, repository analysis, and project runbook analysis.
2. Whether a case is executable: clear steps, explicit expectations, complete dependencies, and correct ordering.
3. How a version is tested: create a plan for an RC, commit, branch, image, offline package, or preview environment.
4. Why the result can be trusted: Codex returns status, screenshots, logs, commands, resources, failure summaries, and human acceptance back to the Issue.

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
- Direct writes from Codex-generated cases into the canonical test case library.
- Treating Codex pass results as release approval without human acceptance.

## Core Object Model

### Test Case

A test case is a project-level object.

This is intentional. A useful case depends on project code, the project runbook, runtime environment, branch, and version. Without a Project, a case can exist only as a draft and cannot enter execution.

Recommended fields:

- workspace
- project
- title
- type: expose only `functional` in the first phase; extend later to `ui`, `api`, and `deployment`
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
- quality score
- quality findings
- creator and updated time

The quality score should start as a deterministic rule set rather than an AI judgment. Examples:

- missing preconditions lower the score;
- missing expected result lowers the score;
- vague one-line steps lower the score;
- phrases such as "verify it works" or "check whether it is normal" lower the score;
- missing environment requirements lower the score;
- highly similar title or steps compared with existing cases lowers the score.

### Test Case Revision

Every manual edit or accepted Codex improvement creates a revision.

This gives the product two important capabilities:

- test knowledge can be rolled back;
- users can compare before and after when Codex refines a case.

### Test Case Proposal

Codex does not directly modify canonical cases. It returns proposals.

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

The user accepts or rejects these proposals in a Proposal Review surface.

### Test Environment

A test environment is a key part of whether a plan can run. It should not be represented as only a URL.

Use two layers:

```text
Project Test Environment Template
  -> long-lived project-level environment template

Test Run Environment Snapshot
  -> frozen environment state for one test run
```

The environment template can include:

- cluster;
- kubeconfig/context;
- registry prefix;
- preview domain;
- ingress class;
- node host;
- runtime machine;
- login method notes;
- component update method;
- mock data setup notes;
- default worker capability requirements.

The run snapshot should freeze:

- cluster used by the plan;
- namespace;
- branch;
- commit;
- image;
- offline package or version URL;
- preview URL;
- environment creation time;
- linked Issue Test Environment;
- current environment state.

In the first phase, mspace can reuse the existing `clusters` and `issue_test_environments` surfaces. It should not introduce heavy environment orchestration yet. The "formal test cluster" concept can first appear as part of Test Plan and Test Run environment snapshots.

### Test Plan

A test plan answers: which version are we testing, which cases are included, which environment is used, and which round is this?

Recommended fields:

- project
- title, for example `rc4 functional test plan`
- target type: branch, commit, source session, image, offline package, version URL, or preview URL
- target value
- linked test environment
- selected cases
- case order and dependencies
- current round
- status: `draft`, `ready`, `running`, `needs_acceptance`, `completed`, `archived`
- creator

### Test Run

One plan can have multiple runs.

Typical rounds:

- Round 1: full run;
- Round 2: failed and blocked cases only;
- Round 3: incremental cases affected by the current commit;
- Self-test: a developer or Codex runs a smaller high-risk set before formal testing.

A Test Run stores:

- round number;
- linked Test Plan;
- linked parent Issue;
- linked environment snapshot;
- status;
- passed, failed, blocked, and skipped counts;
- pass rate;
- start and completion time;
- human acceptance status.

### Test Run Item

A Test Run Item is one case result inside one run.

It stores:

- linked Test Case;
- linked execution Issue;
- linked Agent Session;
- status: `queued`, `running`, `passed`, `failed`, `blocked`, `skipped`, `needs_human_review`;
- actual result;
- failure summary;
- screenshot, log, command, or resource evidence;
- whether human review is required.

## Core Product Workflows

### 1. Import Or Create Test Cases

The user enters `Tests -> Cases`, selects a project, then can:

- paste a rough test list;
- import Markdown;
- import CSV;
- create a case manually;
- generate suggestions from the repository and runbook.

The system does three things:

1. converts rough input into structured draft cases;
2. computes quality scores;
3. highlights missing information and likely duplicates.

Output:

```text
Rough case list
  -> structured drafts
  -> quality evaluation
  -> human review and adjustment
  -> ready test case library
```

### 2. Refine Test Cases With Codex

The user selects draft or low-quality cases and clicks `Optimize`.

mspace should not call Codex from the server. It should create a normal issue-backed Agent Session:

```text
Tests page
  -> create or reuse an "optimize test cases" Issue
  -> attach selected cases, project runbook, and code context to the session context
  -> server creates an agent_session runtime task
  -> worker starts codex app-server
  -> Codex writes test-case-proposals.json
  -> server validates proposals
  -> human accepts or rejects them in Proposal Review
```

This keeps the optimization auditable:

- the Codex run has an Issue;
- logs, failures, and context are inspectable;
- the server remains Codex-free;
- humans control canonical test knowledge.

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

- generate baseline functional cases for the whole project;
- generate incremental cases for the current change.

### 4. Create A Test Plan

The user selects `ready` cases and creates a test plan.

The plan needs:

- title;
- target: branch, commit, source session, image, offline package, version URL, or preview URL;
- environment: existing Issue Test Environment, project default cluster, or a formal test cluster environment;
- execution scope: full, failed retry, blocked retry, incremental, or custom;
- whether parallel execution is allowed;
- whether human acceptance is required.

### 5. Enter Issue-Backed Execution

When a test run starts, mspace creates one parent Issue for the run and execution Issues for batches.

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

### 6. Codex Performs The Actual Test

For the first functional-testing phase, Codex can use:

- project code;
- project runbook;
- test cases;
- test environment snapshot;
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

### 7. Human Acceptance

When a run finishes, it should move to `needs_acceptance`. It should not automatically become release-approved.

The reviewer needs to see:

- pass rate;
- failed and blocked cases;
- Issue and Evidence links for each failure;
- screenshots or logs;
- whether retry is needed;
- whether a defect Issue should be created;
- whether the run is accepted.

Human actions:

- accept the run;
- rerun failed items;
- mark as blocked;
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
  -> human acceptance
```

### UI Tests: Later Phase

UI tests require workers with `browser` or `chrome_cdp` capability.

Future model:

```text
Test Run Item
  -> requires browser capability
  -> server routes only to workers with chrome_cdp capability
  -> worker connects to Chrome CDP
  -> worker returns screenshots, DOM state, and failed step details
```

Screenshots should not be stored in renderer-local storage. The first target is session artifacts. Later, server-owned attachments can make this more durable.

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

> One plan is associated with a formal test cluster environment. Each test cluster maps to N Codex workers and N Chrome CDP services, and each Codex worker tests a different case.

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

Test Plan or Test Environment defines:

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

### Tests / Cases

Purpose: manage project test cases.

Main actions:

- filter by Project;
- import;
- create;
- batch optimize;
- generate from project;
- filter by quality score;
- filter by status;
- filter by type, area, and priority;
- add to test plan.

List columns:

- title;
- type;
- area;
- priority;
- status;
- quality score;
- latest run result;
- updated time.

### Case Detail

Display the case as a document:

- title;
- preconditions;
- steps;
- expected result;
- dependencies;
- historical runs;
- revisions;
- related Issues.

### Proposal Review

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

### Tests / Plans

Purpose: define a test scope for a version, branch, or environment.

Show:

- plan title;
- target under test;
- linked environment;
- case count;
- current round;
- status;
- latest pass rate;
- human acceptance status.

### Tests / Runs

Purpose: inspect each test run's progress and result.

Show:

- current status;
- pass rate;
- failed, blocked, and skipped counts;
- execution Issues;
- Agent Sessions;
- Evidence;
- environment;
- screenshots/logs;
- human acceptance actions.

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

Later:

- `test_run_environment_snapshots`
- `test_artifacts`, if session artifacts and issue attachments are not enough;
- `test_worker_capability_requirements`

### Recommended New APIs

Phase 1:

```text
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-cases
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-cases
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}
PUT    /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/optimize
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/generate
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals/{proposalID}/apply
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals/{proposalID}/reject
```

Phase 2:

```text
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-plans
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-plans
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}
PUT    /api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}/runs
GET    /api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}
POST   /api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/retry
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

### Phase 1: Functional Test Case Library

Goal: let users manage test cases first.

Scope:

- add `Tests` navigation;
- case list;
- case detail;
- Markdown, CSV, and paste import;
- manual create and edit;
- case quality score;
- case revisions;
- English and Simplified Chinese i18n;
- server API and Postgres migration.

This phase does not depend on Codex or workers.

Acceptance:

- user can select a Project;
- user can import a rough test list;
- system creates structured draft cases;
- quality score and missing fields are visible;
- user can edit a case;
- revision history exists.

### Phase 2: Codex Case Generation And Refinement

Goal: let Codex improve cases while humans approve the final writes.

Scope:

- optimize selected cases;
- generate baseline functional cases from a project;
- create traceable optimization Issues;
- run Codex through the existing worker path;
- parse `test-case-proposals.json`;
- Proposal Review page;
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

- test plan list and detail;
- target selection: branch, commit, source session, image, offline package, version URL, or preview URL;
- environment selection;
- create Test Run;
- create parent Issue and execution Issues;
- start Codex sessions for execution Issues;
- parse `test-result.json`;
- update Test Run Items;
- show pass rate, failed, blocked, and skipped items;
- retry failed or blocked items;
- support human acceptance.

Acceptance:

- user can create a plan such as `rc4 functional test plan`;
- user can select ready functional cases;
- user can start a test run;
- parent Issue and execution Issues are created;
- Codex sessions run through the normal worker path;
- results are written back to Test Run Items;
- failed items can be retried or turned into defect Issues;
- human acceptance status is saved.

### Phase 4: Formal Test Environment And Parallel Scheduling

Goal: let one formal test cluster environment host one plan while multiple workers execute in parallel.

Scope:

- test environment template;
- test run environment snapshot;
- worker capability routing;
- concurrency limits;
- dependency ordering;
- write-operation mutex for shared environments;
- incremental test selection;
- self-test rounds.

Acceptance:

- a plan can bind to a formal test cluster;
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
- richer artifact and attachment storage.

Acceptance:

- different test types route to workers by capability;
- UI tests preserve screenshots;
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
- require human acceptance for important runs;
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

- first version is functional testing only;
- UI/CDP, API, and deployment testing are later phases;
- prove one real end-to-end loop first: case -> plan -> Issue -> Codex -> evidence -> human acceptance.

## Recommended MVP

The smallest useful version has three parts:

1. Case library: import, create, edit, and score cases.
2. Case refinement: Codex generates proposals, humans accept them.
3. Functional test plan: select cases, create a run, generate Issues, execute through Codex, collect results, and complete human acceptance.

This MVP covers the main workflow from the source draft:

```text
rough case list
  -> structured by system
  -> refined by Codex
  -> reviewed by human
  -> final case list
  -> test plan
  -> assigned to Codex
  -> result and screenshot/evidence
  -> human acceptance
```

Formal test clusters, multiple Codex/Chrome CDP workers, deployment tests, and multi-machine scheduling should come after the first functional loop works.

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
2. Import a rough test case list.
3. Confirm structured draft cases are created.
4. Confirm quality scores and missing-field findings are reasonable.
5. Edit one case.
6. Confirm revision history exists.

Phase 2 manual acceptance:

1. Select a few low-quality cases.
2. Start Codex optimization.
3. Confirm an Issue and Agent Session are created.
4. Confirm Proposal Review shows before/after differences.
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
9. Confirm human acceptance status can be saved.

## Design Summary

**Building**: a project-level test module that imports, creates, and refines test cases, creates test plans, assigns cases to Codex through Issues, and stores results, evidence, failures, screenshots, and human acceptance state.

**Not building**: the first version does not include a generic test platform, UI/CDP/SSH/deployment orchestration, server-side Codex, execution outside Issues, or direct Codex writes into canonical cases.

**Approach**: add server-owned objects for test cases, plans, and runs. Reuse the existing Issue + Agent Session + Worker + Evidence path for execution. Codex only produces proposal/result artifacts; the server validates them, and humans approve canonical state changes.

**Key decisions**:

- Cases belong to Projects because execution depends on code, runbooks, environments, and versions.
- Case refinement must go through Proposal Review so AI output does not pollute the canonical library.
- Test Run Items store per-case result state; Issues store collaboration and evidence.
- Start with functional tests, then add UI/API/deployment tests after the loop works.
- Formal test clusters and parallel scheduling are important, but they belong in Phase 4 so the first version stays shippable.

**Most fragile assumption**: the existing worker-backed `agent_session` path is stable enough to support case refinement and functional test execution. If this assumption fails, ship only the Phase 1 case library first and delay Phases 2 and 3 until the worker/session loop is stable.

After approval, implementation can start with: `implement this plan`.
