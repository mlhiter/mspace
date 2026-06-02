# Backend Implementation Result

## Accepted

- Added Phase 2/3 control-plane types for optimize/generate requests, proposals, plans, runs, run items, review inputs, and Codex artifact payloads.
- Added Postgres migration `017_test_module_workflow.sql` for `test_case_proposals`, `test_plans`, `test_plan_cases`, `test_runs`, and `test_run_items`.
- Implemented MemoryStore and Postgres store methods for proposal review, plan CRUD, run start/retry, and human accept/block review.
- Reused the existing Issue -> Agent Session -> runtime task path for optimization, generation, and test-run execution.
- Extended runtime reconciliation so completed agent sessions can persist `test-case-proposals.json` as reviewable proposals and `test-result.json` as run item results/counts.
- Kept server Codex-free: the server creates issues and sessions, while workers collect Codex artifacts.
- Extended the worker to attach `test-case-proposals.json` and `test-result.json` to agent session results.

## Rejected

- Did not let Codex artifacts write canonical test cases directly.
- Did not introduce a separate test execution engine or a renderer-local test store.
- Did not implement Phase 4/5 concerns such as CDP worker scheduling, SSH orchestration, or formal environment-template management.

## Verification

- `go test ./internal/control` passed from `server/`.
- `pnpm test:server` passed from the repo root.
- `(cd worker && go test ./...)` passed.
