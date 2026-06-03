Packet ID: P2-implementation

Objective:
Implement modal dialogs for test-case import and quick creation while preserving the list context.

Context:
Tests page already has import/create APIs and detail routes. Existing working tree has broader unrelated changes, so edits stay narrow.

Files / sources:
- packages/views/src/tests-page.tsx
- packages/i18n/src/index.ts

Ownership:
UI and locale strings only.

Do:
- Replace inline import panel with dialog state.
- Replace list toolbar new-case route action with dialog state.
- Reuse case form fields between create dialog and detail page.
- Preserve existing detail route for editing and deep links.

Do not:
- Revert unrelated test-module route/detail changes already present.
- Add backend preview/import APIs.

Expected output:
Type-safe UI patch.

Verification:
Run @mspace/views typecheck and desktop renderer typecheck when possible.
