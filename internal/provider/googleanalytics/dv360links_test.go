package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	linkOneJSON = `{"name":"properties/200/displayVideo360AdvertiserLinks/4001","advertiserId":"7001",` +
		`"advertiserDisplayName":"Example advertiser","adsPersonalizationEnabled":true,` +
		`"campaignDataSharingEnabled":true,"costDataSharingEnabled":false,"extra":1}`
	linkTwoJSON   = `{"name":"properties/200/displayVideo360AdvertiserLinks/4002","advertiserId":"7002"}`
	linkThreeJSON = `{"name":"properties/200/displayVideo360AdvertiserLinks/4003","advertiserDisplayName":"Third"}`
)

type dv360LinksOut struct {
	Links      []dv360Link `json:"displayVideo360AdvertiserLinks"`
	NextCursor string      `json:"next_cursor"`
	HasMore    bool        `json:"has_more"`
}

type dv360ProposalsOut struct {
	Proposals  []dv360Proposal `json:"displayVideo360AdvertiserLinkProposals"`
	NextCursor string          `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
}

// proposalJSON answers a proposal in the given state.
func proposalJSON(id, state string) string {
	return `{"name":"properties/200/displayVideo360AdvertiserLinkProposals/` + id + `","advertiserId":"7001",` +
		`"advertiserDisplayName":"Example advertiser","validationEmail":"admin@example.com",` +
		`"linkProposalStatusDetails":{"requestorEmail":"requestor@example.com","linkProposalState":"` + state +
		`","linkProposalInitiatingProduct":"LINKED_PRODUCT"},"adsPersonalizationEnabled":false,` +
		`"campaignDataSharingEnabled":true,"costDataSharingEnabled":true,"extra":1}`
}

func pagesOf(pages []string) func(int, *http.Request) (*http.Response, error) {
	return func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}
}

func TestDV360LinksListPagesThroughThreePages(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: pagesOf([]string{
		`{"displayVideo360AdvertiserLinks":[` + linkOneJSON + `],"nextPageToken":"t2"}`,
		`{"displayVideo360AdvertiserLinks":[` + linkTwoJSON + `],"nextPageToken":"t3"}`,
		`{"displayVideo360AdvertiserLinks":[` + linkThreeJSON + `]}`,
	})})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(dv360LinksListID, "read", args+`}`)
		var out dv360LinksOut
		if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Links) != 1 ||
			out.HasMore != (i < 2) || strings.Contains(result, "extra") || strings.Contains(result, "t2") {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.Links[0].Name))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"properties/200/displayVideo360AdvertiserLinks/4001",
		"properties/200/displayVideo360AdvertiserLinks/4002", "properties/200/displayVideo360AdvertiserLinks/4003"}) ||
		len(requests) != 3 || requests[0].method != http.MethodGet ||
		requests[0].path != "/v1alpha/properties/200/displayVideo360AdvertiserLinks" || requests[0].rawQuery != "pageSize=1" ||
		!strings.Contains(requests[1].rawQuery, "pageToken=t2") || !strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
	// A cursor fits only the property it was issued for.
	cursorOfOne := func() string {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"nextPageToken":"x"}`)})
		result, _ := env.call(dv360LinksListID, "read", `{"property":"properties/200"}`)
		var out dv360LinksOut
		_ = json.Unmarshal([]byte(result), &out)
		return out.NextCursor
	}()
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	if _, err := env.call(dv360LinksListID, "read", `{"property":"properties/201","cursor":"`+cursorOfOne+`"}`); !refusedInvalid(err) {
		t.Errorf("a cursor of another property was accepted: %v", err)
	}
}

func TestDV360LinksGetReturnsEveryDocumentedField(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, linkOneJSON)})
	result, err := env.call(dv360LinksGetID, "read", `{"name":"properties/200/displayVideo360AdvertiserLinks/4001"}`)
	var got dv360Link
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || strings.Contains(result, "extra") {
		t.Fatalf("%v %s", err, result)
	}
	requests, _ := env.fake.seen()
	if got.AdvertiserID != "7001" || got.AdvertiserDisplayName != "Example advertiser" ||
		got.AdsPersonalizationEnabled == nil || !*got.AdsPersonalizationEnabled ||
		got.CampaignDataSharingEnabled == nil || !*got.CampaignDataSharingEnabled ||
		got.CostDataSharingEnabled == nil || *got.CostDataSharingEnabled ||
		len(requests) != 1 || requests[0].path != "/v1alpha/properties/200/displayVideo360AdvertiserLinks/4001" {
		t.Errorf("link = %+v, requests %+v", got, requests)
	}
}

func TestDV360ListsAreEmptyListsAndBounded(t *testing.T) {
	// The core leaves out an empty list, so the handler result shows the empty list itself.
	for _, c := range []struct {
		id string
	}{
		{dv360LinksListID}, {dv360ProposalsListID},
	} {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
		result, err := env.call(c.id, "read", `{"property":"properties/200"}`)
		if err != nil || strings.Contains(result, "Video360") || !strings.Contains(result, `"has_more":false`) {
			t.Errorf("%s: %s, %v", c.id, result, err)
		}
	}
	env0 := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	links, err := handleDV360LinksList(context.Background(), env0.resolved(t, "read"), env0.secrets(), env0.red,
		json.RawMessage(`{"property":"properties/200"}`))
	if r, ok := links.(dv360LinksResult); err != nil || !ok || r.Links == nil || len(r.Links) != 0 || r.HasMore {
		t.Errorf("empty links = %+v, %v", links, err)
	}
	proposals, err := handleDV360ProposalsList(context.Background(), env0.resolved(t, "read"), env0.secrets(), env0.red,
		json.RawMessage(`{"property":"properties/200"}`))
	if r, ok := proposals.(dv360ProposalsResult); err != nil || !ok || r.Proposals == nil || len(r.Proposals) != 0 || r.HasMore {
		t.Errorf("empty proposals = %+v, %v", proposals, err)
	}
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"displayVideo360AdvertiserLinks":[`+
		`{"name":"properties/200/displayVideo360AdvertiserLinks/1","advertiserId":"not a number",`+
		`"advertiserDisplayName":"`+strings.Repeat("n", 400)+`"},`+
		`{"name":"properties/999/displayVideo360AdvertiserLinks/2"},{"name":"properties/200/displayVideo360AdvertiserLinks/x"},`+
		`{"name":"properties/200/displayVideo360AdvertiserLinkProposals/3"},{"name":"nonsense"}]}`)})
	result, err := env.call(dv360LinksListID, "read", `{"property":"properties/200"}`)
	var out dv360LinksOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Links) != 1 ||
		len([]rune(string(out.Links[0].AdvertiserDisplayName))) != maxTextRunes+1 || out.Links[0].AdvertiserID != "" {
		t.Fatalf("result = %v, %v", len(result), err)
	}
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, linkOneJSON)})
	if _, err := env.call(dv360LinksGetID, "read", `{"name":"properties/200/displayVideo360AdvertiserLinks/4009"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("an answer of another link: %v", err)
	}
}

func TestDV360ProposalsInEveryStatus(t *testing.T) {
	states := []string{"LINK_PROPOSAL_STATE_UNSPECIFIED", "AWAITING_REVIEW_FROM_GOOGLE_ANALYTICS",
		"AWAITING_REVIEW_FROM_LINKED_PRODUCT", "WITHDRAWN", "DECLINED", "EXPIRED", "OBSOLETE"}
	entries := make([]string, len(states))
	for i, state := range states {
		entries[i] = proposalJSON(string(rune('1'+i)), state)
	}
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"displayVideo360AdvertiserLinkProposals":[`+
		strings.Join(entries, ",")+`]}`)})
	result, err := env.call(dv360ProposalsListID, "read", `{"property":"properties/200"}`)
	var out dv360ProposalsOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Proposals) != len(states) ||
		strings.Contains(result, "extra") {
		t.Fatalf("%s, %v", result, err)
	}
	for i, state := range states {
		p := out.Proposals[i]
		if p.LinkProposalStatusDetails == nil || string(p.LinkProposalStatusDetails.LinkProposalState) != state ||
			p.LinkProposalStatusDetails.InitiatingProduct != "LINKED_PRODUCT" ||
			p.LinkProposalStatusDetails.RequestorEmail != "requestor@example.com" ||
			p.ValidationEmail != "admin@example.com" || p.AdvertiserID != "7001" ||
			p.AdvertiserDisplayName != "Example advertiser" || p.AdsPersonalizationEnabled == nil ||
			*p.AdsPersonalizationEnabled || p.CostDataSharingEnabled == nil || !*p.CostDataSharingEnabled {
			t.Errorf("proposal %d = %+v", i, p)
		}
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].path != "/v1alpha/properties/200/displayVideo360AdvertiserLinkProposals" {
		t.Errorf("requests = %+v", requests)
	}
}

