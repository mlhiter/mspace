CREATE TABLE IF NOT EXISTS issue_attachments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id UUID REFERENCES issues(id) ON DELETE CASCADE,
  comment_id UUID REFERENCES comments(id) ON DELETE SET NULL,
  filename TEXT NOT NULL CHECK (filename <> ''),
  content_type TEXT NOT NULL CHECK (content_type <> ''),
  size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
  storage_backend TEXT NOT NULL DEFAULT 'postgres_blob',
  storage_key TEXT NOT NULL DEFAULT '',
  content BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  bound_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_issue_attachments_issue
  ON issue_attachments(workspace_id, issue_id, created_at);

CREATE INDEX IF NOT EXISTS idx_issue_attachments_comment
  ON issue_attachments(comment_id)
  WHERE comment_id IS NOT NULL;
