CREATE TABLE IF NOT EXISTS workspace_skills (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  slug TEXT NOT NULL CHECK (slug <> ''),
  name TEXT NOT NULL CHECK (name <> ''),
  description TEXT NOT NULL DEFAULT '',
  source_type TEXT NOT NULL DEFAULT 'custom' CHECK (source_type = 'custom'),
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  invocable BOOLEAN NOT NULL DEFAULT TRUE,
  current_revision_id UUID,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  deleted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_skills_workspace_slug_active
  ON workspace_skills(workspace_id, slug)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_workspace_skills_workspace_updated
  ON workspace_skills(workspace_id, updated_at DESC)
  WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS workspace_skill_revisions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  skill_id UUID NOT NULL REFERENCES workspace_skills(id) ON DELETE CASCADE,
  revision TEXT NOT NULL CHECK (revision <> ''),
  content_hash TEXT NOT NULL CHECK (content_hash <> ''),
  files_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(files_json) = 'array'),
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_workspace_skill_revisions_skill_created
  ON workspace_skill_revisions(skill_id, created_at DESC);

CREATE TABLE IF NOT EXISTS workspace_builtin_skill_settings (
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  slug TEXT NOT NULL CHECK (slug <> ''),
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  invocable BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (workspace_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_workspace_builtin_skill_settings_updated
  ON workspace_builtin_skill_settings(workspace_id, updated_at DESC);
