ALTER TABLE runtime_workers
  ADD COLUMN IF NOT EXISTS storage_id TEXT NOT NULL DEFAULT '';

ALTER TABLE runtime_workers
  DROP CONSTRAINT IF EXISTS runtime_workers_storage_id_format;

ALTER TABLE runtime_workers
  ADD CONSTRAINT runtime_workers_storage_id_format
  CHECK (storage_id = '' OR storage_id ~ '^msws_[A-Za-z0-9_-]{8,123}$');

ALTER TABLE runtime_tasks
  ADD COLUMN IF NOT EXISTS storage_affinity_id TEXT NOT NULL DEFAULT '';

ALTER TABLE runtime_tasks
  ADD COLUMN IF NOT EXISTS cancel_requested_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS issue_working_copies (
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  branch TEXT NOT NULL CHECK (branch <> ''),
  base_commit_sha TEXT NOT NULL DEFAULT '',
  head_commit_sha TEXT NOT NULL DEFAULT '',
  storage_id TEXT NOT NULL DEFAULT '',
  last_worker_id UUID REFERENCES runtime_workers(id) ON DELETE SET NULL,
  content_state TEXT NOT NULL DEFAULT 'uninitialized'
    CHECK (content_state IN ('uninitialized', 'clean', 'dirty', 'recovery_required')),
  recovery_reason TEXT NOT NULL DEFAULT '',
  active_session_id TEXT NOT NULL DEFAULT '',
  generation BIGINT NOT NULL DEFAULT 0 CHECK (generation >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (workspace_id, issue_id),
  CHECK (storage_id = '' OR storage_id ~ '^msws_[A-Za-z0-9_-]{8,123}$'),
  CHECK (
    (content_state IN ('uninitialized', 'clean', 'dirty') AND recovery_reason = '')
    OR
    (content_state = 'recovery_required' AND recovery_reason IN (
      'worktree_missing',
      'branch_mismatch',
      'head_mismatch',
      'metadata_missing',
      'workspace_probe_failed'
    ))
  )
);

CREATE INDEX IF NOT EXISTS idx_issue_working_copies_storage
  ON issue_working_copies(workspace_id, storage_id)
  WHERE storage_id <> '';

CREATE INDEX IF NOT EXISTS idx_runtime_tasks_storage_affinity
  ON runtime_tasks(workspace_id, storage_affinity_id, status, priority DESC, created_at ASC)
  WHERE storage_affinity_id <> '';
