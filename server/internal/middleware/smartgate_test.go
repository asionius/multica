package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v3"

	"github.com/multica-ai/multica/server/internal/auth"
)

const testSmartGateKey = "abcdefghijklmnopqrstuvwxyz012345" // 32 bytes

func setSmartGateEnvMW(t *testing.T, enabled bool, key string, safeMode bool) {
	t.Helper()
	t.Setenv("SMARTGATE_ENABLED", strconv.FormatBool(enabled))
	t.Setenv("SMARTGATE_KEY", key)
	t.Setenv("SMARTGATE_SAFE_MODE", strconv.FormatBool(safeMode))
	auth.ResetSmartGateConfigForTests()
	t.Cleanup(auth.ResetSmartGateConfigForTests)
}

func encryptMW(t *testing.T, key []byte, payload map[string]any) string {
	t.Helper()
	enc, err := jose.NewEncrypter(jose.A256GCM, jose.Recipient{Algorithm: jose.DIRECT, Key: key}, nil)
	if err != nil {
		t.Fatalf("NewEncrypter: %v", err)
	}
	body, _ := json.Marshal(payload)
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

func validSmartGateHeaders(t *testing.T, key []byte) http.Header {
	t.Helper()
	payload := map[string]any{
		"StaffId":    "10001",
		"LoginName":  "alice",
		"Expiration": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	jwe := encryptMW(t, key, payload)

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	rioSeq := "rio-seq-xyz"

	var sb strings.Builder
	sb.WriteString(ts)
	sb.Write(key)
	sb.WriteString(strings.Join([]string{rioSeq, "", "", ""}, ","))
	sb.WriteString(ts)
	sum := sha256.Sum256([]byte(sb.String()))

	h := http.Header{}
	h.Set("x-tai-identity", jwe)
	h.Set("timestamp", ts)
	h.Set("signature", hex.EncodeToString(sum[:]))
	h.Set("x-rio-seq", rioSeq)
	h.Set("staffid", "10001")
	h.Set("staffname", "alice")
	return h
}

func TestSmartGate_Required_RejectsMissingHeaders(t *testing.T) {
	setSmartGateEnvMW(t, true, testSmartGateKey, true)

	called := false
	mw := SmartGate(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/auth/smartgate-login", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if called {
		t.Fatalf("next handler should not have been called")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected JSON content-type, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), `"smartgate_forbidden"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestSmartGate_Required_DisabledDeployment(t *testing.T) {
	// SMARTGATE_ENABLED=false → required middleware rejects with 403.
	setSmartGateEnvMW(t, false, testSmartGateKey, true)

	mw := SmartGate(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not have been called")
	}))

	req := httptest.NewRequest("POST", "/auth/smartgate-login", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestSmartGate_Required_Success(t *testing.T) {
	setSmartGateEnvMW(t, true, testSmartGateKey, true)

	var captured *auth.SmartGateIdentity
	mw := SmartGate(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := SmartGateFromContext(r.Context())
		if !ok {
			t.Fatal("expected SmartGate identity in context")
		}
		captured = id
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/auth/smartgate-login", nil)
	req.Header = validSmartGateHeaders(t, []byte(testSmartGateKey))

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if captured == nil || captured.LoginName != "alice" {
		t.Fatalf("expected LoginName=alice, got %+v", captured)
	}
}

func TestSmartGate_Optional_PassesThroughWhenDisabled(t *testing.T) {
	setSmartGateEnvMW(t, false, "", true)

	called := false
	mw := SmartGate(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if _, ok := SmartGateFromContext(r.Context()); ok {
			t.Fatal("no identity expected when SmartGate disabled")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/me", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if !called {
		t.Fatalf("next handler should be called in optional mode")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
