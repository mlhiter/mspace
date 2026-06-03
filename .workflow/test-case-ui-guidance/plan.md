# Test case UI guidance

## Goal

Improve the mspace Tests module so users understand what makes a test case valid and runnable without reading server enum constraints or backend error text.

## Success Criteria

- The create/edit case form explains the current functional-case scope in product language.
- The form shows which fields affect readiness before the user saves.
- Field hints and placeholders make title, priority, status, preconditions, steps, expected result, environment, and import format understandable.
- Existing server schema, API, persistence, and quality scoring remain unchanged.
- Copy is localized through `@mspace/i18n` in English and Simplified Chinese.
- Focused verification passes or any skipped check is documented.

## Current Context

- Repo: `/Users/mlhiter/personal-projects/mspace`.
- Existing dirty worktree includes broader Tests module route/detail changes in `packages/views/src/tests-page.tsx`, i18n copy in `packages/i18n/src/index.ts`, and docs updates.
- Test case server constraints already exist: title required, first-phase type is `functional`, priority/status/source enums, step shape, import formats, and deterministic quality findings.
- Product/design docs say the desktop UI should show user-facing meaning and hide raw implementation labels.

## Constraints

- Do not run database writes.
- Do not change server migrations, store contracts, API shapes, or quality scoring semantics.
- Preserve the quiet Notion-like mspace style.
- Visible product copy must be locale-aware through `@mspace/i18n`.
- Work with the existing dirty files instead of reverting unrelated changes.

## Risks

- Duplicating server scoring exactly in the frontend would drift over time.
- Over-explaining backend enum values would make the form feel like an API console.
- Existing dirty Tests page work may already be in progress, so edits should target the shared `CaseFormFields` abstraction.

## Approval Required

No extra approval required. The workflow performs local, non-destructive UI/i18n/docs edits only. No deploy, database writes, secrets, or external systems.

## Work Packets

- Packet A, Product/UX: translate backend requirements into user-facing readiness language.
- Packet B, Implementation: update shared Tests case form UI and localized copy.
- Packet C, Verification: run focused type/format checks and workflow validation.

## Integration Policy

Accept changes that make constraints visible as product guidance. Reject changes that expose raw storage enums as instructions, copy server scoring logic exactly, or alter persistence/API behavior.

## Verification

- `git diff --check`
- `pnpm typecheck`
- `python3 /Users/mlhiter/.codex/skills/codex-dynamic-workflows/scripts/verify_workflow.py .workflow/test-case-ui-guidance`
- If typecheck reveals unrelated dirty-worktree failures, document the blocker and run a narrower check if available.

## Reusable Artifacts

The workflow artifact itself documents the pattern: translate server validation into form-level readiness guidance without duplicating backend scoring.
