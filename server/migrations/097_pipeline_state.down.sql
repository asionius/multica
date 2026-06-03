DROP INDEX IF EXISTS idx_issue_pipeline_kind;
ALTER TABLE issue
    DROP COLUMN IF EXISTS pipeline_dynamic,
    DROP COLUMN IF EXISTS pipeline_base,
    DROP COLUMN IF EXISTS pipeline_kind;
