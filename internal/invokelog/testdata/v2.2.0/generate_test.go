package invokelog

// This file is not built with the module: generate.sh copies it into the code of release v2.2.0 and runs it
// there, so the day files beside it are exactly what that release writes.

import (
	"os"
	"path/filepath"
	"testing"
	"testing/cryptotest"
	"time"

	"filippo.io/age"
)

func TestGenerateFixture(t *testing.T) {
	out := os.Getenv("FIXTURE_OUT")
	if out == "" {
		t.Skip("FIXTURE_OUT is not set")
	}
	cryptotest.SetGlobalRandom(t, 981)
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if id.Recipient().String() != "age1y2gxulng8qv84pn9kahajj92q8x5yr7zuaufaj0edm0e22k0afuswad364" {
		t.Fatal("the fixed test vault key changed")
	}
	key, err := DeriveKey(id)
	if err != nil {
		t.Fatal(err)
	}
	vault := t.TempDir()
	at := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return at }
	signed := New(vault, 0).WithKey(key).WithClock(clock)
	unsigned := New(vault, 0).WithClock(clock)
	size := func(n int64) *int64 { return &n }
	add := func(l *Logger, f Fields) {
		t.Helper()
		if err := l.Append(f); err != nil {
			t.Fatal(err)
		}
	}

	add(signed, Fields{Path: "cli", Operation: "canary.v220.one", Connection: "canary-v220-wiki", Effect: "read",
		Result: "success", Duration: 7 * time.Millisecond})
	at = time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)
	// Retention removes the first day and writes a signed retention cut before this entry.
	add(signed.WithRetention(1), Fields{Path: "cli", Operation: "canary.v220.two", Connection: "canary-v220-wiki",
		Effect: "read", Result: "success", Duration: 9 * time.Millisecond})
	add(unsigned, Fields{Path: "cli", Operation: "canary.v220.unsigned", Connection: "canary-v220-wiki",
		Effect: "read", Result: "success", ReqBytes: size(12), RespBytes: size(345)})
	at = time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	add(signed, Fields{Path: "mcp", Client: &ClientInfo{Name: "canary <client> & co", Version: "1.0"},
		Operation: OperationMCPInitialize, Result: "success", ReqBytes: size(210), RespBytes: size(1890),
		Session: "a1b2c3d4e5f60718"})
	at = time.Date(2026, 2, 4, 8, 0, 0, 0, time.UTC)
	add(signed, Fields{Path: "cli", Operation: OperationConnectionRename, Connection: "canary-v220-docs",
		FormerConnection: "canary-v220-wiki", ConnectionID: "0123456789abcdef0123456789abcdef", Effect: "update",
		Result: "success"})
	add(signed, Fields{Path: "cli", Operation: OperationApprove, Connection: "canary-v220-docs",
		Result: "success", Token: "canary-tok"})
	at = time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	add(signed, Fields{Path: "cli", Operation: "canary.v220.three", Connection: "canary-v220-docs",
		Effect: "delete", Result: "permission", Duration: 1500 * time.Millisecond})

	files, err := filepath.Glob(filepath.Join(vault, "logs", "*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("day files = %v, %v", files, err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, filepath.Base(file)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
