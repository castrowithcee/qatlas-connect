package googleads

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestCatalogIsSorted(t *testing.T) {
	if !sort.SliceIsSorted(catalogFields, func(i, j int) bool { return catalogFields[i].name < catalogFields[j].name }) {
		t.Error("catalogFields is not sorted by name")
	}
	if !sort.SliceIsSorted(catalogResources, func(i, j int) bool { return catalogResources[i].name < catalogResources[j].name }) {
		t.Error("catalogResources is not sorted by name")
	}
	for i, s := range catalogSets {
		if !slices.IsSorted(s) {
			t.Errorf("catalogSets[%d] is not sorted", i)
		}
	}
	if len(catalogSets[0]) != 0 {
		t.Error("catalogSets[0] must be the empty set")
	}
	if catalogAPIVersion != "v26" {
		t.Errorf("catalog version = %s", catalogAPIVersion)
	}
}

func TestLookupField(t *testing.T) {
	f, ok := LookupField("campaign.status")
	if !ok {
		t.Fatal("campaign.status is not in the catalog")
	}
	if f.Category != CategoryAttribute || f.DataType != "ENUM" || !f.Selectable || !f.Filterable || !f.Sortable || f.Repeated {
		t.Errorf("campaign.status = %+v", f)
	}
	if !slices.Contains(f.EnumValues, "ENABLED") || !slices.Contains(f.EnumValues, "PAUSED") || f.EnumType == "" {
		t.Errorf("campaign.status enum = %s %v", f.EnumType, f.EnumValues)
	}
	// The returned values are a copy.
	f.EnumValues[0] = "CHANGED"
	if g, _ := LookupField("campaign.status"); g.EnumValues[0] == "CHANGED" {
		t.Error("EnumValues shares memory with the catalog")
	}

	m, ok := LookupField("metrics.clicks")
	if !ok || m.Category != CategoryMetric || m.DataType != "INT64" {
		t.Errorf("metrics.clicks = %+v, %v", m, ok)
	}
	if s, ok := LookupField("segments.date"); !ok || s.Category != CategorySegment || s.DataType != "DATE" {
		t.Errorf("segments.date = %+v, %v", s, ok)
	}
	if id, _ := LookupField("campaign.id"); len(id.EnumValues) != 0 || id.EnumType != "" {
		t.Errorf("campaign.id has an enum: %+v", id)
	}
	for _, name := range []string{"", "campaign", "campaign.", "campaign.nope", "Campaign.id", "campaign.id ", "metrics.clicks; DROP"} {
		if _, ok := LookupField(name); ok {
			t.Errorf("LookupField(%q) found an entry", name)
		}
	}
}

func TestLookupResource(t *testing.T) {
	names := ResourceNames()
	if len(names) < 150 || !sort.StringsAreSorted(names) {
		t.Fatalf("ResourceNames returned %d names, sorted=%v", len(names), sort.StringsAreSorted(names))
	}
	for _, name := range names {
		r, ok := LookupResource(name)
		if !ok || r.Name() != name {
			t.Fatalf("resource %s not found by its name", name)
		}
		if len(r.Fields()) == 0 {
			t.Errorf("resource %s has no fields", name)
		}
		for _, f := range r.Fields() {
			if !strings.HasPrefix(f.Name, name+".") {
				t.Errorf("resource %s lists %s", name, f.Name)
			}
		}
		for _, a := range r.AttributedResources() {
			if _, ok := LookupResource(a); !ok {
				t.Errorf("resource %s: attributed resource %s is unknown", name, a)
			}
		}
	}
	for _, name := range []string{"", "campaigns", "Campaign", "segments", "metrics", "campaign ", "ad_group_"} {
		if _, ok := LookupResource(name); ok {
			t.Errorf("LookupResource(%q) found a resource", name)
		}
	}
}

func TestResourceFieldsDoNotMixPrefixes(t *testing.T) {
	// ad_group and ad_group_ad share a name prefix; only the dot keeps their fields apart.
	r, _ := LookupResource("ad_group")
	for _, f := range r.Fields() {
		if strings.HasPrefix(f.Name, "ad_group_ad.") {
			t.Fatalf("ad_group lists %s", f.Name)
		}
	}
}

func TestCompat(t *testing.T) {
	campaign, ok := LookupResource("campaign")
	if !ok {
		t.Fatal("campaign is not a resource")
	}
	adGroup, _ := LookupResource("ad_group")
	label, _ := LookupResource("label")

	cases := []struct {
		r     Resource
		field string
		want  Compat
	}{
		{campaign, "campaign.id", CompatOK},                // own field
		{campaign, "customer.id", CompatOK},                // attributed resource
		{campaign, "ad_group.id", CompatNone},              // another resource, not attributed
		{adGroup, "campaign.name", CompatOK},               // attributed resource
		{campaign, "metrics.clicks", CompatOK},             // metric with the resource
		{campaign, "segments.date", CompatOK},              // segment with the resource
		{campaign, "segments.geo_target_city", CompatNone}, // exists, not usable with campaign in FROM
		{label, "metrics.clicks", CompatNone},              // a resource without metrics
		{campaign, "campaign.nope", CompatUnknown},         // no catalog entry
		{campaign, "metrics.nope", CompatUnknown},
		{campaign, "", CompatUnknown},
	}
	for _, c := range cases {
		if got := c.r.Compat(c.field); got != c.want {
			t.Errorf("%s.Compat(%q) = %d, want %d", c.r.Name(), c.field, got, c.want)
		}
	}
}

func TestSegmentsAndMetricsMatchCompat(t *testing.T) {
	for _, name := range ResourceNames() {
		r, _ := LookupResource(name)
		for _, f := range r.SegmentsAndMetrics() {
			if f.Category == CategoryAttribute {
				t.Fatalf("%s: SegmentsAndMetrics holds the attribute %s", name, f.Name)
			}
			if r.Compat(f.Name) != CompatOK {
				t.Fatalf("%s: %s listed but not compatible", name, f.Name)
			}
		}
	}
	label, _ := LookupResource("label")
	if got := label.SegmentsAndMetrics(); len(got) != 0 {
		t.Errorf("label has %d segments and metrics", len(got))
	}
}

func TestEnumFieldsHaveValues(t *testing.T) {
	enums := 0
	for i := range catalogFields {
		f := newField(&catalogFields[i])
		if f.DataType == "ENUM" {
			enums++
			if len(f.EnumValues) == 0 || f.EnumType == "" {
				t.Errorf("%s is an ENUM without values", f.Name)
			}
		} else if f.EnumValues != nil {
			t.Errorf("%s has enum values but is %s", f.Name, f.DataType)
		}
	}
	if enums == 0 {
		t.Error("the catalog has no enum field")
	}
}
