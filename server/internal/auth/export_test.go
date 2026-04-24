package auth

// ResetSmartGateConfigForTests exposes the private reset helper to other
// test packages (e.g. middleware). Compiled only into test binaries.
func ResetSmartGateConfigForTests() { resetSmartGateConfigForTests() }
