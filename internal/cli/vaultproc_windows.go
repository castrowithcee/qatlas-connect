//go:build windows

package cli

// vaultHandoverPlatform reports whether a vault process here hands the vault over to a successor when an
// update asks it to. On Windows it never does: an update locks it instead, whatever the update behaviour
// says. It is a variable only so that a test can run the update flow of either platform.
var vaultHandoverPlatform = false

// sessionWarnings has nothing to read on Windows: no setting of the session ends a vault process that left
// the console it was started from.
func sessionWarnings(string) []string { return nil }
