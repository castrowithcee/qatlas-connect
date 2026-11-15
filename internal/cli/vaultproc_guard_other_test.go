//go:build !(linux || darwin || windows)

package cli

// No platform without a vault process starts a helper process that needs a guard or a registry.
const orphanEnv = "QATLAS_CLI_TEST_ORPHAN"

func guardTestBinary() {}

func registerPID() {}

func unregisterPID() {}

func runOrphan() int { return 1 }
