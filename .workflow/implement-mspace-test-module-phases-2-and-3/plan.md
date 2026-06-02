# Implement mspace test module phases 2 and 3

## Goal
Implement the next shippable slices from `docs/test-module-plan.md` Phase 2 and Phase 3:

- Codex can generate/refine project test cases through issue-backed agent sessions.
- Codex output is stored as reviewable proposals, never direct canonical case writes.
- Users can create test plans from ready cases, start issue-backed test runs, inspect run items, and save human acceptance decisions.

## Success Criteria
- Phase 2 APIs exist for optimize/generate, list proposals, apply, and reject.
- Optimization/generation creates a traceable Issue and normal Codex agent session when an active worker is present.
- `test-case-proposals.json` is validated and stored only as pending proposals; accepted proposals create/update/archive cases and write revisions.
- Phase 3 APIs exist for plan list/detail/create/update and run create/get/retry/accept/block.
- Starting a run creates a parent execution Issue plus child execution Issues and normal Codex sessions for each execution Issue.
- `test-result.json` updates run items, and invalid results do not corrupt run state.
- The desktop `/tests` page exposes Cases, Proposals, Plans, and Runs without adding a second top-level navigation surface.
- Verification covers server tests, typecheck, desktop build, diff whitespace, and workflow artifact completeness.

## Current Context
- Phase 1 is already in the working tree and provides project-scoped functional test cases, revisions, import, quality scoring, `/tests`, server APIs, Postgres migration `016_test_cases.sql`, MemoryStore, SQLite snapshot, core API/types, and i18n.
- `docs/test-module-plan.md` defines Phase 2/3 as proposal-gated Codex refinement and issue-backed functional test runs.
- The existing product path is Issue -> Agent Session -> server runtime task -> worker -> result/evidence reconciliation.

## Constraints
- Keep product truth in `server/`; no renderer-local product store.
- Keep the server Codex-free; the server can queue agent sessions but must not run Codex.
- Use Postgres migrations for shared deployments and SQLite snapshot fields for personal mode.
- Preserve all Phase 1 changes and do not revert unrelated user work.
- Visible product copy in desktop/view packages goes through `@mspace/i18n`.
- Keep UI quiet, document-first, and operational.

## Risks
- Artifact JSON from Codex may be malformed, too large, or reference cases/runs outside the project.
- Run creation can partially create issues/sessions if worker availability is not checked consistently.
- Large Phase 3 scope can drift into Phase 4 parallel scheduling or formal cluster environments.
- Frontend tabs can become dashboard-heavy if they expose implementation protocol details.

## Approval Required
No external writes, production migrations, destructive git operations, pushes, or deployments are planned. Local source edits and local tests are approved by the user's implementation request.

## Work Packets
- P1 Backend/runtime discovery: locate existing session, runtime task, artifact reconciliation, and store boundaries.
- P2 Frontend/product discovery: define smallest `/tests` expansion and core API/type changes.
- P3 Backend implementation: models, migrations, MemoryStore/Postgres store, HTTP routes, artifact validation/reconciliation, tests.
- P4 Frontend implementation: core types/API, i18n, Tests tabs for proposals/plans/runs.
- P5 Integration verification: run repo checks and workflow verifier.

## Integration Policy
- Treat server/store contracts as authoritative; UI follows backend JSON contracts.
- Use the existing `CreateIssue` and `CreateAgentSession` paths for Codex work.
- Accepted proposal writes must go through the same normalization and revision path as manual case edits.
- Test run execution issues are traceability records; per-case truth lives in `test_run_items`.

## Verification
- `go test ./internal/control` from `server/`
- `pnpm typecheck`
- `pnpm --filter @mspace/desktop build`
- `pnpm test:server`
- `git diff --check`
- `python3 /Users/mlhiter/.codex/skills/codex-dynamic-workflows/scripts/verify_workflow.py .workflow/implement-mspace-test-module-phases-2-and-3`

## Reusable Artifacts
- Keep `.workflow/implement-mspace-test-module-phases-2-and-3/` as the durable orchestration record.
