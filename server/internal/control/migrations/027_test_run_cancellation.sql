ALTER TABLE test_runs DROP CONSTRAINT IF EXISTS test_runs_status_check;
ALTER TABLE test_runs
  ADD CONSTRAINT test_runs_status_check
  CHECK (status IN ('queued', 'setup_running', 'setup_failed', 'running', 'needs_acceptance', 'accepted', 'blocked', 'cancelled'));

ALTER TABLE test_runs DROP CONSTRAINT IF EXISTS test_runs_setup_status_check;
ALTER TABLE test_runs
  ADD CONSTRAINT test_runs_setup_status_check
  CHECK (setup_status IN ('not_required', 'queued', 'running', 'passed', 'failed', 'skipped', 'cancelled'));

ALTER TABLE test_run_items DROP CONSTRAINT IF EXISTS test_run_items_status_check;
ALTER TABLE test_run_items
  ADD CONSTRAINT test_run_items_status_check
  CHECK (status IN ('queued', 'running', 'passed', 'failed', 'blocked', 'skipped', 'cancelled'));
