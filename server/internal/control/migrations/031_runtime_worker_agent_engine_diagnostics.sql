ALTER TABLE runtime_workers
  ADD COLUMN IF NOT EXISTS agent_engine_diagnostics JSONB NOT NULL DEFAULT '{}'::jsonb;
