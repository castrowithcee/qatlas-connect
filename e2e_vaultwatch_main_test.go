//go:build linux || darwin || windows

package main

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	switch os.Getenv(roleEnv) {
	case roleWatchdog:
		parent, _ := strconv.Atoi(os.Getenv(parentEnv))
		runWatchdog(os.Getenv(registryEnv), parent, os.Stdin)
		os.Exit(0)
	case roleSleeper:
		time.Sleep(sleeperLifetime)
		os.Exit(0)
	}
	if err := startWatchdog(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: no watchdog for vault processes: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	stopWatchdog()
	os.Exit(code)
}
