package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// These tests exercise resolveAppURL's env-precedence logic and the handler
// shape. They don't touch the DB — the handler deliberately reads no server
// state beyond environment variables.

func TestGetPublicConfig_MulticaAppURLWins(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://app.example.com/")
	t.Setenv("FRONTEND_ORIGIN", "https://should-be-ignored.example.com")

	body := doPublicConfig(t)

	if body.AppURL != "https://app.example.com" {
		t.Fatalf("AppURL = %q, want trailing-slash trimmed MULTICA_APP_URL", body.AppURL)
	}
}

func TestGetPublicConfig_FallsBackToFrontendOrigin(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "https://frontend.example.com")

	body := doPublicConfig(t)

	if body.AppURL != "https://frontend.example.com" {
		t.Fatalf("AppURL = %q, want FRONTEND_ORIGIN", body.AppURL)
	}
}

func TestGetPublicConfig_FrontendOriginList_PicksFirst(t *testing.T) {
	// FRONTEND_ORIGIN is commonly a comma-separated CORS list. The app URL
	// must be a single origin, so we pick the first.
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "https://a.example.com, https://b.example.com")

	body := doPublicConfig(t)

	if body.AppURL != "https://a.example.com" {
		t.Fatalf("AppURL = %q, want first entry of list", body.AppURL)
	}
}

func TestGetPublicConfig_EmptyWhenNothingSet(t *testing.T) {
	// Empty app_url is a valid signal — the CLI falls back to using the
	// server URL as the app URL, which is correct for same-origin setups.
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "")

	body := doPublicConfig(t)

	if body.AppURL != "" {
		t.Fatalf("AppURL = %q, want empty string when nothing is configured", body.AppURL)
	}
}

// doPublicConfig issues a GET against the handler with no DB dependency and
// returns the decoded response.
func doPublicConfig(t *testing.T) PublicConfig {
	t.Helper()
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/public/config", nil)
	rec := httptest.NewRecorder()

	h.GetPublicConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body PublicConfig
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}
