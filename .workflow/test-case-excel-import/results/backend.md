# P1 Backend Contract

Status: complete

Findings:
- Current import API already centralizes parsing in `parseImportedTestCases`.
- CSV header mapping is reusable for spreadsheet rows.
- Excelize official docs support `OpenReader(io.Reader)` and `GetRows(sheet)`.

Decisions:
- Accept `xlsx` and `excel` as API formats, normalize `excel` to `xlsx`.
- Treat `content` as base64-encoded workbook bytes for `xlsx`.
- Parse the first worksheet with at least one non-empty row.
- Reuse the CSV header mapping and normalization path for spreadsheet rows.
- Keep normal text/CSV content at 256 KiB and cap decoded workbook bytes at 2 MiB.

Implemented:
- Added Excelize-backed parsing in `server/internal/control/test_case_helpers.go`.
- Added `fileName` to `ImportTestCasesInput` for renderer/server contract clarity.
- Added HTTP test coverage with an in-memory workbook fixture.

Verification:
- `go test ./internal/control` passed.
