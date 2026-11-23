package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// newOpenChangeEnvironment is a connection with the effects create and update and no tools list.
func audienceNewOpenChangeEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{
		Effects: []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate}})
}

func customInterestsPath(id string) string { return "/v26/customers/" + id + "/customInterests:mutate" }
func audiencesPath(id string) string       { return "/v26/customers/" + id + "/audiences:mutate" }
func customInterestName(n string) string {
	return "customers/" + queryNumber(clientID) + "/customInterests/" + n
}
func audienceName(n string) string { return "customers/" + queryNumber(clientID) + "/audiences/" + n }
func audienceOwned(collection, n string) string {
	return "customers/" + queryNumber(clientID) + "/" + collection + "/" + n
}

const (
	customInterestCreateTestArgs = `{"customer_id":"` + clientID + `","custom_interests":[` +
		`{"name":"Hikers","type":"CUSTOM_INTENT","description":"People who plan hikes","members":[` +
		`{"member_type":"KEYWORD","parameter":"hiking boots"},{"member_type":"URL","parameter":"example.com/trails"}]},` +
		`{"name":"Campers"}]}`
	customInterestUpdateTestArgs = `{"customer_id":"` + clientID + `","custom_interests":[` +
		`{"id":"11","name":"Trail hikers","description":"","members":[{"member_type":"KEYWORD","parameter":"trail map"}]},` +
		`{"id":"12","type":"CUSTOM_AFFINITY"}]}`
	audienceCreateTestArgs = `{"customer_id":"` + clientID + `","audiences":[{"name":"Adults","description":"25 to 44",` +
		`"dimensions":[{"age":{"age_ranges":[{"min_age":25,"max_age":44},{"min_age":65}],"include_undetermined":true}},` +
		`{"gender":{"genders":["FEMALE"]}},{"household_income":{"income_ranges":["INCOME_RANGE_90_UP"]}},` +
		`{"parental_status":{"include_undetermined":true}},{"audience_segments":{"segments":[` +
		`{"user_list":"customers/1000000003/userLists/5"},{"user_interest_category":"customers/1000000003/userInterests/80432"},` +
		`{"life_event":"customers/1000000003/lifeEvents/9"},{"detailed_demographic":"customers/1000000003/detailedDemographics/8"},` +
		`{"custom_audience":"customers/1000000003/customAudiences/7"}]}}],` +
		`"exclusion_dimension":{"exclusions":[{"user_list":"customers/1000000003/userLists/6"}]}},` +
		`{"scope":"ASSET_GROUP","asset_group":"customers/1000000003/assetGroups/4","dimensions":[{"gender":{"genders":["MALE"]}}]}]}`
	audienceUpdateTestArgs = `{"customer_id":"` + clientID + `","audiences":[` +
		`{"id":"21","name":"Adults 25-44","description":"","exclusion_dimension":{"exclusions":[]},"scope":"CUSTOMER",` +
		`"dimensions":[{"gender":{"genders":["FEMALE"]}}]},{"id":"22","name":"Other"}]}`
)

var audienceListCalls = map[string]struct{ args, path string }{
	customInterestsCreateID: {customInterestCreateTestArgs, customInterestsPath(clientID)},
	customInterestsUpdateID: {customInterestUpdateTestArgs, customInterestsPath(clientID)},
	audiencesCreateID:       {audienceCreateTestArgs, audiencesPath(clientID)},
	audiencesUpdateID:       {audienceUpdateTestArgs, audiencesPath(clientID)},
}

func TestAudienceListToolsAreOfferedByEffectAndConfirmed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{customInterestsCreateID: capability.EffectCreate,
		customInterestsUpdateID: capability.EffectUpdate, audiencesCreateID: capability.EffectCreate,
		audiencesUpdateID: capability.EffectUpdate}
	for id := range audienceListCalls {
		d, _, ok := reg.Lookup(id)
		if !ok || d.RequiresToolAllowList || d.Version != 1 || d.Risk.Effect != wantEffect[id] ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s = %+v", id, d)
		}
		if !strings.HasSuffix(d.Description, mutateListNote) || strings.HasSuffix(d.Description, mutateListListedNote) {
			t.Errorf("%s: description does not end on the change note alone", id)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		if got := metadata.Token.Scopes[id]; len(got) != 1 || got[0] != scopeAdwords {
			t.Errorf("scopes of %s = %v", id, got)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Fatalf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "path", "url", "header"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s has no validate_only", id)
		}
		_, content := input.Properties["response_content_type"]
		if content != strings.HasPrefix(id, Provider+".audiences.") {
			t.Errorf("%s: response_content_type present = %v", id, content)
		}
	}
	for _, tool := range metadata.Tools {
		if slices.Contains(notListedOnly, tool.ID) && (tool.RequiresToolAllowList || tool.Effect == config.EffectRead) {
			t.Errorf("tool %+v is not an effect-offered change tool", tool)
		}
	}
}

