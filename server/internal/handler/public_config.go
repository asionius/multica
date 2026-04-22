package handler

import (
	"net/http"
	"os"
	"strings"
)

// PublicConfig is returned by GET /api/public/config. It tells unauthenticated
// clients (primarily the CLI during `multica login`) what they need to know
// about this deployment that can't be derived from the server URL alone.
//
// AppURL is the browser-facing origin — where users click "Authorize" during
// device-flow login. In the default compose topology the API backend and the
// frontend run on different ports (and sometimes different hosts), so the CLI
// cannot infer the browser URL from the server URL. This endpoint removes
// that guessing game: operators set MULTICA_APP_URL on the backend, and the
// CLI reads it here.
//
// Version is wired as a best-effort build stamp for diagnostics. It is not
// load-bearing; "" is an acceptable value for dev builds.
type PublicConfig struct {
	AppURL  string `json:"app_url"`
	Version string `json:"version"`
}

// buildVersion is set via -ldflags at release time:
//
//	go build -ldflags "-X github.com/multica-ai/multica/server/internal/handler.buildVersion=v1.2.3"
//
// Empty for local builds; callers must tolerate that.
var buildVersion = ""

// GetPublicConfig is a public endpoint — no auth required. It reveals only
// information the frontend already serves to anonymous visitors (its own
// origin) plus the build version, so there is nothing sensitive to gate.
func (h *Handler) GetPublicConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, PublicConfig{
		AppURL:  resolveAppURL(),
		Version: buildVersion,
	})
}

// resolveAppURL picks the first non-empty source in priority order:
//  1. MULTICA_APP_URL — explicit, single-value setting (preferred).
//  2. FRONTEND_ORIGIN — already used by CORS; reuse it so single-variable
//     self-host setups work without extra config.
//  3. "" — the CLI falls back to using the server URL as the app URL, which
//     is correct for reverse-proxied same-origin deployments.
func resolveAppURL() string {
	for _, key := range []string{"MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			// FRONTEND_ORIGIN may be a comma-separated list for CORS; we only
			// want the first entry when repurposing it as the canonical app
			// URL. Operators with multi-origin setups should set
			// MULTICA_APP_URL explicitly.
			if idx := strings.Index(v, ","); idx >= 0 {
				v = strings.TrimSpace(v[:idx])
			}
			return strings.TrimRight(v, "/")
		}
	}
	return ""
}
