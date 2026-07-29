CREATE UNIQUE INDEX IF NOT EXISTS idx_runtime_tasks_active_pull_request_handoff
  ON runtime_tasks(workspace_id, issue_id)
  WHERE issue_id <> ''
    AND kind = 'agent_session'
    AND status IN ('queued', 'claimed', 'running')
    AND payload->>'automation' = 'pull_request_handoff';
