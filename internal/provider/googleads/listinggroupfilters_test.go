package googleads

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var listingGroupFilterToolIDs = []string{listingGroupFiltersCreateID, listingGroupFiltersUpdateID, listingGroupFiltersRemoveID}

func filtersPath(id string) string {
	return "/v26/customers/" + id + "/assetGroupListingGroupFilters:mutate"
}

var filterGroup = `"asset_group":"` + own + `/assetGroups/31"`

func filterName(id string) string { return own + "/assetGroupListingGroupFilters/31~" + id }

func filtersCall(items, extra string) string {
	return `{"customer_id":"` + clientID + `","listing_group_filters":` + items + extra + `}`
}

func filterRemoveArgs(extra string) string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + filterName("41") + `"]` + extra + `}`
}

func filterResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

// filterTreeArguments is a root with two children: one unit for new products and the rest, as the Google guide builds it.
var filterTreeArguments = filtersCall(`[{`+filterGroup+`,"temp_id":-1,"type":"SUBDIVISION","listing_source":"SHOPPING"},`+
	`{`+filterGroup+`,"type":"UNIT_INCLUDED","listing_source":"SHOPPING","parent_listing_group_filter":"`+filterName("-1")+`",`+
	`"case_value":{"product_condition":{"condition":"NEW"}}},`+
	`{`+filterGroup+`,"type":"UNIT_EXCLUDED","listing_source":"SHOPPING","parent_listing_group_filter":"`+filterName("-1")+`",`+
	`"case_value":{"product_condition":{}}}]`, "")

