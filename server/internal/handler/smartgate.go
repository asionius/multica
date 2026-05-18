package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
)

// SmartGateConfig exposes whether SmartGate SSO is enabled so the
// frontend knows to attempt the silent /auth/smartgate-login handshake
// before falling back to email+code.
//
// Public route — no middleware, returns just the enabled flag.
func (h *Handler) SmartGateConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"enabled": auth.SmartGateEnabled(),
	})
}

// SmartGateLogin exchanges a validated SmartGate identity (placed on the
// request context by middleware.SmartGate) for a Multica session cookie,
// creating a matching user row on first seen login.
func (h *Handler) SmartGateLogin(w http.ResponseWriter, r *http.Request) {
	identity, ok := middleware.SmartGateFromContext(r.Context())
	if !ok || identity == nil {
		writeError(w, http.StatusForbidden, "smartgate identity missing")
		return
	}

	loginName := strings.TrimSpace(identity.LoginName)
	if loginName == "" {
		writeError(w, http.StatusBadRequest, "smartgate identity has empty LoginName")
		return
	}
	email := strings.ToLower(loginName) + "@tencent.com"

	user, _, err := h.findOrCreateUser(r.Context(), email)
	if err != nil {
		slog.Error("smartgate: findOrCreateUser failed", "error", err, "email", email)
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("smartgate: issue JWT failed", append(logger.RequestAttrs(r), "error", err, "email", email)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	if err := auth.SetAuthCookies(w, tokenString); err != nil {
		slog.Warn("smartgate: set auth cookies failed", "error", err)
	}

	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(time.Now().Add(30 * 24 * time.Hour)) {
			http.SetCookie(w, cookie)
		}
	}

	slog.Info("user logged in via smartgate",
		append(logger.RequestAttrs(r),
			"user_id", uuidToString(user.ID),
			"email", user.Email,
			"staff_id", identity.StaffID,
		)...)

	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  userToResponse(user),
	})
}
