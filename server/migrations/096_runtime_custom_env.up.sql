-- Add custom_env to agent_runtime so per-user secrets (per-runtime API keys,
-- access tokens, machine-specific paths) live with the runtime instead of
-- with the agent. The runtime is the natural owner of these — each runtime
-- represents one user's machine + credentials, so when a shared agent is
-- dispatched to user X's runtime, the daemon merges runtime.custom_env over
-- agent.custom_env (runtime wins on conflict). agent.custom_env stays as the
-- workspace-shared default.
--
-- See migration 040 for agent.custom_env (the workspace-shared half), and
-- migration 095 for issue.runtime_id (the per-issue dispatch override that
-- determines which runtime — and therefore whose secrets — picks up the task).
ALTER TABLE agent_runtime
    ADD COLUMN custom_env JSONB NOT NULL DEFAULT '{}';
