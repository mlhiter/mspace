# Implement mspace test module

## Goal

Implement the first independently shippable slice of `docs/test-module-plan.md`: a server-owned, project-scoped functional test case library for mspace, with import, create, edit, deterministic quality scoring, revision history, desktop navigation, and a basic Tests UI.

## Success Criteria

- Workflow artifacts exist under `.workflow/implement-mspace-test-module`.
- Phase 1 backend state is server-owned and works for MemoryStore, Postgres migrations, and SQLite snapshot persistence.
- Authenticated workspace users can list, import, create, update, and read project test cases.
- Test case quality findings are deterministic and returned by the API.
- Frontend has a `Tests` route in navigation after Issues.
- Tests UI can select a project, show cases, import rough lists, create/edit cases, and inspect revisions.
- Visible product copy uses `@mspace/i18n`.
- Verification covers backend tests plus TypeScript/build checks where feasible.

## Current Context

- `docs/test-module-plan.md` defines a multi-phase feature. Phase 1 is explicitly independent and does not require Codex or workers.
- mspace product truth belongs in `server/`; renderer-local product storage is forbidden.
- Existing desktop routes are registered in `apps/desktop/src/renderer/src/main.tsx`.
- Navigation lives in `packages/ui/src/index.tsx`.
- Core client types/API live in `packages/core/src`.
- Main workflow pages live in `packages/views/src`.

## Constraints

- Do not run database writes against live/prod databases.
- Do not move Codex credentials or execution into the server.
- Do not implement Phase 2/3 runtime execution in this pass.
- Preserve existing unrelated worktree changes.
- Keep the UI document-first and operational, not dashboard-heavy.

## Risks

- This touches more than 8 files and introduces a migration.
- Store interface expansion requires all store implementations to compile.
- i18n key parity can break if English and Simplified Chinese resources diverge.
- Large pasted imports can create noisy/low-quality cases, so import must stay bounded.

## Approval Required

No external writes, destructive git operations, deployments, or live migrations are planned. Local file edits and local verification are safe to proceed.

## Work Packets

- P1 server discovery: identify exact backend insertion points and tests.
- P2 frontend discovery: identify exact route/API/navigation/i18n insertion points.
- P3 backend implementation: types, migration, MemoryStore/PostgresStore/SQLite persistence, HTTP routes, tests.
- P4 frontend implementation: core types/API, Tests page, navigation, route, i18n.
- P5 integration and verification: run tests/builds, reconcile explorer findings, update workflow report.

## Integration Policy

- Phase 1 is the implementation boundary.
- If explorer findings conflict with local implementation, inspect authoritative source before choosing.
- Do not paste explorer output directly; synthesize accepted/rejected decisions into `final-report.md`.

## Verification

- `pnpm typecheck`
- `pnpm --filter @mspace/desktop build`
- `pnpm test:server`
- `go test ./...` in `server` if the repo layout supports it
- `git diff --check`
- workflow completeness script

## Reusable Artifacts

- Keep `.workflow/implement-mspace-test-module/*` as a future recipe for large product-module implementation.
