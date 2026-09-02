-- Fix: enqueue_task_usage_dashboard_dirty_for_issue_project() fails when
-- the issue has bare tasks (agent_id IS NULL). Same defect as the issue
-- delete trigger fixed in 100: the SELECT's atq.agent_id column is NULL
-- for bare tasks, and the INSERT into task_usage_dashboard_dirty.agent_id
-- (NOT NULL) violates the constraint, which aborts the whole
-- `UPDATE issue SET project_id = ...` statement — the issue can't be
-- moved to another project.
--
-- Filter out NULL agent_id rows in both the OLD- and NEW-project INSERTs.
-- Bare tasks don't belong to any agent, so there is no dashboard bucket to
-- invalidate (the rollup window's recomputed CTE joins on agent_id and can
-- never produce a NULL-agent bucket).
CREATE OR REPLACE FUNCTION enqueue_task_usage_dashboard_dirty_for_issue_project()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.project_id IS DISTINCT FROM NEW.project_id THEN
        -- OLD project buckets.
        INSERT INTO task_usage_dashboard_dirty (
            bucket_date, workspace_id, agent_id, project_id, model
        )
        SELECT DISTINCT
            DATE(tu.created_at),
            NEW.workspace_id,
            atq.agent_id,
            OLD.project_id,
            tu.model
          FROM agent_task_queue atq
          JOIN task_usage       tu ON tu.task_id = atq.id
         WHERE atq.issue_id = NEW.id
           AND atq.agent_id IS NOT NULL
        ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);

        -- NEW project buckets.
        INSERT INTO task_usage_dashboard_dirty (
            bucket_date, workspace_id, agent_id, project_id, model
        )
        SELECT DISTINCT
            DATE(tu.created_at),
            NEW.workspace_id,
            atq.agent_id,
            NEW.project_id,
            tu.model
          FROM agent_task_queue atq
          JOIN task_usage       tu ON tu.task_id = atq.id
         WHERE atq.issue_id = NEW.id
           AND atq.agent_id IS NOT NULL
        ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);
    END IF;
    RETURN NEW;
END;
$$;
