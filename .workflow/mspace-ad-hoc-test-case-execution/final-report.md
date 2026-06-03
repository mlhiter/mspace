# Final report

Status: implementation complete.

Accepted:
- Direct selected-case runs use project-level test-runs API.
- Formal plan runs continue through existing plan-scoped API.
- Run records now carry source and may omit planId for ad-hoc runs.
- Run Detail handles optional plan and Runs tab lists project-level history.

Rejected:
- Creating hidden temporary plans for single-case execution.

Verification:
- go test ./internal/control: passed
- pnpm typecheck: passed
- pnpm --filter @mspace/desktop build: passed
- pnpm test:server: passed
- git diff --check: passed
- workflow verifier: passed
