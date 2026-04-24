package auth

// ResetSmartGateConfigForTests clears the memoized SmartGate config so
// tests in downstream packages (e.g. handler, middleware) can toggle
// SMARTGATE_ENABLED / SMARTGATE_KEY between cases and observe the new
// values on the next ParseSmartGateHeaders / SmartGateEnabled call.
//
// This helper is never referenced from any production path: the linker's
// dead-code elimination strips it from release binaries (verified by the
// SmartGate refactor test plan). It lives in a non-_test.go file so that
// other packages' test binaries can link to it — Go's _test.go visibility
// is scoped to the defining package's own test build.
func ResetSmartGateConfigForTests() { resetSmartGateConfigForTests() }
