package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// cleanupDeviceCode removes a device_code row by user_code. Each test
// reserves its own user_code and cleans up after itself.
func cleanupDeviceCode(t *testing.T, userCode string) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM cli_device_code WHERE user_code = $1`, userCode)
	})
}

// extractUserCode reads a StartCLIDeviceAuth response and registers a cleanup.
func startDevice(t *testing.T, hostname string) DeviceStartResponse {
	t.Helper()

	body := map[string]string{"hostname": hostname}
	w := httptest.NewRecorder()
	// No auth required — this is the public endpoint
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/cli/device/start", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")

	testHandler.StartCLIDeviceAuth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartCLIDeviceAuth: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp DeviceStartResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	cleanupDeviceCode(t, resp.UserCode)
	return resp
}

func TestCLIDeviceStart_ShapeAndFormat(t *testing.T) {
	resp := startDevice(t, "test-host")

	if resp.DeviceCode == "" {
		t.Fatal("device_code is empty")
	}
	if len(resp.DeviceCode) != 64 {
		t.Fatalf("device_code should be 64 hex chars, got %d", len(resp.DeviceCode))
	}
	// user_code: 4 chars + "-" + 4 chars
	if len(resp.UserCode) != userCodeLen+1 {
		t.Fatalf("user_code length = %d, want %d", len(resp.UserCode), userCodeLen+1)
	}
	if resp.UserCode[userCodeLen/2] != '-' {
		t.Fatalf("user_code must contain dash at pos %d: %q", userCodeLen/2, resp.UserCode)
	}
	// All non-dash chars must be from the unambiguous alphabet
	for i, c := range resp.UserCode {
		if i == userCodeLen/2 {
			continue
		}
		if !strings.ContainsRune(userCodeAlphabet, c) {
			t.Fatalf("user_code contains disallowed char %q at %d", c, i)
		}
	}
	if resp.ExpiresIn != int(deviceCodeTTL.Seconds()) {
		t.Fatalf("expires_in = %d, want %d", resp.ExpiresIn, int(deviceCodeTTL.Seconds()))
	}
	if resp.Interval <= 0 {
		t.Fatalf("interval should be positive, got %d", resp.Interval)
	}
	if resp.VerifyURI == "" {
		t.Fatal("verify_uri is empty")
	}
}

func TestCLIDeviceVerify_Unauthenticated(t *testing.T) {
	resp := startDevice(t, "verify-unauth")

	body, _ := json.Marshal(map[string]string{"user_code": resp.UserCode})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/verify", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	// deliberately NOT setting X-User-ID

	testHandler.VerifyCLIDeviceAuth(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCLIDeviceVerify_UnknownCode(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"user_code": "ZZZZ-ZZZZ"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/verify", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)

	testHandler.VerifyCLIDeviceAuth(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCLIDeviceVerify_Success(t *testing.T) {
	start := startDevice(t, "alice-mac")

	body, _ := json.Marshal(map[string]string{"user_code": start.UserCode})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/verify", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)

	testHandler.VerifyCLIDeviceAuth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp DeviceVerifyResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Hostname != "alice-mac" {
		t.Fatalf("hostname = %q, want %q", resp.Hostname, "alice-mac")
	}
	if resp.RequestedAt == "" || resp.ExpiresAt == "" {
		t.Fatalf("missing timestamps: %+v", resp)
	}
}

func TestCLIDeviceVerify_AcceptsLowercaseAndSpaces(t *testing.T) {
	start := startDevice(t, "normalize-test")

	// Strip dash, lowercase, spread spaces — should still resolve.
	raw := strings.ReplaceAll(start.UserCode, "-", "")
	messy := strings.ToLower(raw[:4] + " " + raw[4:])

	body, _ := json.Marshal(map[string]string{"user_code": messy})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/verify", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)

	testHandler.VerifyCLIDeviceAuth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for normalized code, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCLIDeviceApprove_IssuesPATAndFlipsStatus(t *testing.T) {
	start := startDevice(t, "approve-host")

	body, _ := json.Marshal(map[string]string{"user_code": start.UserCode})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/approve", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)

	testHandler.ApproveCLIDeviceAuth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Poll should return approved + a mul_ token.
	pollBody, _ := json.Marshal(map[string]string{"device_code": start.DeviceCode})
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/cli/device/poll", strings.NewReader(string(pollBody)))
	req.Header.Set("Content-Type", "application/json")

	testHandler.PollCLIDeviceAuth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("poll: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var poll DevicePollResponse
	json.NewDecoder(w.Body).Decode(&poll)
	if poll.Status != "approved" {
		t.Fatalf("status = %q, want approved", poll.Status)
	}
	if !strings.HasPrefix(poll.Token, "mul_") {
		t.Fatalf("token should start with mul_, got %q", poll.Token)
	}

	// The issued PAT should be usable: it must hit our PAT table.
	var patCount int
	err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM personal_access_token WHERE name = $1 AND user_id = $2 AND revoked = FALSE`,
		"CLI (approve-host)", testUserID,
	).Scan(&patCount)
	if err != nil {
		t.Fatalf("count PAT: %v", err)
	}
	if patCount != 1 {
		t.Fatalf("expected exactly 1 PAT, got %d", patCount)
	}

	// Clean up the PAT — cleanupDeviceCode only removes the device_code row.
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM personal_access_token WHERE name = $1 AND user_id = $2`,
			"CLI (approve-host)", testUserID)
	})
}

func TestCLIDeviceApprove_RejectsSecondAttempt(t *testing.T) {
	start := startDevice(t, "double-approve")
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM personal_access_token WHERE name LIKE 'CLI (double-approve%' AND user_id = $1`,
			testUserID)
	})

	approve := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"user_code": start.UserCode})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/cli/device/approve", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", testUserID)
		testHandler.ApproveCLIDeviceAuth(w, req)
		return w
	}

	if w := approve(); w.Code != http.StatusOK {
		t.Fatalf("first approve: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := approve(); w.Code == http.StatusOK {
		t.Fatalf("second approve should fail, got 200")
	}
}

