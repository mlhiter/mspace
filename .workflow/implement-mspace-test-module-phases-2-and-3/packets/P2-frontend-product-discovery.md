Packet ID: P2-frontend-product-discovery

Objective: Define the smallest coherent `/tests` UI/API expansion for Phase 2/3.

Context: `packages/views/src/tests-page.tsx` already contains Phase 1 Cases UI. Shared product copy belongs in `@mspace/i18n`.

Files / sources:
- `packages/views/src/tests-page.tsx`
- `packages/core/src/types.ts`
- `packages/core/src/api.ts`
- `packages/i18n/src/index.ts`
- `apps/desktop/src/renderer/src/main.tsx`
- `packages/ui/src/index.tsx`

Ownership: Read-only discovery.

Do:
- Recommend types, query keys, APIs, and page shape.

Do not:
- Add new top-level navigation.
- Add Phase 4/5 surfaces.

Expected output: Concise implementation guidance.

Verification: Main agent integrates guidance into UI changes.
