# Backend result

- Added `source` and optional `planId` support for test runs.
- Added project-level `GET/POST /test-runs` routes.
- Added selected-ready-case run creation in MemoryStore and PostgresStore.
- Kept plan-based run route and issue-backed execution path intact.
- Added Postgres migration `019_ad_hoc_test_runs.sql`.
- Added HTTP and SQLite persistence coverage for planless runs.
