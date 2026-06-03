# Packet C: Verification

## Objective

Prove the UI/i18n change does not break the current app type surface.

## Checks

- `git diff --check`
- `pnpm typecheck`
- workflow artifact validation

## Notes

If `pnpm typecheck` fails because of pre-existing dirty work unrelated to the touched form copy, capture the exact failure and do not hide it.
