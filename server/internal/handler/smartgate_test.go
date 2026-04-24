package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	jose "github.com/go-jose/go-jose/v3"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
)

// smartGateTestKey is the fixed 32-byte key used by the end-to-end tests.
// Exactly 32 ASCII bytes so SmartGateEnabled() returns true.
const smartGateTestKey = "01234567890123456789012345678901"

// configureSmartGateEnv wires the SmartGate env vars and resets the cached
// auth config so the next ParseSmartGateHeaders call picks up the new
// values. Test cleanup restores an empty config.
func configureSmartGateEnv(t *testing.T, enabled bool) {
	t.Helper()
	t.Setenv("SMARTGATE_ENABLED", strconv.FormatBool(enabled))
	t.Setenv("SMARTGATE_KEY", smartGateTestKey)
	t.Setenv("SMARTGATE_SAFE_MODE", "true")
	auth.ResetSmartGateConfigForTests()
	t.Cleanup(auth.ResetSmartGateConfigForTests)
}

// encryptSmartGateIdentity produces a SmartGate-compatible JWE compact
// serialization (alg=dir, enc=A256GCM) of the identity payload. Duplicates
// the helper used in auth/smartgate_test.go on purpose — both are test-only
// and keeping them in their own packages avoids cross-package test-helper
// gymnastics.
func encryptSmartGateIdentity(t *testing.T, key []byte, payload map[string]any) string {
	t.Helper()
	enc, err := jose.NewEncrypter(
		jose.A256GCM,
		jose.Recipient{Algorithm: jose.DIRECT, Key: key},
		nil,
	)
	if err != nil {
		t.Fatalf("NewEncrypter: %v", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	obj, err := enc.Encrypt(body)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	compact, err := obj.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return compact
}

// signSmartGateHeaders mirrors the server-side signature algorithm in
// auth.verifySmartGateSignature with safeMode=true so our test headers
// pass validation.
func signSmartGateHeaders(timestamp string, key []byte, rioSeq string) string {
	extHeaders := []string{rioSeq, "", "", ""}
	var sb strings.Builder
	sb.WriteString(timestamp)
	sb.Write(key)
	sb.WriteString(strings.Join(extHeaders, ","))
	sb.WriteString(timestamp)
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

// buildSmartGateHeaders returns a fully populated set of SmartGate headers
// (x-tai-identity + timestamp + signature + x-rio-seq + staffid + staffname)
// that the SmartGate middleware will accept under safeMode=true.
func buildSmartGateHeaders(t *testing.T, key []byte, staffID, loginName string) http.Header {
	t.Helper()
	payload := map[string]any{
		"StaffId":    staffID,
		"LoginName":  loginName,
		"Expiration": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	jwe := encryptSmartGateIdentity(t, key, payload)

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	rioSeq := "rio-seq-handler-test"
	sig := signSmartGateHeaders(ts, key, rioSeq)

	h := http.Header{}
	h.Set("x-tai-identity", jwe)
	h.Set("timestamp", ts)
	h.Set("signature", sig)
	h.Set("x-rio-seq", rioSeq)
	h.Set("staffid", staffID)
	h.Set("staffname", loginName)
	return h
}

// newSmartGateRouter mounts the real SmartGate middleware in front of
// testHandler.SmartGateLogin so every test exercises the end-to-end path
// (header parse → JWE decrypt → signature check → context inject → handler).
func newSmartGateRouter() *chi.Mux {
	r := chi.NewRouter()
	r.With(middleware.SmartGate(true)).Post("/auth/smartgate-login", testHandler.SmartGateLogin)
	return r
}

// doSmartGateLogin runs a POST against the SmartGate-gated route with the
// given headers and returns the recorder for assertions.
func doSmartGateLogin(headers http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/auth/smartgate-login", nil)
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	w := httptest.NewRecorder()
	newSmartGateRouter().ServeHTTP(w, req)
	return w
}

// TestSmartGateLogin_NewUserLowercasesEmail — Case A.
// Enabled + valid headers → 200, response email normalized to lowercase,
// exactly one DB row created.
func TestSmartGateLogin_NewUserLowercasesEmail(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}

	configureSmartGateEnv(t, true)

	const email = "alice@tencent.com"
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})
	testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)

	// LoginName passed in uppercase — server must lowercase it.
	headers := buildSmartGateHeaders(t, []byte(smartGateTestKey), "42", "Alice")

	w := doSmartGateLogin(headers)
	if w.Code != http.StatusOK {
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

// TestSmartGateLogin_ReturningUserReusesID — Case B.
// Second login with the same identity resolves to the same user row and
// the total DB row count stays at 1.
func TestSmartGateLogin_ReturningUserReusesID(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}

	configureSmartGateEnv(t, true)

	const email = "repeat-login@tencent.com"
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})
	testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)

	// First login — creates the user.
	headers1 := buildSmartGateHeaders(t, []byte(smartGateTestKey), "100", "repeat-login")
	w1 := doSmartGateLogin(headers1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first login: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	var first LoginResponse
	if err := json.NewDecoder(w1.Body).Decode(&first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	// Second login — must return the same user.id.
	headers2 := buildSmartGateHeaders(t, []byte(smartGateTestKey), "100", "repeat-login")
	w2 := doSmartGateLogin(headers2)
	if w2.Code != http.StatusOK {
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

// TestSmartGateLogin_NoHeadersRejected — Case C.
// Enabled, but no SmartGate headers at all → middleware responds 403 and
// never invokes SmartGateLogin.
func TestSmartGateLogin_NoHeadersRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}

	configureSmartGateEnv(t, true)

	w := doSmartGateLogin(http.Header{})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from middleware, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "smartgate_forbidden") {
		t.Fatalf("expected smartgate_forbidden error body, got %s", w.Body.String())
	}
}

// TestSmartGateLogin_BadSignatureRejected — Case D.
// Valid JWE + valid timestamp but a tampered signature → middleware 403.
func TestSmartGateLogin_BadSignatureRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}

	configureSmartGateEnv(t, true)

	headers := buildSmartGateHeaders(t, []byte(smartGateTestKey), "42", "alice")
	// 64 hex zeros decodes to 32 bytes — passes the format check, fails the
	// constant-time compare.
	headers.Set("signature", strings.Repeat("0", 64))

	w := doSmartGateLogin(headers)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bad signature, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "smartgate_forbidden") {
		t.Fatalf("expected smartgate_forbidden error body, got %s", w.Body.String())
	}
}

// TestSmartGateLogin_DisabledDeploymentRejected — Case E.
// SMARTGATE_ENABLED=false → middleware required mode returns 403 even with
// otherwise-valid headers (headers are built with the same key, but the
// disabled-path check short-circuits before parsing them).
func TestSmartGateLogin_DisabledDeploymentRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}

	configureSmartGateEnv(t, false)

	headers := buildSmartGateHeaders(t, []byte(smartGateTestKey), "42", "alice")

	w := doSmartGateLogin(headers)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when SmartGate disabled, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not enabled") {
		t.Fatalf("expected disabled-deployment error body, got %s", w.Body.String())
	}
}
