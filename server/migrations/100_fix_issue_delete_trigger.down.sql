CREATE OR REPLACE FUNCTION enqueue_task_usage_dashboard_dirty_for_issue_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_usage_dashboard_dirty (
        bucket_date, workspace_id, agent_id, project_id, model
    )
    SELECT DISTINCT
        DATE(tu.created_at),
        OLD.workspace_id,
        atq.agent_id,
        OLD.project_id,
        tu.model
      FROM agent_task_queue atq
      JOIN task_usage tu ON tu.task_id = atq.id
     WHERE atq.issue_id = OLD.id
    ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;
