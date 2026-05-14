ALTER TABLE issues
  ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE issues
  DROP CONSTRAINT IF EXISTS issues_project_id_fkey;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'issues_project_id_fkey'
      AND conrelid = 'issues'::regclass
  ) THEN
    ALTER TABLE issues
      ADD CONSTRAINT issues_project_id_fkey
      FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;
  END IF;
END $$;
