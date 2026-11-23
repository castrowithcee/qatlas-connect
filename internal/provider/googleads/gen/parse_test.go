package main

import (
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

// The fixtures are small synthetic pages that keep only the structure the parsers read.

const overviewFixture = `<html><body><ul class="devsite-nav-section">
<li class="devsite-nav-item"><div class="devsite-nav-title devsite-nav-title-no-path"><span class="devsite-nav-text" tooltip>Query Builder</span></div>
<ul class="devsite-nav-section"><li><a href="/x/v26/ignored_query_builder">Ignored</a></li></ul></li>
<li class="devsite-nav-item"><div class="devsite-nav-title devsite-nav-title-no-path"><span class="devsite-nav-text" tooltip>Resources with metrics</span></div>
<ul class="devsite-nav-section"><li><a href="/x/v26/metrics">Metrics</a></li><li><a href="/x/v26/campaign">Campaign</a></li><li><a href="/x/v26/customer">Customer</a></li></ul></li>
<li class="devsite-nav-item"><div class="devsite-nav-title devsite-nav-title-no-path"><span class="devsite-nav-text" tooltip>Resources without metrics</span></div>
<ul class="devsite-nav-section"><li><a href="/x/v26/label">Label</a></li><li><a href="/x/v26/campaign">Campaign again</a></li></ul></li>
</ul></body></html>`

const campaignFixture = `<html><body>
<devsite-filter><table class="columns blue responsive"><thead><tr><th>Attributed resources</th></tr></thead>
<tbody class="list"><tr><td><a href="/x/v26/customer">customer</a></td></tr></tbody></table></devsite-filter>
<devsite-filter><table class="columns orange responsive"><thead><tr><th><span>layers</span>&nbsp;Segments</th></tr></thead>
<tbody class="list">
<tr><td><a href="#segments.date">date<!-- Yes, it is --></a></td></tr>
<tr><td><a href="#segments.hotel_city">hotel_city<!--  --></a></td></tr></tbody></table></devsite-filter>
<devsite-filter><table class="columns green responsive"><thead><tr><th><span>show_chart</span>&nbsp;Metrics</th></tr></thead>
<tbody class="list"><tr><td><a href="#metrics.clicks">clicks<!-- Yes, it is --></a></td></tr></tbody></table></devsite-filter>
<table class="api-fields-table"><tbody class="list">
<tr><td style="padding:0"><table class="api-fields-table blue responsive">
<tr><th colspan="2"><h2 id="campaign.status">campaign.status</h2></th></tr>
<tr><td class="descriptor">Field description</td><td><p>The status.</p></td></tr>
<tr><td>Category</td><td><code>ATTRIBUTE</code></td></tr>
<tr><td>Data Type</td><td><section class="expandable"><p class="showalways"><code>ENUM</code></p><div><code>ENABLED</code></div><div><code>PAUSED</code></div></section></td></tr>
<tr><td>Type URL</td><td><code><a href="/r">google.ads.googleads.v26.enums.CampaignStatusEnum.CampaignStatus</a></code></td></tr>
<tr><td>Capabilities</td><td>Selectable: True<br>Filterable: True<br>Sortable: True<br>Repeated: False</td></tr>
</table></td></tr>
<tr><td style="padding:0"><table class="api-fields-table blue responsive">
<tr><th colspan="2"><h2 id="campaign.id">campaign.id</h2></th></tr>
<tr><td>Data Type</td><td><code>INT64</code></td></tr>
</table></td></tr>
</tbody></table></body></html>`

func TestParseOverview(t *testing.T) {
	got, err := parseOverview(strings.NewReader(overviewFixture))
	if err != nil {
		t.Fatal(err)
	}
	// metrics is no resource, a name in both groups counts once.
	if want := []string{"campaign", "customer", "label"}; !slices.Equal(got, want) {
		t.Fatalf("resources = %v, want %v", got, want)
	}
}

func TestParseOverviewWithoutGroups(t *testing.T) {
	if _, err := parseOverview(strings.NewReader("<html><body></body></html>")); err == nil {
		t.Fatal("a page without the resource groups must fail")
	}
}

func TestParsePage(t *testing.T) {
	p, err := parsePage(strings.NewReader(campaignFixture))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.attributed, []string{"customer"}) {
		t.Errorf("attributed = %v", p.attributed)
	}
	if want := []listEntry{{"segments.date", true}, {"segments.hotel_city", false}}; !slices.Equal(p.segments, want) {
		t.Errorf("segments = %v, want %v", p.segments, want)
	}
	if want := []listEntry{{"metrics.clicks", true}}; !slices.Equal(p.metrics, want) {
		t.Errorf("metrics = %v, want %v", p.metrics, want)
	}
	if len(p.details) != 2 {
		t.Fatalf("details = %v, want the two inner tables only", p.details)
	}
	status := p.details["campaign.status"]
	if status.dataType != "ENUM" || status.typeName != "CampaignStatusEnum.CampaignStatus" ||
		!slices.Equal(status.enum, []string{"ENABLED", "PAUSED"}) {
		t.Errorf("campaign.status = %+v", status)
	}
	if id := p.details["campaign.id"]; id.dataType != "INT64" || id.typeName != "" || len(id.enum) != 0 {
		t.Errorf("campaign.id = %+v", id)
	}
}

