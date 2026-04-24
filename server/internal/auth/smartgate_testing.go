// Package auth — this file exposes a single test-only reset helper that is
// consumed exclusively by the internal/authtest helper package. It lives in a
// non-_test.go file only so downstream test binaries can link to it (Go's
// _test.go visibility is scoped to the defining package's own test build).
//
// Do not import this directly from non-test code. Use internal/authtest instead.
package auth

// ResetSmartGateConfigForTests is exported solely for the internal/authtest
// helper package. Production code MUST NOT call this.
//
// Do not import this directly from non-test code. Use internal/authtest instead.
//
// It clears the memoized SmartGate config so tests in downstream packages
// (e.g. handler, middleware) can toggle SMARTGATE_ENABLED / SMARTGATE_KEY
// between cases and observe the new values on the next
// ParseSmartGateHeaders / SmartGateEnabled call.
//
// This helper is never referenced from any production path: the linker's
// dead-code elimination strips it from release binaries.
func ResetSmartGateConfigForTests() { resetSmartGateConfigForTests() }
