package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
	_ "unsafe" // for go:linkname

	"github.com/multica-ai/multica/server/internal/auth"
)

// withSmartGateIdentity reaches into the middleware package (same module)
// to store a SmartGate identity on the request context using the private
// context key. We use go:linkname instead of extending the middleware
// public API with a test-only helper.
//
//go:linkname withSmartGateIdentity github.com/multica-ai/multica/server/internal/middleware.withSmartGateIdentity
func withSmartGateIdentity(ctx context.Context, identity *auth.SmartGateIdentity) context.Context

// TestSmartGateLogin_MissingIdentity locks in the current behavior when the
// handler is called without an identity on the context (middleware bypassed
// or misconfigured). Today SmartGateLogin returns 403; this test guards
// against a silent behavior change rather than prescribing the "right" code.
func TestSmartGateLogin_MissingIdentity(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}

	req := httptest.NewRequest("POST", "/auth/smartgate-login", nil)
	w := httptest.NewRecorder()
	testHandler.SmartGateLogin(w, req)

	if w.Code != 403 {
		t.Fatalf("SmartGateLogin without identity: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSmartGateLogin_NewUserLowercasesEmail verifies that a first-seen
// SmartGate login creates a user row with the lowercase-normalized
// "<loginname>@tencent.com" email, regardless of the LoginName casing
// pushed by SmartGate.
func TestSmartGateLogin_NewUserLowercasesEmail(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}

	const email = "alice@tencent.com"
	ctx := context.Background()

	// Make sure the fixture is clean so we test the real "new user" path.
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})
	testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)

	identity := &auth.SmartGateIdentity{
		StaffID:    "42",
		LoginName:  "Alice", // uppercase to prove lowercase normalization
		Expiration: time.Now().Add(time.Hour),
	}

	req := httptest.NewRequest("POST", "/auth/smartgate-login", nil)
	req = req.WithContext(withSmartGateIdentity(req.Context(), identity))

	w := httptest.NewRecorder()
	testHandler.SmartGateLogin(w, req)

	if w.Code != 200 {
		t.Fatalf("SmartGateLogin: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LoginResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.User.Email != email {
		t.Fatalf("expected response email %q (lowercased), got %q", email, resp.User.Email)
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM "user" WHERE email = $1`, email).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 user row for %s, got %d", email, count)
	}
}

// TestSmartGateLogin_ReturningUserReusesID verifies that a second login
// with the same SmartGate identity resolves to the existing user row
// rather than creating a duplicate.
func TestSmartGateLogin_ReturningUserReusesID(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}

	const email = "repeat-login@tencent.com"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})
	testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)

	identity := &auth.SmartGateIdentity{
		StaffID:    "100",
		LoginName:  "repeat-login",
		Expiration: time.Now().Add(time.Hour),
	}

	// First login — creates the user.
	req1 := httptest.NewRequest("POST", "/auth/smartgate-login", nil)
	req1 = req1.WithContext(withSmartGateIdentity(req1.Context(), identity))
	w1 := httptest.NewRecorder()
	testHandler.SmartGateLogin(w1, req1)
	if w1.Code != 200 {
		t.Fatalf("first login: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	var first LoginResponse
	if err := json.NewDecoder(w1.Body).Decode(&first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	// Second login with the same identity — must return the same user.id.
	req2 := httptest.NewRequest("POST", "/auth/smartgate-login", nil)
	req2 = req2.WithContext(withSmartGateIdentity(req2.Context(), identity))
	w2 := httptest.NewRecorder()
	testHandler.SmartGateLogin(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("second login: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var second LoginResponse
	if err := json.NewDecoder(w2.Body).Decode(&second); err != nil {
		t.Fatalf("decode second response: %v", err)
	}

	if first.User.ID != second.User.ID {
		t.Fatalf("expected same user.id across logins, got %q then %q", first.User.ID, second.User.ID)
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM "user" WHERE email = $1`, email).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 user row after repeat login, got %d", count)
	}
}