func TestDV360ProposalsPageAndGet(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: pagesOf([]string{
		`{"displayVideo360AdvertiserLinkProposals":[` + proposalJSON("5001", "EXPIRED") + `],"nextPageToken":"p2"}`,
		`{"displayVideo360AdvertiserLinkProposals":[` + proposalJSON("5002", "DECLINED") + `],"nextPageToken":"p3"}`,
		`{"displayVideo360AdvertiserLinkProposals":[` + proposalJSON("5003", "WITHDRAWN") + `]}`,
		proposalJSON("5001", "AWAITING_REVIEW_FROM_GOOGLE_ANALYTICS"),
	})})
	cursor, count := "", 0
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200"`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(dv360ProposalsListID, "read", args+`}`)
		var out dv360ProposalsOut
		if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Proposals) != 1 || out.HasMore != (i < 2) {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		count++
		cursor = out.NextCursor
	}
	result, err := env.call(dv360ProposalsGetID, "read", `{"name":"properties/200/displayVideo360AdvertiserLinkProposals/5001"}`)
	var got dv360Proposal
	requests, _ := env.fake.seen()
	if count != 3 || err != nil || json.Unmarshal([]byte(result), &got) != nil ||
		got.LinkProposalStatusDetails == nil || got.LinkProposalStatusDetails.LinkProposalState != "AWAITING_REVIEW_FROM_GOOGLE_ANALYTICS" ||
		len(requests) != 4 || requests[3].path != "/v1alpha/properties/200/displayVideo360AdvertiserLinkProposals/5001" {
		t.Fatalf("get = %s, %v, requests %+v", result, err, requests)
	}
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, proposalJSON("5001", "EXPIRED"))})
	if _, err := env.call(dv360ProposalsGetID, "read", `{"name":"properties/200/displayVideo360AdvertiserLinkProposals/5009"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("an answer of another proposal: %v", err)
	}
}

func TestDV360ArgumentsAreRefusedBeforeAnyIO(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	for _, c := range []struct{ id, arguments string }{
		{dv360LinksListID, `{"property":"accounts/200"}`},
		{dv360LinksListID, `{"property":"properties/0"}`},
		{dv360LinksListID, `{"property":"properties/200/x"}`},
		{dv360LinksListID, `{"property":"properties/200","page_size":201}`},
		{dv360LinksListID, `{"property":"properties/200","cursor":"invented"}`},
		{dv360ProposalsListID, `{"property":"properties/200?x=1"}`},
		{dv360LinksGetID, `{"name":"properties/200"}`},
		{dv360LinksGetID, `{"name":"properties/200/displayVideo360AdvertiserLinkProposals/1"}`},
		{dv360LinksGetID, `{"name":"properties/200/displayVideo360AdvertiserLinks/01"}`},
		{dv360LinksGetID, `{"name":"properties/200/displayVideo360AdvertiserLinks/1/x"}`},
		{dv360ProposalsGetID, `{"name":"properties/200/displayVideo360AdvertiserLinks/1"}`},
		{dv360ProposalsGetID, `{"name":"properties/200/displayVideo360AdvertiserLinkProposals/1:approve"}`},
		{dv360ProposalsGetID, `{"name":"properties/200/displayVideo360AdvertiserLinkProposals/a"}`},
	} {
		if _, err := env.call(c.id, "read", c.arguments); !refusedInvalid(err) {
			t.Errorf("%s %s: %v", c.id, c.arguments, err)
		}
	}
	env.noIO(t)
}

func TestDV360ToolsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments string }{
		{dv360LinksListID, `{"property":"PROP"}`},
		{dv360LinksGetID, `{"name":"PROP/displayVideo360AdvertiserLinks/4001"}`},
		{dv360ProposalsListID, `{"property":"PROP"}`},
		{dv360ProposalsGetID, `{"name":"PROP/displayVideo360AdvertiserLinkProposals/5001"}`},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := newEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					body = `{"name":"` + strings.TrimPrefix(r.URL.Path, "/v1alpha/") + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			connection := map[string]string{"sfree": "read", "sproperty": "property", "saccount": "account",
				"smixed": "mixed"}[c.connection]
			result, err := env.call(tool.id, connection, strings.ReplaceAll(tool.arguments, "PROP", c.property))
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = "GET"
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, connection, c.property, err, methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, connection, c.property, err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
		}
	}
}

