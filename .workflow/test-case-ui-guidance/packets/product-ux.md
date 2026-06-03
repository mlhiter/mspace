# Packet A: Product and UX

## Objective

Make test case format requirements understandable without showing users raw backend contract details.

## Result

The right product model is "runnable case checklist" rather than "format requirements". Users need to know what lets a case become Ready and enter a plan: title, preconditions, concrete steps, expected result, and environment requirements.

## Accepted

- Show a compact readiness panel inside the form.
- Keep `functional` as a phase note, not an editable API field.
- Explain status as workflow meaning: only Ready cases enter runnable plans.
- Explain import format in the import dialog, especially line-based import and CSV columns.

## Rejected

- Do not display `manual`, `import`, `codex_generated`, or `codex_refined` as rules users must understand.
- Do not copy exact server quality scoring into frontend validation.
