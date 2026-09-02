-- Revert to the pre-101 definition (missing the bare-task NULL agent_id
-- filter). Rollback is only safe while no issue with bare tasks needs to
-- be re-project-moved.
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
        ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);
    END IF;
    RETURN NEW;
END;
$$;