func TestListingGroupFilterToolsAreListedOnlyConfirmedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range listingGroupFilterToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired || d.Version != 1 {
			t.Fatalf("%s = %+v", id, d)
		}
		for _, absent := range []string{"login_customer_id", "partial_failure", "path", "id"} {
			if strings.Contains(string(d.InputSchema), `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	update, _, _ := reg.Lookup(listingGroupFiltersUpdateID)
	for _, member := range []string{`"type"`, `"listing_source"`, `"asset_group"`, `"parent_listing_group_filter"`, `"temp_id"`} {
		if strings.Contains(string(update.InputSchema), member+`:{`) {
			t.Errorf("update offers %s, which is immutable or only for creation", member)
		}
	}
	create, _, _ := reg.Lookup(listingGroupFiltersCreateID)
	for _, member := range []string{`"product_brand"`, `"product_category"`, `"category_id"`, `"product_channel"`, `"product_condition"`,
		`"product_custom_attribute"`, `"product_item_id"`, `"product_type"`, `"retail_filter_bundle"`, `"shared_set"`,
		`"webpage"`, `"custom_label"`, `"url_contains"`, `"temp_id"`, `"parent_listing_group_filter"`, `"listing_source"`,
		`"response_content_type"`, `"validate_only"`} {
		if !strings.Contains(string(create.InputSchema), member+`:{`) {
			t.Errorf("create lacks %s", member)
		}
	}
	for _, id := range []string{listingGroupFiltersCreateID, listingGroupFiltersUpdateID} {
		d, _, _ := reg.Lookup(id)
		if !strings.Contains(string(d.OutputSchema), `"path"`) {
			t.Errorf("%s does not publish the path of the answer", id)
		}
	}
}

// Every scalar member that is not part of a repeated message is a field of the catalog of v26.
func TestListingGroupFilterMembersAreFieldsOfTheCatalog(t *testing.T) {
	var walk func(n node, path string)
	walk = func(n node, path string) {
		for _, f := range n.fields {
			full := path + "." + f.name
			if f.list {
				continue
			}
			if _, ok := LookupField(full); f.kind != kindObject && !ok {
				t.Errorf("%s is not in the catalog", full)
			}
			walk(f, full)
		}
	}
	walk(filterTree, "asset_group_listing_group_filter")
}

func TestListingGroupFilterChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		listingGroupFiltersCreateID: filterTreeArguments,
		listingGroupFiltersUpdateID: filtersCall(`[{"resource_name":"`+filterName("41")+`","case_value":{"product_brand":{"value":"Acme"}}}]`, ""),
		listingGroupFiltersRemoveID: filterRemoveArgs(""),
	}
	for id, arguments := range calls {
		t.Run(id+" without the tools list", func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
				config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute}})
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a connection with all effects but no tools list ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" without confirmation", func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			_, err := e.invoke(id, arguments)
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
		t.Run(id+" outside the targets", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + otherID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), clientID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestListingGroupFilterSummaryNamesTheEffect(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	_, err := e.invoke(listingGroupFiltersCreateID, filterTreeArguments)
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Create 3", "UNIT_INCLUDED", "UNIT_EXCLUDED", "product_condition", "assetGroups/31", "advertises"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestListingGroupFilterArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	node := func(extra string) string { return filtersCall(`[{`+filterGroup+extra+`}]`, "") }
	update := func(extra string) string {
		return filtersCall(`[{"resource_name":"`+filterName("41")+`"`+extra+`}]`, "")
	}
	bad := map[string]struct{ id, arguments string }{
		"none":              {listingGroupFiltersCreateID, filtersCall(`[]`, "")},
		"no asset group":    {listingGroupFiltersCreateID, filtersCall(`[{"type":"SUBDIVISION"}]`, "")},
		"foreign group":     {listingGroupFiltersCreateID, filtersCall(`[{"asset_group":"`+foreign+`/assetGroups/31"}]`, "")},
		"temporary group":   {listingGroupFiltersCreateID, filtersCall(`[{"asset_group":"`+own+`/assetGroups/-1"}]`, "")},
		"bad type":          {listingGroupFiltersCreateID, node(`,"type":"UNIT"`)},
		"bad source":        {listingGroupFiltersCreateID, node(`,"listing_source":"FEED"`)},
		"foreign parent":    {listingGroupFiltersCreateID, node(`,"parent_listing_group_filter":"` + foreign + `/assetGroupListingGroupFilters/31~41"`)},
		"unknown temporary": {listingGroupFiltersCreateID, node(`,"parent_listing_group_filter":"` + filterName("-9") + `"`)},
		"parent kind":       {listingGroupFiltersCreateID, node(`,"parent_listing_group_filter":"` + own + `/assetGroups/31"`)},
		"positive temp":     {listingGroupFiltersCreateID, node(`,"temp_id":1`)},
		"zero temp":         {listingGroupFiltersCreateID, node(`,"temp_id":0`)},
		"text temp":         {listingGroupFiltersCreateID, node(`,"temp_id":"-1"`)},
		"temp twice":        {listingGroupFiltersCreateID, filtersCall(`[{`+filterGroup+`,"temp_id":-1},{`+filterGroup+`,"temp_id":-1}]`, "")},
		"two dimensions":    {listingGroupFiltersCreateID, node(`,"case_value":{"product_brand":{"value":"a"},"product_item_id":{"value":"b"}}`)},
		"no dimension":      {listingGroupFiltersCreateID, node(`,"case_value":{}`)},
		"unknown member":    {listingGroupFiltersCreateID, node(`,"case_value":{"product_brand":{"name":"a"}}`)},
		"bad level":         {listingGroupFiltersCreateID, node(`,"case_value":{"product_type":{"level":"LEVEL6","value":"a"}}`)},
		"bad condition":     {listingGroupFiltersCreateID, node(`,"case_value":{"product_condition":{"condition":"BROKEN"}}`)},
		"negative cat":      {listingGroupFiltersCreateID, node(`,"case_value":{"product_category":{"category_id":-1}}`)},
		"foreign bundle":    {listingGroupFiltersCreateID, node(`,"case_value":{"retail_filter_bundle":{"shared_set":"` + foreign + `/sharedSets/5"}}`)},
		"two conditions":    {listingGroupFiltersCreateID, node(`,"case_value":{"webpage":{"conditions":[{"custom_label":"a","url_contains":"b"}]}}`)},
		"empty condition":   {listingGroupFiltersCreateID, node(`,"case_value":{"webpage":{"conditions":[{}]}}`)},
		"long value":        {listingGroupFiltersCreateID, node(`,"case_value":{"product_brand":{"value":"` + strings.Repeat("a", 1001) + `"}}`)},
		"line feed":         {listingGroupFiltersCreateID, node(`,"case_value":{"product_brand":{"value":"a\nb"}}`)},
		"output only":       {listingGroupFiltersCreateID, node(`,"id":5`)},
		"path":              {listingGroupFiltersCreateID, node(`,"path":{"dimensions":[]}`)},
		"partial failure":   {listingGroupFiltersCreateID, filtersCall(`[{`+filterGroup+`}]`, `,"partial_failure":true`)},
		"content type":      {listingGroupFiltersCreateID, filtersCall(`[{`+filterGroup+`}]`, `,"response_content_type":"ALL"`)},
		"101":               {listingGroupFiltersCreateID, filtersCall(`[`+strings.Repeat(`{`+filterGroup+`},`, 100)+`{`+filterGroup+`}]`, "")},

		"update no member": {listingGroupFiltersUpdateID, update(``)},
		"update type":      {listingGroupFiltersUpdateID, update(`,"type":"UNIT_INCLUDED"`)},
		"update parent":    {listingGroupFiltersUpdateID, update(`,"parent_listing_group_filter":"` + filterName("42") + `"`)},
		"update foreign":   {listingGroupFiltersUpdateID, filtersCall(`[{"resource_name":"`+foreign+`/assetGroupListingGroupFilters/31~41","case_value":{"product_brand":{}}}]`, "")},
		"update temporary": {listingGroupFiltersUpdateID, filtersCall(`[{"resource_name":"`+filterName("-1")+`","case_value":{"product_brand":{}}}]`, "")},
		"update kind":      {listingGroupFiltersUpdateID, filtersCall(`[{"resource_name":"`+own+`/assetGroups/31","case_value":{"product_brand":{}}}]`, "")},
		"update bare id":   {listingGroupFiltersUpdateID, filtersCall(`[{"resource_name":"`+own+`/assetGroupListingGroupFilters/41","case_value":{"product_brand":{}}}]`, "")},
		"update twice": {listingGroupFiltersUpdateID, filtersCall(`[{"resource_name":"`+filterName("41")+`","case_value":{"product_brand":{}}},`+
			`{"resource_name":"`+filterName("41")+`","case_value":{"product_item_id":{}}}]`, "")},

		"remove foreign":   {listingGroupFiltersRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + foreign + `/assetGroupListingGroupFilters/31~41"]}`},
		"remove temporary": {listingGroupFiltersRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + filterName("-1") + `"]}`},
		"remove twice":     {listingGroupFiltersRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + filterName("41") + `","` + filterName("41") + `"]}`},
		"remove none":      {listingGroupFiltersRemoveID, `{"customer_id":"` + clientID + `","resource_names":[]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c.id, c.arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestListingGroupFiltersCreateSendsTheWholeTreeAsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+filtersPath(clientID)] = filterResults(filterName("41"), filterName("42"), filterName("43"))
	out, err := e.invokeConfirmed(listingGroupFiltersCreateID, filterTreeArguments)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["validate_only"] != false || len(results) != 3 || results[0].(map[string]any)["resource_name"] != filterName("41") {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+filtersPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	g := `"assetGroup":"` + own + `/assetGroups/31"`
	want := wantWire(t, `[{"create":{`+g+`,"resourceName":"`+filterName("-1")+`","type":"SUBDIVISION","listingSource":"SHOPPING"}},`+
		`{"create":{`+g+`,"type":"UNIT_INCLUDED","listingSource":"SHOPPING","parentListingGroupFilter":"`+filterName("-1")+`",`+
		`"caseValue":{"productCondition":{"condition":"NEW"}}}},`+
		`{"create":{`+g+`,"type":"UNIT_EXCLUDED","listingSource":"SHOPPING","parentListingGroupFilter":"`+filterName("-1")+`",`+
		`"caseValue":{"productCondition":{}}}}]`)
	if !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("operations = %s", p.body)
	}
}

// Every dimension of the resource is an argument and reaches the wire in the nesting of the resource.
func TestListingGroupFilterDimensionsReachTheWire(t *testing.T) {
	cases := map[string][2]string{
		"brand":     {`{"product_brand":{"value":"Acme"}}`, `{"productBrand":{"value":"Acme"}}`},
		"category":  {`{"product_category":{"category_id":267,"level":"LEVEL2"}}`, `{"productCategory":{"categoryId":267,"level":"LEVEL2"}}`},
		"channel":   {`{"product_channel":{"channel":"ONLINE"}}`, `{"productChannel":{"channel":"ONLINE"}}`},
		"condition": {`{"product_condition":{"condition":"USED"}}`, `{"productCondition":{"condition":"USED"}}`},
		"custom":    {`{"product_custom_attribute":{"index":"INDEX1","value":"x"}}`, `{"productCustomAttribute":{"index":"INDEX1","value":"x"}}`},
		"item":      {`{"product_item_id":{"value":"sku-1"}}`, `{"productItemId":{"value":"sku-1"}}`},
		"type":      {`{"product_type":{"level":"LEVEL1","value":"Shoes"}}`, `{"productType":{"level":"LEVEL1","value":"Shoes"}}`},
		"bundle":    {`{"retail_filter_bundle":{"shared_set":"` + own + `/sharedSets/5"}}`, `{"retailFilterBundle":{"sharedSet":"` + own + `/sharedSets/5"}}`},
		"webpage": {`{"webpage":{"conditions":[{"url_contains":"/sale"},{"url_contains":"/new"}]}}`,
			`{"webpage":{"conditions":[{"urlContains":"/sale"},{"urlContains":"/new"}]}}`},
		"label":       {`{"webpage":{"conditions":[{"custom_label":"promo"}]}}`, `{"webpage":{"conditions":[{"customLabel":"promo"}]}}`},
		"empty brand": {`{"product_brand":{}}`, `{"productBrand":{}}`},
		"empty page":  {`{"webpage":{}}`, `{"webpage":{}}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+filtersPath(clientID)] = filterResults(filterName("41"))
			args := filtersCall(`[{`+filterGroup+`,"type":"UNIT_INCLUDED","case_value":`+c[0]+`}]`, "")
			if _, err := e.invokeConfirmed(listingGroupFiltersCreateID, args); err != nil {
				t.Fatal(err)
			}
			ops := bodyOf(t, e.posts()[0])["operations"].([]any)
			create := ops[0].(map[string]any)["create"].(map[string]any)
			if !reflect.DeepEqual(create["caseValue"], wantWire(t, c[1])) {
				t.Errorf("caseValue = %v", create["caseValue"])
			}
		})
	}
}

