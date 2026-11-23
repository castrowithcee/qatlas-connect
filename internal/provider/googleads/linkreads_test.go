package googleads

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

var linkListIDs = []string{accountLinksListID, productLinksListID, productLinkInvitationsListID, dataLinksListID}

// linkResources maps each list to its resource and its fixed field set.
var linkResources = map[string]struct {
	resource string
	fields   []string
	rows     string
}{
	accountLinksListID:           {"account_link", accountLinkFields, "account_links"},
	productLinksListID:           {"product_link", productLinkFields, "product_links"},
	productLinkInvitationsListID: {"product_link_invitation", productInviteFields, "product_link_invitations"},
	dataLinksListID:              {"data_link", dataLinkFields, "data_links"},
}

// oneFilter is a valid structured filter of each list.
var oneFilter = map[string]string{accountLinksListID: `"statuses":["ENABLED"]`, productLinksListID: `"types":["GOOGLE_ADS"]`,
	productLinkInvitationsListID: `"statuses":["ACCEPTED"]`, dataLinksListID: `"statuses":["ENABLED"]`}

func newLinkReadsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: linkListIDs})
}

func accountLinkItem(customer, id string) string {
	return fmt.Sprintf(`{"accountLink":{"resourceName":"customers/%s/accountLinks/%s","accountLinkId":"%s",`+
		`"type":"THIRD_PARTY_APP_ANALYTICS","status":"ENABLED","thirdPartyAppAnalytics":{"appAnalyticsProviderId":"501",`+
		`"appId":"com.example.synthetic","appVendor":"GOOGLE_APP_STORE"}}}`, queryNumber(customer), id, id)
}

func productLinkItem(customer, id string) string {
	return fmt.Sprintf(`{"productLink":{"resourceName":"customers/%s/productLinks/%s","productLinkId":"%s",`+
		`"type":"MERCHANT_CENTER","merchantCenter":{"merchantCenterId":"7001"}}}`, queryNumber(customer), id, id)
}

func partnerLinkItem(customer, id string) string {
	return fmt.Sprintf(`{"productLink":{"resourceName":"customers/%s/productLinks/%s","productLinkId":"%s",`+
		`"type":"ADVERTISING_PARTNER","advertisingPartner":{"customer":"customers/%s"},`+
		`"advertisingPartnerProperties":{"allowedDomain":"partner.example"}}}`, queryNumber(customer), id, id, queryNumber(otherID))
}

func productInviteItem(customer, id string) string {
	return fmt.Sprintf(`{"productLinkInvitation":{"resourceName":"customers/%s/productLinkInvitations/%s",`+
		`"productLinkInvitationId":"%s","type":"HOTEL_CENTER","status":"PENDING_APPROVAL","hotelCenter":{"hotelCenterId":"8001"}}}`,
		queryNumber(customer), id, id)
}

func dataLinkItem(customer, product, id string) string {
	return fmt.Sprintf(`{"dataLink":{"resourceName":"customers/%s/dataLinks/%s~%s","dataLinkId":"%s","productLinkId":"%s",`+
		`"type":"VIDEO","status":"ENABLED","youtubeVideo":{"channelId":"UCsyntheticchannel0001","videoId":"abcdefghijk"}}}`,
		queryNumber(customer), product, id, id, product)
}

func linkItemOf(id, customer string) string {
	switch id {
	case accountLinksListID:
		return accountLinkItem(customer, "11")
	case productLinksListID:
		return productLinkItem(customer, "11")
	case productLinkInvitationsListID:
		return productInviteItem(customer, "11")
	}
	return dataLinkItem(customer, "5", "11")
}

func TestLinkListsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range linkListIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != capability.EffectRead || !d.Risk.OpenWorld ||
			len(d.OutputSchema) == 0 || d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Fatalf("%s = %+v", id, d)
		}
		if !slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("%s is not in the read profile", id)
		}
		if got := metadata.Token.Scopes[id]; !reflect.DeepEqual(got, []string{scopeAdwords}) {
			t.Errorf("scopes of %s = %v", id, got)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
			Required   []string                   `json:"required"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional || !reflect.DeepEqual(input.Required, []string{"customer_id"}) {
			t.Errorf("%s: input schema = %s", id, d.InputSchema)
		}
		for _, free := range []string{"fields", "field", "query", "resource", "conditions", "where", "order_by", "orderings",
			"select", "limit", "page_token", "login_customer_id", "validate_only", "path", "url", "parameters"} {
			if _, found := input.Properties[free]; found {
				t.Errorf("%s offers the argument %s", id, free)
			}
		}
		if len(input.Properties) != len(d.Arguments) {
			t.Errorf("%s: %d schema properties but %d documented arguments", id, len(input.Properties), len(d.Arguments))
		}
	}
	// No tool regenerates the sharing value of an app analytics link, creates or changes a link.
	for _, tool := range metadata.Tools {
		if (strings.Contains(tool.ID, "third-party-app-analytics") && tool.Effect != config.EffectRead) ||
			(strings.Contains(tool.ID, "link") && tool.Effect != config.EffectRead && !strings.Contains(tool.ID, "manager") &&
				!strings.Contains(tool.ID, "client") && !slices.Contains(linkRemovalToolIDs, tool.ID)) {
			t.Errorf("tool %s must not exist in this group", tool.ID)
		}
	}
}

// The field sets are fixed: every query selects exactly the set defined in the code, the set holds every field
// the field reference lets a query select on the resource, and no field of the left-out kind is among them.
func TestLinkListsSelectFixedFieldSets(t *testing.T) {
	for id, spec := range linkResources {
		t.Run(id, func(t *testing.T) {
			var reference []string
			for _, f := range catalogFields {
				if strings.HasPrefix(f.name, spec.resource+".") && f.category == CategoryAttribute && f.flags&flagSelectable != 0 {
					reference = append(reference, strings.TrimPrefix(f.name, spec.resource+"."))
				}
			}
			a, b := slices.Clone(reference), slices.Clone(spec.fields)
			slices.Sort(a)
			slices.Sort(b)
			if !slices.Equal(a, b) {
				t.Errorf("fixed fields = %v, selectable fields of the resource = %v", b, a)
			}
			for _, field := range spec.fields {
				if f, ok := LookupField(spec.resource + "." + field); !ok || !f.Selectable {
					t.Errorf("%s.%s is not a selectable field of the catalog", spec.resource, field)
				}
			}
			selects := make([]string, len(spec.fields))
			for i, field := range spec.fields {
				selects[i] = spec.resource + "." + field
			}
			want := "SELECT " + strings.Join(selects, ", ") + " FROM " + spec.resource
			for _, left := range []string{"shareable_link_id", "youtube_link_metadata", "brand_channel_id", "passkey", "secret",
				"token", "metrics.", "segments."} {
				if strings.Contains(want, left) {
					t.Errorf("fixed set holds %s", left)
				}
			}
			for _, arguments := range []string{`{"customer_id":"` + clientID + `"}`,
				`{"customer_id":"` + clientID + `","page_size":5}`,
				`{"customer_id":"` + clientID + `",` + oneFilter[id] + `}`} {
				e := newLinkReadsEnvironment(t, nil, nil)
				e.fake.routes["POST "+searchPath(clientID)] = rows()
				if _, err := e.invoke(id, arguments); err != nil {
					t.Fatal(err)
				}
				api := e.fake.api()
				if len(api) != 1 || !strings.HasPrefix(api[0].query, want+" ") {
					t.Errorf("query = %q, want the prefix %q", api[0].query, want)
				}
			}
		})
	}
}

// No argument adds a field, a resource, free GAQL or a condition other than the structured filters.
func TestLinkListsRefuseFreeQueryArguments(t *testing.T) {
	for _, id := range linkListIDs {
		for _, extra := range []string{`"fields":["customer.id"]`, `"field":"account_link.status"`,
			`"query":"SELECT customer.id FROM customer"`, `"resource":"customer"`, `"conditions":[{"field":"x"}]`,
			`"where":"1=1"`, `"order_by":"customer.id"`, `"limit":1`, `"page_token":"x"`, `"parameters":"x"`,
			`"login_customer_id":"` + loginID + `"`, `"validate_only":true`, `"customer_ids":["` + clientID + `"]`} {
			e := newLinkReadsEnvironment(t, nil, nil)
			if _, err := e.invoke(id, `{"customer_id":"`+clientID+`",`+extra+`}`); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestLinkEnumListsFollowTheFieldReference(t *testing.T) {
	for field, values := range map[string][]string{
		"account_link.status": accountLinkStatuses, "account_link.type": accountLinkTypes,
		"account_link.third_party_app_analytics.app_vendor": appVendors, "product_link.type": productLinkTypes,
		"product_link_invitation.type": productLinkTypes, "product_link_invitation.status": productInviteStatuses,
		"data_link.status": dataLinkStatuses, "data_link.type": dataLinkTypes} {
		f, ok := LookupField(field)
		if !ok || !f.Filterable {
			t.Fatalf("%s is not a filterable field", field)
		}
		var want []string
		for _, v := range f.EnumValues {
			if v != "UNSPECIFIED" && v != "UNKNOWN" {
				want = append(want, v)
			}
		}
		got := slices.Clone(values)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s: values = %v, reference = %v", field, got, want)
		}
	}
}

func TestLinkListsCheckTargetsBeforeSecretAndIO(t *testing.T) {
	for _, id := range linkListIDs {
		t.Run(id, func(t *testing.T) {
			e := newLinkReadsEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invoke(id, `{"customer_id":"`+otherID+`"}`)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
			e = newLinkReadsEnvironment(t, nil, nil)
			for _, bad := range []string{"123-456-7890", "123456789", "", "1234567890 OR 1=1"} {
				if _, err := e.invoke(id, `{"customer_id":"`+bad+`"}`); err == nil {
					t.Errorf("customer_id %q accepted", bad)
				}
			}
			e.untouched(t)
		})
	}
}

func TestLinkFiltersAreCheckedBeforeSecretAndIO(t *testing.T) {
	bad := map[string][]string{
		accountLinksListID: {`"statuses":["REMOVED","REMOVED"]`, `"statuses":["DISABLED"]`, `"statuses":["UNKNOWN"]`,
			`"types":["OTHER"]`, `"app_vendors":["WINDOWS"]`, `"account_link_ids":["1 OR 1=1"]`, `"account_link_ids":["0"]`,
			`"app_id":"a'b"`, `"app_id":"a b"`, `"app_analytics_provider_ids":["x"]`, `"page_size":0`, `"page_size":1001`,
			`"cursor":"not-a-cursor"`},
		productLinksListID: {`"types":["MERCHANT"]`, `"product_link_ids":["-1"]`, `"data_partner_ids":["a"]`,
			`"merchant_center_ids":["0"]`, `"google_ads_customer_id":"123-456"`, `"google_ads_customer_id":"customers/1234567890"`,
			`"advertising_partner_customer_id":"1"`, `"allowed_domain":"a'b"`, `"allowed_domain":"a b.example"`},
		productLinkInvitationsListID: {`"statuses":["PENDING"]`, `"types":["ADVERTISING"]`, `"product_link_invitation_ids":["x"]`,
			`"hotel_center_ids":["1.5"]`, `"merchant_center_ids":["0"]`, `"allowed_domain":"a\\b"`},
		dataLinksListID: {`"statuses":["ACCEPTED"]`, `"types":["IMAGE"]`, `"data_link_ids":["0"]`, `"product_link_ids":["x"]`,
			`"channel_id":"a'b"`, `"channel_id":"a b"`, `"video_id":"short"`, `"video_id":"abcdefghijk'"`, `"cursor":"x"`},
	}
	for id, list := range bad {
		for _, filter := range list {
			e := newLinkReadsEnvironment(t, nil, nil)
			if _, err := e.invoke(id, `{"customer_id":"`+clientID+`",`+filter+`}`); err == nil {
				t.Errorf("%s accepted %s", id, filter)
			}
			e.untouched(t)
		}
	}
	// The handlers refuse as well when the schema is bypassed.
	e := newLinkReadsEnvironment(t, nil, nil)
	handlers := map[string]func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor, json.RawMessage) (any, error){
		accountLinksListID: handleAccountLinksList, productLinksListID: handleProductLinksList,
		productLinkInvitationsListID: handleProductLinkInvitationsList, dataLinksListID: handleDataLinksList}
	for id, filter := range map[string]string{accountLinksListID: `"app_id":"a'b"`, productLinksListID: `"google_ads_customer_id":"1"`,
		productLinkInvitationsListID: `"statuses":["ACCEPTED'"]`, dataLinksListID: `"video_id":"a'b"`} {
		_, err := handlers[id](context.Background(), e.resolved(), e.secrets(), e.red,
			json.RawMessage(`{"customer_id":"`+clientID+`",`+filter+`}`))
		if !isInvalidRequest(err) {
			t.Errorf("%s handler accepted %s: %v", id, filter, err)
		}
	}
	e.untouched(t)
}

func TestAccountLinksListMapsAndPages(t *testing.T) {
	e := newLinkReadsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+searchPath(clientID)] = rows(accountLinkItem(clientID, "11"), accountLinkItem(clientID, "12"),
		accountLinkItem(clientID, "13"))
	args := `{"customer_id":"` + clientID + `","page_size":2,"statuses":["ENABLED","REQUESTED"],"types":["THIRD_PARTY_APP_ANALYTICS"],` +
		`"account_link_ids":["11","12"],"app_vendors":["GOOGLE_APP_STORE"],"app_id":"com.example.synthetic",` +
		`"app_analytics_provider_ids":["501"]`
	out, err := e.invoke(accountLinksListID, args+`}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	list := got["account_links"].([]any)
	first := list[0].(map[string]any)
	app := first["third_party_app_analytics"].(map[string]any)
	if len(list) != 2 || got["has_more"] != true || got["next_cursor"] == "" || first["account_link_id"] != "11" ||
		first["resource_name"] != "customers/"+queryNumber(clientID)+"/accountLinks/11" || first["status"] != "ENABLED" ||
		first["type"] != "THIRD_PARTY_APP_ANALYTICS" || app["app_analytics_provider_id"] != "501" ||
		app["app_id"] != "com.example.synthetic" || app["app_vendor"] != "GOOGLE_APP_STORE" {
		t.Errorf("out = %s", out)
	}
	want := "SELECT account_link.resource_name, account_link.account_link_id, account_link.type, account_link.status, " +
		"account_link.third_party_app_analytics.app_analytics_provider_id, account_link.third_party_app_analytics.app_id, " +
		"account_link.third_party_app_analytics.app_vendor FROM account_link WHERE account_link.status IN (ENABLED, REQUESTED) " +
		"AND account_link.type IN (THIRD_PARTY_APP_ANALYTICS) AND account_link.account_link_id IN (11, 12) " +
		"AND account_link.third_party_app_analytics.app_vendor IN (GOOGLE_APP_STORE) " +
		"AND account_link.third_party_app_analytics.app_id = 'com.example.synthetic' " +
		"AND account_link.third_party_app_analytics.app_analytics_provider_id IN (501) " +
		"ORDER BY account_link.account_link_id LIMIT 3"
	if api := e.fake.api(); len(api) != 1 || api[0].query != want || api[0].login != loginID {
		t.Errorf("request = %+v\nwant %s", api, want)
	}
	cursor := got["next_cursor"].(string)
	e.fake.routes["POST "+searchPath(clientID)] = rows(accountLinkItem(clientID, "13"))
	out, err = e.invoke(accountLinksListID, args+`,"cursor":"`+cursor+`"}`)
	if err != nil || decode(t, out)["has_more"] != false ||
		!strings.Contains(e.fake.api()[1].query, "account_link.account_link_id > 12 ORDER BY") {
		t.Fatalf("out = %s, err = %v, query = %s", out, err, e.fake.api()[1].query)
	}
	if _, err := e.invoke(accountLinksListID, `{"customer_id":"`+clientID+`","cursor":"`+cursor+`"}`); err == nil {
		t.Error("a cursor of other arguments was accepted")
	}
}

