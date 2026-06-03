Packet ID: P1-discovery

Accepted:
- `packages/views/src/tests-page.tsx` uses real control-plane APIs for list, create, import, plans, proposals, and runs.
- Existing product modal patterns use a fixed overlay, quiet paper surface, `role="dialog"`, close button, and backdrop click.
- Existing test-case detail route remains useful for editing, findings, and revisions.

Rejected:
- Full new route for quick create as the list toolbar primary behavior.
- Backend import preview, because the current API only supports submit-time import.

Decisions:
- Use local modal components in the Tests page to avoid broad shared UI scope.
- Keep detail routes for existing case editing and deep links.
