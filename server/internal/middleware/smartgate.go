package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/auth"
)

// smartGateCtxKey is the context key used to store the decrypted
// SmartGate identity for downstream handlers.
type smartGateCtxKey struct{}

// SmartGate returns a Chi-style middleware that validates the Tencent
// SmartGate SSO headers on the incoming request.
//
// When required is true, a missing/invalid SmartGate handshake results
// in HTTP 403 with a JSON body; the next handler is never called.
//
// When required is false, the middleware becomes best-effort: a valid
// handshake populates the request context, otherwise the request proceeds
// unchanged. This mode exists for routes that want to opportunistically
// read a SmartGate identity without mandating it.
func SmartGate(required bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !auth.SmartGateEnabled() {
				if required {
					writeSmartGateError(w, "SmartGate SSO is not enabled on this deployment")
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			identity, err := auth.ParseSmartGateHeaders(r.Header)
			if err != nil {
				if required {
					writeSmartGateError(w, err.Error())
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), smartGateCtxKey{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SmartGateFromContext retrieves the decrypted SmartGate identity placed
// on the request context by the SmartGate middleware.
func SmartGateFromContext(ctx context.Context) (*auth.SmartGateIdentity, bool) {
	if ctx == nil {
		return nil, false
	}
	identity, ok := ctx.Value(smartGateCtxKey{}).(*auth.SmartGateIdentity)
	return identity, ok
}

func writeSmartGateError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   "smartgate_forbidden",
		"message": message,
	})
}
