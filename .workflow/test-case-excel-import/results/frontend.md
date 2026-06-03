# P2 Frontend UX

Status: complete

Decisions:
- Keep Markdown, text, and CSV as paste-based imports.
- Use a file picker only for Excel mode.
- Show the selected file name and a localized hint so users understand the column contract.

Implemented:
- Added Excel `.xlsx` to the Tests import format selector.
- Added file selection, `.xlsx` guard, base64 conversion, and submit gating.
- Added English and Simplified Chinese import guidance.

Verification:
- `pnpm typecheck` passed.
