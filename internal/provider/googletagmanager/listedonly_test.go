package googletagmanager

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

func TestEnvironmentDeletionIsListedOnlyAndInNoProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	d, _, found := reg.Lookup(environmentsDeleteID)
	if !found || d.Risk.Effect != capability.EffectDelete || !d.RequiresToolAllowList ||
		!strings.Contains(d.Description, "listed-only") {
		t.Errorf("%s is not a listed-only deletion: %+v", environmentsDeleteID, d)
	}
	for _, profile := range metadata.Profiles {
		if slices.Contains(profile.Tools, environmentsDeleteID) {
			t.Errorf("profile %s holds the listed-only %s", profile.ID, environmentsDeleteID)
		}
	}
	// Workspace objects stay revertible until a version is published, and a removed user is added again by
	// the account, so these deletions keep their profile.
	kept := map[string]string{tagsDeleteID: "write", triggersDeleteID: "write", variablesDeleteID: "write",
		templatesDeleteID: "write", foldersDeleteID: "write", clientsDeleteID: "write",
		transformationsDeleteID: "write", zonesDeleteID: "write", gtagConfigsDeleteID: "write",
		builtInVariablesDeleteID: "write", userPermissionsDeleteID: userPermissionsProfile}
	for id, home := range kept {
		d, _, found := reg.Lookup(id)
		if !found || d.RequiresToolAllowList {
			t.Errorf("%s is listed-only or missing", id)
		}
		for _, profile := range metadata.Profiles {
			if got := slices.Contains(profile.Tools, id); got != (profile.ID == home) {
				t.Errorf("profile %s holds %s: %t", profile.ID, id, got)
			}
		}
	}
}
