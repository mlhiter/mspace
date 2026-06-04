CREATE TABLE IF NOT EXISTS environments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (char_length(btrim(name)) > 0),
  kind TEXT NOT NULL CHECK (kind IN ('virtual_machine')),
  status TEXT NOT NULL DEFAULT 'configured' CHECK (status IN ('configured', 'ready', 'unreachable')),
  ssh_host TEXT NOT NULL DEFAULT '',
  ssh_port INTEGER NOT NULL DEFAULT 22 CHECK (ssh_port > 0 AND ssh_port <= 65535),
  ssh_user TEXT NOT NULL DEFAULT '',
  ssh_auth_ref TEXT NOT NULL DEFAULT '',
  workdir TEXT NOT NULL DEFAULT '',
  service_hint TEXT NOT NULL DEFAULT '',
  labels JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(labels) = 'object'),
  last_checked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, kind, name)
);

CREATE INDEX IF NOT EXISTS idx_environments_workspace_updated
  ON environments(workspace_id, updated_at DESC);

ALTER TABLE test_plans
  ADD COLUMN IF NOT EXISTS environment_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS environment_kind TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS environment_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(environment_snapshot) = 'object');

ALTER TABLE test_runs
  ADD COLUMN IF NOT EXISTS environment_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS environment_kind TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS environment_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(environment_snapshot) = 'object');

ALTER TABLE issue_test_environments
  ADD COLUMN IF NOT EXISTS environment_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS environment_kind TEXT NOT NULL DEFAULT 'kubernetes',
  ADD COLUMN IF NOT EXISTS environment_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(environment_snapshot) = 'object');

UPDATE issue_test_environments
SET environment_id = cluster_id::text,
    environment_kind = 'kubernetes',
    environment_snapshot = jsonb_build_object(
      'id', cluster_id::text,
      'kind', 'kubernetes',
      'name', COALESCE(c.name, ''),
      'status', COALESCE(c.status, ''),
      'kubernetes', jsonb_build_object(
        'clusterId', cluster_id::text,
        'kubeconfigPath', COALESCE(c.kubeconfig_path, ''),
        'kubeContext', COALESCE(c.kube_context, ''),
        'imageRegistryPrefix', COALESCE(c.image_registry_prefix, ''),
        'exposureMode', COALESCE(c.exposure_mode, ''),
        'nodeHost', COALESCE(c.node_host, ''),
        'previewDomain', COALESCE(c.preview_domain, ''),
        'ingressClass', COALESCE(c.ingress_class, '')
      )
    )
FROM clusters c
WHERE issue_test_environments.cluster_id = c.id
  AND issue_test_environments.environment_id = '';

CREATE INDEX IF NOT EXISTS idx_test_plans_environment
  ON test_plans(workspace_id, environment_kind, environment_id)
  WHERE environment_id <> '';

CREATE INDEX IF NOT EXISTS idx_test_runs_environment
  ON test_runs(workspace_id, environment_kind, environment_id)
  WHERE environment_id <> '';

CREATE INDEX IF NOT EXISTS idx_issue_test_environments_environment
  ON issue_test_environments(workspace_id, environment_kind, environment_id)
  WHERE environment_id <> '';
