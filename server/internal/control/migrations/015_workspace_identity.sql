ALTER TABLE workspaces
  ADD COLUMN IF NOT EXISTS icon TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';

UPDATE workspaces
SET icon = ''
WHERE icon IS NULL;

UPDATE workspaces
SET description = ''
WHERE description IS NULL;

UPDATE workspaces
SET name = btrim(name)
WHERE name <> btrim(name);

UPDATE workspaces
SET name = 'Workspace'
WHERE name = '';

UPDATE workspaces
SET name = left(name, 120)
WHERE char_length(name) > 120;

UPDATE workspaces
SET icon = left(icon, 16)
WHERE char_length(icon) > 16;

UPDATE workspaces
SET description = left(description, 280)
WHERE char_length(description) > 280;

ALTER TABLE workspaces
  ALTER COLUMN icon SET DEFAULT '',
  ALTER COLUMN icon SET NOT NULL,
  ALTER COLUMN description SET DEFAULT '',
  ALTER COLUMN description SET NOT NULL;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'workspaces_name_length_check'
      AND conrelid = 'workspaces'::regclass
  ) THEN
    ALTER TABLE workspaces
      ADD CONSTRAINT workspaces_name_length_check CHECK (char_length(btrim(name)) BETWEEN 1 AND 120);
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'workspaces_icon_length_check'
      AND conrelid = 'workspaces'::regclass
  ) THEN
    ALTER TABLE workspaces
      ADD CONSTRAINT workspaces_icon_length_check CHECK (char_length(icon) <= 16);
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'workspaces_description_length_check'
      AND conrelid = 'workspaces'::regclass
  ) THEN
    ALTER TABLE workspaces
      ADD CONSTRAINT workspaces_description_length_check CHECK (char_length(description) <= 280);
  END IF;
END $$;
