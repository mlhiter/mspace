Packet ID: backend
Objective: Support test runs from selected cases without requiring a plan.
Ownership: server/internal/control, server migrations.
Expected output: schema/API/store/tests for ad-hoc runs.
Verification: go test ./internal/control, pnpm test:server.
