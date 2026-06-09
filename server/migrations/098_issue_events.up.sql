-- Typed issue events for structured agent wakeup.
--
-- Replaces the fragile "magic header" pattern (### [REVIEW] approve/deny)
-- where agents parsed human-readable comment markup to discover why they
-- were awakened. Every event is deduplicated by source_comment_id so that
-- re-posting or re-delivering the same comment cannot trigger a second
-- wakeup. Consumption is tracked via consumed_at + consumed_by_task_id so
-- the daemon knows which events are fresh when preparing a wakeup payload.
--
-- event kinds (open set — the enum check is in application code, not the DB):
--   review_denied
--   review_approved
--   escalation_requested
--   merge_conflict
--   wakeup_manual

CREATE TABLE issue_events (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id             UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    kind                 TEXT NOT NULL,
    payload              JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_comment_id    UUID UNIQUE,
    source_actor_type    TEXT NOT NULL DEFAULT 'member',
    source_actor_id      UUID NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    consumed_at          TIMESTAMPTZ,
    consumed_by_task_id  UUID
);

CREATE INDEX idx_issue_events_unconsumed
    ON issue_events(issue_id, created_at)
    WHERE consumed_at IS NULL;
