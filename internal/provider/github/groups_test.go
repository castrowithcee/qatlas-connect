package github

import (
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

func TestEveryGitHubToolIsGrouped(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	want := map[string][]string{}
	groupOf := map[string]string{}
	for group, tools := range expectedTools {
		for _, tool := range tools {
			want[group] = append(want[group], tool.id)
			groupOf[tool.id] = group
		}
	}
	got := map[string][]string{}
	for _, tool := range metadata.Tools {
		if tool.Group == "" {
			t.Errorf("tool %q has no group", tool.ID)
		}
		got[tool.Group] = append(got[tool.Group], tool.ID)
		if groupOf[tool.ID] != tool.Group {
			t.Errorf("tool %q is in group %q, expected in %q", tool.ID, tool.Group, groupOf[tool.ID])
		}
	}
	if len(metadata.Groups) != len(want) || len(got) != len(want) {
		t.Errorf("groups = %d, tool groups = %d, want %d groups", len(metadata.Groups), len(got), len(want))
	}
	for group, ids := range want {
		if len(got[group]) != len(ids) {
			t.Errorf("group %q holds %d tools, want %d (%v)", group, len(got[group]), len(ids), ids)
		}
	}
}