func TestProductLinksListMaps(t *testing.T) {
	e := newLinkReadsEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = rows(productLinkItem(clientID, "21"), partnerLinkItem(clientID, "22"))
	out, err := e.invoke(productLinksListID, `{"customer_id":"`+clientID+`","types":["MERCHANT_CENTER","ADVERTISING_PARTNER"],`+
		`"product_link_ids":["21","22"],"data_partner_ids":["3"],"merchant_center_ids":["7001"],`+
		`"google_ads_customer_id":"`+otherID+`","advertising_partner_customer_id":"`+otherID+`","allowed_domain":"partner.example"}`)
	if err != nil {
		t.Fatal(err)
	}
	list := decode(t, out)["product_links"].([]any)
	first, second := list[0].(map[string]any), list[1].(map[string]any)
	if len(list) != 2 || first["merchant_center_id"] != "7001" || first["type"] != "MERCHANT_CENTER" ||
		first["resource_name"] != "customers/"+queryNumber(clientID)+"/productLinks/21" ||
		second["advertising_partner"].(map[string]any)["customer"] != "customers/"+queryNumber(otherID) ||
		second["advertising_partner_properties"].(map[string]any)["allowed_domain"] != "partner.example" {
		t.Errorf("out = %s", out)
	}
	want := "SELECT product_link.resource_name, product_link.product_link_id, product_link.type, " +
		"product_link.data_partner.data_partner_id, product_link.google_ads.customer, " +
		"product_link.merchant_center.merchant_center_id, product_link.advertising_partner.customer, " +
		"product_link.advertising_partner_properties.allowed_domain FROM product_link WHERE " +
		"product_link.type IN (MERCHANT_CENTER, ADVERTISING_PARTNER) AND product_link.product_link_id IN (21, 22) AND " +
		"product_link.data_partner.data_partner_id IN (3) AND product_link.merchant_center.merchant_center_id IN (7001) AND " +
		"product_link.google_ads.customer = 'customers/" + queryNumber(otherID) + "' AND " +
		"product_link.advertising_partner.customer = 'customers/" + queryNumber(otherID) + "' AND " +
		"product_link.advertising_partner_properties.allowed_domain = 'partner.example' " +
		"ORDER BY product_link.product_link_id LIMIT 101"
	if api := e.fake.api(); len(api) != 1 || api[0].query != want {
		t.Errorf("request = %+v\nwant %s", api, want)
	}
}

