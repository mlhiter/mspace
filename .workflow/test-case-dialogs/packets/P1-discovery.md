Packet ID: P1-discovery

Objective:
Find the current Tests page implementation, data flow, and reusable dialog patterns.

Context:
User asked whether import and new case actions should be dialogs, then asked to handle it.

Files / sources:
- packages/views/src/tests-page.tsx
- packages/i18n/src/index.ts
- packages/views/src/projects-page.tsx
- packages/views/src/create-issue-modal.tsx

Ownership:
Read-only discovery.

Do:
- Identify whether test cases use real APIs or mock state.
- Identify existing modal visual and accessibility patterns.

Do not:
- Change backend storage or migrations.

Expected output:
Short discovery notes.

Verification:
Authoritative file references identified.
