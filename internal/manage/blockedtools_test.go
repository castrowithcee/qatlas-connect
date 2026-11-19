package manage

import (
	"reflect"
	"strings"
	"testing"
)

func TestSaveConfigReleasesBlocksTheToolsListCovers(t *testing.T) {
	text := strings.Replace(transferSource, "        effects:\n            - read\n        forward_secrets",
		"        effects:\n            - read\n        blocked_tools:\n            - docs.write\n            - docs.fetch\n        forward_secrets", 1)
	w := newTransferWorld(t, text)
	before, rev, err := w.store.LoadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	if got := before.Connections["wiki"].BlockedTools; len(got) != 2 {
		t.Fatalf("BlockedTools = %v", got)
	}
	after := before.Clone()
	conn := after.Connections["wiki"]
	conn.Effects = nil
	conn.Tools = []string{"docs.read", "docs.write"}
	after.Connections["wiki"] = conn
	if _, err := w.svc.SaveConfig(before, after, rev); err != nil {
		t.Fatalf("SaveConfig() = %v", err)
	}
	if got := w.load().Connections["wiki"].BlockedTools; !reflect.DeepEqual(got, []string{"docs.fetch"}) {
		t.Errorf("saved BlockedTools = %v, want only the uncovered entry", got)
	}
}

func TestExportAndImportCarryBlockedTools(t *testing.T) {
	text := strings.Replace(transferSource, "        effects:\n            - read\n        forward_secrets",
		"        effects:\n            - read\n        blocked_tools:\n            - docs.write\n        forward_secrets", 1)
	src := newTransferWorld(t, text)
	out := t.TempDir() + "/export.yaml"
	if _, err := src.svc.ExportConfig(ExportSelection{All: true}, out); err != nil {
		t.Fatal(err)
	}
	dst := newTransferWorld(t, "")
	if _, err := dst.svc.ApplyImport(mustPlan(t, dst, out, ImportModeCreate)); err != nil {
		t.Fatal(err)
	}
	if got := dst.load().Connections["wiki"].BlockedTools; !reflect.DeepEqual(got, []string{"docs.write"}) {
		t.Errorf("imported BlockedTools = %v", got)
	}
}
