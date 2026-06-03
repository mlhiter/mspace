# Test case dialogs

## Goal
Move the Tests page's import-case and create-case workflows into modal dialogs so users keep the current project/list context.

## Success Criteria
- Import opens as a dialog from the Cases toolbar, submits existing import API, shows pending/error/success states, refreshes cases, and closes cleanly.
- New case opens as a dialog from the Cases toolbar, submits existing create API, refreshes cases, selects the created row, and preserves the existing detail route for editing/deep links.
- Dialogs match mspace product UI: quiet surface, existing shadcn-compatible primitives, accessible role/labels, backdrop/close/cancel behavior.
- Validation runs at least TypeScript check for the touched package/app surface.

## Current Context
- Product register, Notion-like quiet workspace UI.
- Existing `packages/views/src/tests-page.tsx` already has a real test module and server APIs for import/create/update.
- Existing working tree is dirty with broader unrelated mspace changes. Treat current files as user baseline and do not revert unrelated changes.

## Constraints
- No database writes during development.
- No backend/API/data-model change unless needed. Current request is interaction/UI only.
- Keep visible copy locale-aware through `@mspace/i18n`.

## Risks
- `tests-page.tsx` already has in-progress route/detail refactor changes, so edits must be narrow and avoid undoing that work.
- Adding a generic shared modal primitive could broaden scope. Prefer local modal shape matching existing product pages.

## Approval Required
None for local UI edits and local type/build checks.

## Work Packets
- P1 discovery: inspect current Tests page, routes, mutations, and modal patterns.
- P2 implementation: add create/import dialogs and shared form fields.
- P3 verification: run focused type/build checks and workflow artifact verification.

## Integration Policy
Accept changes that keep the list as the main browsing surface and use dialogs only for short transactional flows. Keep existing detail pages for inspection/editing.

## Verification
- `pnpm typecheck` or narrower available typecheck if full repo is too broad.
- Workflow artifact completeness check.

## Reusable Artifacts
No reusable recipe expected unless this becomes a repeated modalization workflow.
