package googleanalytics

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

func TestDeletionsOfEntitiesWithoutRestoreAreListedOnlyAndInNoProfile(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	listed := []string{dataStreamsDeleteID, mpSecretsDeleteID, edsDeleteID, customDimensionsArchiveID,
		customMetricsArchiveID, audiencesArchiveID}
	for _, id := range listed {
		d, _, found := reg.Lookup(id)
		if !found || d.Risk.Effect != capability.EffectDelete || !d.RequiresToolAllowList ||
			!strings.Contains(d.Description, "listed-only") {
			t.Errorf("%s is not a listed-only deletion: %+v", id, d)
		}
		for _, profile := range metadata.Profiles {
			if slices.Contains(profile.Tools, id) {
				t.Errorf("profile %s holds the listed-only %s", profile.ID, id)
			}
		}
	}
	// These deletions are recreated from their values or reversible, so they stay in their profile.
	kept := map[string]string{keyEventsDeleteID: "write", eventRulesDeleteID: "write", calculatedDeleteID: "write",
		annotationsDeleteID: "write", channelGroupsDeleteID: "write", adsLinksDeleteID: "write",
		firebaseLinksDeleteID: "write", bigQueryLinksDeleteID: "write", adsenseLinksDeleteID: "write",
		searchAdsLinksDeleteID: "write", dv360LinksDeleteID: "write", dv360ProposalsCancelID: "write",
		dv360ProposalsDeleteID: "write", rollupLinksDeleteID: "write", accessBindingsDeleteID: accessBindingsProfile}
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
