ALTER TABLE workspace_settings
  ADD COLUMN IF NOT EXISTS auto_deploy_test_environment BOOLEAN NOT NULL DEFAULT FALSE;
