# Orchestration: Implement mspace test module

## Execution Rules

- Keep the original objective intact.
- Ask for approval before risky, expensive, external, or destructive actions.
- Keep immediate blocking work local.
- Delegate only bounded, disjoint, materially useful packets.
- Integrate packet results before final verification.

## Branching Rules

- If backend implementation grows beyond Phase 1, stop at the case library and document follow-up.
- If Postgres or MemoryStore behavior conflicts, prefer the server-owned API contract and make MemoryStore mirror Postgres behavior.
- If i18n key volume becomes too large, keep Phase 1 copy minimal but complete.
- If tests reveal unrelated pre-existing failures, record them separately and do not hide them.

## Packet Prompts

### P1 Server Discovery

Inspect `server/internal/control` for Store, MemoryStore, PostgresStore, migrations, HTTP handlers, and test conventions. Do not edit files. Report exact insertion points.

### P2 Frontend Discovery

Inspect `packages/core`, `packages/views`, `packages/ui`, `packages/i18n`, and `apps/desktop` for route/API/navigation/i18n patterns. Do not edit files. Report exact insertion points.

### P3 Backend Implementation

Add Phase 1 test case library state and routes. Keep all data server-owned. Add deterministic quality scoring and import parsing. Add tests.

### P4 Frontend Implementation

Add core types/API, Tests page, route, navigation, and localized copy. Keep UI quiet and project-scoped.

### P5 Integration

Run verification, integrate explorer results, document accepted/rejected decisions and remaining risks.

## Completion Audit

- [ ] Workflow artifacts complete.
- [ ] Backend routes implemented and tested.
- [ ] Frontend route/navigation implemented.
- [ ] i18n keys added in both locales.
- [ ] Verification run and recorded.
