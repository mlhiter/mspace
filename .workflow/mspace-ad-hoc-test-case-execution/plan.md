# mspace ad hoc test case execution

## Goal
Implement the approved Tests product change: users can start an issue-backed test run directly from one ready test case or a selected set of ready test cases, without first creating a test plan. Plan-based runs must continue to work.

## Success Criteria
- A project test run can be created from selected test case IDs through a server API that does not require `planID`.
- Existing plan-based run creation remains compatible and continues to create the same issue-backed execution sessions.
- Test run records expose their source (`ad_hoc`, `plan`, `retry`, future `incremental`) and can have an empty/nullable `planId`.
- The Tests UI offers direct execution from selected ready cases and from a single case detail, while keeping formal plan execution available.
- Runs list no longer depends on selecting a plan just to see run history.
- Docs and i18n describe Run as an execution record rather than a plan-only result.
- Verification covers server workflow tests, typecheck/build surfaces touched by the change, and workflow artifact completeness.

## Current Context
- Current UI blocks the Run stage on ready plans, matching the screenshot complaint.
- Current frontend `startRun` mutation accepts `TestPlan`; direct case execution has no command.
- Current server API only exposes `POST /test-plans/{planID}/runs`.
- Current Postgres migration has `test_runs.plan_id NOT NULL REFERENCES test_plans(id)`.
- There are already uncommitted related changes for workflow cards and worker preflight; preserve and build on them.

## Constraints
- Keep execution issue-backed through existing Issue -> Agent Session -> Worker -> Evidence path.
- Do not add server-side Codex execution or renderer-local storage.
- Do not run database write operations against any live database.
- Include Postgres migration for shared deployments and SQLite snapshot compatibility.
- Visible product copy must use `@mspace/i18n`.
- Preserve unrelated dirty files and do not revert existing edits.

## Risks
- Making `planId` optional can break code that loads plan details for every run.
- Runs listing by plan cannot be the only frontend data source after ad-hoc runs exist.
- Issue titles/bodies must stay clear enough that ad-hoc runs are auditable.
- Existing tests may assume `detail.Plan` is always populated.

## Approval Required
No further approval is required for local code edits and local verification. Do not deploy, push, run live database migrations, delete workdirs, or mutate external systems.

## Work Packets
- Backend packet: schema/API/store/helpers/tests for ad-hoc run creation and optional plan IDs.
- Frontend packet: API client/types/views/i18n for selected-case and single-case run actions plus run history.
- Docs packet: update durable product docs, README/API notes, and changelog if needed.
- Verification packet: server tests, typecheck/build, workflow verifier, closeout review.

## Integration Policy
Main agent integrates all changes in this workspace. Subagents are read-only explorers unless explicitly assigned a disjoint write scope later. Existing dirty changes are treated as prior work and preserved.

## Verification
- `pnpm typecheck`
- `pnpm --filter @mspace/desktop build`
- `pnpm test:server`
- `go test ./internal/control` from `server/`
- `git diff --check`
- `python3 /Users/mlhiter/.codex/skills/codex-dynamic-workflows/scripts/verify_workflow.py .workflow/mspace-ad-hoc-test-case-execution`

## Reusable Artifacts
Keep this workflow artifact as the run record. If the final pattern is useful, document the test-run source model in project docs rather than creating a generic recipe.
