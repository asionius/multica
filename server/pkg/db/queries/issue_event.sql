-- name: CreateIssueEvent :exec
-- Creates an issue event. The unique constraint on source_comment_id
-- (ON CONFLICT DO NOTHING) provides automatic deduplication — re-posting
-- the same comment can never create a second event, preventing agent
-- double-wakeup.
INSERT INTO issue_events (
    issue_id, kind, payload, source_comment_id,
    source_actor_type, source_actor_id
) VALUES (
    $1, $2, $3, $4, $5, $6
) ON CONFLICT (source_comment_id) DO NOTHING;

-- name: GetFreshIssueEvent :one
-- Returns the oldest unconsumed event for a given issue. The daemon
-- calls this when building a wakeup payload so it can inject a
-- structured event instead of raw comment text.
SELECT
    id, issue_id, kind, payload,
    source_comment_id, source_actor_type, source_actor_id,
    created_at
FROM issue_events
WHERE issue_id = $1 AND consumed_at IS NULL
ORDER BY created_at ASC
LIMIT 1;

-- name: MarkIssueEventConsumed :exec
-- Marks an event as consumed by a specific task. Called by the daemon
-- after it injects the event into the agent's wakeup payload.
UPDATE issue_events
SET consumed_at = now(),
    consumed_by_task_id = $2
WHERE id = $1;
