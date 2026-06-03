# Test case Excel import

## Goal
Add Excel import support to the Tests module's existing project test case import workflow.

## Success Criteria
- Users can choose an `.xlsx` file from the import dialog.
- The renderer sends the file through the existing test-case import API instead of introducing a separate persistence path.
- The server parses the first non-empty worksheet with the same header contract used by CSV.
- Imported cases continue to run through the existing test-case normalization, source, quality, and max-count rules.
- UI copy is localized in `@mspace/i18n` and explains the accepted Excel columns.
- Documentation, changelog, and verification evidence are updated.

## Current Context
- The server already supports `markdown`, `text`, and `csv` import formats through `POST /api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import`.
- CSV import maps `title`, `area`, `priority`, `preconditions`, `steps`, `expected_result`, `environment_requirements`, and `tags`.
- The Tests import dialog currently accepts pasted text only.
- Product memory says test cases stay project-owned objects and Issues remain the execution boundary.

## Constraints
- Keep storage and database write behavior unchanged beyond the existing import API.
- Do not add renderer-owned stores or sidecar APIs.
- Keep visible Tests copy locale-aware through `@mspace/i18n`.
- Keep the UI quiet and operational, with user-facing requirements made explicit.
- Do not revert unrelated dirty worktree changes.

## Risks
- Excel parsing can expand the accepted request size, so the API needs a conservative limit.
- Spreadsheet rows can be irregular; parsing must tolerate short rows and skip rows without titles.
- Excel content should not bypass existing per-case validation.

## Approval Required
None. This is a local code/docs/test change and does not touch production, credentials, database writes, deploys, or destructive operations.

## Work Packets
- P1 backend contract: extend the import parser for base64 `.xlsx` content, reuse CSV header mapping, and add focused tests.
- P2 frontend UX: add Excel to the format selector, file selection, base64 conversion, validation, and localized copy.
- P3 documentation and verification: update docs/changelog/workflow artifacts and run targeted checks.

## Integration Policy
Accept only changes that keep the existing import endpoint as the single entrypoint and reuse existing normalization. Reject any separate Excel-only storage path or UI behavior that hides failed file reads.

## Verification
- `go test ./internal/control` from `server/`
- `pnpm typecheck`
- `pnpm --filter @mspace/desktop build`
- `git diff --check`
- `python3 /Users/mlhiter/.codex/skills/codex-dynamic-workflows/scripts/verify_workflow.py .workflow/test-case-excel-import`

## Reusable Artifacts
Keep the workflow notes as a recipe for future test-case import format additions.
