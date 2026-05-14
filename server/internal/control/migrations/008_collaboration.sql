CREATE TABLE IF NOT EXISTS projects (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (name <> ''),
  repo_path TEXT NOT NULL DEFAULT '',
  source_type TEXT NOT NULL DEFAULT 'local' CHECK (source_type IN ('local', 'github')),
  remote_url TEXT NOT NULL DEFAULT '',
  git_provider TEXT NOT NULL DEFAULT '',
  git_owner TEXT NOT NULL DEFAULT '',
  git_repo TEXT NOT NULL DEFAULT '',
  default_branch TEXT NOT NULL DEFAULT '',
  deploy_command TEXT NOT NULL DEFAULT '',
  validation_command TEXT NOT NULL DEFAULT '',
  kube_context TEXT NOT NULL DEFAULT '',
  kubeconfig_path TEXT NOT NULL DEFAULT '',
  namespace TEXT NOT NULL DEFAULT '',
  image_registry_prefix TEXT NOT NULL DEFAULT '',
  preview_domain TEXT NOT NULL DEFAULT '',
  ingress_class TEXT NOT NULL DEFAULT '',
  node_host TEXT NOT NULL DEFAULT '',
  default_cluster_id TEXT NOT NULL DEFAULT '',
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(workspace_id, name)
);

CREATE INDEX IF NOT EXISTS idx_projects_workspace_updated
  ON projects(workspace_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS project_runbooks (
  project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  content TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'empty' CHECK (status IN ('empty', 'learned', 'stale')),
  source TEXT NOT NULL DEFAULT '',
  source_session_id TEXT NOT NULL DEFAULT '',
  content_hash TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_project_runbooks_workspace
  ON project_runbooks(workspace_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS project_runbook_revisions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL DEFAULT '',
  author_type TEXT NOT NULL CHECK (author_type IN ('human', 'agent', 'system')),
  author_name TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  content_hash TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'learned',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_project_runbook_revisions_project_created
  ON project_runbook_revisions(project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS issues (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
  parent_issue_id UUID REFERENCES issues(id) ON DELETE CASCADE,
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  title TEXT NOT NULL CHECK (title <> ''),
  body TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'needs_review', 'changes_requested', 'ready_for_test', 'blocked', 'cancelled', 'closed')),
  close_reason TEXT NOT NULL DEFAULT '',
  triage_status TEXT NOT NULL DEFAULT 'pending' CHECK (triage_status IN ('none', 'pending', 'classified', 'failed')),
  assignee TEXT NOT NULL DEFAULT '',
  assignee_type TEXT NOT NULL DEFAULT 'human' CHECK (assignee_type IN ('human', 'agent')),
  creator_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  creator_name TEXT NOT NULL DEFAULT '',
  creator_avatar_url TEXT NOT NULL DEFAULT '',
  environment_url TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_issues_workspace_parent_updated
  ON issues(workspace_id, parent_issue_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_issues_workspace_project_updated
  ON issues(workspace_id, project_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS comments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  author_type TEXT NOT NULL CHECK (author_type IN ('human', 'agent', 'system')),
  author_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  author_name TEXT NOT NULL DEFAULT '',
  author_avatar_url TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  edited_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_comments_issue_created
  ON comments(issue_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS comment_reactions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  comment_id UUID NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
  reaction TEXT NOT NULL CHECK(reaction IN ('thumbs_up', 'thumbs_down', 'laugh', 'hooray', 'confused', 'heart', 'rocket', 'eyes')),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  actor_name TEXT NOT NULL DEFAULT '',
  actor_avatar_url TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(comment_id, user_id, reaction)
);

CREATE INDEX IF NOT EXISTS idx_comment_reactions_issue_comment
  ON comment_reactions(issue_id, comment_id);

CREATE TABLE IF NOT EXISTS issue_label_definitions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  key TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  dimension TEXT NOT NULL CHECK (dimension IN ('type', 'priority')),
  color TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0,
  built_in BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_issue_label_definitions_dimension
  ON issue_label_definitions(dimension, sort_order);

CREATE TABLE IF NOT EXISTS issue_labels (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  label_id UUID REFERENCES issue_label_definitions(id) ON DELETE SET NULL,
  name TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(issue_id, name),
  UNIQUE(issue_id, label_id)
);

CREATE INDEX IF NOT EXISTS idx_issue_labels_issue_id
  ON issue_labels(issue_id);

INSERT INTO issue_label_definitions (key, name, dimension, sort_order, built_in)
VALUES
  ('type:feat', 'feat', 'type', 10, TRUE),
  ('type:fix', 'fix', 'type', 20, TRUE),
  ('type:docs', 'docs', 'type', 30, TRUE),
  ('type:style', 'style', 'type', 40, TRUE),
  ('type:refactor', 'refactor', 'type', 50, TRUE),
  ('type:perf', 'perf', 'type', 60, TRUE),
  ('type:test', 'test', 'type', 70, TRUE),
  ('type:build', 'build', 'type', 80, TRUE),
  ('type:ci', 'ci', 'type', 90, TRUE),
  ('type:chore', 'chore', 'type', 100, TRUE),
  ('type:revert', 'revert', 'type', 110, TRUE),
  ('priority:p0', 'P0', 'priority', 10, TRUE),
  ('priority:p1', 'P1', 'priority', 20, TRUE),
  ('priority:p2', 'P2', 'priority', 30, TRUE),
  ('priority:p3', 'P3', 'priority', 40, TRUE)
ON CONFLICT (key) DO NOTHING;
