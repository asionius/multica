-- Per-issue pipeline state.
--
-- Splits the per-issue pipeline state out of issue.description (where the
-- ccproc pipeline-initializer agent currently stores it as YAML inside an
-- HTML comment block) into structured columns.
--
-- Why: the bounce_with_prev / bounce_with_next counters in the description
-- YAML mutate on every ping-pong, breaking the prefix prompt cache and
-- driving cache_read_tokens to zero. By keeping the static topology in
-- pipeline_base (cache-friendly, rarely changes) and the mutable counters
-- in pipeline_dynamic (passed via wakeup payload, never enters prefix),
-- the prefix can finally be cached.
--
-- pipeline_kind disambiguates:
--   ccproc-top      -- normal "requirement" top issue (parent of 5 stages)
--   ccproc-stage    -- one of 5 sub stages (requirement/designer/coder/
--                      tester/integrator)
--   ccproc-packager -- release packager issue (hangs directly under project)
--
-- pipeline_base for ccproc-top / ccproc-stage:
--   {
--     "siblings": [
--       {"role": "requirement", "id": "<uuid>"},
--       {"role": "designer",    "id": "<uuid>"},
--       {"role": "coder",       "id": "<uuid>"},
--       {"role": "tester",      "id": "<uuid>"},
--       {"role": "integrator",  "id": "<uuid>"}
--     ],
--     "top_issue_id": "<uuid>",
--     "version": "v0.1.3",
--     "branch": "ccproc/v0.1.3",
--     "bounce_cap": 3
--   }
-- pipeline_base for ccproc-packager:
--   { "version": "v0.1.3", "branch": "ccproc/v0.1.3", "bounce_cap": 3 }
--
-- pipeline_dynamic:
--   { "bounce_with_prev": 0, "bounce_with_next": 0, "escalated": false }
--
-- All columns nullable / defaulted so existing rows are unaffected.
-- Legacy issues with pipeline_kind IS NULL keep their YAML in description
-- and continue to work via dual-read fallback in agent instructions.
ALTER TABLE issue
    ADD COLUMN pipeline_kind TEXT NULL,
    ADD COLUMN pipeline_base JSONB NULL,
    ADD COLUMN pipeline_dynamic JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX idx_issue_pipeline_kind
    ON issue(pipeline_kind) WHERE pipeline_kind IS NOT NULL;
