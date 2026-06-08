ALTER TABLE test_runs
  ADD COLUMN IF NOT EXISTS result_locale TEXT NOT NULL DEFAULT 'en';

ALTER TABLE test_runs DROP CONSTRAINT IF EXISTS test_runs_result_locale_check;
ALTER TABLE test_runs
  ADD CONSTRAINT test_runs_result_locale_check
  CHECK (result_locale IN ('en', 'zh-CN'));
