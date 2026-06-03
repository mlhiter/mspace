Packet ID: P2
Objective: Add an understandable Excel import path to the Tests import dialog.
Context: Current dialog is paste-only and users need visible guidance for accepted formats.
Files / sources: `packages/views/src/tests-page.tsx`, `packages/core/src/types.ts`, `packages/i18n/src/index.ts`.
Ownership: Renderer state, file selection, base64 conversion, localized copy, and submit gating.
Do: Show a file picker in Excel mode and keep paste-based modes unchanged.
Do not: Store imported file content in renderer persistence or hide file read errors.
Expected output: Users can select an `.xlsx` workbook and submit through the existing API.
Verification: `pnpm typecheck` and `pnpm --filter @mspace/desktop build`.
