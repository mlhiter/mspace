# Orchestration: Test case UI guidance

## Execution Rules

- Keep the original objective intact.
- Ask for approval before risky, expensive, external, or destructive actions.
- Keep immediate blocking work local.
- Delegate only bounded, disjoint, materially useful packets.
- Integrate packet results before final verification.

## Branching Rules

- If implementation requires server schema changes, stop and redesign.
- If TypeScript fails inside files touched by this packet, fix before final report.
- If TypeScript fails in unrelated dirty files, report the failure and run the narrowest useful follow-up check.

## Packet Prompts

- Product/UX packet: turn backend test case constraints into product-language readiness guidance.
- Implementation packet: update shared Tests case form and i18n only.
- Verification packet: run focused checks and summarize evidence.

## Completion Audit

- [x] Workflow files complete.
- [x] UI/i18n edits integrated.
- [x] Verification run and recorded.
