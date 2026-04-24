// Package authtest provides helpers used exclusively by tests of other packages
// that need to manipulate auth package internals (e.g. resetting sync.Once-backed
// config). Nothing in this package should be imported by production code.
package authtest

import "github.com/multica-ai/multica/server/internal/auth"

// ResetSmartGateConfig resets the cached SmartGate configuration. Intended only
// for tests in downstream packages (middleware, handler) that need to re-read
// env vars between test cases.
func ResetSmartGateConfig() {
	auth.ResetSmartGateConfigForTests()
}