func TestListingGroupFiltersUpdateNamesExactlyTheGivenMembersInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+filtersPath(clientID)] = filterResults(filterName("41"), filterName("42"), filterName("43"))
	_, err := e.invokeConfirmed(listingGroupFiltersUpdateID, filtersCall(`[`+
		`{"resource_name":"`+filterName("41")+`","case_value":{"product_brand":{"value":"Acme"}}},`+
		`{"resource_name":"`+filterName("42")+`","case_value":{"product_brand":{}}},`+
		`{"resource_name":"`+filterName("43")+`","case_value":{"webpage":{"conditions":[{"url_contains":"/a"}]}}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	masks := []string{"caseValue.productBrand.value", "caseValue.productBrand", "caseValue.webpage.conditions"}
	for i, m := range masks {
		if ops[i].(map[string]any)["updateMask"] != m {
			t.Errorf("mask %d = %v, want %s", i, ops[i].(map[string]any)["updateMask"], m)
		}
	}
	if !reflect.DeepEqual(ops[0].(map[string]any)["update"], wantWire(t, `{"resourceName":"`+filterName("41")+
		`","caseValue":{"productBrand":{"value":"Acme"}}}`)) {
		t.Errorf("update = %v", ops[0])
	}
}

func TestListingGroupFiltersRemoveKeepsTheOrderGiven(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+filtersPath(clientID)] = filterResults(filterName("42"), filterName("41"))
	out, err := e.invokeConfirmed(listingGroupFiltersRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+
		filterName("42")+`","`+filterName("41")+`"]}`)
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+filterName("42")+`"},{"remove":"`+filterName("41")+`"}]`)) ||
		len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestListingGroupFilterValidateOnlyAndContentTypeReachTheRequest(t *testing.T) {
	for id, arguments := range map[string]string{
		listingGroupFiltersCreateID: filtersCall(`[{`+filterGroup+`}]`, `,"validate_only":true,"response_content_type":"MUTABLE_RESOURCE"`),
		listingGroupFiltersUpdateID: filtersCall(`[{"resource_name":"`+filterName("41")+`","case_value":{"product_brand":{}}}]`, `,"validate_only":true`),
		listingGroupFiltersRemoveID: filterRemoveArgs(`,"validate_only":true`),
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+filtersPath(clientID)] = okReply(answer)
				out, err := e.invokeConfirmed(id, arguments)
				got := decode(t, out)
				if err != nil || got["validate_only"] != true || got["results"] != nil {
					t.Fatalf("out=%s err=%v", out, err)
				}
				if bodyOf(t, e.posts()[0])["validateOnly"] != true || len(e.posts()) != 1 {
					t.Errorf("posts = %+v", e.posts())
				}
			})
		}
	}
}

