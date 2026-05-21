package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateIssueWithRuntimeID covers the new POST /api/issues runtime_id
// pin: workspace owner can pin any runtime; bogus / cross-workspace UUIDs
// are rejected; the response carries runtime_id back.
func TestCreateIssueWithRuntimeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "issue with runtime pin",
		"status":     "todo",
		"priority":   "low",
		"runtime_id": testRuntimeID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })

	if issue.RuntimeID == nil || *issue.RuntimeID != testRuntimeID {
		t.Fatalf("expected runtime_id=%s in response, got %v", testRuntimeID, issue.RuntimeID)
	}
}

// TestCreateIssueRejectsInvalidRuntimeID — bogus UUID string and
// well-formed-but-nonexistent UUID both 400.
func TestCreateIssueRejectsInvalidRuntimeID(t *testing.T) {
	cases := []struct {
		desc      string
		runtimeID string
		wantCode  int
	}{
		{"bogus_string", "not-a-uuid", http.StatusBadRequest},
		{"missing_uuid", "00000000-0000-0000-0000-000000000000", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
				"title":      "x",
				"status":     "todo",
				"priority":   "low",
				"runtime_id": tc.runtimeID,
			})
			testHandler.CreateIssue(w, req)
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

// TestCreateIssueRejectsCrossWorkspaceRuntime — a runtime in another workspace
// is rejected with 400, even if the caller is a workspace owner of their own.
func TestCreateIssueRejectsCrossWorkspaceRuntime(t *testing.T) {
	ctx := context.Background()
	// Spin up a second workspace + runtime owned by someone else.
	var otherWS, otherRuntime string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug) VALUES ('other-ws', 'other-ws-`+t.Name()+`') RETURNING id
	`).Scan(&otherWS); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, otherWS) })
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, visibility
		) VALUES ($1, NULL, 'other-runtime', 'cloud', 'codex', 'online', 'x', '{}'::jsonb, now(), 'public')
		RETURNING id
	`, otherWS).Scan(&otherRuntime); err != nil {
		t.Fatalf("create other runtime: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "x",
		"status":     "todo",
		"priority":   "low",
		"runtime_id": otherRuntime,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (cross-workspace), got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateIssueRuntimeIDTriState exercises the PATCH tri-state:
// 1) field absent → no change
// 2) UUID → set
// 3) explicit null → clear back to NULL
func TestUpdateIssueRuntimeIDTriState(t *testing.T) {
	id := createTestIssue(t, "patch runtime tri-state", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, id) })

	// 1. PATCH a field not related to runtime_id → runtime_id stays NULL.
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+id, map[string]any{"title": "renamed"})
	req = withURLParam(req, "id", id)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT title-only: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	if issue.RuntimeID != nil {
		t.Fatalf("step 1: expected runtime_id stays nil, got %v", issue.RuntimeID)
	}

	// 2. PATCH runtime_id → set.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+id, map[string]any{"runtime_id": testRuntimeID})
	req = withURLParam(req, "id", id)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT runtime_id=set: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&issue)
	if issue.RuntimeID == nil || *issue.RuntimeID != testRuntimeID {
		t.Fatalf("step 2: expected runtime_id=%s, got %v", testRuntimeID, issue.RuntimeID)
	}

	// 3. PATCH runtime_id explicit null → clear.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+id, map[string]any{"runtime_id": nil})
	req = withURLParam(req, "id", id)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT runtime_id=null: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&issue)
	if issue.RuntimeID != nil {
		t.Fatalf("step 3: expected runtime_id=nil, got %v", issue.RuntimeID)
	}
}

// TestCreateIssuePrivateRuntimeOwnerOnly — non-owner cannot use someone
// else's private runtime; same caller as workspace owner CAN use it because
// roleAllowed("owner") is the canUseRuntimeForAgent admin override.
func TestCreateIssuePrivateRuntimeOwnerOnly(t *testing.T) {
	ctx := context.Background()
	// Add a second user (non-owner) and a private runtime owned by them.
	var otherUser, privateRT string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (email, name) VALUES ($1, 'other') RETURNING id
	`, "other-"+t.Name()+"@x.com").Scan(&otherUser); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, otherUser) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUser); err != nil {
		t.Fatalf("add other as member: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, visibility, owner_id
		) VALUES ($1, NULL, 'other-private', 'cloud', 'codex', 'online', 'x', '{}'::jsonb, now(), 'private', $2)
		RETURNING id
	`, testWorkspaceID, otherUser).Scan(&privateRT); err != nil {
		t.Fatalf("create private runtime: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, privateRT) })

	// Caller = testUserID, who is workspace 'owner' → admin override applies →
	// MUST succeed because canUseRuntimeForAgent's first branch returns true
	// for owner/admin. This test guards the admin-bypass behavior so a future
	// "tighten by default" change has to come here on purpose.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "owner can use foreign private runtime",
		"status":     "todo",
		"priority":   "low",
		"runtime_id": privateRT,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("workspace owner pinning foreign private runtime: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })

	// Now caller = otherUser (member, not owner). canUseRuntimeForAgent for
	// 'member' role on a private runtime they DO own → allowed. Switch to
	// a third user (non-member-owner) for the rejection case.
	var thirdUser string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (email, name) VALUES ($1, 'third') RETURNING id
	`, "third-"+t.Name()+"@x.com").Scan(&thirdUser); err != nil {
		t.Fatalf("create third user: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, thirdUser) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, thirdUser); err != nil {
		t.Fatalf("add third as member: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "third user using foreign private runtime",
		"status":     "todo",
		"priority":   "low",
		"runtime_id": privateRT,
	})
	// override the X-User-ID header set by newRequest
	req.Header.Set("X-User-ID", thirdUser)
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-owner pinning foreign private runtime: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
