CREATE TABLE IF NOT EXISTS clusters (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  kubeconfig_path TEXT NOT NULL,
  kube_context TEXT NOT NULL DEFAULT '',
  image_registry_prefix TEXT NOT NULL DEFAULT '',
  exposure_mode TEXT NOT NULL DEFAULT 'nodeport',
  node_host TEXT NOT NULL DEFAULT '',
  preview_domain TEXT NOT NULL DEFAULT '',
  ingress_class TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'configured',
  last_checked_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_clusters_updated_at ON clusters(updated_at);

CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  repo_path TEXT NOT NULL,
  source_type TEXT NOT NULL DEFAULT 'local',
  remote_url TEXT NOT NULL DEFAULT '',
  git_provider TEXT NOT NULL DEFAULT '',
  git_owner TEXT NOT NULL DEFAULT '',
  git_repo TEXT NOT NULL DEFAULT '',
  default_branch TEXT NOT NULL,
  deploy_command TEXT NOT NULL,
  validation_command TEXT NOT NULL,
  kube_context TEXT NOT NULL,
  kubeconfig_path TEXT NOT NULL DEFAULT '',
  namespace TEXT NOT NULL,
  image_registry_prefix TEXT NOT NULL DEFAULT '',
  preview_domain TEXT NOT NULL DEFAULT '',
  ingress_class TEXT NOT NULL DEFAULT '',
  node_host TEXT NOT NULL DEFAULT '',
  default_cluster_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS issues (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  parent_issue_id TEXT REFERENCES issues(id) ON DELETE CASCADE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  status TEXT NOT NULL,
  close_reason TEXT NOT NULL DEFAULT '',
  triage_status TEXT NOT NULL DEFAULT 'none',
  assignee TEXT NOT NULL,
  assignee_type TEXT NOT NULL DEFAULT 'human',
  creator_name TEXT NOT NULL DEFAULT '',
  creator_avatar_url TEXT NOT NULL DEFAULT '',
  environment_url TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS inbox_items (
  id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  unread INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS comments (
  id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  author_type TEXT NOT NULL,
  author_user_id TEXT NOT NULL DEFAULT '',
  author_name TEXT NOT NULL DEFAULT '',
  author_avatar_url TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT '',
  edited_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS issue_attachments (
  id TEXT PRIMARY KEY,
  issue_id TEXT REFERENCES issues(id) ON DELETE CASCADE,
  comment_id TEXT REFERENCES comments(id) ON DELETE SET NULL,
  filename TEXT NOT NULL,
  content_type TEXT NOT NULL,
  size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
  storage_backend TEXT NOT NULL DEFAULT 'sqlite_blob',
  storage_key TEXT NOT NULL DEFAULT '',
  content BLOB,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  bound_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_issue_attachments_issue_id ON issue_attachments(issue_id);
CREATE INDEX IF NOT EXISTS idx_issue_attachments_comment_id ON issue_attachments(comment_id);
CREATE INDEX IF NOT EXISTS idx_issue_attachments_created_at ON issue_attachments(created_at);

CREATE TABLE IF NOT EXISTS issue_label_definitions (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  dimension TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0,
  built_in INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_issue_label_definitions_dimension ON issue_label_definitions(dimension, sort_order);

CREATE TABLE IF NOT EXISTS issue_labels (
  id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  label_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(issue_id, name),
  UNIQUE(issue_id, label_id)
);

CREATE INDEX IF NOT EXISTS idx_issue_labels_issue_id ON issue_labels(issue_id);

CREATE TABLE IF NOT EXISTS agent_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  mention TEXT NOT NULL UNIQUE,
  provider TEXT NOT NULL DEFAULT 'codex',
  description TEXT NOT NULL DEFAULT '',
  instructions TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  built_in INTEGER NOT NULL DEFAULT 0,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_profiles_enabled ON agent_profiles(enabled);

CREATE TABLE IF NOT EXISTS agent_sessions (
  id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  agent_profile TEXT NOT NULL DEFAULT '',
  runtime_mode TEXT NOT NULL,
  command TEXT NOT NULL,
  status TEXT NOT NULL,
  branch TEXT NOT NULL,
  workdir TEXT NOT NULL,
  codex_thread_id TEXT NOT NULL DEFAULT '',
  codex_turn_id TEXT NOT NULL DEFAULT '',
  agent_status TEXT NOT NULL DEFAULT '',
  artifact_dir TEXT NOT NULL DEFAULT '',
  source_session_id TEXT NOT NULL DEFAULT '',
  source_commit_sha TEXT NOT NULL DEFAULT '',
  trigger_comment_id TEXT NOT NULL DEFAULT '',
  agent_token TEXT NOT NULL DEFAULT '',
  cleanup_status TEXT NOT NULL DEFAULT 'retained',
  cleaned_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS session_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
  stream TEXT NOT NULL,
  message TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS project_runbooks (
  project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  content TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'empty',
  source TEXT NOT NULL DEFAULT '',
  source_session_id TEXT NOT NULL DEFAULT '',
  content_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS project_runbook_revisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL DEFAULT '',
  author_type TEXT NOT NULL,
  author_name TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  content_hash TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'learned',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_project_runbook_revisions_project_created ON project_runbook_revisions(project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS deployment_evidence (
  id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
  cluster TEXT NOT NULL,
  namespace TEXT NOT NULL,
  summary TEXT NOT NULL,
  details TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS issue_change_nodes (
  id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
  commit_sha TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '',
  files_changed INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  UNIQUE(session_id),
  UNIQUE(issue_id, commit_sha)
);

CREATE INDEX IF NOT EXISTS idx_issue_change_nodes_issue_created ON issue_change_nodes(issue_id, created_at DESC);

CREATE TABLE IF NOT EXISTS session_review_evidence (
  id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
  source_session_id TEXT NOT NULL DEFAULT '',
  source_commit_sha TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '',
  agent_summary TEXT NOT NULL DEFAULT '',
  commands_json TEXT NOT NULL DEFAULT '[]',
  tests_json TEXT NOT NULL DEFAULT '[]',
  build_result_json TEXT NOT NULL DEFAULT '{}',
  deployment_result_json TEXT NOT NULL DEFAULT '{}',
  risks_json TEXT NOT NULL DEFAULT '[]',
  follow_ups_json TEXT NOT NULL DEFAULT '[]',
  preview_url TEXT NOT NULL DEFAULT '',
  cluster TEXT NOT NULL DEFAULT '',
  namespace TEXT NOT NULL DEFAULT '',
  namespace_status TEXT NOT NULL DEFAULT '',
  cleanup_status TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(session_id)
);

CREATE INDEX IF NOT EXISTS idx_session_review_evidence_issue_created ON session_review_evidence(issue_id, created_at DESC);

CREATE TABLE IF NOT EXISTS issue_test_environments (
  issue_id TEXT PRIMARY KEY REFERENCES issues(id) ON DELETE CASCADE,
  namespace TEXT NOT NULL,
  namespace_status TEXT NOT NULL DEFAULT 'planned',
  cleanup_status TEXT NOT NULL DEFAULT 'retained',
  preview_url TEXT NOT NULL DEFAULT '',
  cluster_id TEXT NOT NULL DEFAULT '',
  image_registry_prefix TEXT NOT NULL DEFAULT '',
  kubeconfig_path TEXT NOT NULL DEFAULT '',
  kube_context TEXT NOT NULL DEFAULT '',
  exposure_mode TEXT NOT NULL DEFAULT 'nodeport',
  preview_domain TEXT NOT NULL DEFAULT '',
  ingress_class TEXT NOT NULL DEFAULT '',
  node_host TEXT NOT NULL DEFAULT '',
  last_deploy_session_id TEXT NOT NULL DEFAULT '',
  last_cleanup_session_id TEXT NOT NULL DEFAULT '',
  source_session_id TEXT NOT NULL DEFAULT '',
  source_commit_sha TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_issue_test_environments_namespace ON issue_test_environments(namespace);
