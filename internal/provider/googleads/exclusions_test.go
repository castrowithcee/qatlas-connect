package googleads

import (
	"context"
	"encoding/json"
	"errors"
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

var exclusionToolIDs = []string{customerNegativeCriteriaCreateID, customerNegativeCriteriaRemoveID,
	sharedCriteriaCreateID, sharedCriteriaRemoveID}

func isExclusionRemove(id string) bool {
	return id == customerNegativeCriteriaRemoveID || id == sharedCriteriaRemoveID
}

func exclusionPathOf(tool string) string {
	service := customerNegativeCriteriaService
	if strings.Contains(tool, ".shared-criteria.") {
		service = sharedCriteriaService
	}
	return "/v26/customers/" + clientID + service + ":mutate"
}

func negativeName(id string) string { return own + "/customerNegativeCriteria/" + id }
func sharedName(set, id string) string {
	return own + "/sharedCriteria/" + set + "~" + id
}
func ownSet(id string) string { return own + "/sharedSets/" + id }

func newExclusionsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: exclusionToolIDs})
}

var exclusionCalls = map[string]string{
	customerNegativeCriteriaCreateID: `{"customer_id":"` + clientID + `","criteria":[{"placement":{"url":"www.example.com"}},` +
		`{"youtube_channel":{"channel_id":"channel_one"}},{"placement_list":{"shared_set":"` + ownSet("111") + `"}}]}`,
	customerNegativeCriteriaRemoveID: `{"customer_id":"` + clientID + `","resource_names":["` + negativeName("11") + `","` +
		negativeName("12") + `"]}`,
	sharedCriteriaCreateID: `{"customer_id":"` + clientID + `","criteria":[{"shared_set":"` + ownSet("111") +
		`","keyword":{"text":"free trial","match_type":"PHRASE"},"negative":true},{"shared_set":"` + ownSet("111") +
		`","placement":{"url":"www.example.com"}}]}`,
	sharedCriteriaRemoveID: `{"customer_id":"` + clientID + `","resource_names":["` + sharedName("111", "7") + `","` +
		sharedName("111", "8") + `"]}`,
}

var exclusionAnswers = map[string]string{
	customerNegativeCriteriaCreateID: `{"results":[{"resourceName":"` + negativeName("11") + `"},{"resourceName":"` +
		negativeName("12") + `"},{"resourceName":"` + negativeName("13") + `"}]}`,
	customerNegativeCriteriaRemoveID: `{"results":[{"resourceName":"` + negativeName("11") + `"},{"resourceName":"` +
		negativeName("12") + `"}]}`,
	sharedCriteriaCreateID: `{"results":[{"resourceName":"` + sharedName("111", "7") + `"},{"resourceName":"` +
		sharedName("111", "8") + `"}]}`,
	sharedCriteriaRemoveID: `{"results":[{"resourceName":"` + sharedName("111", "7") + `"},{"resourceName":"` +
		sharedName("111", "8") + `"}]}`,
}

func TestExclusionToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{customerNegativeCriteriaCreateID: capability.EffectCreate,
		customerNegativeCriteriaRemoveID: capability.EffectDelete, sharedCriteriaCreateID: capability.EffectCreate,
		sharedCriteriaRemoveID: capability.EffectDelete}
	for _, id := range exclusionToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		// Only the removals are listed-only; a creation is reached by its effect.
		if d.RequiresToolAllowList != isExclusionRemove(id) {
			t.Errorf("%s: tools list required = %v", id, d.RequiresToolAllowList)
		}
		wantNote := exclusionsChangeNote
		if isExclusionRemove(id) {
			wantNote = exclusionsListedNote
		}
		if !strings.HasSuffix(d.Description, wantNote) || strings.HasSuffix(d.Description, exclusionsListedNote) != isExclusionRemove(id) {
			t.Errorf("%s: description does not end on the note of how it is offered", id)
		}
		// The quota is counted per entry of the list, not once per call.
		if !strings.Contains(d.Description, "per entry") || strings.Contains(d.Description, "Costs one operation of the daily quota, plus") ||
			!strings.Contains(d.Description, "outcome is unknown") || !strings.Contains(d.Description, "atomic") {
			t.Errorf("%s: description lacks the quota per entry or the unknown outcome: %s", id, d.Description)
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
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "path", "url", "resource_name",
			"update_mask", "shared_set", "status"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
		if _, found := input.Properties["response_content_type"]; found == isExclusionRemove(id) {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
		for _, tool := range metadata.Tools {
			if tool.ID == id && (string(tool.Effect) != string(wantEffect[id]) || tool.RequiresToolAllowList != isExclusionRemove(id)) {
				t.Errorf("tool %+v", tool)
			}
		}
		for _, absent := range []string{`"login_customer_id"`, `"partial_failure"`, `"criterion_id"`, `"display_name"`,
			`"primary_url"`, `"rejection_reason"`, `"coverage_percentage"`, `"sample_urls"`, `"webpage_list"`, `"UNSPECIFIED"`,
			`"UNKNOWN"`} {
			if strings.Contains(string(d.InputSchema), absent) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	negative, _, _ := reg.Lookup(customerNegativeCriteriaCreateID)
	for _, present := range []string{`"content_label"`, `"ip_block"`, `"mobile_app_category"`, `"mobile_application"`, `"placement"`,
		`"youtube_channel"`, `"youtube_video"`, `"negative_keyword_list"`, `"placement_list"`, `"maxItems":50`} {
		if !strings.Contains(string(negative.InputSchema), present) {
			t.Errorf("customer-negative-criteria.create lacks %s", present)
		}
	}
	for _, absent := range []string{`"keyword"`, `"negative"`, `"brand"`, `"retail_filter"`, `"webpage"`} {
		if strings.Contains(string(negative.InputSchema), absent) {
			t.Errorf("customer-negative-criteria.create offers %s", absent)
		}
	}
	shared, _, _ := reg.Lookup(sharedCriteriaCreateID)
	for _, present := range []string{`"shared_set"`, `"negative"`, `"keyword"`, `"mobile_app_category"`, `"mobile_application"`,
		`"placement"`, `"webpage"`, `"youtube_channel"`, `"youtube_video"`, `"brand"`, `"retail_filter"`,
		`"vertical_ads_item_group_rule"`, `"hotel_class"`, `"user_rating"`, `"venue"`, `"item_code"`,
		`"event_participant_display_name"`, `"city_criterion_id"`, `"expression_name"`} {
		if !strings.Contains(string(shared.InputSchema), present) {
			t.Errorf("shared-criteria.create lacks %s", present)
		}
	}
	for _, absent := range []string{`"content_label"`, `"ip_block"`, `"placement_list"`, `"negative_keyword_list"`} {
		if strings.Contains(string(shared.InputSchema), absent) {
			t.Errorf("shared-criteria.create offers %s", absent)
		}
	}
}

// Every scalar member of a type is a field of the catalog of v26, except the few that Google accepts but does
// not make selectable.
func TestExclusionMembersAreFieldsOfTheCatalog(t *testing.T) {
	exempt := map[string]bool{"shared_criterion.webpage.conditions.operand": true,
		"shared_criterion.retail_filter.expression": true, "shared_criterion.retail_filter.tag": true}
	var walk func(n node, path string)
	walk = func(n node, path string) {
		for _, f := range n.fields {
			full := path + "." + f.name
			if f.list && f.kind == kindObject {
				continue
			}
			if _, ok := LookupField(full); f.kind != kindObject && !ok && !exempt[full] {
				t.Errorf("%s is not in the catalog", full)
			}
			walk(f, full)
		}
	}
	walk(customerNegativeCriterionTree, "customer_negative_criterion")
	walk(sharedCriterionTree, "shared_criterion")
}

// A creation is reached by its effect or by the tools list; a removal only by the tools list.
func TestExclusionToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+exclusionPathOf(id)] = okReply(exclusionAnswers[id])
		_, err := e.invokeConfirmed(id, exclusionCalls[id])
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
		{"all effects, no tools list", config.Connection{Effects: all}, map[string]bool{customerNegativeCriteriaCreateID: true,
			sharedCriteriaCreateID: true}},
		{"create effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate}},
			map[string]bool{customerNegativeCriteriaCreateID: true, sharedCriteriaCreateID: true}},
		{"delete effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectDelete}}, map[string]bool{}},
		{"read effect only", config.Connection{Effects: []config.Effect{config.EffectRead}}, map[string]bool{}},
		{"removals listed", config.Connection{Tools: []string{customerNegativeCriteriaRemoveID, sharedCriteriaRemoveID}},
			map[string]bool{customerNegativeCriteriaRemoveID: true, sharedCriteriaRemoveID: true}},
		{"all listed", config.Connection{Tools: exclusionToolIDs}, map[string]bool{customerNegativeCriteriaCreateID: true,
			customerNegativeCriteriaRemoveID: true, sharedCriteriaCreateID: true, sharedCriteriaRemoveID: true}},
	}
	for _, c := range cases {
		for _, id := range exclusionToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestExclusionsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range exclusionToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newExclusionsEnvironment(t, nil, nil)
			_, err := e.invoke(id, exclusionCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestExclusionsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"x"`} {
		for _, id := range exclusionToolIDs {
			e := newExclusionsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(exclusionCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestExclusionTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range exclusionToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newExclusionsEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.ReplaceAll(exclusionCalls[id], clientID, otherID)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestExclusionArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	cnc := func(items string) string { return head + `"criteria":[` + items + `]}` }
	sc := func(members string) string {
		return head + `"criteria":[{"shared_set":"` + ownSet("111") + `"` + members + `}]}`
	}
	remove := func(names ...string) string { return head + `"resource_names":["` + strings.Join(names, `","`) + `"]}` }
	many := func(item string, n int) string { return strings.TrimSuffix(strings.Repeat(item+",", n), ",") }
	foreign := "customers/" + otherID + "/sharedSets/111"
	bad := map[string][2]string{
		"cnc none":                  {customerNegativeCriteriaCreateID, cnc(``)},
		"cnc empty entry":           {customerNegativeCriteriaCreateID, cnc(`{}`)},
		"cnc two types":             {customerNegativeCriteriaCreateID, cnc(`{"placement":{"url":"www.example.com"},"youtube_channel":{"channel_id":"c1"}}`)},
		"cnc keyword":               {customerNegativeCriteriaCreateID, cnc(`{"keyword":{"text":"a","match_type":"EXACT"}}`)},
		"cnc negative flag":         {customerNegativeCriteriaCreateID, cnc(`{"placement":{"url":"www.example.com"},"negative":true}`)},
		"cnc id":                    {customerNegativeCriteriaCreateID, cnc(`{"placement":{"url":"www.example.com"},"id":5}`)},
		"cnc type member":           {customerNegativeCriteriaCreateID, cnc(`{"placement":{"url":"www.example.com"},"type":"PLACEMENT"}`)},
		"cnc type as text":          {customerNegativeCriteriaCreateID, cnc(`{"placement":"www.example.com"}`)},
		"cnc placement user info":   {customerNegativeCriteriaCreateID, cnc(`{"placement":{"url":"https://user:pw@example.com/"}}`)},
		"cnc placement scheme":      {customerNegativeCriteriaCreateID, cnc(`{"placement":{"url":"ftp://example.com/"}}`)},
		"cnc channel pattern":       {customerNegativeCriteriaCreateID, cnc(`{"youtube_channel":{"channel_id":"a b"}}`)},
		"cnc video length":          {customerNegativeCriteriaCreateID, cnc(`{"youtube_video":{"video_id":"short"}}`)},
		"cnc list foreign":          {customerNegativeCriteriaCreateID, cnc(`{"placement_list":{"shared_set":"` + foreign + `"}}`)},
		"cnc list temporary":        {customerNegativeCriteriaCreateID, cnc(`{"placement_list":{"shared_set":"` + own + `/sharedSets/-1"}}`)},
		"cnc list campaign":         {customerNegativeCriteriaCreateID, cnc(`{"negative_keyword_list":{"shared_set":"` + own + `/campaigns/1"}}`)},
		"cnc list no set":           {customerNegativeCriteriaCreateID, cnc(`{"negative_keyword_list":{}}`)},
		"cnc ip block":              {customerNegativeCriteriaCreateID, cnc(`{"ip_block":{"ip_address":"example"}}`)},
		"cnc label":                 {customerNegativeCriteriaCreateID, cnc(`{"content_label":{"type":"NOT_A_LABEL"}}`)},
		"cnc app category":          {customerNegativeCriteriaCreateID, cnc(`{"mobile_app_category":{"mobile_app_category_constant":"customers/1/x/2"}}`)},
		"cnc too many":              {customerNegativeCriteriaCreateID, cnc(many(`{"placement":{"url":"www.example.com"}}`, 51))},
		"cnc bad content type":      {customerNegativeCriteriaCreateID, strings.TrimSuffix(cnc(`{"placement":{"url":"www.example.com"}}`), "}") + `,"response_content_type":"X"}`},
		"cnc validate only alone":   {customerNegativeCriteriaCreateID, head + `"validate_only":true}`},
		"cnc bad customer":          {customerNegativeCriteriaCreateID, `{"customer_id":"12-34","criteria":[{"placement":{"url":"www.example.com"}}]}`},
		"cnc remove none":           {customerNegativeCriteriaRemoveID, remove()},
		"cnc remove id only":        {customerNegativeCriteriaRemoveID, remove("11")},
		"cnc remove other account":  {customerNegativeCriteriaRemoveID, remove("customers/" + otherID + "/customerNegativeCriteria/11")},
		"cnc remove other resource": {customerNegativeCriteriaRemoveID, remove(own + "/campaignCriteria/1")},
		"cnc remove pair":           {customerNegativeCriteriaRemoveID, remove(own + "/customerNegativeCriteria/1~2")},
		"cnc remove zero":           {customerNegativeCriteriaRemoveID, remove(own + "/customerNegativeCriteria/0")},
		"cnc remove twice":          {customerNegativeCriteriaRemoveID, remove(negativeName("11"), negativeName("11"))},
		"cnc remove too many": {customerNegativeCriteriaRemoveID, head + `"resource_names":["` +
			strings.Join(slices.Repeat([]string{negativeName("11")}, 51), `","`) + `"]}`},
		"cnc remove content type": {customerNegativeCriteriaRemoveID, strings.TrimSuffix(remove(negativeName("11")), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
		"sc none":                 {sharedCriteriaCreateID, head + `"criteria":[]}`},
		"sc no set":               {sharedCriteriaCreateID, cnc(`{"keyword":{"text":"a","match_type":"EXACT"}}`)},
		"sc set foreign":          {sharedCriteriaCreateID, cnc(`{"shared_set":"` + foreign + `","keyword":{"text":"a","match_type":"EXACT"}}`)},
		"sc set temporary":        {sharedCriteriaCreateID, cnc(`{"shared_set":"` + own + `/sharedSets/-3","keyword":{"text":"a","match_type":"EXACT"}}`)},
		"sc set as id":            {sharedCriteriaCreateID, cnc(`{"shared_set":"111","keyword":{"text":"a","match_type":"EXACT"}}`)},
		"sc no type":              {sharedCriteriaCreateID, sc(``)},
		"sc two types":            {sharedCriteriaCreateID, sc(`,"keyword":{"text":"a","match_type":"EXACT"},"placement":{"url":"www.example.com"}`)},
		"sc device":               {sharedCriteriaCreateID, sc(`,"device":{"type":"MOBILE"}`)},
		"sc content label":        {sharedCriteriaCreateID, sc(`,"content_label":{"type":"TRAGEDY"}`)},
		"sc criterion id":         {sharedCriteriaCreateID, sc(`,"keyword":{"text":"a","match_type":"EXACT"},"criterion_id":4`)},
		"sc status":               {sharedCriteriaCreateID, sc(`,"keyword":{"text":"a","match_type":"EXACT"},"status":"ENABLED"`)},
		"sc negative as text":     {sharedCriteriaCreateID, sc(`,"keyword":{"text":"a","match_type":"EXACT"},"negative":"yes"`)},
		"sc keyword no match":     {sharedCriteriaCreateID, sc(`,"keyword":{"text":"a"}`)},
		"sc keyword words":        {sharedCriteriaCreateID, sc(`,"keyword":{"text":"a b c d e f g h i j k","match_type":"EXACT"}`)},
		"sc keyword match":        {sharedCriteriaCreateID, sc(`,"keyword":{"text":"a","match_type":"FUZZY"}`)},
		"sc brand empty":          {sharedCriteriaCreateID, sc(`,"brand":{}`)},
		"sc brand display name":   {sharedCriteriaCreateID, sc(`,"brand":{"entity_id":"/g/example","display_name":"x"}`)},
		"sc brand status":         {sharedCriteriaCreateID, sc(`,"brand":{"entity_id":"/g/example","status":"APPROVED"}`)},
		"sc retail none":          {sharedCriteriaCreateID, sc(`,"retail_filter":{}`)},
		"sc retail both":          {sharedCriteriaCreateID, sc(`,"retail_filter":{"expression":{"name":"a"},"tag":{"value":"b"}}`)},
		"sc retail name upper":    {sharedCriteriaCreateID, sc(`,"retail_filter":{"expression":{"name":"Abc"}}`)},
		"sc retail name long":     {sharedCriteriaCreateID, sc(`,"retail_filter":{"expression":{"name":"` + strings.Repeat("a", 21) + `"}}`)},
		"sc retail expression no": {sharedCriteriaCreateID, sc(`,"retail_filter":{"expression":{}}`)},
		"sc retail tag no value":  {sharedCriteriaCreateID, sc(`,"retail_filter":{"tag":{"expression_name":"a"}}`)},
		"sc retail tag comma":     {sharedCriteriaCreateID, sc(`,"retail_filter":{"tag":{"value":"a,b"}}`)},
		"sc retail tag newline":   {sharedCriteriaCreateID, sc(`,"retail_filter":{"tag":{"value":"a\nb"}}`)},
		"sc retail tag long":      {sharedCriteriaCreateID, sc(`,"retail_filter":{"tag":{"value":"` + strings.Repeat("a", 51) + `"}}`)},
		"sc retail tag expr name": {sharedCriteriaCreateID, sc(`,"retail_filter":{"tag":{"value":"a","expression_name":"A B"}}`)},
		"sc vertical none":        {sharedCriteriaCreateID, sc(`,"vertical_ads_item_group_rule":{}`)},
		"sc vertical two":         {sharedCriteriaCreateID, sc(`,"vertical_ads_item_group_rule":{"venue":"a","item_code":"b"}`)},
		"sc vertical class":       {sharedCriteriaCreateID, sc(`,"vertical_ads_item_group_rule":{"hotel_class":6}`)},
		"sc vertical rating":      {sharedCriteriaCreateID, sc(`,"vertical_ads_item_group_rule":{"user_rating":0}`)},
		"sc vertical country":     {sharedCriteriaCreateID, sc(`,"vertical_ads_item_group_rule":{"country_criterion_id":"customers/1/geoTargetConstants/2"}`)},
		"sc vertical unknown":     {sharedCriteriaCreateID, sc(`,"vertical_ads_item_group_rule":{"venue":"a","other":"b"}`)},
		"sc webpage no conds":     {sharedCriteriaCreateID, sc(`,"webpage":{"criterion_name":"a"}`)},
		"sc too many":             {sharedCriteriaCreateID, head + `"criteria":[` + many(`{"shared_set":"`+ownSet("111")+`","placement":{"url":"www.example.com"}}`, 51) + `]}`},
		"sc remove none":          {sharedCriteriaRemoveID, remove()},
		"sc remove negative form": {sharedCriteriaRemoveID, remove(negativeName("11"))},
		"sc remove single id":     {sharedCriteriaRemoveID, remove(own + "/sharedCriteria/7")},
		"sc remove other account": {sharedCriteriaRemoveID, remove("customers/" + otherID + "/sharedCriteria/111~7")},
		"sc remove zero":          {sharedCriteriaRemoveID, remove(own + "/sharedCriteria/0~7")},
		"sc remove injection":     {sharedCriteriaRemoveID, remove(own + "/sharedCriteria/111~7/../x")},
		"sc remove twice":         {sharedCriteriaRemoveID, remove(sharedName("111", "7"), sharedName("111", "7"))},
		"sc remove content type": {sharedCriteriaRemoveID, strings.TrimSuffix(remove(sharedName("111", "7")), "}") +
			`,"response_content_type":"MUTABLE_RESOURCE"}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newExclusionsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	// Exactly at the documented limits, the call passes.
	pass := map[string]struct{ id, call, answer string }{
		"retail limits": {sharedCriteriaCreateID, sc(`,"retail_filter":{"tag":{"expression_name":"` + strings.Repeat("a_1", 6) + `ab","value":"` +
			strings.Repeat("ä", 50) + `"}}`), `{"results":[{"resourceName":"` + sharedName("111", "1") + `"}]}`},
		"vertical limits": {sharedCriteriaCreateID, sc(`,"vertical_ads_item_group_rule":{"hotel_class":5}`), `{"results":[{"resourceName":"` + sharedName("111", "1") + `"}]}`},
		"fifty": {customerNegativeCriteriaCreateID, cnc(many(`{"placement":{"url":"www.example.com"}}`, 50)), func() string {
			var items []string
			for i := 1; i <= 50; i++ {
				items = append(items, `{"resourceName":"`+negativeName(strconv.Itoa(i))+`"}`)
			}
			return `{"results":[` + strings.Join(items, ",") + `]}`
		}()},
	}
	for name, c := range pass {
		t.Run(name, func(t *testing.T) {
			e := newExclusionsEnvironment(t, nil, nil)
			e.fake.routes["POST "+exclusionPathOf(c.id)] = okReply(c.answer)
			if _, err := e.invokeConfirmed(c.id, c.call); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExclusionsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		customerNegativeCriteriaCreateID: `[{"create":{"placement":{"url":"www.example.com"}}},` +
			`{"create":{"youtubeChannel":{"channelId":"channel_one"}}},` +
			`{"create":{"placementList":{"sharedSet":"customers/1000000003/sharedSets/111"}}}]`,
		customerNegativeCriteriaRemoveID: `[{"remove":"customers/1000000003/customerNegativeCriteria/11"},` +
			`{"remove":"customers/1000000003/customerNegativeCriteria/12"}]`,
		sharedCriteriaCreateID: `[{"create":{"sharedSet":"customers/1000000003/sharedSets/111","negative":true,` +
			`"keyword":{"text":"free trial","matchType":"PHRASE"}}},` +
			`{"create":{"sharedSet":"customers/1000000003/sharedSets/111","placement":{"url":"www.example.com"}}}]`,
		sharedCriteriaRemoveID: `[{"remove":"customers/1000000003/sharedCriteria/111~7"},` +
			`{"remove":"customers/1000000003/sharedCriteria/111~8"}]`,
	}
	for _, id := range exclusionToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newExclusionsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+exclusionPathOf(id)] = okReply(exclusionAnswers[id])
			out, err := e.invokeConfirmed(id, exclusionCalls[id])
			if err != nil {
				t.Fatal(err)
			}
			posts := e.posts()
			if len(posts) != 1 || posts[0].login != loginID || posts[0].developerToken != "" || posts[0].path != exclusionPathOf(id) {
				t.Fatalf("posts = %+v", posts)
			}
			body := bodyOf(t, posts[0])
			if len(body) != 1 || !reflect.DeepEqual(body["operations"], wantWire(t, want[id])) {
				t.Errorf("body = %s", posts[0].body)
			}
			got := decode(t, out)
			results := got["results"].([]any)
			if got["customer_id"] != clientID || got["validate_only"] != false ||
				len(results) != strings.Count(exclusionAnswers[id], "resourceName") {
				t.Fatalf("out = %s", out)
			}
			if first := results[0].(map[string]any); first["resource_name"] == "" || first["resource_name"] == nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestExclusionsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range exclusionToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newExclusionsEnvironment(t, nil, nil)
			e.fake.routes["POST "+exclusionPathOf(id)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(exclusionCalls[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			body := bodyOf(t, e.posts()[0])
			got := decode(t, out)
			if body["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil && len(got["results"].([]any)) != 0 {
				t.Errorf("body = %v, out = %s", body, out)
			}
		})
	}
	t.Run("summary says only validate", func(t *testing.T) {
		summaries := map[string]capability.SummaryFunc{
			customerNegativeCriteriaCreateID: exclusionSummary(customerNegativeCriteriaCreateID, customerNegativeCriterionTypes, "customer negative criteria"),
			customerNegativeCriteriaRemoveID: exclusionSummary(customerNegativeCriteriaRemoveID, customerNegativeCriterionTypes, "customer negative criteria"),
			sharedCriteriaCreateID:           exclusionSummary(sharedCriteriaCreateID, sharedCriterionTypes, "shared criteria"),
			sharedCriteriaRemoveID:           exclusionSummary(sharedCriteriaRemoveID, sharedCriterionTypes, "shared criteria")}
		for id, summary := range summaries {
			if text := summary(json.RawMessage(strings.TrimSuffix(exclusionCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
				t.Errorf("%s: summary %q", id, text)
			}
			if text := summary(json.RawMessage(exclusionCalls[id])); strings.Contains(text, "validate") {
				t.Errorf("%s: summary %q kept state", id, text)
			}
		}
	})
}

func TestExclusionResponseContentTypeShowsTheObjects(t *testing.T) {
	t.Run("customer negative criterion", func(t *testing.T) {
		e := newExclusionsEnvironment(t, nil, nil)
		name := negativeName("11")
		e.fake.routes["POST "+exclusionPathOf(customerNegativeCriteriaCreateID)] = okReply(`{"results":[{"resourceName":"` + name +
			`","customerNegativeCriterion":{"resourceName":"` + name + `","id":"11","type":"PLACEMENT",` +
			`"placement":{"url":"www.example.com\u0007"},"unknownMember":"dropped"}}]}`)
		out, err := e.invokeConfirmed(customerNegativeCriteriaCreateID, `{"customer_id":"`+clientID+
			`","criteria":[{"placement":{"url":"www.example.com"}}],"response_content_type":"MUTABLE_RESOURCE"}`)
		if err != nil {
			t.Fatal(err)
		}
		c := decode(t, out)["results"].([]any)[0].(map[string]any)["customer_negative_criterion"].(map[string]any)
		if c["id"] != float64(11) || c["type"] != "PLACEMENT" || c["resource_name"] != name || c["unknownMember"] != nil ||
			strings.Contains(c["placement"].(map[string]any)["url"].(string), "\a") {
			t.Errorf("criterion = %v", c)
		}
		if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
			t.Errorf("body = %s", e.posts()[0].body)
		}
	})
	t.Run("shared criterion", func(t *testing.T) {
		e := newExclusionsEnvironment(t, nil, nil)
		name := sharedName("111", "7")
		e.fake.routes["POST "+exclusionPathOf(sharedCriteriaCreateID)] = okReply(`{"results":[{"resourceName":"` + name +
			`","sharedCriterion":{"resourceName":"` + name + `","sharedSet":"` + ownSet("111") + `","criterionId":"7",` +
			`"type":"BRAND","negative":true,"brand":{"entityId":"/g/example","displayName":"Example","primaryUrl":"www.example.com",` +
			`"status":"APPROVED"}}}]}`)
		out, err := e.invokeConfirmed(sharedCriteriaCreateID, `{"customer_id":"`+clientID+`","criteria":[{"shared_set":"`+ownSet("111")+
			`","brand":{"entity_id":"/g/example"}}],"response_content_type":"MUTABLE_RESOURCE"}`)
		if err != nil {
			t.Fatal(err)
		}
		c := decode(t, out)["results"].([]any)[0].(map[string]any)["shared_criterion"].(map[string]any)
		brand := c["brand"].(map[string]any)
		if c["criterion_id"] != float64(7) || c["shared_set"] != ownSet("111") || c["negative"] != true ||
			brand["entity_id"] != "/g/example" || brand["display_name"] != "Example" || brand["status"] != "APPROVED" {
			t.Errorf("criterion = %v", c)
		}
	})
}

// A resource name of another account, another object or another count never passes, and neither does an object
// whose own name, ID, shared set or constants differ. The request was sent, so the error says that the outcome is
// unknown.
func TestExclusionResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	item := func(name string) string { return `{"resourceName":"` + name + `"}` }
	foreignNegative := "customers/" + otherID + "/customerNegativeCriteria/11"
	foreignShared := "customers/" + otherID + "/sharedCriteria/111~7"
	sharedWith := func(object string) string {
		return `{"results":[{"resourceName":"` + sharedName("111", "7") + `","sharedCriterion":` + object + `},` +
			item(sharedName("111", "8")) + `]}`
	}
	negativeWith := func(object string) string {
		return `{"results":[{"resourceName":"` + negativeName("11") + `","customerNegativeCriterion":` + object + `},` +
			item(negativeName("12")) + `,` + item(negativeName("13")) + `]}`
	}
	cases := map[string]struct{ id, body string }{
		"cnc create foreign":      {customerNegativeCriteriaCreateID, `{"results":[` + item(foreignNegative) + `,` + item(negativeName("12")) + `,` + item(negativeName("13")) + `]}`},
		"cnc create none":         {customerNegativeCriteriaCreateID, `{}`},
		"cnc create fewer":        {customerNegativeCriteriaCreateID, `{"results":[` + item(negativeName("11")) + `]}`},
		"cnc create twice":        {customerNegativeCriteriaCreateID, `{"results":[` + item(negativeName("11")) + `,` + item(negativeName("11")) + `,` + item(negativeName("13")) + `]}`},
		"cnc create other type":   {customerNegativeCriteriaCreateID, `{"results":[` + item(own+"/campaigns/1") + `,` + item(negativeName("12")) + `,` + item(negativeName("13")) + `]}`},
		"cnc create pair":         {customerNegativeCriteriaCreateID, `{"results":[` + item(own+"/customerNegativeCriteria/1~2") + `,` + item(negativeName("12")) + `,` + item(negativeName("13")) + `]}`},
		"cnc create no name":      {customerNegativeCriteriaCreateID, `{"results":[{},` + item(negativeName("12")) + `,` + item(negativeName("13")) + `]}`},
		"cnc create partial":      {customerNegativeCriteriaCreateID, strings.TrimSuffix(exclusionAnswers[customerNegativeCriteriaCreateID], "}") + `,"partialFailureError":{"code":3}}`},
		"cnc remove other":        {customerNegativeCriteriaRemoveID, `{"results":[` + item(negativeName("12")) + `,` + item(negativeName("11")) + `]}`},
		"cnc remove foreign":      {customerNegativeCriteriaRemoveID, `{"results":[` + item(foreignNegative) + `,` + item(negativeName("12")) + `]}`},
		"cnc remove none":         {customerNegativeCriteriaRemoveID, `{"results":[]}`},
		"cnc object name":         {customerNegativeCriteriaCreateID, negativeWith(`{"resourceName":"` + negativeName("99") + `"}`)},
		"cnc object foreign name": {customerNegativeCriteriaCreateID, negativeWith(`{"resourceName":"` + foreignNegative + `"}`)},
		"cnc object id":           {customerNegativeCriteriaCreateID, negativeWith(`{"resourceName":"` + negativeName("11") + `","id":"99"}`)},
		"cnc object list foreign": {customerNegativeCriteriaCreateID, negativeWith(`{"placementList":{"sharedSet":"customers/` + otherID + `/sharedSets/1"}}`)},
		"cnc object bad text":     {customerNegativeCriteriaCreateID, negativeWith(`{"placement":"www.example.com"}`)},
		"sc create foreign":       {sharedCriteriaCreateID, `{"results":[` + item(foreignShared) + `,` + item(sharedName("111", "8")) + `]}`},
		"sc create other set":     {sharedCriteriaCreateID, `{"results":[` + item(sharedName("222", "7")) + `,` + item(sharedName("111", "8")) + `]}`},
		"sc create none":          {sharedCriteriaCreateID, `{}`},
		"sc create one of two":    {sharedCriteriaCreateID, `{"results":[` + item(sharedName("111", "7")) + `]}`},
		"sc create twice":         {sharedCriteriaCreateID, `{"results":[` + item(sharedName("111", "7")) + `,` + item(sharedName("111", "7")) + `]}`},
		"sc create not a pair":    {sharedCriteriaCreateID, `{"results":[` + item(own+"/sharedCriteria/7") + `,` + item(sharedName("111", "8")) + `]}`},
		"sc create other type":    {sharedCriteriaCreateID, `{"results":[` + item(negativeName("11")) + `,` + item(sharedName("111", "8")) + `]}`},
		"sc create partial":       {sharedCriteriaCreateID, strings.TrimSuffix(exclusionAnswers[sharedCriteriaCreateID], "}") + `,"partialFailureError":{"code":3}}`},
		"sc remove other":         {sharedCriteriaRemoveID, `{"results":[` + item(sharedName("111", "8")) + `,` + item(sharedName("111", "7")) + `]}`},
		"sc remove foreign":       {sharedCriteriaRemoveID, `{"results":[` + item(foreignShared) + `,` + item(sharedName("111", "8")) + `]}`},
		"sc remove none":          {sharedCriteriaRemoveID, `{"results":[]}`},
		"sc object name":          {sharedCriteriaCreateID, sharedWith(`{"resourceName":"` + sharedName("111", "9") + `"}`)},
		"sc object foreign set":   {sharedCriteriaCreateID, sharedWith(`{"sharedSet":"customers/` + otherID + `/sharedSets/111"}`)},
		"sc object other set":     {sharedCriteriaCreateID, sharedWith(`{"sharedSet":"` + ownSet("222") + `"}`)},
		"sc object id":            {sharedCriteriaCreateID, sharedWith(`{"criterionId":"99"}`)},
		"sc object constant":      {sharedCriteriaCreateID, sharedWith(`{"verticalAdsItemGroupRule":{"countryCriterionId":"customers/1/geoTargetConstants/2"}}`)},
		"sc object bad number":    {sharedCriteriaCreateID, sharedWith(`{"verticalAdsItemGroupRule":{"hotelClass":"many"}}`)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newExclusionsEnvironment(t, nil, nil)
			e.fake.routes["POST "+exclusionPathOf(c.id)] = okReply(c.body)
			arguments := exclusionCalls[c.id]
			if strings.Contains(name, "object") {
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

func TestExclusionManagerAccountsNeedAManagerTarget(t *testing.T) {
	call := `{"customer_id":"` + managerID + `","resource_names":["customers/` + managerID + `/sharedCriteria/7~8"]}`
	answer := okReply(`{"results":[{"resourceName":"customers/` + managerID + `/sharedCriteria/7~8"}]}`)
	path := "/v26/customers/" + managerID + sharedCriteriaService + ":mutate"
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newExclusionsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+path] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(sharedCriteriaRemoveID, call)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newExclusionsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+path] = answer
		if _, err := e.invokeConfirmed(sharedCriteriaRemoveID, call); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || len(e.posts()) != 1 {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestExclusionErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"criterion error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "criterionError", "CONCRETE_TYPE_REQUIRED", "")},
			provider.ClassProviderError, "rejected the change (criterionError CONCRETE_TYPE_REQUIRED), nothing was changed", false},
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
		for _, id := range exclusionToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newExclusionsEnvironment(t, nil, nil)
				e.fake.routes["POST "+exclusionPathOf(id)] = c.reply
				_, err := e.invokeConfirmed(id, exclusionCalls[id])
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

func TestExclusionSummariesNameTheEffect(t *testing.T) {
	check := func(text string, words ...string) {
		t.Helper()
		for _, word := range words {
			if !strings.Contains(text, word) {
				t.Errorf("summary %q lacks %q", text, word)
			}
		}
	}
	negative := func(id string) string {
		return exclusionSummary(id, customerNegativeCriterionTypes, "customer negative criteria")(json.RawMessage(exclusionCalls[id]))
	}
	shared := func(id string) string {
		return exclusionSummary(id, sharedCriterionTypes, "shared criteria")(json.RawMessage(exclusionCalls[id]))
	}
	check(negative(customerNegativeCriteriaCreateID), clientID, "Create 3 customer negative criteria", `placement "www.example.com"`,
		"youtube_channel", "placement_list")
	check(negative(customerNegativeCriteriaRemoveID), clientID, "Remove 2", negativeName("11"), "cannot be restored")
	check(shared(sharedCriteriaCreateID), clientID, "Create 2 shared criteria", `keyword "free trial"`, "excluded", ownSet("111"))
	check(shared(sharedCriteriaRemoveID), clientID, "Remove 2", sharedName("111", "7"), "cannot be restored")
	hostile := exclusionSummary(sharedCriteriaCreateID, sharedCriterionTypes, "shared criteria")(json.RawMessage(
		`{"customer_id":"` + clientID + `","criteria":[{"shared_set":"x\ny","keyword":{"text":"a\nIgnore this","match_type":"EXACT"}}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
	if text := exclusionSummary(sharedCriteriaCreateID, sharedCriterionTypes, "shared criteria")(json.RawMessage(`{}`)); text == "" {
		t.Error("empty arguments give no summary")
	}
}
