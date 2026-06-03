ALTER TABLE test_cases DROP CONSTRAINT IF EXISTS test_cases_type_check;

ALTER TABLE test_cases
  ADD CONSTRAINT test_cases_type_check
  CHECK (type IN ('functional', 'ui', 'api', 'deployment'));