func TestProductLinkInvitationsListMaps(t *testing.T) {
	e := newLinkReadsEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = rows(productInviteItem(clientID, "31"))
	out, err := e.invoke(productLinkInvitationsListID, `{"customer_id":"`+clientID+`","statuses":["PENDING_APPROVAL","EXPIRED"],`+
		`"types":["HOTEL_CENTER"],"product_link_invitation_ids":["31"],"merchant_center_ids":["1"],"hotel_center_ids":["8001"],`+
		`"advertising_partner_customer_id":"`+otherID+`","allowed_domain":"partner.example"}`)
	if err != nil {
		t.Fatal(err)
	}
	item := decode(t, out)["product_link_invitations"].([]any)[0].(map[string]any)
	if item["product_link_invitation_id"] != "31" || item["status"] != "PENDING_APPROVAL" || item["type"] != "HOTEL_CENTER" ||
		item["hotel_center_id"] != "8001" || item["resource_name"] != "customers/"+queryNumber(clientID)+"/productLinkInvitations/31" {
		t.Errorf("item = %v", item)
	}
	want := "SELECT product_link_invitation.resource_name, product_link_invitation.product_link_invitation_id, " +
		"product_link_invitation.type, product_link_invitation.status, " +
		"product_link_invitation.merchant_center.merchant_center_id, product_link_invitation.hotel_center.hotel_center_id, " +
		"product_link_invitation.advertising_partner.customer, " +
		"product_link_invitation.advertising_partner_properties.allowed_domain FROM product_link_invitation WHERE " +
		"product_link_invitation.status IN (PENDING_APPROVAL, EXPIRED) AND product_link_invitation.type IN (HOTEL_CENTER) AND " +
		"product_link_invitation.product_link_invitation_id IN (31) AND " +
		"product_link_invitation.merchant_center.merchant_center_id IN (1) AND " +
		"product_link_invitation.hotel_center.hotel_center_id IN (8001) AND " +
		"product_link_invitation.advertising_partner.customer = 'customers/" + queryNumber(otherID) + "' AND " +
		"product_link_invitation.advertising_partner_properties.allowed_domain = 'partner.example' " +
		"ORDER BY product_link_invitation.product_link_invitation_id LIMIT 101"
	if api := e.fake.api(); len(api) != 1 || api[0].query != want {
		t.Errorf("request = %+v\nwant %s", api, want)
	}
}

