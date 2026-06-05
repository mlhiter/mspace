ALTER TABLE test_plans
  ADD COLUMN IF NOT EXISTS setup_steps TEXT NOT NULL DEFAULT '';

ALTER TABLE test_runs
  ADD COLUMN IF NOT EXISTS setup_steps TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS setup_status TEXT NOT NULL DEFAULT 'not_required',
  ADD COLUMN IF NOT EXISTS setup_issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS setup_session_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS setup_result JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS run_context JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE test_runs DROP CONSTRAINT IF EXISTS test_runs_status_check;
ALTER TABLE test_runs
  ADD CONSTRAINT test_runs_status_check
  CHECK (status IN ('queued', 'setup_running', 'setup_failed', 'running', 'needs_acceptance', 'accepted', 'blocked'));

ALTER TABLE test_runs DROP CONSTRAINT IF EXISTS test_runs_setup_status_check;
ALTER TABLE test_runs
  ADD CONSTRAINT test_runs_setup_status_check
  CHECK (setup_status IN ('not_required', 'queued', 'running', 'passed', 'failed', 'skipped'));

ALTER TABLE test_runs DROP CONSTRAINT IF EXISTS test_runs_setup_result_check;
ALTER TABLE test_runs
  ADD CONSTRAINT test_runs_setup_result_check
  CHECK (jsonb_typeof(setup_result) = 'object');

ALTER TABLE test_runs DROP CONSTRAINT IF EXISTS test_runs_run_context_check;
ALTER TABLE test_runs
  ADD CONSTRAINT test_runs_run_context_check
  CHECK (jsonb_typeof(run_context) = 'object');

CREATE INDEX IF NOT EXISTS idx_test_runs_setup_issue
  ON test_runs(workspace_id, setup_issue_id)
  WHERE setup_issue_id IS NOT NULL;
