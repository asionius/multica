package middleware

import (
	_ "unsafe" // for go:linkname
)

// resetSmartGateAuthConfig gives middleware tests a way to clear the
// memoized SmartGate config in the auth package so successive cases can
// flip env vars (SMARTGATE_ENABLED, SMARTGATE_KEY) and observe the new
// values. The reset helper is deliberately unexported in the auth package
// so it never reaches the production binary; we reach it here via
// go:linkname, which only affects the middleware test binary.
//
//go:linkname resetSmartGateAuthConfig github.com/multica-ai/multica/server/internal/auth.resetSmartGateConfigForTests
func resetSmartGateAuthConfig()
