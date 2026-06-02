CREATE TABLE IF NOT EXISTS test_case_proposals (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source_issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
  source_session_id TEXT NOT NULL DEFAULT '',
  target_case_id UUID REFERENCES test_cases(id) ON DELETE SET NULL,
  proposal_type TEXT NOT NULL CHECK (proposal_type IN ('create', 'update', 'archive')),
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'rejected', 'invalid')),
  title TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  rationale TEXT NOT NULL DEFAULT '',
  current_case JSONB,
  proposed_case JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(proposed_case) = 'object'),
  quality_score INTEGER NOT NULL DEFAULT 0 CHECK (quality_score >= 0 AND quality_score <= 100),
  quality_findings JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(quality_findings) = 'array'),
  validation_errors JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(validation_errors) = 'array'),
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  reviewed_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  applied_case_id UUID REFERENCES test_cases(id) ON DELETE SET NULL,
  review_note TEXT NOT NULL DEFAULT '',
  reviewed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_test_case_proposals_project_status
  ON test_case_proposals(workspace_id, project_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_test_case_proposals_source_session
  ON test_case_proposals(workspace_id, project_id, source_session_id);

CREATE TABLE IF NOT EXISTS test_plans (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title TEXT NOT NULL CHECK (char_length(btrim(title)) > 0),
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'ready', 'archived')),
  target_type TEXT NOT NULL DEFAULT 'branch',
  target_value TEXT NOT NULL DEFAULT '',
  environment TEXT NOT NULL DEFAULT '',
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_test_plans_project_updated
  ON test_plans(workspace_id, project_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS test_plan_cases (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  plan_id UUID NOT NULL REFERENCES test_plans(id) ON DELETE CASCADE,
  case_id UUID NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(plan_id, case_id)
);

CREATE INDEX IF NOT EXISTS idx_test_plan_cases_plan_order
  ON test_plan_cases(workspace_id, project_id, plan_id, sort_order ASC);

CREATE TABLE IF NOT EXISTS test_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  plan_id UUID NOT NULL REFERENCES test_plans(id) ON DELETE CASCADE,
  parent_issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'needs_acceptance', 'accepted', 'blocked')),
  target_type TEXT NOT NULL DEFAULT 'branch',
  target_value TEXT NOT NULL DEFAULT '',
  environment TEXT NOT NULL DEFAULT '',
  total_count INTEGER NOT NULL DEFAULT 0,
  passed_count INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0,
  blocked_count INTEGER NOT NULL DEFAULT 0,
  skipped_count INTEGER NOT NULL DEFAULT 0,
  acceptance_status TEXT NOT NULL DEFAULT 'pending' CHECK (acceptance_status IN ('pending', 'accepted', 'blocked')),
  acceptance_note TEXT NOT NULL DEFAULT '',
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  accepted_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  completed_at TIMESTAMPTZ,
  accepted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_test_runs_project_updated
  ON test_runs(workspace_id, project_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_test_runs_plan_updated
  ON test_runs(workspace_id, project_id, plan_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS test_run_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id UUID NOT NULL REFERENCES test_runs(id) ON DELETE CASCADE,
  case_id UUID NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
  execution_issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
  agent_session_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'passed', 'failed', 'blocked', 'skipped')),
  actual_result TEXT NOT NULL DEFAULT '',
  failure_summary TEXT NOT NULL DEFAULT '',
  evidence JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(evidence) = 'object'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(run_id, case_id)
);

CREATE INDEX IF NOT EXISTS idx_test_run_items_run_status
  ON test_run_items(workspace_id, project_id, run_id, status);

CREATE INDEX IF NOT EXISTS idx_test_run_items_execution_issue
  ON test_run_items(workspace_id, execution_issue_id)
  WHERE execution_issue_id IS NOT NULL;
