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
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  status TEXT NOT NULL,
  assignee TEXT NOT NULL,
  assignee_type TEXT NOT NULL DEFAULT 'human',
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
  body TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS issue_labels (
  id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(issue_id, name)
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
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_issue_test_environments_namespace ON issue_test_environments(namespace);
