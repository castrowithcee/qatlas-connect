//go:build linux || darwin

package cli

// vaultHandoverPlatform reports whether a vault process here hands the vault over to a successor when an
// update asks it to. It is a variable only so that a test can run the update flow of either platform.
var vaultHandoverPlatform = true