func TestDataLinksListMapsAndPagesOverTheCompositeKey(t *testing.T) {
	e := newLinkReadsEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = rows(dataLinkItem(clientID, "5", "11"), dataLinkItem(clientID, "5", "12"),
		dataLinkItem(clientID, "9", "3"))
	args := `{"customer_id":"` + clientID + `","page_size":2,"statuses":["ENABLED"],"types":["VIDEO"],"data_link_ids":["11","12","3"],` +
		`"product_link_ids":["5","9"],"channel_id":"UCsyntheticchannel0001","video_id":"abcdefghijk"`
	out, err := e.invoke(dataLinksListID, args+`}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	list := got["data_links"].([]any)
	first := list[0].(map[string]any)
	video := first["youtube_video"].(map[string]any)
	if len(list) != 2 || got["has_more"] != true || first["data_link_id"] != "11" || first["product_link_id"] != "5" ||
		first["resource_name"] != "customers/"+queryNumber(clientID)+"/dataLinks/5~11" || first["type"] != "VIDEO" ||
		first["status"] != "ENABLED" || video["channel_id"] != "UCsyntheticchannel0001" || video["video_id"] != "abcdefghijk" {
		t.Errorf("out = %s", out)
	}
	want := "SELECT data_link.resource_name, data_link.data_link_id, data_link.product_link_id, data_link.type, " +
		"data_link.status, data_link.youtube_video.channel_id, data_link.youtube_video.video_id FROM data_link WHERE " +
		"data_link.status IN (ENABLED) AND data_link.type IN (VIDEO) AND data_link.data_link_id IN (11, 12, 3) AND " +
		"data_link.product_link_id IN (5, 9) AND data_link.youtube_video.channel_id = 'UCsyntheticchannel0001' AND " +
		"data_link.youtube_video.video_id = 'abcdefghijk' " +
		"ORDER BY data_link.product_link_id, data_link.data_link_id LIMIT 3"
	if api := e.fake.api(); len(api) != 1 || api[0].query != want {
		t.Errorf("request = %+v\nwant %s", api, want)
	}
	// The next page reads the rest of the product link, then the product links that follow.
	cursor := got["next_cursor"].(string)
	e.fake.answer = func(s seen) *reply {
		if strings.Contains(s.query, "data_link.product_link_id = 5 AND") {
			r := rows()
			return &r
		}
		r := rows(dataLinkItem(clientID, "9", "3"))
		return &r
	}
	out, err = e.invoke(dataLinksListID, args+`,"cursor":"`+cursor+`"}`)
	api := e.fake.api()
	if err != nil || len(api) != 3 || decode(t, out)["has_more"] != false ||
		len(decode(t, out)["data_links"].([]any)) != 1 ||
		!strings.Contains(api[1].query, "data_link.product_link_id = 5 AND data_link.data_link_id > 12 ORDER BY data_link.data_link_id LIMIT 3") ||
		!strings.Contains(api[2].query, "data_link.product_link_id > 5 ORDER BY data_link.product_link_id, data_link.data_link_id LIMIT 3") {
		t.Fatalf("out = %s, err = %v, requests = %+v", out, err, api)
	}
	// A first read that fills the page needs no second one.
	e2 := newLinkReadsEnvironment(t, nil, nil)
	e2.fake.routes["POST "+searchPath(clientID)] = rows(dataLinkItem(clientID, "5", "11"), dataLinkItem(clientID, "5", "12"),
		dataLinkItem(clientID, "5", "13"))
	out, err = e2.invoke(dataLinksListID, `{"customer_id":"`+clientID+`","page_size":2}`)
	if err != nil {
		t.Fatal(err)
	}
	e2.fake.routes["POST "+searchPath(clientID)] = rows(dataLinkItem(clientID, "5", "13"), dataLinkItem(clientID, "5", "14"),
		dataLinkItem(clientID, "5", "15"))
	if out, err = e2.invoke(dataLinksListID, `{"customer_id":"`+clientID+`","page_size":2,"cursor":"`+
		decode(t, out)["next_cursor"].(string)+`"}`); err != nil || len(e2.fake.api()) != 2 ||
		decode(t, out)["has_more"] != true {
		t.Errorf("out = %s, err = %v, requests = %+v", out, err, e2.fake.api())
	}
}

