//go:build windows || ((linux || darwin) && legacy_vault_names)

package vaultproc

// runSuccessorHelper runs no successor where these tests start none.
func runSuccessorHelper(helperSpec) int { return 2 }
