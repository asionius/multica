package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Device authorization grant for CLI authentication (RFC 8628-style).
//
// Flow:
//  1. CLI  → POST /api/cli/device/start    : obtain {device_code, user_code, verify_uri, expires_in, interval}
//  2. User → opens verify_uri?code=<user_code> in browser, logs in, approves
//  3. Browser (authenticated) → POST /api/cli/device/verify  with user_code to fetch device metadata
//  4. Browser (authenticated) → POST /api/cli/device/approve with user_code → server issues PAT
//  5. CLI  → POST /api/cli/device/poll     : returns {status, token?}
//
// This replaces the old localhost HTTP callback pattern, which required the
// browser to be able to reach the CLI's machine directly — a constraint that
// breaks when the CLI runs on a headless/remote host.

const (
	deviceCodeTTL    = 10 * time.Minute
	devicePollInterval = 2 // seconds (hint to clients)
	deviceTokenName  = "CLI (%s)"
	deviceTokenDays  = 90

	userCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // removes 0,1,I,L,O
	userCodeLen      = 8                                  // split 4-4 with dash
)

// --- Request / Response types -------------------------------------------------

type DeviceStartRequest struct {
	Hostname string `json:"hostname"`
}

type DeviceStartResponse struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	VerifyURI  string `json:"verify_uri"`
	ExpiresIn  int    `json:"expires_in"`
	Interval   int    `json:"interval"`
}

type DeviceVerifyRequest struct {
	UserCode string `json:"user_code"`
}

type DeviceVerifyResponse struct {
	Hostname    string `json:"hostname"`
	RequestedAt string `json:"requested_at"`
	ExpiresAt   string `json:"expires_at"`
}

type DeviceApproveRequest struct {
	UserCode string `json:"user_code"`
}

type DevicePollRequest struct {
	DeviceCode string `json:"device_code"`
}

type DevicePollResponse struct {
	Status string `json:"status"`
	Token  string `json:"token,omitempty"`
}

// --- Handlers -----------------------------------------------------------------

// StartCLIDeviceAuth issues a fresh device+user code pair.
// Public endpoint — the CLI has no credentials yet.
func (h *Handler) StartCLIDeviceAuth(w http.ResponseWriter, r *http.Request) {
	var req DeviceStartRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body is optional

	deviceCode, err := randomHex(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate device code")
		return
	}

	// Try a handful of times to tolerate the (astronomically unlikely) case of
	// a user_code collision with an outstanding active record.
	var record db.CliDeviceCode
	for attempt := 0; attempt < 5; attempt++ {
		userCode, err := generateUserCode()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate user code")
			return
		}

		record, err = h.Queries.CreateCLIDeviceCode(r.Context(), db.CreateCLIDeviceCodeParams{
			DeviceCode: deviceCode,
			UserCode:   userCode,
			Hostname:   req.Hostname,
			ExpiresAt: pgtype.Timestamptz{
				Time:  time.Now().Add(deviceCodeTTL),
				Valid: true,
			},
		})
		if err == nil {
			break
		}
		if !isUniqueViolation(err) {
			writeError(w, http.StatusInternalServerError, "failed to create device code")
			return
		}
		if attempt == 4 {
			writeError(w, http.StatusInternalServerError, "failed to generate unique user code")
			return
		}
	}

	writeJSON(w, http.StatusOK, DeviceStartResponse{
		DeviceCode: record.DeviceCode,
		UserCode:   record.UserCode,
		// VerifyURI is just the path; the CLI composes it with the configured app_url.
		VerifyURI: "/cli/verify",
		ExpiresIn: int(deviceCodeTTL.Seconds()),
		Interval:  devicePollInterval,
	})
}

// VerifyCLIDeviceAuth looks up a pending code by user_code and returns metadata
// so the browser can show "are you authorizing this device?" before approving.
// Requires a logged-in user.
func (h *Handler) VerifyCLIDeviceAuth(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	var req DeviceVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	code := normalizeUserCode(req.UserCode)
	if code == "" {
		writeError(w, http.StatusBadRequest, "user_code is required")
		return
	}

	record, err := h.Queries.GetCLIDeviceCodeByUserCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "code not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up code")
		return
	}
	if record.Status != "pending" {
		writeError(w, http.StatusGone, "code is no longer pending")
		return
	}
	if record.ExpiresAt.Valid && time.Now().After(record.ExpiresAt.Time) {
		writeError(w, http.StatusGone, "code expired")
		return
	}

	writeJSON(w, http.StatusOK, DeviceVerifyResponse{
		Hostname:    record.Hostname,
		RequestedAt: timestampToString(record.CreatedAt),
		ExpiresAt:   timestampToString(record.ExpiresAt),
	})
}