func TestLinkReadsRefuseWrongOrForeignRows(t *testing.T) {
	foreign := map[string][]string{
		accountLinksListID: {accountLinkItem(otherID, "21"),
			strings.Replace(accountLinkItem(clientID, "21"), `"accountLinkId":"21"`, `"accountLinkId":"22"`, 1),
			`{"accountLink":{"accountLinkId":"-4"}}`, `{"accountLink":{"resourceName":"x"}}`, `{}`,
			strings.Replace(accountLinkItem(clientID, "21"), `"appAnalyticsProviderId":"501"`, `"appAnalyticsProviderId":"0"`, 1)},
		productLinksListID: {productLinkItem(otherID, "21"),
			strings.Replace(productLinkItem(clientID, "21"), `"productLinkId":"21"`, `"productLinkId":"22"`, 1), `{}`,
			strings.Replace(partnerLinkItem(clientID, "22"), `"customer":"customers/`+queryNumber(otherID)+`"`, `"customer":"accounts/1"`, 1),
			strings.Replace(productLinkItem(clientID, "21"), `"merchantCenterId":"7001"`, `"merchantCenterId":"x"`, 1)},
		productLinkInvitationsListID: {productInviteItem(otherID, "21"),
			strings.Replace(productInviteItem(clientID, "21"), `"productLinkInvitationId":"21"`, `"productLinkInvitationId":"22"`, 1),
			`{}`, strings.Replace(productInviteItem(clientID, "21"), `"hotelCenterId":"8001"`, `"hotelCenterId":"-1"`, 1)},
		dataLinksListID: {dataLinkItem(otherID, "5", "21"),
			strings.Replace(dataLinkItem(clientID, "5", "21"), `"productLinkId":"5"`, `"productLinkId":"6"`, 1),
			strings.Replace(dataLinkItem(clientID, "5", "21"), `"dataLinkId":"21"`, `"dataLinkId":"22"`, 1), `{}`,
			`{"dataLink":{"dataLinkId":"1"}}`},
	}
	for id, bad := range foreign {
		for i, row := range bad {
			e := newLinkReadsEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = rows(row)
			if _, err := e.invoke(id, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("%s row %d: err = %v, want invalid-provider-response", id, i, err)
			}
		}
	}
	t.Run("rows out of the requested order", func(t *testing.T) {
		for id, pair := range map[string][2]string{
			accountLinksListID:           {accountLinkItem(clientID, "12"), accountLinkItem(clientID, "11")},
			productLinksListID:           {productLinkItem(clientID, "12"), productLinkItem(clientID, "11")},
			productLinkInvitationsListID: {productInviteItem(clientID, "12"), productInviteItem(clientID, "11")},
			dataLinksListID:              {dataLinkItem(clientID, "6", "1"), dataLinkItem(clientID, "5", "9")},
		} {
			e := newLinkReadsEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = rows(pair[0], pair[1])
			if _, err := e.invoke(id, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("%s: err = %v", id, err)
			}
		}
	})
}

