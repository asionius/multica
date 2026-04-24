package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v3"
)

const testSmartGateKey = "abcdefghijklmnopqrstuvwxyz012345" // exactly 32 bytes

// setSmartGateEnv wires the package-level config to the given values and
// returns a cleanup function that restores the empty state.
func setSmartGateEnv(t *testing.T, enabled bool, key string, safeMode bool) {
	t.Helper()
	t.Setenv("SMARTGATE_ENABLED", strconv.FormatBool(enabled))
	t.Setenv("SMARTGATE_KEY", key)
	t.Setenv("SMARTGATE_SAFE_MODE", strconv.FormatBool(safeMode))
	resetSmartGateConfigForTests()
	t.Cleanup(resetSmartGateConfigForTests)
}

// encryptIdentity produces a JWE compact serialization compatible with the
// SmartGate payload format (alg=dir, enc=A256GCM, raw 32-byte key).
func encryptIdentity(t *testing.T, key []byte, payload map[string]any) string {
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

// signHeaders produces a SmartGate-compatible signature for the request.
// safeMode=true → extHeaders = [rioSeq, "", "", ""].
// safeMode=false → extHeaders = [rioSeq, staffID, staffName, extData].
func signHeaders(timestamp string, key []byte, rioSeq, staffID, staffName, extData string, safeMode bool) string {
	var extHeaders []string
	if safeMode {
		extHeaders = []string{rioSeq, "", "", ""}
	} else {
		extHeaders = []string{rioSeq, staffID, staffName, extData}
	}
	var sb strings.Builder
	sb.WriteString(timestamp)
	sb.Write(key)
	sb.WriteString(strings.Join(extHeaders, ","))
	sb.WriteString(timestamp)
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

func baseHeaders(t *testing.T, key []byte, safeMode bool, payload map[string]any, tsOverride int64) http.Header {
	t.Helper()
	ts := time.Now().Unix()
	if tsOverride != 0 {
		ts = tsOverride
	}
	tsStr := strconv.FormatInt(ts, 10)
	rioSeq := "rio-seq-123"
	staffID := "10001"
	staffName := "testuser"
	extData := ""

	jwe := encryptIdentity(t, key, payload)
	sig := signHeaders(tsStr, key, rioSeq, staffID, staffName, extData, safeMode)

	h := http.Header{}
	h.Set("x-tai-identity", jwe)
	h.Set("timestamp", tsStr)
	h.Set("signature", sig)
	h.Set("x-rio-seq", rioSeq)
	h.Set("staffid", staffID)
	h.Set("staffname", staffName)
	return h
}

func goodPayload() map[string]any {
	return map[string]any{
		"StaffId":    "10001",
		"LoginName":  "alice",
		"Expiration": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
}

func TestSmartGateEnabled_RequiresKeyExact32(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		key     string
		want    bool
	}{
		{"disabled", false, testSmartGateKey, false},
		{"enabled with 32-byte key", true, testSmartGateKey, true},
		{"enabled but key too short", true, "short", false},
		{"enabled but key too long", true, testSmartGateKey + "x", false},
		{"enabled but empty key", true, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setSmartGateEnv(t, tc.enabled, tc.key, true)
			if got := SmartGateEnabled(); got != tc.want {
				t.Fatalf("SmartGateEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseSmartGateHeaders_Success(t *testing.T) {
	setSmartGateEnv(t, true, testSmartGateKey, true)
	headers := baseHeaders(t, []byte(testSmartGateKey), true, goodPayload(), 0)

	id, err := ParseSmartGateHeaders(headers)
	if err != nil {
		t.Fatalf("ParseSmartGateHeaders: %v", err)
	}
	if id.StaffID != "10001" || id.LoginName != "alice" {
		t.Fatalf("unexpected identity: %+v", id)
	}
	if id.Expiration.IsZero() {
		t.Fatalf("expected Expiration to be parsed, got zero")
	}
}

func TestParseSmartGateHeaders_SignatureMismatch(t *testing.T) {
	setSmartGateEnv(t, true, testSmartGateKey, true)
	headers := baseHeaders(t, []byte(testSmartGateKey), true, goodPayload(), 0)
	headers.Set("signature", strings.Repeat("0", 64))

	_, err := ParseSmartGateHeaders(headers)
	if err == nil {
		t.Fatalf("expected signature mismatch error")
	}
	if !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected signature mismatch, got %v", err)
	}
}

func TestParseSmartGateHeaders_TimestampOutOfWindow_SafeMode(t *testing.T) {
	setSmartGateEnv(t, true, testSmartGateKey, true)
	// 10 minutes ago — way past the 180s window.
	old := time.Now().Add(-10 * time.Minute).Unix()
	headers := baseHeaders(t, []byte(testSmartGateKey), true, goodPayload(), old)

	_, err := ParseSmartGateHeaders(headers)
	if err == nil || !strings.Contains(err.Error(), "timestamp outside") {
		t.Fatalf("expected timestamp window error, got %v", err)
	}
}

func TestParseSmartGateHeaders_TimestampOutOfWindow_CompatMode(t *testing.T) {
	setSmartGateEnv(t, true, testSmartGateKey, false)
	old := time.Now().Add(-10 * time.Minute).Unix()
	// signature uses compat extHeaders — baseHeaders computes it that way.
	headers := baseHeaders(t, []byte(testSmartGateKey), false, goodPayload(), old)

	id, err := ParseSmartGateHeaders(headers)
	if err != nil {
		t.Fatalf("safeMode=false should skip timestamp check, got err=%v", err)
	}
	if id.LoginName != "alice" {
		t.Fatalf("unexpected identity: %+v", id)
	}
}

func TestParseSmartGateHeaders_ExpirationExpired_SafeMode(t *testing.T) {
	setSmartGateEnv(t, true, testSmartGateKey, true)
	payload := goodPayload()
	// expired 10 minutes ago (past the 3-min buffer).
	payload["Expiration"] = time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)

	headers := baseHeaders(t, []byte(testSmartGateKey), true, payload, 0)

	_, err := ParseSmartGateHeaders(headers)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expiration error, got %v", err)
	}
}

func TestParseSmartGateHeaders_JWEDecryptFails(t *testing.T) {
	setSmartGateEnv(t, true, testSmartGateKey, true)
	// JWE encrypted with a DIFFERENT 32-byte key → decrypt will fail.
	wrongKey := []byte("00000000000000000000000000000000")
	jwe := encryptIdentity(t, wrongKey, goodPayload())

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signHeaders(ts, []byte(testSmartGateKey), "rio-seq-123", "10001", "testuser", "", true)

	headers := http.Header{}
	headers.Set("x-tai-identity", jwe)
	headers.Set("timestamp", ts)
	headers.Set("signature", sig)
	headers.Set("x-rio-seq", "rio-seq-123")
	headers.Set("staffid", "10001")
	headers.Set("staffname", "testuser")

	_, err := ParseSmartGateHeaders(headers)
	if err == nil || !strings.Contains(err.Error(), "decrypt JWE") {
		t.Fatalf("expected decrypt error, got %v", err)
	}
}

func TestParseSmartGateHeaders_Disabled(t *testing.T) {
	setSmartGateEnv(t, false, testSmartGateKey, true)
	headers := baseHeaders(t, []byte(testSmartGateKey), true, goodPayload(), 0)

	_, err := ParseSmartGateHeaders(headers)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}