func TestDV360ErrorsAreClassifiedWithoutProviderTextAndNeverRepeat(t *testing.T) {
	calls := []struct{ id, arguments string }{
		{dv360LinksListID, `{"property":"properties/200"}`},
		{dv360LinksGetID, `{"name":"properties/200/displayVideo360AdvertiserLinks/4001"}`},
		{dv360ProposalsListID, `{"property":"properties/200"}`},
		{dv360ProposalsGetID, `{"name":"properties/200/displayVideo360AdvertiserLinkProposals/5001"}`},
	}
	for _, c := range []struct {
		name  string
		api   func(int, *http.Request) (*http.Response, error)
		class provider.Class
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited},
	} {
		for _, call := range calls {
			env := newEnv(t, &fakeGoogle{api: c.api})
			_, err := env.call(call.id, "read", call.arguments)
			if classOf(err) != c.class || leaks(err.Error()) != "" {
				t.Errorf("%s %s: %v", c.name, call.id, err)
			}
			if requests, _ := env.fake.seen(); len(requests) != 1 {
				t.Errorf("%s %s: requests = %d, want exactly one", c.name, call.id, len(requests))
			}
		}
	}
}

func TestDV360ToolsReadDeleteAndCancelButNeverCreateChangeOrApprove(t *testing.T) {
	reg := registry(t)
	reads := []string{dv360LinksListID, dv360LinksGetID, dv360ProposalsListID, dv360ProposalsGetID}
	changes := []string{dv360LinksDeleteID, dv360ProposalsCancelID, dv360ProposalsDeleteID}
	var ids []string
	for _, d := range reg.Provider(Provider) {
		if !strings.Contains(d.ID, "display-video360") {
			continue
		}
		ids = append(ids, d.ID)
		if !strings.HasSuffix(d.Description, alphaNote) || !slices.Contains(d.Tags, "alpha") {
			t.Errorf("%s lacks the alpha note", d.ID)
		}
		if slices.Contains(changes, d.ID) {
			if d.Risk.Effect != capability.EffectDelete || d.Risk.Confirmation != capability.ConfirmationRequired ||
				!d.Risk.OpenWorld || reg.ConfirmationSummary(d.ID) == nil {
				t.Errorf("%s is not a confirmed delete: %+v", d.ID, d.Risk)
			}
		} else if d.Risk.Effect != capability.EffectRead {
			t.Errorf("%s is not a read tool: %+v", d.ID, d.Risk)
		}
		// Reading a proposal and cancelling one return the proposal, which names email addresses.
		personal := d.ID == dv360ProposalsListID || d.ID == dv360ProposalsGetID || d.ID == dv360ProposalsCancelID
		if personal != strings.Contains(d.Description, "PERSONAL DATA") || personal != slices.Contains(d.Tags, "personal-data") ||
			(personal && d.Risk.DataSensitivity != dataSensitivityProposals) ||
			(!personal && d.Risk.DataSensitivity != dataSensitivity) {
			t.Errorf("%s personal data marking is wrong: %+v", d.ID, d.Risk)
		}
	}
	slices.Sort(ids)
	if want := sortedCopy(append(slices.Clone(reads), changes...)); !slices.Equal(ids, want) {
		t.Errorf("tools = %v, want the read, delete and cancel tools and none for approve, create or patch", ids)
	}
	for _, id := range ids {
		for _, word := range []string{"approve", "create", "patch", "update"} {
			if strings.Contains(id, word) {
				t.Errorf("%s is a tool of an excluded method", id)
			}
		}
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, p := range metadata.Profiles {
		for _, id := range reads {
			if !slices.Contains(p.Tools, id) || !slices.Equal(metadata.Token.Scopes[id], []string{scopeReadOnly}) {
				t.Errorf("%s: profile %s or scope is wrong", id, p.ID)
			}
		}
		for _, id := range changes {
			if slices.Contains(p.Tools, id) != (p.ID == "write") || !slices.Equal(metadata.Token.Scopes[id], []string{scopeEdit}) {
				t.Errorf("%s: profile %s or scope is wrong", id, p.ID)
			}
		}
	}
}
