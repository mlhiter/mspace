CREATE TABLE IF NOT EXISTS runtime_tasks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  issue_id TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  project_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind <> ''),
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'claimed', 'running', 'completed', 'failed', 'cancelled')),
  priority INTEGER NOT NULL DEFAULT 0 CHECK (priority >= 0),
  runtime_mode TEXT NOT NULL DEFAULT 'team' CHECK (runtime_mode IN ('personal', 'team')),
  required_capabilities JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(required_capabilities) = 'object'),
  payload JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
  result JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(result) = 'object'),
  claimed_by_worker_id UUID REFERENCES runtime_workers(id) ON DELETE SET NULL,
  claimed_at TIMESTAMPTZ,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  error TEXT NOT NULL DEFAULT '',
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_runtime_tasks_workspace_status_priority
  ON runtime_tasks(workspace_id, status, priority DESC, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_runtime_tasks_worker_status
  ON runtime_tasks(claimed_by_worker_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_runtime_tasks_issue_created
  ON runtime_tasks(workspace_id, issue_id, created_at DESC)
  WHERE issue_id <> '';

CREATE TABLE IF NOT EXISTS runtime_task_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  task_id UUID NOT NULL REFERENCES runtime_tasks(id) ON DELETE CASCADE,
  worker_id UUID REFERENCES runtime_workers(id) ON DELETE SET NULL,
  actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  kind TEXT NOT NULL CHECK (kind <> ''),
  payload JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_runtime_task_events_task_created
  ON runtime_task_events(task_id, created_at ASC);
