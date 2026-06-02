# Orchestration: Implement mspace test module phases 2 and 3

## Execution Rules

- Keep the original objective intact.
- Ask for approval before risky, expensive, external, or destructive actions.
- Keep immediate blocking work local.
- Delegate only bounded, disjoint, materially useful packets.
- Integrate packet results before final verification.

## Branching Rules

## Packet Prompts

## Completion Audit
# Orchestration

## Sequence

1. Update the workflow artifact with Phase 2/3 success criteria and constraints.
2. Run two read-only explorer packets in parallel:
   - backend/runtime integration;
   - frontend/product integration.
3. Implement the server contract first:
   - proposal/plan/run types;
   - Postgres migration;
   - MemoryStore + SQLite snapshot;
   - HTTP routes;
   - runtime artifact reconciliation.
4. Extend worker artifact pickup so `test-case-proposals.json` and `test-result.json` travel through the existing runtime result payload.
5. Add core TypeScript types, query keys, and API wrappers.
6. Extend `/tests` with compact tabs for Cases, Proposals, Plans, and Runs.
7. Verify and record evidence.

## Branching Rules

- If Codex worker availability is missing, optimize/generate/run creation must fail with the existing 409 `no active codex worker` behavior instead of creating orphaned canonical writes.
- If an artifact is malformed or references another project/workspace, store no canonical case changes and leave a validation failure on proposals/run items.
- If full Phase 3 scope threatens Phase 4 features, stop at issue-backed runs and human acceptance.

## Packet Prompts

See `packets/`.