func TestCLIDevicePoll_Pending(t *testing.T) {
	start := startDevice(t, "pending-host")

	body, _ := json.Marshal(map[string]string{"device_code": start.DeviceCode})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/poll", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	testHandler.PollCLIDeviceAuth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var poll DevicePollResponse
	json.NewDecoder(w.Body).Decode(&poll)
	if poll.Status != "pending" {
		t.Fatalf("status = %q, want pending", poll.Status)
	}
	if poll.Token != "" {
		t.Fatalf("token should be empty for pending, got %q", poll.Token)
	}
}

func TestCLIDevicePoll_UnknownDeviceCode(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"device_code": strings.Repeat("0", 64)})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/poll", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	testHandler.PollCLIDeviceAuth(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCLIDevicePoll_Expired(t *testing.T) {
	start := startDevice(t, "expiring-host")

	// Forcibly expire the code by moving its expires_at into the past.
	_, err := testPool.Exec(context.Background(),
		`UPDATE cli_device_code SET expires_at = $1 WHERE user_code = $2`,
		time.Now().Add(-time.Minute), start.UserCode)
	if err != nil {
		t.Fatalf("force-expire: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"device_code": start.DeviceCode})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/poll", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	testHandler.PollCLIDeviceAuth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var poll DevicePollResponse
	json.NewDecoder(w.Body).Decode(&poll)
	if poll.Status != "expired" {
		t.Fatalf("status = %q, want expired", poll.Status)
	}
}

func TestCLIDeviceApprove_RejectsExpired(t *testing.T) {
	start := startDevice(t, "expired-approve")

	_, err := testPool.Exec(context.Background(),
		`UPDATE cli_device_code SET expires_at = $1 WHERE user_code = $2`,
		time.Now().Add(-time.Minute), start.UserCode)
	if err != nil {
		t.Fatalf("force-expire: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"user_code": start.UserCode})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/approve", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)

	testHandler.ApproveCLIDeviceAuth(w, req)
	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCLIDeviceDeny_FlipsStatusAndPollReports(t *testing.T) {
	start := startDevice(t, "deny-host")

	body, _ := json.Marshal(map[string]string{"user_code": start.UserCode})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/deny", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)

	testHandler.DenyCLIDeviceAuth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("deny: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Poll should now show denied.
	pollBody, _ := json.Marshal(map[string]string{"device_code": start.DeviceCode})
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/cli/device/poll", strings.NewReader(string(pollBody)))
	req.Header.Set("Content-Type", "application/json")

	testHandler.PollCLIDeviceAuth(w, req)
	var poll DevicePollResponse
	json.NewDecoder(w.Body).Decode(&poll)
	if poll.Status != "denied" {
		t.Fatalf("poll status = %q, want denied", poll.Status)
	}
	if poll.Token != "" {
		t.Fatalf("denied poll should not carry a token, got %q", poll.Token)
	}
}

func TestCLIDeviceDeny_Unauthenticated(t *testing.T) {
	start := startDevice(t, "deny-unauth")

	body, _ := json.Marshal(map[string]string{"user_code": start.UserCode})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/deny", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	// no X-User-ID

	testHandler.DenyCLIDeviceAuth(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCLIDeviceDeny_UnknownCode(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"user_code": "ZZZZ-ZZZZ"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/deny", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)

	testHandler.DenyCLIDeviceAuth(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCLIDeviceDeny_RejectsAlreadyResolved(t *testing.T) {
	start := startDevice(t, "deny-resolved")

	// First approve so the code is no longer pending.
	body, _ := json.Marshal(map[string]string{"user_code": start.UserCode})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/cli/device/approve", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	testHandler.ApproveCLIDeviceAuth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approve setup: got %d", w.Code)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM personal_access_token WHERE name = $1 AND user_id = $2`,
			"CLI (deny-resolved)", testUserID)
	})

	// Now deny — must be rejected as 410.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/cli/device/deny", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	testHandler.DenyCLIDeviceAuth(w, req)
	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d: %s", w.Code, w.Body.String())
	}
}
