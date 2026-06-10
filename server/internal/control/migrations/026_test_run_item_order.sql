ALTER TABLE test_run_items
  ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0;

WITH ordered_items AS (
  SELECT
    id,
    row_number() OVER (PARTITION BY run_id ORDER BY created_at ASC, id ASC) AS item_order
  FROM test_run_items
  WHERE sort_order = 0
)
UPDATE test_run_items i
SET sort_order = ordered_items.item_order
FROM ordered_items
WHERE i.id = ordered_items.id;

CREATE INDEX IF NOT EXISTS idx_test_run_items_run_order
  ON test_run_items(workspace_id, run_id, sort_order ASC, created_at ASC);
