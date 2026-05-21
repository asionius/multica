DROP INDEX IF EXISTS idx_issue_runtime_id;
ALTER TABLE issue DROP COLUMN IF EXISTS runtime_id;