func TestAudienceListToolsNeedTheEffectAndTheConfirmation(t *testing.T) {
	for id, c := range audienceListCalls {
		t.Run(id, func(t *testing.T) {
			e := audienceNewOpenChangeEnvironment(t, nil, nil)
			_, err := e.invoke(id, c.args)
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
			read := newEnvironment(t, nil, nil)
			if _, err := read.invokeConfirmed(id, c.args); err == nil {
				t.Fatal("a read connection ran the tool")
			}
			read.untouched(t)
			listed := newEnvironmentWith(t, nil, nil, config.Connection{Tools: []string{id}})
			listed.fake.routes["POST "+c.path] = okReply(`{}`)
			if _, err := listed.invokeConfirmed(id, strings.TrimSuffix(c.args, "}")+`,"validate_only":true}`); err != nil {
				t.Errorf("a connection that names the tool does not offer it: %v", err)
			}
		})
	}
}

func TestAudienceListTargetsAndArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	foreign := map[string]string{
		customInterestsCreateID: `{"customer_id":"` + otherID + `","custom_interests":[{"name":"x"}]}`,
		customInterestsUpdateID: `{"customer_id":"` + otherID + `","custom_interests":[{"id":"1","name":"x"}]}`,
		audiencesCreateID:       `{"customer_id":"` + otherID + `","audiences":[{"name":"x","dimensions":[{"gender":{"genders":["MALE"]}}]}]}`,
		audiencesUpdateID:       `{"customer_id":"` + otherID + `","audiences":[{"id":"1","name":"x"}]}`,
	}
	for id, args := range foreign {
		e := audienceNewOpenChangeEnvironment(t, []string{"customers/" + clientID}, nil)
		_, err := e.invokeConfirmed(id, args)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
			t.Errorf("%s: err = %v", id, err)
		}
		e.untouched(t)
	}
	c := `{"customer_id":"` + clientID + `",`
	many := func(item string) string { return strings.Repeat(item+",", 10) + item }
	gender := `{"gender":{"genders":["MALE"]}}`
	bad := map[string][2]string{
		"interests: no entry":       {customInterestsCreateID, c + `"custom_interests":[]}`},
		"interests: 11 entries":     {customInterestsCreateID, c + `"custom_interests":[` + many(`{"name":"x"}`) + `]}`},
		"interests: blank name":     {customInterestsCreateID, c + `"custom_interests":[{"name":" "}]}`},
		"interests: type":           {customInterestsCreateID, c + `"custom_interests":[{"name":"x","type":"OTHER"}]}`},
		"interests: member type":    {customInterestsCreateID, c + `"custom_interests":[{"name":"x","members":[{"member_type":"APP","parameter":"a"}]}]}`},
		"interests: empty member":   {customInterestsCreateID, c + `"custom_interests":[{"name":"x","members":[{"member_type":"URL","parameter":""}]}]}`},
		"interests: control":        {customInterestsCreateID, c + `"custom_interests":[{"name":"x","members":[{"member_type":"URL","parameter":"a\nb"}]}]}`},
		"interests: 101 members":    {customInterestsCreateID, c + `"custom_interests":[{"name":"x","members":[` + strings.Repeat(`{"member_type":"KEYWORD","parameter":"k"},`, 100) + `{"member_type":"KEYWORD","parameter":"k"}]}]}`},
		"interests: status":         {customInterestsUpdateID, c + `"custom_interests":[{"id":"1","status":"REMOVED"}]}`},
		"interests: id not number":  {customInterestsUpdateID, c + `"custom_interests":[{"id":"customers/1/customInterests/2","name":"x"}]}`},
		"interests: id twice":       {customInterestsUpdateID, c + `"custom_interests":[{"id":"1","name":"x"},{"id":"1","name":"y"}]}`},
		"interests: nothing":        {customInterestsUpdateID, c + `"custom_interests":[{"id":"1"}]}`},
		"interests: empty members":  {customInterestsUpdateID, c + `"custom_interests":[{"id":"1","members":[]}]}`},
		"interests: response type":  {customInterestsCreateID, c + `"custom_interests":[{"name":"x"}],"response_content_type":"MUTABLE_RESOURCE"}`},
		"audiences: no dimension":   {audiencesCreateID, c + `"audiences":[{"name":"x"}]}`},
		"audiences: no name":        {audiencesCreateID, c + `"audiences":[{"dimensions":[` + gender + `]}]}`},
		"audiences: name too long":  {audiencesCreateID, c + `"audiences":[{"name":"` + strings.Repeat("a", 256) + `","dimensions":[` + gender + `]}]}`},
		"audiences: group name":     {audiencesCreateID, c + `"audiences":[{"name":"x","scope":"ASSET_GROUP","asset_group":"customers/1000000003/assetGroups/4","dimensions":[` + gender + `]}]}`},
		"audiences: group missing":  {audiencesCreateID, c + `"audiences":[{"scope":"ASSET_GROUP","dimensions":[` + gender + `]}]}`},
		"audiences: group foreign":  {audiencesCreateID, c + `"audiences":[{"scope":"ASSET_GROUP","asset_group":"customers/1000000009/assetGroups/4","dimensions":[` + gender + `]}]}`},
		"audiences: group customer": {audiencesCreateID, c + `"audiences":[{"name":"x","asset_group":"customers/1000000003/assetGroups/4","dimensions":[` + gender + `]}]}`},
		"audiences: two kinds":      {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[{"gender":{"genders":["MALE"]},"age":{"age_ranges":[{"min_age":18}]}}]}]}`},
		"audiences: min age":        {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[{"age":{"age_ranges":[{"min_age":20}]}}]}]}`},
		"audiences: max age":        {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[{"age":{"age_ranges":[{"min_age":25,"max_age":44},{"min_age":45,"max_age":44}]}}]}]}`},
		"audiences: max below min":  {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[{"age":{"age_ranges":[{"min_age":45,"max_age":34}]}}]}]}`},
		"audiences: gender":         {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[{"gender":{"genders":["OTHER"]}}]}]}`},
		"audiences: empty gender":   {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[{"gender":{}}]}]}`},
		"audiences: foreign list":   {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[{"audience_segments":{"segments":[{"user_list":"customers/1000000009/userLists/5"}]}}]}]}`},
		"audiences: constant":       {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[{"audience_segments":{"segments":[{"user_interest_category":"userInterestConstants/80432"}]}}]}]}`},
		"audiences: wrong kind":     {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[{"audience_segments":{"segments":[{"user_list":"customers/1000000003/customAudiences/5"}]}}]}]}`},
		"audiences: two refs":       {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[{"audience_segments":{"segments":[{"user_list":"customers/1000000003/userLists/5","life_event":"customers/1000000003/lifeEvents/5"}]}}]}]}`},
		"audiences: foreign excl":   {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[` + gender + `],"exclusion_dimension":{"exclusions":[{"user_list":"customers/1000000009/userLists/5"}]}}]}`},
		"audiences: 11 dimensions":  {audiencesCreateID, c + `"audiences":[{"name":"x","dimensions":[` + many(gender) + `]}]}`},
		"audiences: update scope":   {audiencesUpdateID, c + `"audiences":[{"id":"1","scope":"ASSET_GROUP"}]}`},
		"audiences: update group":   {audiencesUpdateID, c + `"audiences":[{"id":"1","asset_group":"customers/1000000003/assetGroups/4"}]}`},
		"audiences: update no dims": {audiencesUpdateID, c + `"audiences":[{"id":"1","dimensions":[]}]}`},
		"audiences: update status":  {audiencesUpdateID, c + `"audiences":[{"id":"1","status":"REMOVED"}]}`},
		"audiences: update twice":   {audiencesUpdateID, c + `"audiences":[{"id":"1","name":"x"},{"id":"1","name":"y"}]}`},
		"audiences: partial":        {audiencesUpdateID, c + `"audiences":[{"id":"1","name":"x"}],"partial_failure":true}`},
		"audiences: content type":   {audiencesUpdateID, c + `"audiences":[{"id":"1","name":"x"}],"response_content_type":"ALL"}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := audienceNewOpenChangeEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestCustomInterestsAreSentAsOneAtomicRequest(t *testing.T) {
	e := audienceNewOpenChangeEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+customInterestsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + customInterestName("31") + `"},{"resourceName":"` + customInterestName("32") + `"}]}`)
	out, err := e.invokeConfirmed(customInterestsCreateID, customInterestCreateTestArgs)
	if err != nil {
		t.Fatal(err)
	}
	results := decode(t, out)["results"].([]any)
	if len(results) != 2 || results[0].(map[string]any)["resource_name"] != customInterestName("31") ||
		results[1].(map[string]any)["resource_name"] != customInterestName("32") {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+customInterestsPath(clientID) || p.login != loginID || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	ops := body["operations"].([]any)
	if len(body) != 1 || len(ops) != 2 {
		t.Fatalf("body = %s", p.body)
	}
	first := ops[0].(map[string]any)["create"].(map[string]any)
	members := first["members"].([]any)
	if first["name"] != "Hikers" || first["type"] != "CUSTOM_INTENT" || first["description"] != "People who plan hikes" ||
		len(members) != 2 || members[1].(map[string]any)["memberType"] != "URL" ||
		members[1].(map[string]any)["parameter"] != "example.com/trails" {
		t.Errorf("first = %v", first)
	}
	if second := ops[1].(map[string]any)["create"].(map[string]any); len(second) != 1 || second["name"] != "Campers" {
		t.Errorf("second = %v", second)
	}
	for _, word := range []string{"partialFailure", "validateOnly", "responseContentType", "updateMask"} {
		if strings.Contains(p.body, word) {
			t.Errorf("body %s carries %s", p.body, word)
		}
	}
	// Only the keywords and URLs are data: no other request leaves the process.
}

