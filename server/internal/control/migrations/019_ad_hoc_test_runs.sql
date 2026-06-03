ALTER TABLE test_runs
  ALTER COLUMN plan_id DROP NOT NULL;

ALTER TABLE test_runs
  ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'plan'
  CHECK (source IN ('ad_hoc', 'plan', 'retry', 'incremental'));

CREATE INDEX IF NOT EXISTS idx_test_runs_project_source_updated
  ON test_runs(workspace_id, project_id, source, updated_at DESC);
