ALTER TABLE workspaces
  ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'personal';

UPDATE workspaces
SET kind = 'personal'
WHERE kind = '';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'workspaces_kind_check'
      AND conrelid = 'workspaces'::regclass
  ) THEN
    ALTER TABLE workspaces
      ADD CONSTRAINT workspaces_kind_check CHECK (kind IN ('personal', 'team'));
  END IF;
END $$;