func TestListingGroupFilterMutableResourceIsMappedAndBound(t *testing.T) {
	arguments := filtersCall(`[{`+filterGroup+`,"type":"UNIT_INCLUDED","case_value":{"product_category":{"category_id":267,"level":"LEVEL2"}}}]`,
		`,"response_content_type":"MUTABLE_RESOURCE"`)
	node := func(extra string) reply {
		return okReply(`{"results":[{"resourceName":"` + filterName("41") + `","assetGroupListingGroupFilter":{"resourceName":"` +
			filterName("41") + `","assetGroup":"` + own + `/assetGroups/31","id":"41","type":"UNIT_INCLUDED","listingSource":"SHOPPING",` +
			`"caseValue":{"productCategory":{"categoryId":"267","level":"LEVEL2"}},"unknownMember":"x"` + extra + `}}]}`)
	}
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+filtersPath(clientID)] = node(`,"parentListingGroupFilter":"` + filterName("40") + `",` +
		`"path":{"dimensions":[{"productCategory":{"categoryId":"267","level":"LEVEL2"}}]}`)
	out, err := e.invokeConfirmed(listingGroupFiltersCreateID, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
	got := decode(t, out)["results"].([]any)[0].(map[string]any)["listing_group_filter"].(map[string]any)
	want := map[string]any{"resource_name": filterName("41"), "asset_group": own + "/assetGroups/31", "id": float64(41),
		"type": "UNIT_INCLUDED", "listing_source": "SHOPPING", "parent_listing_group_filter": filterName("40"),
		"case_value": map[string]any{"product_category": map[string]any{"category_id": float64(267), "level": "LEVEL2"}},
		"path": map[string]any{"dimensions": []any{map[string]any{"product_category": map[string]any{
			"category_id": float64(267), "level": "LEVEL2"}}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("node = %v", got)
	}
	for name, extra := range map[string]string{
		"foreign parent":  `,"parentListingGroupFilter":"` + foreign + `/assetGroupListingGroupFilters/31~40"`,
		"other group":     `,"parentListingGroupFilter":"` + own + `/assetGroupListingGroupFilters/32~40"`,
		"temporary":       `,"parentListingGroupFilter":"` + filterName("-1") + `"`,
		"foreign bundle":  `,"path":{"dimensions":[{"retailFilterBundle":{"sharedSet":"` + foreign + `/sharedSets/5"}}]}`,
		"foreign node":    `,"assetGroup":"` + foreign + `/assetGroups/31"`,
		"another node id": `,"id":"99"`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+filtersPath(clientID)] = node(extra)
			_, err := e.invokeConfirmed(listingGroupFiltersCreateID, arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// An answer that names another account, another asset group, another node, or another number of results than
// asked for never passes. The change was sent, so the error says that the outcome is unknown.
func TestListingGroupFilterAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	update := filtersCall(`[{"resource_name":"`+filterName("41")+`","case_value":{"product_brand":{}}}]`, "")
	create := filtersCall(`[{`+filterGroup+`}]`, "")
	remove := filterRemoveArgs("")
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result":    {listingGroupFiltersCreateID, create, `{"results":[{"resourceName":"` + foreign + `/assetGroupListingGroupFilters/31~41"}]}`},
		"other asset group": {listingGroupFiltersCreateID, create, `{"results":[{"resourceName":"` + own + `/assetGroupListingGroupFilters/32~41"}]}`},
		"temporary result":  {listingGroupFiltersCreateID, create, `{"results":[{"resourceName":"` + filterName("-1") + `"}]}`},
		"other kind":        {listingGroupFiltersCreateID, create, `{"results":[{"resourceName":"` + own + `/assetGroups/31"}]}`},
		"other node":        {listingGroupFiltersUpdateID, update, `{"results":[{"resourceName":"` + filterName("99") + `"}]}`},
		"other removed":     {listingGroupFiltersRemoveID, remove, `{"results":[{"resourceName":"` + filterName("99") + `"}]}`},
		"no name":           {listingGroupFiltersUpdateID, update, `{"results":[{}]}`},
		"no results":        {listingGroupFiltersUpdateID, update, `{}`},
		"too many":          {listingGroupFiltersCreateID, create, `{"results":[{"resourceName":"` + filterName("1") + `"},{"resourceName":"` + filterName("2") + `"}]}`},
		"partial failure":   {listingGroupFiltersUpdateID, update, `{"results":[{"resourceName":"` + filterName("41") + `"}],"partialFailureError":{"code":3}}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+filtersPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestListingGroupFilterFailuresMapWithoutProviderText(t *testing.T) {
	arguments := filterTreeArguments
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "assetGroupListingGroupFilterError", "MULTIPLE_ROOTS", ""), "",
			provider.ClassProviderError},
		"permission": {403, adsFailure(403, "PERMISSION_DENIED", "authorizationError", "USER_PERMISSION_DENIED", ""), "",
			provider.ClassPermission},
		"quota": {429, adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED",
			`{"rateScope":"ACCOUNT","retryDelay":"3s"}`), "", provider.ClassRateLimited},
		"server": {503, googleError(503, "UNAVAILABLE", ""), "unknown", provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+filtersPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(listingGroupFiltersCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+filtersPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(listingGroupFiltersCreateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// A manager account has no asset groups: a customer target that names one is refused before the change.
func TestListingGroupFilterChangesRefuseAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(listingGroupFiltersRemoveID, `{"customer_id":"`+managerID+`","resource_names":["customers/`+
		queryNumber(managerID)+`/assetGroupListingGroupFilters/31~41"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}
