package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUpdateAgentRuntime_CustomEnvOwnerOnly walks the per-runtime env-vars
// gate end-to-end: only canEditRuntime callers can write or read true values.
// Mirrors the visibility/timezone tests in runtime_visibility_test.go and is
// the P8 counterpart to migration 095's per-issue runtime pin (issue routes
// task to a runtime; runtime owns the secrets that runtime daemon injects).
func TestUpdateAgentRuntime_CustomEnvOwnerOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)

	// --- Owner sets custom_env ---
	w := httptest.NewRecorder()
	req := newRequestAs(runtimeOwnerID, http.MethodPatch, "/api/runtimes/"+runtimeID, map[string]any{
		"custom_env": map[string]any{
			"ANTHROPIC_API_KEY": "sk-test-aaa",
			"NICE_TO_HAVE":      "yes",
		},
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner PATCH custom_env: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.CustomEnvRedacted {
		t.Fatalf("owner response should not be redacted")
	}
	if resp.CustomEnv["ANTHROPIC_API_KEY"] != "sk-test-aaa" {
		t.Fatalf("owner response missing real value, got %v", resp.CustomEnv)
	}

	// --- Plain member can't write ---
	w = httptest.NewRecorder()
	req = newRequestAs(plainMemberID, http.MethodPatch, "/api/runtimes/"+runtimeID, map[string]any{
		"custom_env": map[string]any{"FOO": "bar"},
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member PATCH custom_env: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// --- Plain member sees redacted values via list ---
	// Make the runtime public so plainMember can even reach it (otherwise
	// List filtering / picker filtering hides it). The list endpoint still
	// returns it unredacted ONLY for canEditRuntime — public visibility
	// doesn't unlock env-var values, that's the whole P8 point.
	w = httptest.NewRecorder()
	req = newRequestAs(runtimeOwnerID, http.MethodPatch, "/api/runtimes/"+runtimeID, map[string]any{
		"visibility": "public",
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("flip to public: %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(plainMemberID, http.MethodGet, "/api/runtimes/", nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	testHandler.ListAgentRuntimes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("plainMember GET /api/runtimes/: %d: %s", w.Code, w.Body.String())
	}
	var listResp []AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var found *AgentRuntimeResponse
	for i := range listResp {
		if listResp[i].ID == runtimeID {
			found = &listResp[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("plainMember should see public runtime in list")
	}
	if !found.CustomEnvRedacted {
		t.Fatalf("plainMember should see redacted env: %+v", found.CustomEnv)
	}
	if got := found.CustomEnv["ANTHROPIC_API_KEY"]; got != "****" {
		t.Fatalf("plainMember should see masked value, got %q", got)
	}
	if _, ok := found.CustomEnv["NICE_TO_HAVE"]; !ok {
		t.Fatalf("plainMember should see env keys (just not values)")
	}

	// --- Owner clears custom_env with empty map ---
	w = httptest.NewRecorder()
	req = newRequestAs(runtimeOwnerID, http.MethodPatch, "/api/runtimes/"+runtimeID, map[string]any{
		"custom_env": map[string]any{},
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear custom_env: %d: %s", w.Code, w.Body.String())
	}
	// Use a fresh response struct — json.Unmarshal merges into an existing
	// map, so reusing `resp` here would silently keep the old keys.
	var clearedResp AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&clearedResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(clearedResp.CustomEnv) != 0 {
		t.Fatalf("expected empty env after clear, got %v", clearedResp.CustomEnv)
	}
}
