CREATE INDEX IF NOT EXISTS idx_test_plans_workspace_updated
  ON test_plans(workspace_id, updated_at DESC, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_test_plan_cases_workspace_plan_order
  ON test_plan_cases(workspace_id, plan_id, sort_order ASC);

CREATE INDEX IF NOT EXISTS idx_test_plan_cases_workspace_project_plan
  ON test_plan_cases(workspace_id, project_id, plan_id);

CREATE INDEX IF NOT EXISTS idx_test_runs_workspace_updated
  ON test_runs(workspace_id, updated_at DESC, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_test_runs_workspace_source_updated
  ON test_runs(workspace_id, source, updated_at DESC, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_test_runs_workspace_plan_updated
  ON test_runs(workspace_id, plan_id, updated_at DESC, created_at DESC)
  WHERE plan_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_test_run_items_workspace_run_status
  ON test_run_items(workspace_id, run_id, status);

CREATE INDEX IF NOT EXISTS idx_test_run_items_workspace_project_case
  ON test_run_items(workspace_id, project_id, case_id);

CREATE INDEX IF NOT EXISTS idx_test_artifacts_workspace_run_created
  ON test_artifacts(workspace_id, run_id, created_at DESC);
