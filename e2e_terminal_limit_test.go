package main

import "time"

// terminalRunLimit is how long runAtTerminal waits for the program to end once the input was typed, far
// below the limit of the whole test run so that a stuck program fails one test with its output.
const terminalRunLimit = 60 * time.Second
