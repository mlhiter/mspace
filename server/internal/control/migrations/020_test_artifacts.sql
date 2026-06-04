CREATE TABLE IF NOT EXISTS test_artifacts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id UUID NOT NULL REFERENCES test_runs(id) ON DELETE CASCADE,
  run_item_id UUID NOT NULL REFERENCES test_run_items(id) ON DELETE CASCADE,
  case_id UUID NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
  source_issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
  source_task_id UUID REFERENCES runtime_tasks(id) ON DELETE SET NULL,
  source_session_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind IN ('screenshot', 'trace', 'log', 'dom_snapshot', 'network', 'resource', 'other')),
  role TEXT NOT NULL DEFAULT 'evidence' CHECK (role IN ('thumbnail', 'original', 'evidence')),
  filename TEXT NOT NULL CHECK (char_length(btrim(filename)) > 0),
  content_type TEXT NOT NULL CHECK (content_type <> ''),
  size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
  sha256 TEXT NOT NULL CHECK (char_length(sha256) = 64),
  storage_backend TEXT NOT NULL DEFAULT 'postgres_blob',
  storage_key TEXT NOT NULL DEFAULT '',
  content BYTEA NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(run_item_id, sha256)
);

CREATE INDEX IF NOT EXISTS idx_test_artifacts_run_created
  ON test_artifacts(workspace_id, project_id, run_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_test_artifacts_run_item_kind
  ON test_artifacts(workspace_id, project_id, run_item_id, kind, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_test_artifacts_case_created
  ON test_artifacts(workspace_id, project_id, case_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_test_artifacts_source_session
  ON test_artifacts(workspace_id, source_session_id)
  WHERE source_session_id <> '';

CREATE INDEX IF NOT EXISTS idx_test_artifacts_source_task
  ON test_artifacts(source_task_id)
  WHERE source_task_id IS NOT NULL;
