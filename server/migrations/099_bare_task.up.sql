ALTER TABLE agent_task_queue ALTER COLUMN agent_id DROP NOT NULL;
ALTER TABLE agent_task_queue ALTER COLUMN runtime_id DROP NOT NULL;