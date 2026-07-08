CREATE TABLE IF NOT EXISTS workspace_github_app_installations (
  workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'not_connected' CHECK (status IN ('not_connected', 'connected', 'needs_attention')),
  installation_id TEXT NOT NULL DEFAULT '',
  account_login TEXT NOT NULL DEFAULT '',
  account_type TEXT NOT NULL DEFAULT '',
  repository_selection TEXT NOT NULL DEFAULT '',
  permissions_json JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(permissions_json) = 'object'),
  html_url TEXT NOT NULL DEFAULT '',
  repositories_url TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  last_synced_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_workspace_github_app_installations_updated
  ON workspace_github_app_installations(updated_at DESC);
