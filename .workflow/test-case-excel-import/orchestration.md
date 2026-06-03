# Orchestration: Test case Excel import

## Execution Rules

- Keep the original objective intact.
- Ask for approval before risky, expensive, external, or destructive actions.
- Keep immediate blocking work local.
- Delegate only bounded, disjoint, materially useful packets.
- Integrate packet results before final verification.

## Branching Rules

## Packet Prompts

## Completion Audit
# Test Case Excel Import Orchestration

Goal:
Add `.xlsx` import to the Tests module without changing where test cases live or how imported cases are normalized.

Packets:

P1 Backend contract
- Inspect current import types, parser, limits, and tests.
- Add `xlsx` as an accepted format.
- Decode base64 content, open it with Excelize, find the first worksheet with rows, then feed rows into the same record-to-test-case mapper used by CSV.
- Keep the 100-case cap and row skipping behavior.
- Add a focused helper test that builds a small workbook in memory.

P2 Frontend UX
- Add Excel to the format selector.
- For Excel mode, show a file picker for `.xlsx` instead of a paste textarea.
- Convert the selected file to base64 for the existing import endpoint.
- Disable submit until the relevant input is present.
- Localize all visible text and guidance.

P3 Documentation and verification
- Update `docs/test-module-plan.md` and `apps/website/src/changelog.ts`.
- Record accepted decisions and checks in this workflow.
- Run targeted Go, TypeScript, build, diff, and workflow checks.

Branching rules:
- If Excelize cannot be added cleanly, stop at backend package research and report the blocker.
- If frontend build rejects file APIs, keep the API implementation and adjust the renderer conversion to browser-compatible primitives.
- Do not touch migrations or data model fields for this task.
