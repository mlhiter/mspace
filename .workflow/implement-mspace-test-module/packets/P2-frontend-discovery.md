# Packet P2: Frontend Discovery

## Objective

Identify route, navigation, API, type, and i18n insertion points for the Tests UI.

## Result

Completed by explorer subagent. Accepted findings:

- Add `TestsPage` under `packages/views/src/tests-page.tsx`.
- Export it from `packages/views/src/index.ts`.
- Register `/tests` in `apps/desktop/src/renderer/src/main.tsx`.
- Add a sidebar item after Issues in `packages/ui/src/index.tsx`.
- Extend `packages/core/src/types.ts` and `packages/core/src/api.ts`.
- Add visible copy through `@mspace/i18n` in both English and Simplified Chinese.

