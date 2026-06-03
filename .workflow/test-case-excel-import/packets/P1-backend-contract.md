Packet ID: P1
Objective: Extend the existing server import contract so `.xlsx` workbooks can create project test cases.
Context: Current imports support Markdown, text, and CSV through the project test-case import API.
Files / sources: `server/internal/control/test_case_helpers.go`, `server/internal/control/types.go`, `server/internal/control/http.go`, `server/internal/control/http_test.go`, `server/go.mod`, `server/go.sum`.
Ownership: Server parsing, request limits, and HTTP test coverage.
Do: Reuse CSV column mapping and existing normalization. Preserve row skip behavior and max imported case count.
Do not: Add migrations, new stores, or a separate Excel-only endpoint.
Expected output: `.xlsx` import accepted by the existing endpoint.
Verification: `go test ./internal/control` and `pnpm test:server`.
