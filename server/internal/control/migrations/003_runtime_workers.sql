CREATE TABLE IF NOT EXISTS runtime_registration_tokens (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  token_prefix TEXT NOT NULL,
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  last_used_at TIMESTAMPTZ,
  revoked BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_runtime_registration_tokens_workspace
  ON runtime_registration_tokens(workspace_id, revoked, expires_at);

CREATE TABLE IF NOT EXISTS runtime_workers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  registration_token_id UUID REFERENCES runtime_registration_tokens(id) ON DELETE SET NULL,
  name TEXT NOT NULL,
  mode TEXT NOT NULL DEFAULT 'team' CHECK (mode IN ('personal', 'team')),
  status TEXT NOT NULL DEFAULT 'online' CHECK (status IN ('online', 'draining', 'offline')),
  version TEXT NOT NULL DEFAULT '',
  current_load INTEGER NOT NULL DEFAULT 0 CHECK (current_load >= 0),
  capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
  labels JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(workspace_id, name)
);

CREATE INDEX IF NOT EXISTS idx_runtime_workers_workspace_seen
  ON runtime_workers(workspace_id, last_seen_at DESC);
