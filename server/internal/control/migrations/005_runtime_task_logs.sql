CREATE TABLE IF NOT EXISTS runtime_task_logs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  task_id UUID NOT NULL REFERENCES runtime_tasks(id) ON DELETE CASCADE,
  worker_id UUID REFERENCES runtime_workers(id) ON DELETE SET NULL,
  stream TEXT NOT NULL CHECK (stream <> ''),
  message TEXT NOT NULL CHECK (message <> ''),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_runtime_task_logs_task_created
  ON runtime_task_logs(task_id, created_at ASC);