func TestCustomInterestUpdatesNameTheObjectsOfTheAccount(t *testing.T) {
	e := audienceNewOpenChangeEnvironment(t, nil, nil)
	e.fake.routes["POST "+customInterestsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + customInterestName("11") + `"},{"resourceName":"` + customInterestName("12") + `"}]}`)
	out, err := e.invokeConfirmed(customInterestsUpdateID, customInterestUpdateTestArgs)
	if err != nil {
		t.Fatal(err)
	}
	results := decode(t, out)["results"].([]any)
	if len(results) != 2 || len(results[0].(map[string]any)["updated"].([]any)) != 3 ||
		results[1].(map[string]any)["updated"].([]any)[0] != "type" {
		t.Errorf("out = %s", out)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first := ops[0].(map[string]any)
	update := first["update"].(map[string]any)
	if first["updateMask"] != "name,description,members" || update["resourceName"] != customInterestName("11") ||
		update["name"] != "Trail hikers" || update["description"] != nil || len(update["members"].([]any)) != 1 {
		t.Errorf("first = %v", first)
	}
	second := ops[1].(map[string]any)
	if second["updateMask"] != "type" || second["update"].(map[string]any)["resourceName"] != customInterestName("12") {
		t.Errorf("second = %v", second)
	}
}

func TestAudiencesBodiesFollowTheArguments(t *testing.T) {
	e := audienceNewOpenChangeEnvironment(t, nil, nil)
	e.fake.routes["POST "+audiencesPath(clientID)] = okReply(`{"results":[{"resourceName":"` + audienceName("41") + `"},{"resourceName":"` + audienceName("42") + `"}]}`)
	if _, err := e.invokeConfirmed(audiencesCreateID, audienceCreateTestArgs); err != nil {
		t.Fatal(err)
	}
	body := bodyOf(t, e.posts()[0])
	ops := body["operations"].([]any)
	first := ops[0].(map[string]any)["create"].(map[string]any)
	dims := first["dimensions"].([]any)
	age := dims[0].(map[string]any)["age"].(map[string]any)
	ranges := age["ageRanges"].([]any)
	segments := dims[4].(map[string]any)["audienceSegments"].(map[string]any)["segments"].([]any)
	exclusion := first["exclusionDimension"].(map[string]any)["exclusions"].([]any)[0].(map[string]any)["userList"].(map[string]any)
	if first["name"] != "Adults" || len(dims) != 5 || age["includeUndetermined"] != true || len(ranges) != 2 ||
		ranges[0].(map[string]any)["maxAge"] != float64(44) || ranges[1].(map[string]any)["maxAge"] != nil ||
		dims[1].(map[string]any)["gender"].(map[string]any)["genders"].([]any)[0] != "FEMALE" ||
		dims[2].(map[string]any)["householdIncome"].(map[string]any)["incomeRanges"].([]any)[0] != "INCOME_RANGE_90_UP" ||
		dims[3].(map[string]any)["parentalStatus"].(map[string]any)["includeUndetermined"] != true ||
		len(segments) != 5 || segments[1].(map[string]any)["userInterest"].(map[string]any)["userInterestCategory"] != "customers/1000000003/userInterests/80432" ||
		segments[4].(map[string]any)["customAudience"].(map[string]any)["customAudience"] != "customers/1000000003/customAudiences/7" ||
		exclusion["userList"] != "customers/1000000003/userLists/6" {
		t.Errorf("first = %v", first)
	}
	second := ops[1].(map[string]any)["create"].(map[string]any)
	if second["scope"] != "ASSET_GROUP" || second["assetGroup"] != "customers/1000000003/assetGroups/4" || second["name"] != nil {
		t.Errorf("second = %v", second)
	}
	if strings.Contains(e.posts()[0].body, "partialFailure") || len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}

	u := audienceNewOpenChangeEnvironment(t, nil, nil)
	u.fake.routes["POST "+audiencesPath(clientID)] = okReply(`{"results":[{"resourceName":"` + audienceName("21") + `"},{"resourceName":"` + audienceName("22") + `"}]}`)
	if _, err := u.invokeConfirmed(audiencesUpdateID, audienceUpdateTestArgs+""); err != nil {
		t.Fatal(err)
	}
	uops := bodyOf(t, u.posts()[0])["operations"].([]any)
	one := uops[0].(map[string]any)
	update := one["update"].(map[string]any)
	if one["updateMask"] != "name,description,dimensions,exclusionDimension,scope" || update["resourceName"] != audienceName("21") ||
		update["exclusionDimension"] != nil || update["description"] != nil || update["scope"] != "CUSTOMER" {
		t.Errorf("update = %v", one)
	}
	if two := uops[1].(map[string]any); two["updateMask"] != "name" {
		t.Errorf("update = %v", two)
	}
}

