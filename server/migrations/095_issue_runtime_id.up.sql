-- Add per-issue runtime override.
--
-- When set, the daemon dispatches the issue's assigned agent on this runtime
-- instead of agent.runtime_id. This lets a workspace member create an issue
-- and pin it to their own runtime (and credentials), so a shared agent
-- definition can be invoked with the issue creator's identity.
--
-- Permission to set this column reuses canUseRuntimeForAgent (handler/runtime.go):
-- private runtimes only by their owner; public runtimes by any workspace
-- member; workspace owners/admins always.
--
-- NULL preserves existing behavior — the daemon falls back to agent.runtime_id.
ALTER TABLE issue
    ADD COLUMN runtime_id UUID NULL REFERENCES agent_runtime(id) ON DELETE SET NULL;

CREATE INDEX idx_issue_runtime_id
    ON issue(runtime_id) WHERE runtime_id IS NOT NULL;
