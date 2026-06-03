package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIssuePipelineRoundtrip exercises GET / PATCH /api/issues/{id}/pipeline
// against the columns added by migration 097. It verifies:
//   - a fresh issue starts with kind=NULL, base=NULL, dynamic={} (DEFAULT)
//   - PATCH /pipeline/base writes pipeline_kind + pipeline_base atomically
//   - PATCH /pipeline/dynamic writes pipeline_dynamic without disturbing base
//   - GET round-trips both blobs unchanged
//   - dynamic edits leave the static topology untouched (prefix-cache promise)
//
// This is the end-to-end check for切片 C — it pins together migration ↔
// sqlc query ↔ handler ↔ JSON shape so PR2 (agent instructions) has a stable
// substrate to call against.
func TestIssuePipelineRoundtrip(t *testing.T) {
	if testHandler == nil {
		t.Skip("DB not reachable")
	}

	// 1. Seed a plain issue and ensure it starts in the "legacy" shape.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Pipeline state roundtrip",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })

	got := getPipeline(t, issue.ID)
	if got.Kind != "" {
		t.Fatalf("fresh issue should have empty kind, got %q", got.Kind)
	}
	// json.RawMessage of a SQL NULL field is encoded over the wire as the
	// literal "null" (4 bytes), so accept either zero-length or "null".
	if len(got.Base) != 0 && string(got.Base) != "null" {
		t.Fatalf("fresh issue should have null base, got %s", string(got.Base))
	}
	// pipeline_dynamic has DEFAULT '{}'::jsonb in migration 097.
	if string(got.Dynamic) != `{}` {
		t.Fatalf("fresh issue dynamic should default to {}, got %s", string(got.Dynamic))
	}

	// 2. Write base (the static topology half — siblings, version, branch).
	baseDoc := map[string]any{
		"siblings": []map[string]string{
			{"role": "requirement", "id": "11111111-1111-1111-1111-111111111111"},
			{"role": "designer", "id": "22222222-2222-2222-2222-222222222222"},
			{"role": "coder", "id": issue.ID},
			{"role": "tester", "id": "44444444-4444-4444-4444-444444444444"},
			{"role": "integrator", "id": "55555555-5555-5555-5555-555555555555"},
		},
		"top_issue_id": "00000000-0000-0000-0000-0000000000aa",
		"version":      "v0.1.3",
		"branch":       "ccproc/v0.1.3",
		"bounce_cap":   3,
	}
	patchBase(t, issue.ID, "ccproc-stage", baseDoc)

	got = getPipeline(t, issue.ID)
	if got.Kind != "ccproc-stage" {
		t.Fatalf("after base write: kind = %q, want ccproc-stage", got.Kind)
	}
	if len(got.Base) == 0 {
		t.Fatal("after base write: base should not be empty")
	}
	// Decode and check a couple of fields survived round-trip — JSONB
	// preserves array order so siblings[0].role must still be "requirement".
	var baseRT struct {
		Siblings []struct {
			Role string `json:"role"`
			ID   string `json:"id"`
		} `json:"siblings"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(got.Base, &baseRT); err != nil {
		t.Fatalf("decode roundtripped base: %v (raw=%s)", err, string(got.Base))
	}
	if got, want := len(baseRT.Siblings), 5; got != want {
		t.Fatalf("siblings length = %d, want %d", got, want)
	}
	if baseRT.Siblings[0].Role != "requirement" || baseRT.Siblings[4].Role != "integrator" {
		t.Fatalf("sibling order not preserved by JSONB: %#v", baseRT.Siblings)
	}
	if baseRT.Version != "v0.1.3" {
		t.Fatalf("version = %q, want v0.1.3", baseRT.Version)
	}

	// 3. Write dynamic — must not disturb base.
	dynDoc := map[string]any{
		"bounce_with_prev": 1,
		"bounce_with_next": 0,
		"escalated":        false,
	}
	patchDynamic(t, issue.ID, dynDoc)

	got2 := getPipeline(t, issue.ID)
	if got2.Kind != "ccproc-stage" {
		t.Fatalf("dynamic write must not disturb kind, got %q", got2.Kind)
	}
	if !bytes.Equal(got.Base, got2.Base) {
		t.Fatalf("dynamic write disturbed base:\nbefore=%s\nafter =%s", string(got.Base), string(got2.Base))
	}
	var dynRT struct {
		BounceWithPrev int  `json:"bounce_with_prev"`
		BounceWithNext int  `json:"bounce_with_next"`
		Escalated      bool `json:"escalated"`
	}
	if err := json.Unmarshal(got2.Dynamic, &dynRT); err != nil {
		t.Fatalf("decode roundtripped dynamic: %v", err)
	}
	if dynRT.BounceWithPrev != 1 {
		t.Fatalf("bounce_with_prev = %d, want 1", dynRT.BounceWithPrev)
	}

	// 4. DB-side sanity: row really has both columns populated, not just the
	//    handler echoing the request.
	var kindOnDB string
	var baseOnDB, dynOnDB []byte
	if err := testPool.QueryRow(context.Background(), `
		SELECT pipeline_kind, pipeline_base::text, pipeline_dynamic::text
		FROM issue WHERE id = $1
	`, issue.ID).Scan(&kindOnDB, &baseOnDB, &dynOnDB); err != nil {
		t.Fatalf("read row direct: %v", err)
	}
	if kindOnDB != "ccproc-stage" {
		t.Fatalf("DB kind = %q, want ccproc-stage", kindOnDB)
	}
	if !bytes.Contains(baseOnDB, []byte(`"requirement"`)) {
		t.Fatalf("DB base missing requirement role: %s", string(baseOnDB))
	}
	if !bytes.Contains(dynOnDB, []byte(`"bounce_with_prev"`)) {
		t.Fatalf("DB dynamic missing bounce_with_prev key: %s", string(dynOnDB))
	}
}

// TestIssuePipelineValidation covers the bad-request paths for the two PATCH
// endpoints: missing kind on base writes, malformed JSON, empty body.
func TestIssuePipelineValidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("DB not reachable")
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Pipeline validation",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })

	// base write with empty body → 400
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/issues/"+issue.ID+"/pipeline/base", map[string]any{})
	req = withURLParam(req, "id", issue.ID)
	testHandler.PatchIssuePipelineBase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("base empty body: want 400, got %d: %s", w.Code, w.Body.String())
	}

	// base write with kind but no base → 400
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/issues/"+issue.ID+"/pipeline/base", map[string]any{
		"kind": "ccproc-stage",
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.PatchIssuePipelineBase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("base missing base field: want 400, got %d: %s", w.Code, w.Body.String())
	}

	// base write with base but no kind → 400
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/issues/"+issue.ID+"/pipeline/base", map[string]any{
		"base": map[string]any{"version": "v1"},
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.PatchIssuePipelineBase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("base missing kind: want 400, got %d: %s", w.Code, w.Body.String())
	}

	// dynamic write with no dynamic field → 400
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/issues/"+issue.ID+"/pipeline/dynamic", map[string]any{})
	req = withURLParam(req, "id", issue.ID)
	testHandler.PatchIssuePipelineDynamic(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("dynamic empty body: want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// helpers — kept local to this test file so the public handler surface stays clean.

func getPipeline(t *testing.T, issueID string) IssuePipelineResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/pipeline", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.GetIssuePipeline(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssuePipeline %s: want 200, got %d: %s", issueID, w.Code, w.Body.String())
	}
	var resp IssuePipelineResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode pipeline response: %v", err)
	}
	return resp
}

func patchBase(t *testing.T, issueID, kind string, base map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/issues/"+issueID+"/pipeline/base", map[string]any{
		"kind": kind,
		"base": base,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.PatchIssuePipelineBase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PatchIssuePipelineBase: want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func patchDynamic(t *testing.T, issueID string, dynamic map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/issues/"+issueID+"/pipeline/dynamic", map[string]any{
		"dynamic": dynamic,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.PatchIssuePipelineDynamic(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PatchIssuePipelineDynamic: want 200, got %d: %s", w.Code, w.Body.String())
	}
}
