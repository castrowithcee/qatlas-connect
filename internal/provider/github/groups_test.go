package github

import (
	"reflect"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

func TestEveryGitHubToolIsGrouped(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	counts := map[string]int{}
	for _, tool := range metadata.Tools {
		if tool.Group == "" {
			t.Errorf("tool %q has no group", tool.ID)
		}
		counts[tool.Group]++
	}
	want := map[string]int{"issues": 44, "pull-requests": 39, "projects": 41, "actions": 86, "code": 26,
		"repositories": 58, "releases": 9, "security": 31, "discussions": 16, "gists": 5, "notifications": 5, "accounts": 46, "organization-admin": 10}
	if len(metadata.Groups) != 13 || !reflect.DeepEqual(counts, want) {
		t.Errorf("groups = %d, counts = %v, want 13 groups and %v", len(metadata.Groups), counts, want)
	}
}