// ApproveCLIDeviceAuth finalizes device authorization: issues a PAT on behalf
// of the current user and binds it to the device_code.
func (h *Handler) ApproveCLIDeviceAuth(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req DeviceApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	code := normalizeUserCode(req.UserCode)
	if code == "" {
		writeError(w, http.StatusBadRequest, "user_code is required")
		return
	}

	// Load first to give precise error messages (the UPDATE ... WHERE
	// status='pending' AND expires_at>now() can't distinguish reasons).
	record, err := h.Queries.GetCLIDeviceCodeByUserCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "code not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up code")
		return
	}
	if record.Status != "pending" {
		writeError(w, http.StatusGone, "code is no longer pending")
		return
	}
	if record.ExpiresAt.Valid && time.Now().After(record.ExpiresAt.Time) {
		writeError(w, http.StatusGone, "code expired")
		return
	}

	rawToken, err := auth.GeneratePATToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	name := "CLI"
	if record.Hostname != "" {
		name = "CLI (" + record.Hostname + ")"
	}
	prefix := rawToken
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}

	if _, err := h.Queries.CreatePersonalAccessToken(r.Context(), db.CreatePersonalAccessTokenParams{
		UserID:      parseUUID(userID),
		Name:        name,
		TokenHash:   auth.HashToken(rawToken),
		TokenPrefix: prefix,
		ExpiresAt: pgtype.Timestamptz{
			Time:  time.Now().Add(deviceTokenDays * 24 * time.Hour),
			Valid: true,
		},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	if _, err := h.Queries.ApproveCLIDeviceCode(r.Context(), db.ApproveCLIDeviceCodeParams{
		UserCode: code,
		UserID:   parseUUID(userID),
		Token:    pgtype.Text{String: rawToken, Valid: true},
	}); err != nil {
		// The PAT was just persisted but we couldn't mark the device code as
		// approved, so the CLI will keep polling and never get the token.
		// The user can retry "multica login"; the orphan PAT is discoverable
		// in /api/tokens and can be revoked.
		writeError(w, http.StatusInternalServerError, "failed to approve device code")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DenyCLIDeviceAuth marks a pending device code as denied so the polling CLI
// aborts immediately instead of timing out. Safe no-op on already-resolved
// codes because the SQL filters on status='pending'. Requires a logged-in
// user — the endpoint must not be abusable anonymously.
func (h *Handler) DenyCLIDeviceAuth(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	var req DeviceApproveRequest // same shape: {user_code}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	code := normalizeUserCode(req.UserCode)
	if code == "" {
		writeError(w, http.StatusBadRequest, "user_code is required")
		return
	}

	// We look up first so we can distinguish "never existed" (404) from
	// "already resolved" (410). For a newly-pending code, the UPDATE below
	// flips it to denied.
	record, err := h.Queries.GetCLIDeviceCodeByUserCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "code not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up code")
		return
	}
	if record.Status != "pending" {
		writeError(w, http.StatusGone, "code is no longer pending")
		return
	}

	if err := h.Queries.DenyCLIDeviceCode(r.Context(), code); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to deny code")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PollCLIDeviceAuth returns the current status for a device_code and, if
// approved, the PAT. Public endpoint — the CLI holds device_code, not credentials.
func (h *Handler) PollCLIDeviceAuth(w http.ResponseWriter, r *http.Request) {
	var req DevicePollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "device_code is required")
		return
	}

	record, err := h.Queries.GetCLIDeviceCodeByDeviceCode(r.Context(), req.DeviceCode)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "device_code not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up code")
		return
	}

	// Expired overrides stored status so we never hand out a token from a
	// stale code.
	if record.ExpiresAt.Valid && time.Now().After(record.ExpiresAt.Time) && record.Status == "pending" {
		writeJSON(w, http.StatusOK, DevicePollResponse{Status: "expired"})
		return
	}

	switch record.Status {
	case "approved":
		token := ""
		if record.Token.Valid {
			token = record.Token.String
		}
		writeJSON(w, http.StatusOK, DevicePollResponse{Status: "approved", Token: token})
	case "denied":
		writeJSON(w, http.StatusOK, DevicePollResponse{Status: "denied"})
	default:
		writeJSON(w, http.StatusOK, DevicePollResponse{Status: "pending"})
	}
}

// --- Internal helpers ---------------------------------------------------------

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// generateUserCode returns an 8-character code drawn from the unambiguous
// alphabet, split with a dash in the middle (e.g. "ABCD-EFGH").
func generateUserCode() (string, error) {
	b := make([]byte, userCodeLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, userCodeLen+1)
	for i, c := range b {
		if i == userCodeLen/2 {
			out = append(out, '-')
		}
		out = append(out, userCodeAlphabet[int(c)%len(userCodeAlphabet)])
	}
	return string(out), nil
}

// normalizeUserCode upper-cases and inserts the dash so the API accepts
// user-typed variants like "abcdefgh" or "ABCD EFGH" interchangeably.
func normalizeUserCode(s string) string {
	var raw []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			raw = append(raw, c-32)
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			raw = append(raw, c)
		}
	}
	if len(raw) != userCodeLen {
		return ""
	}
	return string(raw[:userCodeLen/2]) + "-" + string(raw[userCodeLen/2:])
}