func TestLinkListsBoundTheTextOfTheProvider(t *testing.T) {
	e := newLinkReadsEnvironment(t, nil, nil)
	long := strings.Repeat("x", 3000)
	e.fake.routes["POST "+searchPath(clientID)] = rows(strings.Replace(accountLinkItem(clientID, "11"),
		"com.example.synthetic", long+`\u0007`, 1))
	out, err := e.invoke(accountLinksListID, `{"customer_id":"`+clientID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	app := decode(t, out)["account_links"].([]any)[0].(map[string]any)["third_party_app_analytics"].(map[string]any)
	if text := app["app_id"].(string); len(text) > maxText || strings.ContainsAny(text, "\a") {
		t.Errorf("app_id has %d bytes", len(text))
	}
}

func TestLinkListsNeedAManagerTargetOnManagerAccounts(t *testing.T) {
	for _, id := range linkListIDs {
		managerAnswer := func(manager bool) func(s seen) *reply {
			return func(s seen) *reply {
				if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					return &reply{status: 200, body: fmt.Sprintf(`{"results":[{"customer":{"id":"%s","manager":%t}}]}`, managerID, manager)}
				}
				if strings.Contains(s.query, " FROM "+linkResources[id].resource+" ") {
					return &reply{status: 200, body: `{"results":[]}`}
				}
				return nil
			}
		}
		t.Run(id+" a customer target that names a manager is refused", func(t *testing.T) {
			e := newLinkReadsEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = managerAnswer(true)
			_, err := e.invoke(id, `{"customer_id":"`+managerID+`"}`)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") {
				t.Fatalf("err = %v", err)
			}
			for _, s := range e.fake.api() {
				if strings.Contains(s.query, " FROM "+linkResources[id].resource+" ") {
					t.Errorf("the tool read the manager's data: %+v", s)
				}
			}
		})
		t.Run(id+" a manager target that names it passes without the check", func(t *testing.T) {
			e := newLinkReadsEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.answer = managerAnswer(true)
			if _, err := e.invoke(id, `{"customer_id":"`+managerID+`"}`); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 {
				t.Errorf("requests = %+v, want the call alone", got)
			}
		})
		t.Run(id+" a connection without targets makes no check", func(t *testing.T) {
			e := newLinkReadsEnvironment(t, nil, nil)
			e.fake.answer = managerAnswer(true)
			if _, err := e.invoke(id, `{"customer_id":"`+managerID+`"}`); err != nil || len(e.fake.api()) != 1 {
				t.Errorf("err = %v, requests = %+v", err, e.fake.api())
			}
		})
	}
}

func TestLinkReadErrorsAreMapped(t *testing.T) {
	for _, id := range linkListIDs {
		e := newLinkReadsEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED",
			"authorizationError", "USER_PERMISSION_DENIED", "")}
		_, err := e.invoke(id, `{"customer_id":"`+clientID+`"}`)
		if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), detailCanary) {
			t.Errorf("%s: err = %v", id, err)
		}
		e = newLinkReadsEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED",
			"quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"5s"}`)}
		if _, err := e.invoke(id, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassRateLimited {
			t.Errorf("%s: err = %v", id, err)
		}
	}
}
