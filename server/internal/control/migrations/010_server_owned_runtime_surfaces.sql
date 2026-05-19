CREATE TABLE IF NOT EXISTS workspace_settings (
  workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
  auto_create_draft_pr BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_profiles (
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  id TEXT NOT NULL CHECK (id <> ''),
  name TEXT NOT NULL CHECK (name <> ''),
  mention TEXT NOT NULL CHECK (mention <> ''),
  provider TEXT NOT NULL DEFAULT 'codex' CHECK (provider = 'codex'),
  description TEXT NOT NULL DEFAULT '',
  instructions TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  built_in BOOLEAN NOT NULL DEFAULT FALSE,
  sort_order INTEGER NOT NULL DEFAULT 100,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (workspace_id, id),
  UNIQUE (workspace_id, mention)
);

CREATE INDEX IF NOT EXISTS idx_agent_profiles_workspace_enabled
  ON agent_profiles(workspace_id, enabled, sort_order);

CREATE TABLE IF NOT EXISTS clusters (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (name <> ''),
  kubeconfig_path TEXT NOT NULL DEFAULT '',
  kube_context TEXT NOT NULL DEFAULT '',
  image_registry_prefix TEXT NOT NULL DEFAULT '',
  exposure_mode TEXT NOT NULL DEFAULT 'nodeport' CHECK (exposure_mode IN ('nodeport', 'ingress')),
  node_host TEXT NOT NULL DEFAULT '',
  preview_domain TEXT NOT NULL DEFAULT '',
  ingress_class TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'configured' CHECK (status IN ('configured', 'ready', 'unreachable')),
  last_checked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, kubeconfig_path, kube_context)
);

CREATE INDEX IF NOT EXISTS idx_clusters_workspace_updated
  ON clusters(workspace_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS issue_test_environments (
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id UUID PRIMARY KEY REFERENCES issues(id) ON DELETE CASCADE,
  cluster_id UUID REFERENCES clusters(id) ON DELETE SET NULL,
  namespace TEXT NOT NULL DEFAULT '',
  namespace_status TEXT NOT NULL DEFAULT 'not_requested',
  cleanup_status TEXT NOT NULL DEFAULT 'not_decided',
  preview_url TEXT NOT NULL DEFAULT '',
  image_registry_prefix TEXT NOT NULL DEFAULT '',
  kubeconfig_path TEXT NOT NULL DEFAULT '',
  kube_context TEXT NOT NULL DEFAULT '',
  exposure_mode TEXT NOT NULL DEFAULT 'nodeport' CHECK (exposure_mode IN ('nodeport', 'ingress')),
  preview_domain TEXT NOT NULL DEFAULT '',
  ingress_class TEXT NOT NULL DEFAULT '',
  node_host TEXT NOT NULL DEFAULT '',
  last_deploy_session_id TEXT NOT NULL DEFAULT '',
  last_cleanup_session_id TEXT NOT NULL DEFAULT '',
  source_session_id TEXT NOT NULL DEFAULT '',
  source_commit_sha TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_issue_test_environments_workspace_updated
  ON issue_test_environments(workspace_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_issue_test_environments_cluster
  ON issue_test_environments(cluster_id)
  WHERE cluster_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS issue_handoffs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  source_session_id TEXT NOT NULL DEFAULT '',
  source_commit_sha TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '',
  head_commit_sha TEXT NOT NULL DEFAULT '',
  commits_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(commits_json) = 'array'),
  kind TEXT NOT NULL DEFAULT 'branch' CHECK (kind IN ('branch', 'pr')),
  pr_url TEXT NOT NULL DEFAULT '',
  pr_number INTEGER NOT NULL DEFAULT 0 CHECK (pr_number >= 0),
  pr_state TEXT NOT NULL DEFAULT '',
  pr_title TEXT NOT NULL DEFAULT '',
  preview_url TEXT NOT NULL DEFAULT '',
  evidence_summary TEXT NOT NULL DEFAULT '',
  created_via TEXT NOT NULL DEFAULT 'server',
  last_checked_at TIMESTAMPTZ,
  error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_issue_handoffs_issue_updated
  ON issue_handoffs(workspace_id, issue_id, updated_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_issue_handoffs_one_pr_per_issue
  ON issue_handoffs(workspace_id, issue_id)
  WHERE kind = 'pr';
