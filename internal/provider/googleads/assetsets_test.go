package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var assetSetToolIDs = []string{assetSetsCreateID, assetSetsUpdateID, assetSetsRemoveID}

func assetSetsPath(id string) string { return "/v26/customers/" + id + "/assetSets:mutate" }

// assetSetResource is the resource name of an asset set of the account, as Google answers it.
func assetSetResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/assetSets/" + id
}

func newAssetSetsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: assetSetToolIDs})
}

var assetSetCalls = map[string]string{
	assetSetsCreateID: `{"customer_id":"` + clientID + `","asset_sets":[{"name":"Spring pages","type":"PAGE_FEED"},` +
		`{"name":"Shop feed","type":"MERCHANT_CENTER_FEED","merchant_center_feed":{"merchant_id":123456,"feed_label":"DE"}}]}`,
	assetSetsUpdateID: `{"customer_id":"` + clientID + `","asset_sets":[{"resource_name":"customers/` + clientID +
		`/assetSets/31","name":"Summer pages"},{"resource_name":"customers/` + clientID + `/assetSets/32",` +
		`"merchant_center_feed":{"feed_label":"AT"}}]}`,
	assetSetsRemoveID: `{"customer_id":"` + clientID + `","resource_names":["customers/` + clientID + `/assetSets/31",` +
		`"customers/` + clientID + `/assetSets/32"]}`,
}

var assetSetAnswers = map[string]string{
	assetSetsCreateID: `{"results":[{"resourceName":"` + assetSetResource(clientID, "31") + `"},{"resourceName":"` +
		assetSetResource(clientID, "32") + `"}]}`,
	assetSetsUpdateID: `{"results":[{"resourceName":"` + assetSetResource(clientID, "31") + `"},{"resourceName":"` +
		assetSetResource(clientID, "32") + `"}]}`,
	assetSetsRemoveID: `{"results":[{"resourceName":"` + assetSetResource(clientID, "31") + `"},{"resourceName":"` +
		assetSetResource(clientID, "32") + `"}]}`,
}

func TestAssetSetToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{assetSetsCreateID: capability.EffectCreate,
		assetSetsUpdateID: capability.EffectUpdate, assetSetsRemoveID: capability.EffectDelete}
	for _, id := range assetSetToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		if d.RequiresToolAllowList != (id == assetSetsRemoveID) {
			t.Errorf("%s: tools list required = %v", id, d.RequiresToolAllowList)
		}
		note := changeNote
		if id == assetSetsRemoveID {
			note = mutateNote
		}
		if !strings.HasSuffix(d.Description, note) || (id != assetSetsRemoveID && strings.HasSuffix(d.Description, mutateNote)) {
			t.Errorf("%s: description does not end on the note of how it is offered", id)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) || slices.Contains(readTools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		if got := metadata.Token.Scopes[id]; !reflect.DeepEqual(got, []string{scopeAdwords}) {
			t.Errorf("scopes of %s = %v", id, got)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "path", "url", "update_mask"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
		if _, found := input.Properties["response_content_type"]; found == (id == assetSetsRemoveID) {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
		if !strings.Contains(d.Description, "outcome is unknown") || !strings.Contains(d.Description, "never repeats") ||
			!strings.Contains(d.Description, "atomic") {
			t.Errorf("%s: description lacks the unknown outcome", id)
		}
	}
	// The credential of a Business Profile appears in no schema; its email address and account ID only in the output.
	for _, id := range assetSetToolIDs {
		d, _, _ := reg.Lookup(id)
		if strings.Contains(string(d.InputSchema), "http_authorization_token") || strings.Contains(string(d.OutputSchema), "http_authorization_token") {
			t.Errorf("%s mentions the credential in a schema", id)
		}
		for _, text := range []string{"email_address", "business_account_id"} {
			if strings.Contains(string(d.InputSchema), text) {
				t.Errorf("%s offers %s as an argument", id, text)
			}
		}
		if strings.Contains(string(d.InputSchema), `"business_profile_location_set"`) {
			t.Errorf("%s offers business_profile_location_set as an argument", id)
		}
	}
	create, _, _ := reg.Lookup(assetSetsCreateID)
	for _, text := range []string{`"maxLength":128`, `"STATIC_LOCATION_GROUP"`, `"PAGE_FEED"`, `"AFFILIATE"`, `"GENERAL_RETAILERS"`,
		`"EXACT"`, `"required":["name","type"]`} {
		if !strings.Contains(string(create.InputSchema), text) {
			t.Errorf("create schema lacks %s", text)
		}
	}
	update, _, _ := reg.Lookup(assetSetsUpdateID)
	for _, text := range []string{"location_group_parent_asset_set_id", `"location_ownership_type"`, `"relationship_type"`} {
		if strings.Contains(string(update.InputSchema), text) {
			t.Errorf("update schema offers %s, which Google accepts only at creation", text)
		}
	}
	var updateInput struct {
		Properties struct {
			AssetSets struct {
				Items struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"items"`
			} `json:"asset_sets"`
		} `json:"properties"`
	}
	if json.Unmarshal(update.InputSchema, &updateInput) != nil {
		t.Fatal("update schema unreadable")
	}
	if _, found := updateInput.Properties.AssetSets.Items.Properties["type"]; found {
		t.Error("update schema offers type")
	}
}

func TestAssetSetToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+assetSetsPath(clientID)] = okReply(assetSetAnswers[id])
		_, err := e.invokeConfirmed(id, assetSetCalls[id])
		if err != nil {
			e.untouched(t)
		}
		return err == nil
	}
	cases := []struct {
		name string
		conn config.Connection
		want map[string]bool
	}{
		{"all effects, no tools list", config.Connection{Effects: all},
			map[string]bool{assetSetsCreateID: true, assetSetsUpdateID: true, assetSetsRemoveID: false}},
		{"update effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectUpdate}},
			map[string]bool{assetSetsCreateID: false, assetSetsUpdateID: true, assetSetsRemoveID: false}},
		{"read effect only", config.Connection{Effects: []config.Effect{config.EffectRead}},
			map[string]bool{assetSetsCreateID: false, assetSetsUpdateID: false, assetSetsRemoveID: false}},
		{"delete effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectDelete}},
			map[string]bool{assetSetsCreateID: false, assetSetsUpdateID: false, assetSetsRemoveID: false}},
		{"remove listed", config.Connection{Tools: []string{assetSetsRemoveID}},
			map[string]bool{assetSetsCreateID: false, assetSetsUpdateID: false, assetSetsRemoveID: true}},
		{"all listed", config.Connection{Tools: assetSetToolIDs},
			map[string]bool{assetSetsCreateID: true, assetSetsUpdateID: true, assetSetsRemoveID: true}},
	}
	for _, c := range cases {
		for _, id := range assetSetToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestAssetSetsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range assetSetToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newAssetSetsEnvironment(t, nil, nil)
			_, err := e.invoke(id, assetSetCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestAssetSetsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"name"`} {
		for _, id := range assetSetToolIDs {
			e := newAssetSetsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(assetSetCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestAssetSetsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range assetSetToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newAssetSetsEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.ReplaceAll(assetSetCalls[id], clientID, otherID)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestAssetSetArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	own := func(n string) string { return `customers/` + clientID + `/assetSets/` + n }
	long := func(n int) string { return strings.Repeat("a", n) }
	set := func(members string) string { return head + `"asset_sets":[{` + members + `}]}` }
	upd := func(members string) string {
		return head + `"asset_sets":[{"resource_name":"` + own("1") + `",` + members + `}]}`
	}
	bad := map[string][2]string{
		"create none":                  {assetSetsCreateID, head + `"asset_sets":[]}`},
		"create no name":               {assetSetsCreateID, set(`"type":"PAGE_FEED"`)},
		"create no type":               {assetSetsCreateID, set(`"name":"a"`)},
		"create empty name":            {assetSetsCreateID, set(`"name":"","type":"PAGE_FEED"`)},
		"create name too long":         {assetSetsCreateID, set(`"name":"` + long(129) + `","type":"PAGE_FEED"`)},
		"create control in name":       {assetSetsCreateID, set(`"name":"a\u0007b","type":"PAGE_FEED"`)},
		"create unknown type":          {assetSetsCreateID, set(`"name":"a","type":"NOPE"`)},
		"create unspecified type":      {assetSetsCreateID, set(`"name":"a","type":"UNSPECIFIED"`)},
		"create status":                {assetSetsCreateID, set(`"name":"a","type":"PAGE_FEED","status":"ENABLED"`)},
		"create id":                    {assetSetsCreateID, set(`"name":"a","type":"PAGE_FEED","id":5`)},
		"create travel data":           {assetSetsCreateID, set(`"name":"a","type":"PAGE_FEED","travel_feed_data":{"subset_id":1}`)},
		"create hotel data":            {assetSetsCreateID, set(`"name":"a","type":"PAGE_FEED","hotel_property_data":{"hotel_center_id":1}`)},
		"create resource name":         {assetSetsCreateID, set(`"name":"a","type":"PAGE_FEED","resource_name":"` + own("1") + `"`)},
		"create too many":              {assetSetsCreateID, head + `"asset_sets":[` + strings.Repeat(`{"name":"a","type":"PAGE_FEED"},`, 10) + `{"name":"a","type":"PAGE_FEED"}]}`},
		"create merchant no id":        {assetSetsCreateID, set(`"name":"a","type":"MERCHANT_CENTER_FEED","merchant_center_feed":{"feed_label":"DE"}`)},
		"create merchant zero":         {assetSetsCreateID, set(`"name":"a","type":"MERCHANT_CENTER_FEED","merchant_center_feed":{"merchant_id":0}`)},
		"create merchant text":         {assetSetsCreateID, set(`"name":"a","type":"MERCHANT_CENTER_FEED","merchant_center_feed":{"merchant_id":"7"}`)},
		"create ownership missing":     {assetSetsCreateID, set(`"name":"a","type":"LOCATION_SYNC","location_set":{"maps_location_set":{"maps_locations":[{"place_id":"p"}]}}`)},
		"create ownership value":       {assetSetsCreateID, set(`"name":"a","type":"LOCATION_SYNC","location_set":{"location_ownership_type":"MINE"}`)},
		"create profile token":         {assetSetsCreateID, set(`"name":"a","type":"LOCATION_SYNC","location_set":{"location_ownership_type":"BUSINESS_OWNER","business_profile_location_set":{"http_authorization_token":"x","email_address":"x"}}`)},
		"create two sources":           {assetSetsCreateID, set(`"name":"a","type":"LOCATION_SYNC","location_set":{"location_ownership_type":"BUSINESS_OWNER","maps_location_set":{"maps_locations":[{"place_id":"p"}]},"chain_location_set":{"relationship_type":"AUTO_DEALERS","chains":[{"chain_id":1}]}}`)},
		"create two kinds":             {assetSetsCreateID, set(`"name":"a","type":"LOCATION_SYNC","chain_location_group":{"dynamic_chain_location_group_filters":[{"chain_id":1}]},"business_profile_location_group":{"dynamic_business_profile_location_group_filter":{"listing_id_filters":[1]}}`)},
		"create chain no relation":     {assetSetsCreateID, set(`"name":"a","type":"LOCATION_SYNC","location_set":{"location_ownership_type":"BUSINESS_OWNER","chain_location_set":{"chains":[{"chain_id":1}]}}`)},
		"create chain no chain id":     {assetSetsCreateID, set(`"name":"a","type":"CHAIN_DYNAMIC_LOCATION_GROUP","location_group_parent_asset_set_id":4,"chain_location_group":{"dynamic_chain_location_group_filters":[{"location_attributes":["x"]}]}`)},
		"create chain id zero":         {assetSetsCreateID, set(`"name":"a","type":"CHAIN_DYNAMIC_LOCATION_GROUP","chain_location_group":{"dynamic_chain_location_group_filters":[{"chain_id":0}]}`)},
		"create chain too many":        {assetSetsCreateID, set(`"name":"a","type":"CHAIN_DYNAMIC_LOCATION_GROUP","chain_location_group":{"dynamic_chain_location_group_filters":[` + strings.Repeat(`{"chain_id":1},`, 50) + `{"chain_id":1}]}`)},
		"create maps empty":            {assetSetsCreateID, set(`"name":"a","type":"LOCATION_SYNC","location_set":{"location_ownership_type":"AFFILIATE","maps_location_set":{"maps_locations":[]}}`)},
		"create maps no list":          {assetSetsCreateID, set(`"name":"a","type":"LOCATION_SYNC","location_set":{"location_ownership_type":"AFFILIATE","maps_location_set":{}}`)},
		"create filter type":           {assetSetsCreateID, set(`"name":"a","type":"BUSINESS_PROFILE_DYNAMIC_LOCATION_GROUP","business_profile_location_group":{"dynamic_business_profile_location_group_filter":{"business_name_filter":{"business_name":"x","filter_type":"REGEX"}}}`)},
		"create parent text":           {assetSetsCreateID, set(`"name":"a","type":"STATIC_LOCATION_GROUP","location_group_parent_asset_set_id":"4"`)},
		"create bad content type":      {assetSetsCreateID, head + `"asset_sets":[{"name":"a","type":"PAGE_FEED"}],"response_content_type":"X"}`},
		"update none":                  {assetSetsUpdateID, head + `"asset_sets":[]}`},
		"update no name":               {assetSetsUpdateID, head + `"asset_sets":[{"name":"a"}]}`},
		"update foreign account":       {assetSetsUpdateID, head + `"asset_sets":[{"resource_name":"customers/` + otherID + `/assetSets/1","name":"a"}]}`},
		"update temporary id":          {assetSetsUpdateID, head + `"asset_sets":[{"resource_name":"customers/` + clientID + `/assetSets/-1","name":"a"}]}`},
		"update zero id":               {assetSetsUpdateID, head + `"asset_sets":[{"resource_name":"` + own("0") + `","name":"a"}]}`},
		"update other collection":      {assetSetsUpdateID, head + `"asset_sets":[{"resource_name":"customers/` + clientID + `/assets/1","name":"a"}]}`},
		"update nothing":               {assetSetsUpdateID, head + `"asset_sets":[{"resource_name":"` + own("1") + `"}]}`},
		"update type":                  {assetSetsUpdateID, upd(`"type":"PAGE_FEED"`)},
		"update parent":                {assetSetsUpdateID, upd(`"location_group_parent_asset_set_id":4`)},
		"update ownership":             {assetSetsUpdateID, upd(`"location_set":{"location_ownership_type":"AFFILIATE"}`)},
		"update relationship":          {assetSetsUpdateID, upd(`"location_set":{"chain_location_set":{"relationship_type":"AUTO_DEALERS"}}`)},
		"update chain without id":      {assetSetsUpdateID, upd(`"chain_location_group":{"dynamic_chain_location_group_filters":[{"location_attributes":["a"]}]}`)},
		"update empty name":            {assetSetsUpdateID, upd(`"name":""`)},
		"update twice":                 {assetSetsUpdateID, head + `"asset_sets":[{"resource_name":"` + own("1") + `","name":"a"},{"resource_name":"` + own("1") + `","name":"b"}]}`},
		"remove none":                  {assetSetsRemoveID, head + `"resource_names":[]}`},
		"remove bare id":               {assetSetsRemoveID, head + `"resource_names":["31"]}`},
		"remove foreign":               {assetSetsRemoveID, head + `"resource_names":["customers/` + otherID + `/assetSets/1"]}`},
		"remove temporary":             {assetSetsRemoveID, head + `"resource_names":["customers/` + clientID + `/assetSets/-3"]}`},
		"remove twice":                 {assetSetsRemoveID, head + `"resource_names":["` + own("1") + `","` + own("1") + `"]}`},
		"remove too many":              {assetSetsRemoveID, head + `"resource_names":["` + strings.Join(manyAssetSets(clientID, 11), `","`) + `"]}`},
		"remove content type":          {assetSetsRemoveID, head + `"resource_names":["` + own("1") + `"],"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":                 {assetSetsRemoveID, `{"customer_id":"12-34","resource_names":["` + own("1") + `"]}`},
		"validate only alone":          {assetSetsCreateID, head + `"validate_only":true}`},
		"create name of control lines": {assetSetsCreateID, set(`"name":"a\nb","type":"PAGE_FEED"`)},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newAssetSetsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newAssetSetsEnvironment(t, nil, nil)
		var items, results []string
		for i := range 10 {
			items = append(items, `{"name":"`+strings.Repeat("ä", 128)+`","type":"DYNAMIC_CUSTOM"}`)
			results = append(results, `{"resourceName":"`+assetSetResource(clientID, strconv.Itoa(i+1))+`"}`)
		}
		e.fake.routes["POST "+assetSetsPath(clientID)] = okReply(`{"results":[` + strings.Join(results, ",") + `]}`)
		if _, err := e.invokeConfirmed(assetSetsCreateID, head+`"asset_sets":[`+strings.Join(items, ",")+`]}`); err != nil {
			t.Fatal(err)
		}
	})
}

func manyAssetSets(customer string, n int) []string {
	var out []string
	for i := 1; i <= n; i++ {
		out = append(out, assetSetResource(customer, strconv.Itoa(i)))
	}
	return out
}

func TestAssetSetsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		assetSetsCreateID: `[{"create":{"name":"Spring pages","type":"PAGE_FEED"}},` +
			`{"create":{"name":"Shop feed","type":"MERCHANT_CENTER_FEED","merchantCenterFeed":{"merchantId":123456,"feedLabel":"DE"}}}]`,
		assetSetsUpdateID: `[{"update":{"resourceName":"customers/1000000003/assetSets/31","name":"Summer pages"},"updateMask":"name"},` +
			`{"update":{"resourceName":"customers/1000000003/assetSets/32","merchantCenterFeed":{"feedLabel":"AT"}},` +
			`"updateMask":"merchantCenterFeed.feedLabel"}]`,
		assetSetsRemoveID: `[{"remove":"customers/1000000003/assetSets/31"},{"remove":"customers/1000000003/assetSets/32"}]`,
	}
	for _, id := range assetSetToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newAssetSetsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+assetSetsPath(clientID)] = okReply(assetSetAnswers[id])
			out, err := e.invokeConfirmed(id, assetSetCalls[id])
			if err != nil {
				t.Fatal(err)
			}
			posts := e.posts()
			if len(posts) != 1 || posts[0].login != loginID || posts[0].developerToken != "" {
				t.Fatalf("posts = %+v", posts)
			}
			body := bodyOf(t, posts[0])
			if len(body) != 1 || !reflect.DeepEqual(body["operations"], wantWire(t, want[id])) {
				t.Errorf("body = %s", posts[0].body)
			}
			got := decode(t, out)
			results := got["results"].([]any)
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
				results[1].(map[string]any)["resource_name"] != assetSetResource(clientID, "32") {
				t.Errorf("out = %s", out)
			}
		})
	}
}

// The nested members of every kind of asset set reach the request body in the form of the API, with a mask
// that names exactly the members given on an update.
func TestAssetSetsNestedMembersAreSentAsTheApiTakesThem(t *testing.T) {
	cases := map[string]struct{ tool, arguments, operation string }{
		"chain sync": {assetSetsCreateID, `{"name":"Dealers","type":"LOCATION_SYNC","location_set":{"location_ownership_type":"AFFILIATE",` +
			`"chain_location_set":{"relationship_type":"AUTO_DEALERS","chains":[{"chain_id":11,"location_attributes":["a","b"]}]}}}`,
			`{"create":{"name":"Dealers","type":"LOCATION_SYNC","locationSet":{"locationOwnershipType":"AFFILIATE",` +
				`"chainLocationSet":{"relationshipType":"AUTO_DEALERS","chains":[{"chainId":11,"locationAttributes":["a","b"]}]}}}}`},
		"maps sync": {assetSetsCreateID, `{"name":"Maps","type":"LOCATION_SYNC","location_set":{"location_ownership_type":"BUSINESS_OWNER",` +
			`"maps_location_set":{"maps_locations":[{"place_id":"place-1"}]}}}`,
			`{"create":{"name":"Maps","type":"LOCATION_SYNC","locationSet":{"locationOwnershipType":"BUSINESS_OWNER",` +
				`"mapsLocationSet":{"mapsLocations":[{"placeId":"place-1"}]}}}}`},
		"profile group": {assetSetsCreateID, `{"name":"Group","type":"BUSINESS_PROFILE_DYNAMIC_LOCATION_GROUP","location_group_parent_asset_set_id":4,` +
			`"business_profile_location_group":{"dynamic_business_profile_location_group_filter":{"label_filters":["l"],` +
			`"business_name_filter":{"business_name":"Shop","filter_type":"EXACT"},"listing_id_filters":[7,8]}}}`,
			`{"create":{"name":"Group","type":"BUSINESS_PROFILE_DYNAMIC_LOCATION_GROUP","locationGroupParentAssetSetId":4,` +
				`"businessProfileLocationGroup":{"dynamicBusinessProfileLocationGroupFilter":{"labelFilters":["l"],` +
				`"businessNameFilter":{"businessName":"Shop","filterType":"EXACT"},"listingIdFilters":[7,8]}}}}`},
		"chain group": {assetSetsCreateID, `{"name":"Chains","type":"CHAIN_DYNAMIC_LOCATION_GROUP","location_group_parent_asset_set_id":4,` +
			`"chain_location_group":{"dynamic_chain_location_group_filters":[{"chain_id":3}]}}`,
			`{"create":{"name":"Chains","type":"CHAIN_DYNAMIC_LOCATION_GROUP","locationGroupParentAssetSetId":4,` +
				`"chainLocationGroup":{"dynamicChainLocationGroupFilters":[{"chainId":3}]}}}`},
		"update chains": {assetSetsUpdateID, `{"resource_name":"customers/` + clientID + `/assetSets/5","location_set":` +
			`{"chain_location_set":{"chains":[{"chain_id":9}]}}}`,
			`{"update":{"resourceName":"customers/1000000003/assetSets/5","locationSet":{"chainLocationSet":{"chains":[{"chainId":9}]}}},` +
				`"updateMask":"locationSet.chainLocationSet.chains"}`},
		"update filter": {assetSetsUpdateID, `{"resource_name":"customers/` + clientID + `/assetSets/5","business_profile_location_group":` +
			`{"dynamic_business_profile_location_group_filter":{"listing_id_filters":[1]}}}`,
			`{"update":{"resourceName":"customers/1000000003/assetSets/5","businessProfileLocationGroup":` +
				`{"dynamicBusinessProfileLocationGroupFilter":{"listingIdFilters":[1]}}},` +
				`"updateMask":"businessProfileLocationGroup.dynamicBusinessProfileLocationGroupFilter.listingIdFilters"}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newAssetSetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+assetSetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + assetSetResource(clientID, "5") + `"}]}`)
			key := "asset_sets"
			if _, err := e.invokeConfirmed(c.tool, `{"customer_id":"`+clientID+`","`+key+`":[`+c.arguments+`]}`); err != nil {
				t.Fatal(err)
			}
			if got := bodyOf(t, e.posts()[0])["operations"]; !reflect.DeepEqual(got, wantWire(t, "["+c.operation+"]")) {
				t.Errorf("operations = %s", e.posts()[0].body)
			}
		})
	}
}

func TestAssetSetsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range assetSetToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newAssetSetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+assetSetsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(assetSetCalls[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			body := bodyOf(t, e.posts()[0])
			got := decode(t, out)
			if body["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
				t.Errorf("body = %v, out = %s", body, out)
			}
		})
	}
	for _, id := range assetSetToolIDs {
		summary := assetSetSummary(id)
		if text := summary(json.RawMessage(strings.TrimSuffix(assetSetCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
			t.Errorf("%s: summary %q", id, text)
		}
		if text := summary(json.RawMessage(assetSetCalls[id])); strings.Contains(text, "validate") {
			t.Errorf("%s: summary %q kept state", id, text)
		}
	}
}

func TestAssetSetsResponseContentTypeShowsTheAssetSet(t *testing.T) {
	e := newAssetSetsEnvironment(t, nil, nil)
	e.fake.routes["POST "+assetSetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + assetSetResource(clientID, "31") +
		`","assetSet":{"resourceName":"` + assetSetResource(clientID, "31") + `","id":"31","name":"Summer\u0007 pages","type":"TRAVEL_FEED",` +
		`"status":"ENABLED","merchantCenterFeed":{"merchantId":"123456","feedLabel":"DE"},` +
		`"locationSet":{"locationOwnershipType":"AFFILIATE","businessProfileLocationSet":{"httpAuthorizationToken":"synthetic-token-value",` +
		`"emailAddress":"synthetic-mail-value","businessNameFilter":"Shop","labelFilters":["l"],"listingIdFilters":["9"],` +
		`"businessAccountId":"synthetic-account"},"chainLocationSet":{"relationshipType":"AUTO_DEALERS","chains":[{"chainId":"4"}]}},` +
		`"hotelPropertyData":{"hotelCenterId":"7","partnerName":"Partner"},` +
		`"travelFeedData":{"subsetId":"2","travelFeedVerticalType":"HOTELS","hotelCenterAccountId":"7"},"unknownMember":"x"}}]}`)
	out, err := e.invokeConfirmed(assetSetsUpdateID, `{"customer_id":"`+clientID+`","asset_sets":[{"resource_name":"`+
		assetSetResource(clientID, "31")+`","name":"Summer pages"}],"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	set := decode(t, out)["results"].([]any)[0].(map[string]any)["asset_set"].(map[string]any)
	if set["name"] != "Summer  pages" && set["name"] != "Summer pages" || set["status"] != "ENABLED" || set["type"] != "TRAVEL_FEED" ||
		set["id"] != float64(31) || set["unknownMember"] != nil {
		t.Errorf("asset set = %v", set)
	}
	if feed := set["merchant_center_feed"].(map[string]any); feed["merchant_id"] != float64(123456) || feed["feed_label"] != "DE" {
		t.Errorf("feed = %v", feed)
	}
	if hotel := set["hotel_property_data"].(map[string]any); hotel["hotel_center_id"] != float64(7) || hotel["partner_name"] != "Partner" {
		t.Errorf("hotel = %v", hotel)
	}
	if travel := set["travel_feed_data"].(map[string]any); travel["subset_id"] != float64(2) ||
		travel["travel_feed_vertical_type"] != "HOTELS" || travel["hotel_center_account_id"] != float64(7) {
		t.Errorf("travel = %v", travel)
	}
	profile := set["location_set"].(map[string]any)["business_profile_location_set"].(map[string]any)
	if profile["business_name_filter"] != "Shop" || profile["email_address"] != "synthetic-mail-value" ||
		profile["business_account_id"] != "synthetic-account" || len(profile) != 5 {
		t.Errorf("profile = %v", profile)
	}
	for _, leaked := range []string{"synthetic-token-value", "httpAuthorizationToken", "http_authorization_token"} {
		if strings.Contains(string(out), leaked) {
			t.Errorf("out carries %q", leaked)
		}
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// A resource name of another account, another asset set or another count never passes. The request was sent,
// so the error says that the outcome is unknown.
func TestAssetSetsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	foreign := assetSetResource(otherID, "31")
	own := func(id string) string { return `{"resourceName":"` + assetSetResource(clientID, id) + `"}` }
	withSet := func(set string) string {
		return `{"results":[{"resourceName":"` + assetSetResource(clientID, "31") + `","assetSet":` + set + `},` + own("32") + `]}`
	}
	cases := map[string]struct{ id, body string }{
		"create foreign":    {assetSetsCreateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("32") + `]}`},
		"create no results": {assetSetsCreateID, `{}`},
		"create one of two": {assetSetsCreateID, `{"results":[` + own("31") + `]}`},
		"create other kind": {assetSetsCreateID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) +
			`/assets/31"},` + own("32") + `]}`},
		"create partial failure": {assetSetsCreateID, `{"results":[` + own("31") + `,` + own("32") + `],"partialFailureError":{"code":3}}`},
		"update other set":       {assetSetsUpdateID, `{"results":[` + own("32") + `,` + own("31") + `]}`},
		"update foreign":         {assetSetsUpdateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("32") + `]}`},
		"update set foreign":     {assetSetsUpdateID, withSet(`{"resourceName":"` + foreign + `"}`)},
		"update set id":          {assetSetsUpdateID, withSet(`{"resourceName":"` + assetSetResource(clientID, "31") + `","id":"99"}`)},
		"update set shape":       {assetSetsUpdateID, withSet(`{"resourceName":"` + assetSetResource(clientID, "31") + `","name":5}`)},
		"update set not object":  {assetSetsUpdateID, withSet(`"text"`)},
		"remove foreign":         {assetSetsRemoveID, `{"results":[{"resourceName":"` + foreign + `"},` + own("32") + `]}`},
		"remove none":            {assetSetsRemoveID, `{"results":[]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newAssetSetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+assetSetsPath(clientID)] = okReply(c.body)
			arguments := assetSetCalls[c.id]
			if strings.Contains(name, "set ") {
				arguments = strings.TrimSuffix(arguments, "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
			}
			_, err := e.invokeConfirmed(c.id, arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAssetSetsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","resource_names":["` + assetSetResource(managerID, "7") + `"]}`
	answer := okReply(`{"results":[{"resourceName":"` + assetSetResource(managerID, "7") + `"}]}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newAssetSetsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+assetSetsPath(managerID)] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(assetSetsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newAssetSetsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+assetSetsPath(managerID)] = answer
		if _, err := e.invokeConfirmed(assetSetsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestAssetSetsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"asset set error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "assetSetError", "DUPLICATE_ASSET_SET_NAME", "")},
			provider.ClassProviderError, "rejected the change (assetSetError DUPLICATE_ASSET_SET_NAME), nothing was changed", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"unreadable json", okReply(`{"results":`), provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	for _, c := range cases {
		for _, id := range assetSetToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newAssetSetsEnvironment(t, nil, nil)
				e.fake.routes["POST "+assetSetsPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, assetSetCalls[id])
				if classOf(err) != c.class || !strings.Contains(err.Error(), c.message) {
					t.Fatalf("err = %v, want %s with %q", err, c.class, c.message)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != c.unknown {
					t.Errorf("unknown outcome reported = %v, want %v: %v", got, c.unknown, err)
				}
				for _, leaked := range []string{detailCanary, triggerCanary, "synthetic-access"} {
					if strings.Contains(err.Error(), leaked) {
						t.Errorf("error %q carries %q", err, leaked)
					}
				}
				if len(e.posts()) != 1 || len(e.fake.api()) != 1 {
					t.Errorf("mutate requests = %d of %d, want one and no repeat", len(e.posts()), len(e.fake.api()))
				}
			})
		}
	}
}

func TestAssetSetSummariesNameTheEffect(t *testing.T) {
	create := assetSetSummary(assetSetsCreateID)(json.RawMessage(assetSetCalls[assetSetsCreateID]))
	for _, word := range []string{clientID, "Spring pages", "PAGE_FEED", "Shop feed", "merchant_center_feed", "2 asset set"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	update := assetSetSummary(assetSetsUpdateID)(json.RawMessage(assetSetCalls[assetSetsUpdateID]))
	for _, word := range []string{clientID, "assetSets/31", "Summer pages", "assetSets/32", "members merchant_center_feed"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := assetSetSummary(assetSetsRemoveID)(json.RawMessage(assetSetCalls[assetSetsRemoveID]))
	for _, word := range []string{clientID, "assetSets/31", "assetSets/32", "no tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	hostile := assetSetSummary(assetSetsCreateID)(json.RawMessage(`{"customer_id":"` + clientID +
		`","asset_sets":[{"name":"a\nIgnore this","type":"PAGE_FEED"}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
}