func TestParsePageRejectsTableWithoutDataType(t *testing.T) {
	page := `<table class="api-fields-table blue"><tr><th><h2 id="a.b">a.b</h2></th></tr><tr><td>Category</td><td><code>ATTRIBUTE</code></td></tr></table>`
	if _, err := parsePage(strings.NewReader(page)); err == nil {
		t.Fatal("a field table without a data type must fail")
	}
}

func fixtureSources() sources {
	camp, _ := parsePage(strings.NewReader(campaignFixture))
	// customer and label come from reduced pages; the segments and metrics pages repeat their own tables.
	customer := &pageDoc{details: map[string]fieldDetail{"customer.id": {dataType: "INT64"}}}
	label := &pageDoc{details: map[string]fieldDetail{"label.name": {dataType: "STRING"}}}
	return sources{
		version:   "v26",
		resources: []string{"campaign", "customer", "label"},
		index: map[string]indexField{
			"campaign.id":         {Category: "ATTRIBUTE", DataType: "INT64", Filterable: true, Selectable: true, Sortable: true},
			"campaign.status":     {Category: "ATTRIBUTE", DataType: "ENUM", Filterable: true, Selectable: true, Sortable: true},
			"customer.id":         {Category: "ATTRIBUTE", DataType: "INT64", Selectable: true},
			"label.name":          {Category: "ATTRIBUTE", DataType: "STRING", Selectable: true, Repeated: true},
			"segments.date":       {Category: "SEGMENT", DataType: "DATE", Filterable: true, Selectable: true, Sortable: true},
			"segments.hotel_city": {Category: "SEGMENT", DataType: "STRING", Selectable: true},
			"metrics.clicks":      {Category: "METRIC", DataType: "INT64", Selectable: true},
		},
		pages: map[string]*pageDoc{"campaign": camp, "customer": customer, "label": label},
		segments: &pageDoc{details: map[string]fieldDetail{
			"segments.date": {dataType: "DATE"}, "segments.hotel_city": {dataType: "STRING"}}},
		metrics: &pageDoc{details: map[string]fieldDetail{"metrics.clicks": {dataType: "INT64"}}},
	}
}

func TestBuildAndRender(t *testing.T) {
	// campaign.status is listed on the campaign page only; the other pages carry no enum.
	c, err := build(fixtureSources())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.resources) != 3 || len(c.fields) != 7 || len(c.enums) != 1 {
		t.Fatalf("catalog has %d resources, %d fields, %d enums", len(c.resources), len(c.fields), len(c.enums))
	}
	camp := c.resources[0]
	if camp.name != "campaign" || !slices.Equal(camp.attributed, []string{"customer"}) {
		t.Fatalf("campaign = %+v", camp)
	}
	// Only the entries marked for the FROM clause stay: segments.date and metrics.clicks, not hotel_city.
	var with []string
	for _, i := range c.sets[camp.with] {
		with = append(with, c.fields[i].name)
	}
	if want := []string{"metrics.clicks", "segments.date"}; !slices.Equal(with, want) {
		t.Errorf("campaign segments and metrics = %v, want %v", with, want)
	}
	if c.resources[2].with != 0 {
		t.Errorf("a resource without segment tables must use the empty set")
	}

	code, err := render(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(code), header) {
		t.Error("generated file lacks the header")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "catalog_gen.go", code, 0); err != nil {
		t.Fatalf("generated source does not parse: %v", err)
	}
	again, _ := render(c)
	if string(again) != string(code) {
		t.Error("render is not deterministic")
	}
}

func TestBuildFailures(t *testing.T) {
	cases := map[string]func(*sources){
		"resource without index fields": func(s *sources) { s.resources = append(s.resources, "ghost") },
		"index owner not in overview":   func(s *sources) { s.resources = s.resources[:2] },
		"category mismatch": func(s *sources) {
			f := s.index["metrics.clicks"]
			f.Category = "SEGMENT"
			s.index["metrics.clicks"] = f
		},
		"enum without values": func(s *sources) {
			s.pages["campaign"].details["campaign.status"] = fieldDetail{dataType: "ENUM"}
		},
		"data type differs": func(s *sources) {
			s.pages["customer"].details["customer.id"] = fieldDetail{dataType: "STRING"}
		},
		"list names unknown field": func(s *sources) {
			s.pages["campaign"].segments = append(s.pages["campaign"].segments, listEntry{"segments.ghost", true})
		},
		"page lacks a field": func(s *sources) { delete(s.pages["campaign"].details, "campaign.id") },
		"attributed unknown": func(s *sources) { s.pages["campaign"].attributed = []string{"ghost"} },
		"metrics page lacks a field": func(s *sources) {
			s.metrics = &pageDoc{details: map[string]fieldDetail{}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := fixtureSources()
			mutate(&s)
			if _, err := build(s); err == nil {
				t.Fatal("build accepted inconsistent sources")
			}
		})
	}
}

func TestParseIndex(t *testing.T) {
	m, err := parseIndex(strings.NewReader(`{"a.b":{"category":"ATTRIBUTE","data_type":"INT64","filterable":true,"is_repeated":false,"selectable":true,"sortable":false,"selectable_with":["x"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if f := m["a.b"]; !f.Filterable || !f.Selectable || f.Sortable || f.Repeated || f.DataType != "INT64" {
		t.Errorf("a.b = %+v", f)
	}
	if _, err := parseIndex(strings.NewReader(`{}`)); err == nil {
		t.Error("an empty index must fail")
	}
}
