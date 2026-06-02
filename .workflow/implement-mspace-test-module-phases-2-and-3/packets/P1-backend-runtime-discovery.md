Packet ID: P1-backend-runtime-discovery

Objective: Locate backend/runtime integration points for Phase 2 proposals and Phase 3 runs.

Context: Phase 1 test cases are already in the working tree. The implementation must follow the existing server-owned Issue -> Agent Session -> Runtime Task -> Worker path.

Files / sources:
- `docs/test-module-plan.md`
- `server/internal/control/types.go`
- `server/internal/control/http.go`
- `server/internal/control/postgres_collaboration_store.go`
- `server/internal/control/server_owned_runtime_store.go`
- `server/internal/control/memory_store.go`
- `worker/main.go`

Ownership: Read-only discovery.

Do:
- Identify store methods, HTTP handlers, artifact reconciliation hooks, and worker artifact readers.
- Note pitfalls.

Do not:
- Edit files.
- Propose a server-side Codex runner.

Expected output: Concise recommendations with file/function names.

Verification: Main agent integrates recommendations into implementation.
