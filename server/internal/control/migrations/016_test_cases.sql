CREATE TABLE IF NOT EXISTS test_cases (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title TEXT NOT NULL CHECK (char_length(btrim(title)) > 0),
  type TEXT NOT NULL DEFAULT 'functional' CHECK (type IN ('functional')),
  area TEXT NOT NULL DEFAULT '',
  priority TEXT NOT NULL DEFAULT '' CHECK (priority IN ('', 'p0', 'p1', 'p2', 'p3')),
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'needs_review', 'ready', 'archived')),
  source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'import', 'codex_generated', 'codex_refined')),
  preconditions TEXT NOT NULL DEFAULT '',
  steps JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(steps) = 'array'),
  expected_result TEXT NOT NULL DEFAULT '',
  environment_requirements TEXT NOT NULL DEFAULT '',
  dependencies JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(dependencies) = 'array'),
  tags JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(tags) = 'array'),
  quality_score INTEGER NOT NULL DEFAULT 0 CHECK (quality_score >= 0 AND quality_score <= 100),
  quality_findings JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(quality_findings) = 'array'),
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_test_cases_project_updated
  ON test_cases(workspace_id, project_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_test_cases_project_status
  ON test_cases(workspace_id, project_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS test_case_revisions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  case_id UUID NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
  author_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  revision_number INTEGER NOT NULL CHECK (revision_number > 0),
  snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(case_id, revision_number)
);

CREATE INDEX IF NOT EXISTS idx_test_case_revisions_case_created
  ON test_case_revisions(workspace_id, project_id, case_id, created_at DESC);
