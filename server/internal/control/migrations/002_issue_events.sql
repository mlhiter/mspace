CREATE TABLE IF NOT EXISTS issue_watchers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id TEXT NOT NULL,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reason TEXT NOT NULL CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual')),
  muted BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(workspace_id, issue_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_issue_watchers_user
  ON issue_watchers(user_id, muted);

CREATE INDEX IF NOT EXISTS idx_issue_watchers_issue
  ON issue_watchers(workspace_id, issue_id, muted);

CREATE TABLE IF NOT EXISTS issue_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id TEXT NOT NULL,
  actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  kind TEXT NOT NULL CHECK (kind <> ''),
  summary TEXT NOT NULL DEFAULT '',
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_issue_events_workspace_created
  ON issue_events(workspace_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_issue_events_issue_created
  ON issue_events(workspace_id, issue_id, created_at DESC);

CREATE TABLE IF NOT EXISTS issue_event_receipts (
  event_id UUID NOT NULL REFERENCES issue_events(id) ON DELETE CASCADE,
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id TEXT NOT NULL,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  state TEXT NOT NULL DEFAULT 'unread' CHECK (state IN ('unread', 'read', 'archived')),
  read_at TIMESTAMPTZ,
  archived_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(event_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_issue_event_receipts_user_state
  ON issue_event_receipts(user_id, state, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_issue_event_receipts_issue_user
  ON issue_event_receipts(workspace_id, issue_id, user_id, state);