func TestAudienceListValidateOnlyValidatesWithoutResults(t *testing.T) {
	for id, c := range audienceListCalls {
		for _, answer := range []string{`{}`, `{"results":[]}`, `{"results":[{},{}]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := audienceNewOpenChangeEnvironment(t, nil, nil)
				e.fake.routes["POST "+c.path] = okReply(answer)
				out, err := e.invokeConfirmed(id, strings.TrimSuffix(c.args, "}")+`,"validate_only":true}`)
				if err != nil {
					t.Fatal(err)
				}
				got := decode(t, out)
				if list, _ := got["results"].([]any); got["validate_only"] != true || len(list) != 0 {
					t.Errorf("out = %s", out)
				}
				if body := bodyOf(t, e.posts()[0]); body["validateOnly"] != true || len(e.posts()) != 1 {
					t.Errorf("body = %v", body)
				}
			})
		}
	}
}

func TestAudienceMutableResourceIsBoundToTheAccount(t *testing.T) {
	good := `{"results":[{"resourceName":"` + audienceName("21") + `","audience":{"resourceName":"` + audienceName("21") + `",` +
		`"id":"21","status":"ENABLED","name":"Adults","description":"d","scope":"CUSTOMER","dimensions":[` +
		`{"age":{"ageRanges":[{"minAge":25,"maxAge":44}],"includeUndetermined":true}},{"gender":{"genders":["FEMALE","NEW"]}},` +
		`{"audienceSegments":{"segments":[{"userList":{"userList":"` + audienceOwned("userLists", "5") + `"}},` +
		`{"userInterest":{"userInterestCategory":"` + audienceOwned("userInterests", "6") + `"}}]}}],` +
		`"exclusionDimension":{"exclusions":[{"userList":{"userList":"` + audienceOwned("userLists", "7") + `"}}]},` +
		`"assetGroup":"` + audienceOwned("assetGroups", "4") + `","unknown":"never held"}}]}`
	args := `{"customer_id":"` + clientID + `","audiences":[{"id":"21","name":"Adults"}],"response_content_type":"MUTABLE_RESOURCE"}`
	e := audienceNewOpenChangeEnvironment(t, nil, nil)
	e.fake.routes["POST "+audiencesPath(clientID)] = okReply(good)
	out, err := e.invokeConfirmed(audiencesUpdateID, args)
	if err != nil {
		t.Fatal(err)
	}
	a := decode(t, out)["results"].([]any)[0].(map[string]any)["audience"].(map[string]any)
	dims := a["dimensions"].([]any)
	segs := dims[2].(map[string]any)["audience_segments"].(map[string]any)["segments"].([]any)
	if a["id"] != "21" || a["status"] != "ENABLED" || a["scope"] != "CUSTOMER" || len(dims) != 3 ||
		dims[0].(map[string]any)["age"].(map[string]any)["age_ranges"].([]any)[0].(map[string]any)["max_age"] != float64(44) ||
		dims[1].(map[string]any)["gender"].(map[string]any)["genders"].([]any)[1] != "NEW" ||
		segs[1].(map[string]any)["user_interest_category"] != audienceOwned("userInterests", "6") ||
		a["asset_group"] != audienceOwned("assetGroups", "4") || a["unknown"] != nil {
		t.Errorf("audience = %v", a)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
	foreign := "customers/" + queryNumber(otherID)
	for name, bad := range map[string]string{
		"another object":    strings.Replace(good, `"audience":{"resourceName":"`+audienceName("21"), `"audience":{"resourceName":"`+audienceName("99"), 1),
		"foreign list":      strings.Replace(good, audienceOwned("userLists", "5"), foreign+"/userLists/5", 1),
		"constant":          strings.Replace(good, audienceOwned("userInterests", "6"), "userInterestConstants/6", 1),
		"foreign exclusion": strings.Replace(good, audienceOwned("userLists", "7"), foreign+"/userLists/7", 1),
		"foreign group":     strings.Replace(good, audienceOwned("assetGroups", "4"), foreign+"/assetGroups/4", 1),
	} {
		t.Run(name, func(t *testing.T) {
			e := audienceNewOpenChangeEnvironment(t, nil, nil)
			e.fake.routes["POST "+audiencesPath(clientID)] = okReply(bad)
			_, err := e.invokeConfirmed(audiencesUpdateID, args)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAudienceListResultsAreBoundToTheOperationsSent(t *testing.T) {
	foreign := "customers/" + queryNumber(otherID)
	cases := map[string]struct{ id, body string }{
		"too few":         {customInterestsCreateID, `{"results":[{"resourceName":"` + customInterestName("31") + `"}]}`},
		"too many":        {customInterestsCreateID, `{"results":[{"resourceName":"` + customInterestName("31") + `"},{"resourceName":"` + customInterestName("32") + `"},{"resourceName":"` + customInterestName("33") + `"}]}`},
		"none":            {customInterestsCreateID, `{}`},
		"foreign create":  {customInterestsCreateID, `{"results":[{"resourceName":"` + customInterestName("31") + `"},{"resourceName":"` + foreign + `/customInterests/32"}]}`},
		"other kind":      {customInterestsCreateID, `{"results":[{"resourceName":"` + customInterestName("31") + `"},{"resourceName":"` + audienceName("32") + `"}]}`},
		"same twice":      {customInterestsCreateID, `{"results":[{"resourceName":"` + customInterestName("31") + `"},{"resourceName":"` + customInterestName("31") + `"}]}`},
		"other update":    {customInterestsUpdateID, `{"results":[{"resourceName":"` + customInterestName("11") + `"},{"resourceName":"` + customInterestName("99") + `"}]}`},
		"swapped update":  {customInterestsUpdateID, `{"results":[{"resourceName":"` + customInterestName("12") + `"},{"resourceName":"` + customInterestName("11") + `"}]}`},
		"audience create": {audiencesCreateID, `{"results":[{"resourceName":"` + audienceName("41") + `"},{"resourceName":"` + foreign + `/audiences/42"}]}`},
		"audience update": {audiencesUpdateID, `{"results":[{"resourceName":"` + audienceName("21") + `"},{"resourceName":"` + audienceName("23") + `"}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := audienceNewOpenChangeEnvironment(t, nil, nil)
			e.fake.routes["POST "+audienceListCalls[c.id].path] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, audienceListCalls[c.id].args)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAudienceListErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"refusal", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "customInterestError", "NAME_ALREADY_USED", "")},
			provider.ClassProviderError, "customInterestError NAME_ALREADY_USED), nothing was changed"},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED",
			`{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown"},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown"},
		{"unreadable", okReply(`{"results":`), provider.ClassInvalidResponse, "outcome of the change is unknown"},
	}
	for _, c := range cases {
		for id, call := range audienceListCalls {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := audienceNewOpenChangeEnvironment(t, nil, nil)
				e.fake.routes["POST "+call.path] = c.reply
				_, err := e.invokeConfirmed(id, call.args)
				if classOf(err) != c.class || !strings.Contains(err.Error(), c.message) || strings.Contains(err.Error(), detailCanary) {
					t.Fatalf("err = %v, want %s with %q", err, c.class, c.message)
				}
				if len(e.posts()) != 1 {
					t.Errorf("posts = %d, a change is never repeated", len(e.posts()))
				}
			})
		}
	}
}

func TestAudienceListManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerArgs := map[string]string{
		customInterestsCreateID: `{"customer_id":"` + managerID + `","custom_interests":[{"name":"x"}]}`,
		customInterestsUpdateID: `{"customer_id":"` + managerID + `","custom_interests":[{"id":"1","name":"x"}]}`,
		audiencesCreateID:       `{"customer_id":"` + managerID + `","audiences":[{"name":"x","dimensions":[{"gender":{"genders":["MALE"]}}]}]}`,
		audiencesUpdateID:       `{"customer_id":"` + managerID + `","audiences":[{"id":"1","name":"x"}]}`,
	}
	for id, args := range managerArgs {
		t.Run(id+" customer target", func(t *testing.T) {
			e := audienceNewOpenChangeEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = func(s seen) *reply {
				if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
					return &r
				}
				return nil
			}
			_, err := e.invokeConfirmed(id, args)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
		t.Run(id+" manager target", func(t *testing.T) {
			e := audienceNewOpenChangeEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.routes["POST /v26/customers/"+managerID+"/"+strings.TrimPrefix(audienceListCalls[id].path, "/v26/customers/"+clientID+"/")] = okReply(`{}`)
			if _, err := e.invokeConfirmed(id, strings.TrimSuffix(args, "}")+`,"validate_only":true}`); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
				t.Errorf("requests = %+v", got)
			}
		})
	}
}

func TestAudienceListSummariesNameEveryObject(t *testing.T) {
	text := customInterestsCreateSummary(json.RawMessage(customInterestCreateTestArgs))
	for _, word := range []string{clientID, "2 custom interests", `"Hikers"`, "CUSTOM_INTENT", "1 keyword, 1 URL", `"Campers"`, "CUSTOM_AFFINITY"} {
		if !strings.Contains(text, word) {
			t.Errorf("summary %q lacks %q", text, word)
		}
	}
	text = customInterestsUpdateSummary(json.RawMessage(customInterestUpdateTestArgs))
	for _, word := range []string{clientID, "custom interest 11", "custom interest 12", `"Trail hikers"`, "replace all members by 1 keyword"} {
		if !strings.Contains(text, word) {
			t.Errorf("summary %q lacks %q", text, word)
		}
	}
	text = audiencesCreateSummary(json.RawMessage(audienceCreateTestArgs))
	for _, word := range []string{clientID, "2 audiences", `"Adults"`, "5 audience segments", "excludes 1 user list", "assetGroups/4", "ASSET_GROUP"} {
		if !strings.Contains(text, word) {
			t.Errorf("summary %q lacks %q", text, word)
		}
	}
	text = audiencesUpdateSummary(json.RawMessage(audienceUpdateTestArgs))
	for _, word := range []string{clientID, "audience 21", "audience 22", "no exclusions", "set the scope to CUSTOMER"} {
		if !strings.Contains(text, word) {
			t.Errorf("summary %q lacks %q", text, word)
		}
	}
	if !strings.HasPrefix(customInterestsCreateSummary(json.RawMessage(strings.TrimSuffix(customInterestCreateTestArgs, "}")+`,"validate_only":true}`)), "Only validate") {
		t.Error("a validate-only summary does not say so")
	}
	if got := customInterestsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID + `","custom_interests":[{"name":"a\nb` + strings.Repeat("x", 200) + `"}]}`)); strings.Contains(got, "\n") || len(got) > 400 {
		t.Errorf("summary %q holds an uncut name", got)
	}
}
