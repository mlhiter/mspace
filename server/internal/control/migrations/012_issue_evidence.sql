CREATE TABLE IF NOT EXISTS deployment_evidence (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL DEFAULT '',
  cluster TEXT NOT NULL DEFAULT '',
  namespace TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  details TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_deployment_evidence_issue_created
  ON deployment_evidence(workspace_id, issue_id, created_at DESC);

CREATE TABLE IF NOT EXISTS session_review_evidence (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL DEFAULT '',
  source_session_id TEXT NOT NULL DEFAULT '',
  source_commit_sha TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '',
  agent_summary TEXT NOT NULL DEFAULT '',
  commands_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(commands_json) = 'array'),
  tests_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(tests_json) = 'array'),
  build_result_json JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(build_result_json) = 'object'),
  deployment_result_json JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(deployment_result_json) = 'object'),
  risks_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(risks_json) = 'array'),
  follow_ups_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(follow_ups_json) = 'array'),
  preview_url TEXT NOT NULL DEFAULT '',
  cluster TEXT NOT NULL DEFAULT '',
  namespace TEXT NOT NULL DEFAULT '',
  namespace_status TEXT NOT NULL DEFAULT '',
  cleanup_status TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_session_review_evidence_issue_updated
  ON session_review_evidence(workspace_id, issue_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS session_failures (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL DEFAULT '',
  phase TEXT NOT NULL DEFAULT 'unknown',
  status TEXT NOT NULL DEFAULT 'open',
  failed_command TEXT NOT NULL DEFAULT '',
  error_summary TEXT NOT NULL DEFAULT '',
  error_excerpt TEXT NOT NULL DEFAULT '',
  cluster TEXT NOT NULL DEFAULT '',
  namespace TEXT NOT NULL DEFAULT '',
  resource_kind TEXT NOT NULL DEFAULT '',
  resource_name TEXT NOT NULL DEFAULT '',
  evidence_id TEXT NOT NULL DEFAULT '',
  review_evidence_id TEXT NOT NULL DEFAULT '',
  retry_session_id TEXT NOT NULL DEFAULT '',
  continued_session_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_session_failures_issue_updated
  ON session_failures(workspace_id, issue_id, updated_at DESC);
