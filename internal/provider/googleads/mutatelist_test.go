package googleads

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// listsEntries reports whether a top-level argument is an array that allows more than one entry; an array without
// maxItems is unbounded.
func listsEntries(t *testing.T, id string, schema json.RawMessage) bool {
	t.Helper()
	var s struct {
		Properties map[string]struct {
			Type     string `json:"type"`
			MaxItems *int   `json:"maxItems"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		t.Fatalf("%s: input schema: %v", id, err)
	}
	for _, p := range s.Properties {
		if p.Type == "array" && (p.MaxItems == nil || *p.MaxItems > 1) {
			return true
		}
	}
	return false
}

// TestChangeToolDescriptionsCountQuotaPerEntryOfAList keeps the quota sentence true: a changing tool that takes a
// list of entries sends one operation per entry, every other one sends exactly one.
func TestChangeToolDescriptionsCountQuotaPerEntryOfAList(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	// The array of these two tools lies inside the one operation they send.
	oneOperation := map[string]bool{customerBudgetNetworkSettingsCreateID: true, customerBudgetNetworkSettingsUpdateID: true}
	checked := 0
	for _, d := range reg.Provider(Provider) {
		if d.Risk.Effect == capability.EffectRead {
			continue
		}
		checked++
		note := changeNote
		if d.RequiresToolAllowList {
			note = mutateNote
		}
		if !oneOperation[d.ID] && listsEntries(t, d.ID, d.InputSchema) {
			note = mutateListNote
			if d.RequiresToolAllowList {
				note = mutateListListedNote
			}
		}
		if !strings.HasSuffix(d.Description, note) {
			t.Errorf("%s: description does not end on the expected quota note", d.ID)
		}
	}
	if checked == 0 {
		t.Fatal("no changing tool found")
	}
}
