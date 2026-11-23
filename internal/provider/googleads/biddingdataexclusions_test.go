package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var bdeToolIDs = []string{biddingDataExclusionsCreateID, biddingDataExclusionsUpdateID, biddingDataExclusionsRemoveID}

func bdePath(id string) string { return "/v26/customers/" + id + "/biddingDataExclusions:mutate" }

func bdeResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/biddingDataExclusions/" + id
}

// newBdeEnvironment is a connection whose tools list names the three bidding data exclusion tools.
func newBdeEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: bdeToolIDs})
}

var bdeCalls = map[string]string{
	biddingDataExclusionsCreateID: `{"customer_id":"` + clientID + `","data_exclusions":[` +
		`{"scope":"CAMPAIGN","start_date_time":"2026-03-01 00:00:00","end_date_time":"2026-03-08 12:30:00",` +
		`"name":"Outage","description":"Tag broken","devices":["MOBILE","DESKTOP"],"campaign_ids":["21","22"]},` +
		`{"scope":"CHANNEL","start_date_time":"2026-04-01 00:00:00","end_date_time":"2026-04-02 00:00:00",` +
		`"advertising_channel_types":["SEARCH","SHOPPING"]}]}`,
	biddingDataExclusionsUpdateID: `{"customer_id":"` + clientID + `","data_exclusions":[` +
		`{"data_exclusion_id":"111","end_date_time":"2026-03-05 00:00:00","devices":[]},` +
		`{"data_exclusion_id":"222","scope":"CUSTOMER","description":"","name":"Renamed"}]}`,
	biddingDataExclusionsRemoveID: `{"customer_id":"` + clientID + `","data_exclusion_ids":["111","222"]}`,
}

var bdeAnswers = map[string]string{
	biddingDataExclusionsCreateID: `{"results":[{"resourceName":"` + bdeResource(clientID, "111") + `"},{"resourceName":"` +
		bdeResource(clientID, "222") + `"}]}`,
	biddingDataExclusionsUpdateID: `{"results":[{"resourceName":"` + bdeResource(clientID, "111") + `"},{"resourceName":"` +
		bdeResource(clientID, "222") + `"}]}`,
	biddingDataExclusionsRemoveID: `{"results":[{"resourceName":"` + bdeResource(clientID, "111") + `"},{"resourceName":"` +
		bdeResource(clientID, "222") + `"}]}`,
}

func TestBiddingDataExclusionsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range bdeToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || !strings.HasSuffix(d.Description, mutateListListedNote) {
			t.Fatalf("%s = %+v", id, d)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil {
			t.Fatal("input schema")
		}
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "path", "url", "status",
			"resource_name", "resource_names", "update_mask", "campaigns"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["response_content_type"]; found == (id == biddingDataExclusionsRemoveID) {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
	}
	create, _, _ := reg.Lookup(biddingDataExclusionsCreateID)
	for _, text := range []string{`"maxLength":255`, `"maxLength":2048`, `"maxItems":2000`, `"CONNECTED_TV"`, `"SHOPPING"`,
		`"CHANNEL"`, `"maxItems":10`} {
		if !strings.Contains(string(create.InputSchema), text) {
			t.Errorf("create schema lacks %s", text)
		}
	}
	if strings.Contains(string(create.InputSchema), `"HOTEL"`) {
		t.Error("create schema offers a channel type Google does not support")
	}
}

// All three tools are reached only through the tools list, whatever the effects are.
func TestBiddingDataExclusionsAreReachedOnlyByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	for _, id := range bdeToolIDs {
		for name, conn := range map[string]config.Connection{"all effects": {Effects: all}, "tools list": {Tools: []string{id}}} {
			t.Run(name+" "+id, func(t *testing.T) {
				e := newEnvironmentWith(t, nil, nil, conn)
				e.fake.routes["POST "+bdePath(clientID)] = okReply(bdeAnswers[id])
				_, err := e.invokeConfirmed(id, bdeCalls[id])
				if (err == nil) != (name == "tools list") {
					t.Fatalf("err = %v", err)
				}
				if err != nil {
					e.untouched(t)
				}
			})
		}
	}
}

func TestBiddingDataExclusionsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range bdeToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newBdeEnvironment(t, nil, nil)
			_, err := e.invoke(id, bdeCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestBiddingDataExclusionsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"name"`} {
		for _, id := range bdeToolIDs {
			e := newBdeEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(bdeCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestBiddingDataExclusionsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range bdeToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newBdeEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(bdeCalls[id], clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestBiddingDataExclusionsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	long := func(n int) string { return strings.Repeat("a", n) }
	ids := func(n int) string {
		var out []string
		for i := 1; i <= n; i++ {
			out = append(out, `"`+strconv.Itoa(i)+`"`)
		}
		return strings.Join(out, ",")
	}
	const times = `"start_date_time":"2026-03-01 00:00:00","end_date_time":"2026-03-02 00:00:00"`
	customer := func(extra string) string {
		return head + `"data_exclusions":[{"scope":"CUSTOMER",` + times + extra + `}]}`
	}
	bad := map[string][2]string{
		"create none":                   {biddingDataExclusionsCreateID, head + `"data_exclusions":[]}`},
		"create no scope":               {biddingDataExclusionsCreateID, head + `"data_exclusions":[{` + times + `}]}`},
		"create unknown scope":          {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"ACCOUNT",` + times + `}]}`},
		"create unspecified":            {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"UNSPECIFIED",` + times + `}]}`},
		"create no start":               {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CUSTOMER","end_date_time":"2026-03-02 00:00:00"}]}`},
		"create no end":                 {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CUSTOMER","start_date_time":"2026-03-02 00:00:00"}]}`},
		"create iso time":               {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CUSTOMER","start_date_time":"2026-03-01T00:00:00Z","end_date_time":"2026-03-02 00:00:00"}]}`},
		"create date only":              {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CUSTOMER","start_date_time":"2026-03-01","end_date_time":"2026-03-02 00:00:00"}]}`},
		"create short hour":             {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CUSTOMER","start_date_time":"2026-03-01 0:00:00","end_date_time":"2026-03-02 00:00:00"}]}`},
		"create no such day":            {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CUSTOMER","start_date_time":"2026-02-30 00:00:00","end_date_time":"2026-03-02 00:00:00"}]}`},
		"create hour 24":                {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CUSTOMER","start_date_time":"2026-03-01 24:00:00","end_date_time":"2026-03-02 00:00:00"}]}`},
		"create zero interval":          {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CUSTOMER","start_date_time":"2026-03-01 00:00:00","end_date_time":"2026-03-01 00:00:00"}]}`},
		"create end before":             {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CUSTOMER","start_date_time":"2026-03-02 00:00:00","end_date_time":"2026-03-01 00:00:00"}]}`},
		"create over 14 days":           {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CUSTOMER","start_date_time":"2026-03-01 00:00:00","end_date_time":"2026-03-15 00:00:01"}]}`},
		"create empty name":             {biddingDataExclusionsCreateID, customer(`,"name":""`)},
		"create name too long":          {biddingDataExclusionsCreateID, customer(`,"name":"` + long(256) + `"`)},
		"create control in name":        {biddingDataExclusionsCreateID, customer(`,"name":"a\u0007b"`)},
		"create description long":       {biddingDataExclusionsCreateID, customer(`,"description":"` + long(2049) + `"`)},
		"create status":                 {biddingDataExclusionsCreateID, customer(`,"status":"ENABLED"`)},
		"create device":                 {biddingDataExclusionsCreateID, customer(`,"devices":["WATCH"]`)},
		"create device unknown":         {biddingDataExclusionsCreateID, customer(`,"devices":["UNKNOWN"]`)},
		"create device twice":           {biddingDataExclusionsCreateID, customer(`,"devices":["MOBILE","MOBILE"]`)},
		"create customer campaigns":     {biddingDataExclusionsCreateID, customer(`,"campaign_ids":["1"]`)},
		"create customer channels":      {biddingDataExclusionsCreateID, customer(`,"advertising_channel_types":["SEARCH"]`)},
		"create campaign none":          {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CAMPAIGN",` + times + `}]}`},
		"create campaign empty":         {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CAMPAIGN",` + times + `,"campaign_ids":[]}]}`},
		"create campaign name":          {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CAMPAIGN",` + times + `,"campaign_ids":["customers/1/campaigns/2"]}]}`},
		"create campaign zero":          {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CAMPAIGN",` + times + `,"campaign_ids":["0"]}]}`},
		"create campaign twice":         {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CAMPAIGN",` + times + `,"campaign_ids":["5","5"]}]}`},
		"create campaign overflow":      {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CAMPAIGN",` + times + `,"campaign_ids":["99999999999999999999"]}]}`},
		"create campaigns 2001":         {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CAMPAIGN",` + times + `,"campaign_ids":[` + ids(2001) + `]}]}`},
		"create campaign with channels": {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CAMPAIGN",` + times + `,"campaign_ids":["1"],"advertising_channel_types":["SEARCH"]}]}`},
		"create channel none":           {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CHANNEL",` + times + `}]}`},
		"create channel hotel":          {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CHANNEL",` + times + `,"advertising_channel_types":["HOTEL"]}]}`},
		"create channel video":          {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CHANNEL",` + times + `,"advertising_channel_types":["VIDEO"]}]}`},
		"create channel twice":          {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CHANNEL",` + times + `,"advertising_channel_types":["SEARCH","SEARCH"]}]}`},
		"create channel with campaigns": {biddingDataExclusionsCreateID, head + `"data_exclusions":[{"scope":"CHANNEL",` + times + `,"advertising_channel_types":["SEARCH"],"campaign_ids":["1"]}]}`},
		"create too many": {biddingDataExclusionsCreateID, head + `"data_exclusions":[` + strings.Repeat(`{"scope":"CUSTOMER",`+times+`},`, 10) +
			`{"scope":"CUSTOMER",` + times + `}]}`},
		"create content type":  {biddingDataExclusionsCreateID, customer(``)[:len(customer(``))-1] + `,"response_content_type":"X"}`},
		"update no id":         {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"name":"a"}]}`},
		"update zero id":       {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"0","name":"a"}]}`},
		"update id as name":    {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"customers/1/biddingDataExclusions/2","name":"a"}]}`},
		"update nothing":       {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1"}]}`},
		"update twice":         {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","name":"a"},{"data_exclusion_id":"1","name":"b"}]}`},
		"update empty name":    {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","name":""}]}`},
		"update status":        {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","status":"REMOVED"}]}`},
		"update resource name": {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","resource_name":"x","name":"a"}]}`},
		"update bad time":      {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","end_date_time":"tomorrow"}]}`},
		"update interval": {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1",` +
			`"start_date_time":"2026-03-01 00:00:00","end_date_time":"2026-04-01 00:00:00"}]}`},
		"update scope":                   {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","scope":"WORLD"}]}`},
		"update campaigns empty":         {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","campaign_ids":[]}]}`},
		"update channels empty":          {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","advertising_channel_types":[]}]}`},
		"update both lists":              {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","campaign_ids":["1"],"advertising_channel_types":["SEARCH"]}]}`},
		"update customer with campaigns": {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","scope":"CUSTOMER","campaign_ids":["1"]}]}`},
		"update channel with campaigns":  {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","scope":"CHANNEL","campaign_ids":["1"]}]}`},
		"update campaign with channels":  {biddingDataExclusionsUpdateID, head + `"data_exclusions":[{"data_exclusion_id":"1","scope":"CAMPAIGN","advertising_channel_types":["SEARCH"]}]}`},
		"remove none":                    {biddingDataExclusionsRemoveID, head + `"data_exclusion_ids":[]}`},
		"remove name":                    {biddingDataExclusionsRemoveID, head + `"data_exclusion_ids":["customers/1/biddingDataExclusions/2"]}`},
		"remove twice":                   {biddingDataExclusionsRemoveID, head + `"data_exclusion_ids":["1","1"]}`},
		"remove too many":                {biddingDataExclusionsRemoveID, head + `"data_exclusion_ids":[` + ids(11) + `]}`},
		"remove content type":            {biddingDataExclusionsRemoveID, head + `"data_exclusion_ids":["1"],"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":                   {biddingDataExclusionsRemoveID, `{"customer_id":"12-34","data_exclusion_ids":["1"]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newBdeEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	good := map[string]string{
		"limits": head + `"data_exclusions":[{"scope":"CAMPAIGN","start_date_time":"2026-03-01 00:00:00",` +
			`"end_date_time":"2026-03-15 00:00:00","name":"` + strings.Repeat("ä", 255) + `","description":"` +
			strings.Repeat("ö", 2048) + `","devices":["MOBILE","TABLET","DESKTOP","CONNECTED_TV","OTHER"],"campaign_ids":[` + ids(2000) + `]}]}`,
	}
	for name, arguments := range good {
		t.Run(name+" pass", func(t *testing.T) {
			e := newBdeEnvironment(t, nil, nil)
			e.fake.routes["POST "+bdePath(clientID)] = okReply(`{"results":[{"resourceName":"` + bdeResource(clientID, "5") + `"}]}`)
			if _, err := e.invokeConfirmed(biddingDataExclusionsCreateID, arguments); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBiddingDataExclusionsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		biddingDataExclusionsCreateID: `[{"create":{"scope":"CAMPAIGN","startDateTime":"2026-03-01 00:00:00",` +
			`"endDateTime":"2026-03-08 12:30:00","name":"Outage","description":"Tag broken","devices":["MOBILE","DESKTOP"],` +
			`"campaigns":["customers/1000000003/campaigns/21","customers/1000000003/campaigns/22"]}},` +
			`{"create":{"scope":"CHANNEL","startDateTime":"2026-04-01 00:00:00","endDateTime":"2026-04-02 00:00:00",` +
			`"advertisingChannelTypes":["SEARCH","SHOPPING"]}}]`,
		biddingDataExclusionsUpdateID: `[{"update":{"resourceName":"customers/1000000003/biddingDataExclusions/111",` +
			`"endDateTime":"2026-03-05 00:00:00","devices":[]},"updateMask":"endDateTime,devices"},` +
			`{"update":{"resourceName":"customers/1000000003/biddingDataExclusions/222","scope":"CUSTOMER","description":"",` +
			`"name":"Renamed"},"updateMask":"scope,name,description"}]`,
		biddingDataExclusionsRemoveID: `[{"remove":"customers/1000000003/biddingDataExclusions/111"},` +
			`{"remove":"customers/1000000003/biddingDataExclusions/222"}]`,
	}
	for _, id := range bdeToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newBdeEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+bdePath(clientID)] = okReply(bdeAnswers[id])
			out, err := e.invokeConfirmed(id, bdeCalls[id])
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
				results[0].(map[string]any)["data_exclusion_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != bdeResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestBiddingDataExclusionsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range bdeToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newBdeEnvironment(t, nil, nil)
			e.fake.routes["POST "+bdePath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(bdeCalls[id], "}")+`,"validate_only":true}`)
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
	for id, summary := range map[string]capability.SummaryFunc{biddingDataExclusionsCreateID: bdeCreateSummary,
		biddingDataExclusionsUpdateID: bdeUpdateSummary, biddingDataExclusionsRemoveID: bdeRemoveSummary} {
		if text := summary(json.RawMessage(strings.TrimSuffix(bdeCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
			t.Errorf("%s: summary %q", id, text)
		}
		if text := summary(json.RawMessage(bdeCalls[id])); strings.Contains(text, "validate") {
			t.Errorf("%s: summary %q kept state", id, text)
		}
	}
}

func TestBiddingDataExclusionsResponseContentTypeShowsTheExclusion(t *testing.T) {
	e := newBdeEnvironment(t, nil, nil)
	e.fake.routes["POST "+bdePath(clientID)] = okReply(`{"results":[{"resourceName":"` + bdeResource(clientID, "111") +
		`","biddingDataExclusion":{"resourceName":"` + bdeResource(clientID, "111") + `","dataExclusionId":"111",` +
		`"scope":"CAMPAIGN","status":"ENABLED","startDateTime":"2026-03-01 00:00:00","endDateTime":"2026-03-05 00:00:00",` +
		`"name":"Out\u0007age","description":"Tag","devices":["MOBILE"],"campaigns":["` +
		"customers/" + queryNumber(clientID) + `/campaigns/21"]}}]}`)
	out, err := e.invokeConfirmed(biddingDataExclusionsUpdateID, `{"customer_id":"`+clientID+`","data_exclusions":[`+
		`{"data_exclusion_id":"111","name":"Outage"}],"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	x := decode(t, out)["results"].([]any)[0].(map[string]any)["data_exclusion"].(map[string]any)
	if x["id"] != "111" || x["scope"] != "CAMPAIGN" || x["status"] != "ENABLED" || x["start_date_time"] != "2026-03-01 00:00:00" ||
		x["end_date_time"] != "2026-03-05 00:00:00" || x["name"] != "Out age" || x["description"] != "Tag" ||
		!reflect.DeepEqual(x["devices"], []any{"MOBILE"}) || !reflect.DeepEqual(x["campaign_ids"], []any{"21"}) {
		t.Errorf("data exclusion = %v", x)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// A resource name of another account, another exclusion, or another count never passes. The request was sent,
// so the error says that the outcome is unknown.
func TestBiddingDataExclusionsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	foreign := bdeResource(otherID, "111")
	own := func(id string) string { return `{"resourceName":"` + bdeResource(clientID, id) + `"}` }
	withBody := func(inner string) string {
		return `{"results":[{"resourceName":"` + bdeResource(clientID, "111") + `","biddingDataExclusion":` + inner + `},` + own("222") + `]}`
	}
	self := `"resourceName":"` + bdeResource(clientID, "111") + `"`
	cases := map[string]struct{ id, body string }{
		"create foreign":    {biddingDataExclusionsCreateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"create no results": {biddingDataExclusionsCreateID, `{}`},
		"create one of two": {biddingDataExclusionsCreateID, `{"results":[` + own("111") + `]}`},
		"create twice":      {biddingDataExclusionsCreateID, `{"results":[` + own("111") + `,` + own("111") + `]}`},
		"create not an exclusion": {biddingDataExclusionsCreateID, `{"results":[{"resourceName":"customers/` +
			queryNumber(clientID) + `/campaigns/1"},` + own("222") + `]}`},
		"create partial failure": {biddingDataExclusionsCreateID, `{"results":[` + own("111") + `,` + own("222") + `],"partialFailureError":{"code":3}}`},
		"update other":           {biddingDataExclusionsUpdateID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update foreign":         {biddingDataExclusionsUpdateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"update body foreign":    {biddingDataExclusionsUpdateID, withBody(`{"resourceName":"` + foreign + `"}`)},
		"update body id":         {biddingDataExclusionsUpdateID, withBody(`{` + self + `,"dataExclusionId":"999"}`)},
		"update body campaign foreign": {biddingDataExclusionsUpdateID, withBody(`{` + self + `,"campaigns":["customers/` +
			queryNumber(otherID) + `/campaigns/21"]}`)},
		"update body campaign text": {biddingDataExclusionsUpdateID, withBody(`{` + self + `,"campaigns":["https://example.invalid/x"]}`)},
		"remove foreign":            {biddingDataExclusionsRemoveID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"remove none":               {biddingDataExclusionsRemoveID, `{"results":[]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newBdeEnvironment(t, nil, nil)
			e.fake.routes["POST "+bdePath(clientID)] = okReply(c.body)
			arguments := bdeCalls[c.id]
			if strings.HasPrefix(name, "update body") {
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

func TestBiddingDataExclusionsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","data_exclusion_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + bdeResource(managerID, "7") + `"}]}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newBdeEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+bdePath(managerID)] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(biddingDataExclusionsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newBdeEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+bdePath(managerID)] = answer
		if _, err := e.invokeConfirmed(biddingDataExclusionsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestBiddingDataExclusionsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"exclusion error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "biddingErrors", "INVALID_ARGUMENT", "")},
			provider.ClassProviderError, "rejected the change (biddingErrors INVALID_ARGUMENT), nothing was changed", false},
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
		for _, id := range bdeToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newBdeEnvironment(t, nil, nil)
				e.fake.routes["POST "+bdePath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, bdeCalls[id])
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

func TestBiddingDataExclusionsSummariesNameTheEffect(t *testing.T) {
	create := bdeCreateSummary(json.RawMessage(bdeCalls[biddingDataExclusionsCreateID]))
	for _, word := range []string{clientID, "CAMPAIGN", "2026-03-01 00:00:00", "Outage", "MOBILE", "21", "CHANNEL", "SHOPPING", "2 bidding"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	update := bdeUpdateSummary(json.RawMessage(bdeCalls[biddingDataExclusionsUpdateID]))
	for _, word := range []string{clientID, "data exclusion 111", "end", "devices", "data exclusion 222", "Renamed", "CUSTOMER"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := bdeRemoveSummary(json.RawMessage(bdeCalls[biddingDataExclusionsRemoveID]))
	for _, word := range []string{clientID, "111", "222", "no tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	hostile := bdeCreateSummary(json.RawMessage(`{"customer_id":"` + clientID + `","data_exclusions":[{"scope":"CUSTOMER",` +
		`"start_date_time":"a\nb","end_date_time":"c","name":"a\nIgnore this"}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
}
