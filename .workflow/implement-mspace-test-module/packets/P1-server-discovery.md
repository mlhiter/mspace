# Packet P1: Server Discovery

## Objective

Identify backend insertion points for a Phase 1 project-scoped test case library.

## Result

Completed by explorer subagent. Accepted findings:

- Store interface belongs in `server/internal/control/types.go`.
- HTTP routes should sit near project/runbook routes in `server/internal/control/http.go`.
- MemoryStore needs maps for test cases and revisions.
- SQLite persistence needs snapshot fields because SQLite wraps MemoryStore snapshots.
- Postgres needs a new migration and store methods.
- Phase 1 must stay independent of Codex, workers, plans, and runs.
