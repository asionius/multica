package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClaimTask_RuntimeEnvOverridesAgentEnv pins the P8 dispatch contract:
// the daemon receives task.agent.custom_env merged with the runtime's
// custom_env, with runtime keys winning on conflict so per-user secrets
// override the workspace-shared default. The merge happens server-side at
// claim time so older daemons (pre-P8) work with no code changes.
func TestClaimTask_RuntimeEnvOverridesAgentEnv(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	// Fresh agent with custom_env including both an "A" (only on agent) and
	// a "B" (will collide with runtime).
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, custom_env, instructions, runtime_id
		)
		SELECT $1, 'p8-merge-agent', 'cloud', $2::jsonb, '', id
		FROM agent_runtime WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
		RETURNING id
	`, testWorkspaceID, []byte(`{"A":"from_agent","B":"from_agent"}`)).Scan(&agentID); err != nil {
		t.Fatalf("create test agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID) })

	// Fresh runtime owned by testUserID with custom_env that overlaps "B"
	// and adds "C".
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, custom_env, last_seen_at
		)
		VALUES ($1, NULL, 'p8-merge-runtime', 'cloud', 'p8_test_provider', 'online',
		        'p8 merge test', '{}'::jsonb, $2, 'private', $3::jsonb, now())
		RETURNING id
	`, testWorkspaceID, testUserID, []byte(`{"B":"from_runtime","C":"from_runtime"}`)).Scan(&runtimeID); err != nil {
		t.Fatalf("create test runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID) })

	// Issue assigned to the agent, pinned to this runtime.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_id, creator_type,
			assignee_type, assignee_id, runtime_id, number, position
		) VALUES ($1, 'P8 merge test', 'todo', 'medium', $2, 'member',
		          'agent', $3, $4, 99001, 0)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID, runtimeID).Scan(&issueID); err != nil {
		t.Fatalf("create test issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// Queue a task for that runtime so claim returns it.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("queue task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	// Claim and decode the agent block.
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "p8-merge-test")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Task *struct {
			Agent *TaskAgentData `json:"agent"`
		} `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Task == nil || resp.Task.Agent == nil {
		t.Fatalf("expected task.agent in response: %s", w.Body.String())
	}

	got := resp.Task.Agent.CustomEnv
	if got["A"] != "from_agent" {
		t.Errorf("agent-only key A: expected from_agent, got %q", got["A"])
	}
	if got["B"] != "from_runtime" {
		t.Errorf("collision key B: runtime should win, got %q", got["B"])
	}
	if got["C"] != "from_runtime" {
		t.Errorf("runtime-only key C: expected from_runtime, got %q", got["C"])
	}
}
