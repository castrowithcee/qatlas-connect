package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func variablesPath(id string) string {
	return "/v26/customers/" + id + "/conversionCustomVariables:mutate"
}
func remarketingPath(id string) string { return "/v26/customers/" + id + "/remarketingActions:mutate" }

func variableName(id, n string) string {
	return "customers/" + queryNumber(id) + "/conversionCustomVariables/" + n
}
func remarketingName(id, n string) string {
	return "customers/" + queryNumber(id) + "/remarketingActions/" + n
}

const (
	variablesCreateArgs = `"conversion_custom_variables":[{"name":"Plan","tag":"plan","status":"PAUSED"},{"name":"Tier","tag":"tier_2"}]`
	variablesUpdateArgs = `"conversion_custom_variables":[{"conversion_custom_variable_id":"11","name":"Plan B"},` +
		`{"conversion_custom_variable_id":"12","status":"ENABLED"},{"conversion_custom_variable_id":"13","name":"C","status":"PAUSED"}]`
	remarketingCreateArgs = `"remarketing_actions":[{"name":"Shop visitors"},{"name":"Cart"}]`
	remarketingUpdateArgs = `"remarketing_actions":[{"remarketing_action_id":"21","name":"All visitors"}]`
)

// packageCalls are the calls of the four tools of this package, each valid on its own.
func packageCalls() map[string]string {
	return map[string]string{
		conversionCustomVariablesCreateID: `{"customer_id":"` + clientID + `",` + variablesCreateArgs + `}`,
		conversionCustomVariablesUpdateID: `{"customer_id":"` + clientID + `",` + variablesUpdateArgs + `}`,
		remarketingActionsCreateID:        `{"customer_id":"` + clientID + `",` + remarketingCreateArgs + `}`,
		remarketingActionsUpdateID:        `{"customer_id":"` + clientID + `",` + remarketingUpdateArgs + `}`,
	}
}

func pathOfPackageTool(id string) string {
	if strings.Contains(id, "remarketing") {
		return remarketingPath(clientID)
	}
	return variablesPath(clientID)
}

func TestConversionToolsDescribeTheirLimits(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for id := range packageCalls() {
		d, _, ok := reg.Lookup(id)
		if !ok {
			t.Fatalf("%s is not registered", id)
		}
		var input struct {
			Properties map[string]struct {
				MaxItems int `json:"maxItems"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(d.InputSchema, &input); err != nil {
			t.Fatal(err)
		}
		list := "conversion_custom_variables"
		if strings.Contains(id, "remarketing") {
			list = "remarketing_actions"
		}
		if input.Properties[list].MaxItems != maxListOperations {
			t.Errorf("%s: list bound = %d", id, input.Properties[list].MaxItems)
		}
		if _, found := input.Properties["response_content_type"]; found == strings.Contains(id, "remarketing") {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
		for _, word := range []string{"atomic", "never repeats", "outcome is unknown", "one operation of the daily quota per entry"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", id, word)
			}
		}
		if strings.Contains(string(d.InputSchema), "ACTIVATION_NEEDED") || strings.Contains(string(d.InputSchema), "tag_snippets") {
			t.Errorf("%s: schema offers a value that is not settable", id)
		}
	}
	d, _, _ := reg.Lookup(remarketingActionsCreateID)
	if !strings.Contains(d.Description, "neither takes nor returns snippets") {
		t.Errorf("remarketing: description does not say that the snippets stay out: %s", d.Description)
	}
	for _, field := range d.Fields {
		if strings.Contains(field.Name, "snippet") {
			t.Errorf("remarketing: field %s", field.Name)
		}
	}
}

func TestConversionChangesNeedTheEffectAndTheConfirmation(t *testing.T) {
	wantEffect := map[string]config.Effect{conversionCustomVariablesCreateID: config.EffectCreate,
		conversionCustomVariablesUpdateID: config.EffectUpdate, remarketingActionsCreateID: config.EffectCreate,
		remarketingActionsUpdateID: config.EffectUpdate}
	for id, arguments := range packageCalls() {
		t.Run(id+" over the read effect", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a read connection ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" over the other changing effects", func(t *testing.T) {
			other := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
				config.EffectExecute}
			other = slices.DeleteFunc(other, func(effect config.Effect) bool { return effect == wantEffect[id] })
			e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: other})
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a connection without the effect of the tool ran it")
			}
			e.untouched(t)
		})
		t.Run(id+" through the tools list", func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Tools: []string{id}})
			e.fake.routes["POST "+pathOfPackageTool(id)] = okReply(`{"results":[]}`)
			if _, err := e.invokeConfirmed(id, strings.TrimSuffix(arguments, "}")+`,"validate_only":true}`); err != nil {
				t.Fatalf("the tools list does not offer the tool: %v", err)
			}
		})
		t.Run(id+" without confirmation", func(t *testing.T) {
			e := newEffectEnvironment(t, nil, nil)
			_, err := e.invoke(id, arguments)
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
		t.Run(id+" target is checked before secret and IO", func(t *testing.T) {
			e := newEffectEnvironment(t, []string{"customers/" + otherID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), clientID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestConversionArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	long := strings.Repeat("a", 101)
	many := func(item string) string { return strings.TrimSuffix(strings.Repeat(item+",", 11), ",") }
	bad := map[string]map[string]string{
		conversionCustomVariablesCreateID: {
			"no tag":            `[{"name":"Plan"}]`,
			"no name":           `[{"tag":"plan"}]`,
			"upper case tag":    `[{"name":"Plan","tag":"Plan"}]`,
			"tag with space":    `[{"name":"Plan","tag":"a b"}]`,
			"tag too long":      `[{"name":"Plan","tag":"` + long + `"}]`,
			"name too long":     `[{"name":"` + long + `","tag":"plan"}]`,
			"name with space":   `[{"name":" Plan","tag":"plan"}]`,
			"blank name":        `[{"name":"  ","tag":"plan"}]`,
			"tag twice":         `[{"name":"A","tag":"plan"},{"name":"B","tag":"plan"}]`,
			"activation needed": `[{"name":"Plan","tag":"plan","status":"ACTIVATION_NEEDED"}]`,
			"unknown status":    `[{"name":"Plan","tag":"plan","status":"REMOVED"}]`,
			"id on create":      `[{"name":"Plan","tag":"plan","conversion_custom_variable_id":"5"}]`,
			"empty list":        `[]`,
			"too many":          `[` + many(`{"name":"Plan","tag":"plan"}`) + `]`,
		},
		conversionCustomVariablesUpdateID: {
			"no id":           `[{"name":"Plan"}]`,
			"nothing to set":  `[{"conversion_custom_variable_id":"5"}]`,
			"tag change":      `[{"conversion_custom_variable_id":"5","tag":"plan"}]`,
			"id as name":      `[{"conversion_custom_variable_id":"customers/` + queryNumber(clientID) + `/conversionCustomVariables/5","name":"A"}]`,
			"foreign path":    `[{"conversion_custom_variable_id":"../5","name":"A"}]`,
			"leading zero":    `[{"conversion_custom_variable_id":"05","name":"A"}]`,
			"id twice":        `[{"conversion_custom_variable_id":"5","name":"A"},{"conversion_custom_variable_id":"5","name":"B"}]`,
			"activation":      `[{"conversion_custom_variable_id":"5","status":"ACTIVATION_NEEDED"}]`,
			"name with space": `[{"conversion_custom_variable_id":"5","name":"A "}]`,
			"too many":        `[` + many(`{"conversion_custom_variable_id":"5","name":"A"}`) + `]`,
		},
		remarketingActionsCreateID: {
			"no name":     `[{}]`,
			"blank name":  `[{"name":"  "}]`,
			"control":     `[{"name":"a\u0007b"}]`,
			"too long":    `[{"name":"` + strings.Repeat("a", 1025) + `"}]`,
			"id on write": `[{"name":"A","remarketing_action_id":"5"}]`,
			"empty list":  `[]`,
			"too many":    `[` + many(`{"name":"A"}`) + `]`,
		},
		remarketingActionsUpdateID: {
			"no id":        `[{"name":"A"}]`,
			"no name":      `[{"remarketing_action_id":"5"}]`,
			"bad id":       `[{"remarketing_action_id":"x","name":"A"}]`,
			"id twice":     `[{"remarketing_action_id":"5","name":"A"},{"remarketing_action_id":"5","name":"B"}]`,
			"snippet sent": `[{"remarketing_action_id":"5","name":"A","tag_snippets":[]}]`,
			"too many":     `[` + many(`{"remarketing_action_id":"5","name":"A"}`) + `]`,
		},
	}
	for id, cases := range bad {
		list := "conversion_custom_variables"
		if strings.Contains(id, "remarketing") {
			list = "remarketing_actions"
		}
		for name, entries := range cases {
			t.Run(id+" "+name, func(t *testing.T) {
				e := newEffectEnvironment(t, nil, nil)
				if _, err := e.invokeConfirmed(id, `{"customer_id":"`+clientID+`","`+list+`":`+entries+`}`); err == nil {
					t.Fatal("accepted")
				}
				e.untouched(t)
			})
		}
	}
	t.Run("a response content type remarketing does not know", func(t *testing.T) {
		e := newEffectEnvironment(t, nil, nil)
		_, err := e.invokeConfirmed(remarketingActionsCreateID, `{"customer_id":"`+clientID+`",`+remarketingCreateArgs+
			`,"response_content_type":"MUTABLE_RESOURCE"}`)
		if err == nil {
			t.Fatal("accepted")
		}
		e.untouched(t)
	})
	for _, extra := range []string{`"partial_failure":true`, `"operations":[]`, `"login_customer_id":"` + loginID + `"`} {
		for id, arguments := range packageCalls() {
			e := newEffectEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, strings.TrimSuffix(arguments, "}")+","+extra+"}"); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestConversionBodiesAreOneAtomicRequestWithoutPartialFailure(t *testing.T) {
	type want struct {
		path       string
		operations string
		answer     string
		results    int
	}
	cases := map[string]want{
		conversionCustomVariablesCreateID: {variablesPath(clientID), `[{"create":{"name":"Plan","tag":"plan","status":"PAUSED"}},` +
			`{"create":{"name":"Tier","tag":"tier_2"}}]`,
			`{"results":[{"resourceName":"` + variableName(clientID, "31") + `"},{"resourceName":"` + variableName(clientID, "32") + `"}]}`, 2},
		conversionCustomVariablesUpdateID: {variablesPath(clientID), `[{"update":{"resourceName":"` + variableName(clientID, "11") +
			`","name":"Plan B"},"updateMask":"name"},{"update":{"resourceName":"` + variableName(clientID, "12") +
			`","status":"ENABLED"},"updateMask":"status"},{"update":{"resourceName":"` + variableName(clientID, "13") +
			`","name":"C","status":"PAUSED"},"updateMask":"name,status"}]`,
			`{"results":[{"resourceName":"` + variableName(clientID, "11") + `"},{"resourceName":"` + variableName(clientID, "12") +
				`"},{"resourceName":"` + variableName(clientID, "13") + `"}]}`, 3},
		remarketingActionsCreateID: {remarketingPath(clientID), `[{"create":{"name":"Shop visitors"}},{"create":{"name":"Cart"}}]`,
			`{"results":[{"resourceName":"` + remarketingName(clientID, "41") + `"},{"resourceName":"` + remarketingName(clientID, "42") + `"}]}`, 2},
		remarketingActionsUpdateID: {remarketingPath(clientID), `[{"update":{"resourceName":"` + remarketingName(clientID, "21") +
			`","name":"All visitors"},"updateMask":"name"}]`,
			`{"results":[{"resourceName":"` + remarketingName(clientID, "21") + `"}]}`, 1},
	}
	for id, c := range cases {
		t.Run(id, func(t *testing.T) {
			e := newEffectEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+c.path] = okReply(c.answer)
			out, err := e.invokeConfirmed(id, packageCalls()[id])
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			results, _ := got["results"].([]any)
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != c.results {
				t.Errorf("out = %s", out)
			}
			first := results[0].(map[string]any)
			if first["resource_name"] == "" || (first["conversion_custom_variable_id"] == nil) == (first["remarketing_action_id"] == nil) {
				t.Errorf("first result = %v", first)
			}
			if len(e.fake.api()) != 1 {
				t.Fatalf("requests = %+v", e.fake.api())
			}
			p := e.posts()[0]
			if p.url != "https://googleads.googleapis.com"+c.path || p.auth != "Bearer synthetic-access-1" ||
				p.login != loginID || p.contentType != "application/json" || p.developerToken != "" {
				t.Errorf("request = %+v", p)
			}
			body := bodyOf(t, p)
			if len(body) != 1 || !reflect.DeepEqual(body["operations"], wantWire(t, c.operations)) {
				t.Errorf("body = %s", p.body)
			}
			if strings.Contains(p.body, "partialFailure") {
				t.Errorf("body %s sets partial failure", p.body)
			}
		})
	}
}

func TestConversionValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range packageCalls() {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newEffectEnvironment(t, nil, nil)
				e.fake.routes["POST "+pathOfPackageTool(id)] = okReply(answer)
				out, err := e.invokeConfirmed(id, strings.TrimSuffix(arguments, "}")+`,"validate_only":true}`)
				if err != nil {
					t.Fatal(err)
				}
				got := decode(t, out)
				if results, _ := got["results"].([]any); got["validate_only"] != true || len(results) != 0 {
					t.Errorf("out = %s", out)
				}
				posts := e.posts()
				if len(posts) != 1 || bodyOf(t, posts[0])["validateOnly"] != true {
					t.Errorf("posts = %+v", posts)
				}
			})
		}
	}
	t.Run("a validation error is the refusal of Google", func(t *testing.T) {
		e := newEffectEnvironment(t, nil, nil)
		e.fake.routes["POST "+variablesPath(clientID)] = reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT",
			"conversionCustomVariableError", "DUPLICATE_TAG", "")}
		_, err := e.invokeConfirmed(conversionCustomVariablesCreateID, strings.TrimSuffix(packageCalls()[conversionCustomVariablesCreateID], "}")+
			`,"validate_only":true}`)
		if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "conversionCustomVariableError DUPLICATE_TAG") ||
			!strings.Contains(err.Error(), "nothing was changed") || strings.Contains(err.Error(), "outcome") ||
			strings.Contains(err.Error(), detailCanary) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestConversionVariableShowsTheMutableFields(t *testing.T) {
	e := newEffectEnvironment(t, nil, nil)
	e.fake.routes["POST "+variablesPath(clientID)] = okReply(`{"results":[{"resourceName":"` + variableName(clientID, "11") +
		`","conversionCustomVariable":{"resourceName":"` + variableName(clientID, "11") + `","id":"11","name":"Plan B",` +
		`"tag":"plan","status":"ENABLED","ownerCustomer":"customers/` + queryNumber(clientID) + `","unknownMember":"never held"}},` +
		`{"resourceName":"` + variableName(clientID, "12") + `"},{"resourceName":"` + variableName(clientID, "13") + `"}]}`)
	out, err := e.invokeConfirmed(conversionCustomVariablesUpdateID, strings.TrimSuffix(packageCalls()[conversionCustomVariablesUpdateID], "}")+
		`,"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	v := decode(t, out)["results"].([]any)[0].(map[string]any)["conversion_custom_variable"].(map[string]any)
	if v["name"] != "Plan B" || v["tag"] != "plan" || v["status"] != "ENABLED" || v["id"] != "11" ||
		v["owner_customer"] != "customers/"+queryNumber(clientID) || v["unknownMember"] != nil {
		t.Errorf("variable = %v", v)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// Nothing of an answer that is not exactly what was asked for is believed. The request was sent, so the error
// says that the outcome is unknown.
func TestConversionResultsAreBoundToTheCheckedAccountAndEntries(t *testing.T) {
	foreign := func(n string) string { return remarketingName(otherID, n) }
	own := func(n string) string { return variableName(clientID, n) }
	mutable := `,"response_content_type":"MUTABLE_RESOURCE"}`
	update := strings.TrimSuffix(packageCalls()[conversionCustomVariablesUpdateID], "}")
	create := packageCalls()[conversionCustomVariablesCreateID]
	three := `{"results":[{"resourceName":"` + own("11") + `"},{"resourceName":"` + own("12") + `"},{"resourceName":"` + own("13") + `"}]}`
	cases := map[string]struct{ id, arguments, path, body string }{
		"too few results": {conversionCustomVariablesCreateID, create, variablesPath(clientID), `{"results":[{"resourceName":"` + own("1") + `"}]}`},
		"no results":      {conversionCustomVariablesCreateID, create, variablesPath(clientID), `{}`},
		"foreign account": {conversionCustomVariablesCreateID, create, variablesPath(clientID),
			`{"results":[{"resourceName":"` + strings.Replace(own("1"), queryNumber(clientID), queryNumber(otherID), 1) + `"},{"resourceName":"` + own("2") + `"}]}`},
		"other collection": {conversionCustomVariablesCreateID, create, variablesPath(clientID),
			`{"results":[{"resourceName":"` + foreign("1") + `"},{"resourceName":"` + own("2") + `"}]}`},
		"path in the name": {conversionCustomVariablesCreateID, create, variablesPath(clientID),
			`{"results":[{"resourceName":"` + own("1/../2") + `"},{"resourceName":"` + own("2") + `"}]}`},
		"another variable than asked": {conversionCustomVariablesUpdateID, update + "}", variablesPath(clientID),
			`{"results":[{"resourceName":"` + own("12") + `"},{"resourceName":"` + own("11") + `"},{"resourceName":"` + own("13") + `"}]}`},
		"variable of another owner": {conversionCustomVariablesUpdateID, update + mutable, variablesPath(clientID),
			`{"results":[{"resourceName":"` + own("11") + `","conversionCustomVariable":{"resourceName":"` + own("11") +
				`","ownerCustomer":"customers/` + queryNumber(otherID) + `"}},{"resourceName":"` + own("12") + `"},{"resourceName":"` + own("13") + `"}]}`},
		"variable with another name": {conversionCustomVariablesUpdateID, update + mutable, variablesPath(clientID),
			`{"results":[{"resourceName":"` + own("11") + `","conversionCustomVariable":{"resourceName":"` + own("99") +
				`"}},{"resourceName":"` + own("12") + `"},{"resourceName":"` + own("13") + `"}]}`},
		"variable with another id": {conversionCustomVariablesUpdateID, update + mutable, variablesPath(clientID),
			`{"results":[{"resourceName":"` + own("11") + `","conversionCustomVariable":{"id":"99"}},{"resourceName":"` + own("12") +
				`"},{"resourceName":"` + own("13") + `"}]}`},
		"a partial failure": {conversionCustomVariablesUpdateID, update + "}", variablesPath(clientID),
			strings.TrimSuffix(three, "}") + `,"partialFailureError":{"code":3,"message":"` + detailCanary + `"}}`},
		"remarketing result of another account": {remarketingActionsUpdateID, packageCalls()[remarketingActionsUpdateID], remarketingPath(clientID),
			`{"results":[{"resourceName":"` + foreign("21") + `"}]}`},
		"remarketing other action": {remarketingActionsUpdateID, packageCalls()[remarketingActionsUpdateID], remarketingPath(clientID),
			`{"results":[{"resourceName":"` + remarketingName(clientID, "22") + `"}]}`},
		"remarketing too many": {remarketingActionsUpdateID, packageCalls()[remarketingActionsUpdateID], remarketingPath(clientID),
			`{"results":[{"resourceName":"` + remarketingName(clientID, "21") + `"},{"resourceName":"` + remarketingName(clientID, "21") + `"}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEffectEnvironment(t, nil, nil)
			e.fake.routes["POST "+c.path] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				strings.Contains(err.Error(), detailCanary) || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

// Tag snippets are website code of the provider: even when Google sent them, no answer of these tools holds
// them, nor any member that is not documented.
func TestRemarketingNeverReturnsTagSnippets(t *testing.T) {
	e := newEffectEnvironment(t, nil, nil)
	e.fake.routes["POST "+remarketingPath(clientID)] = okReply(`{"results":[{"resourceName":"` + remarketingName(clientID, "21") +
		`","remarketingAction":{"tagSnippets":[{"globalSiteTag":"<script>site-tag-canary</script>","eventSnippet":"event-canary"}]}}]}`)
	out, err := e.invokeConfirmed(remarketingActionsUpdateID, packageCalls()[remarketingActionsUpdateID])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "canary") || strings.Contains(out, "script") || strings.Contains(strings.ToLower(out), "snippet") {
		t.Errorf("out = %s", out)
	}
}

func TestConversionManagerAccountsNeedAManagerTarget(t *testing.T) {
	for id, arguments := range packageCalls() {
		arguments = strings.Replace(arguments, clientID, managerID, 1)
		t.Run(id, func(t *testing.T) {
			e := newEffectEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = func(s seen) *reply {
				if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
					return &r
				}
				return nil
			}
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestConversionErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"variable error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "conversionCustomVariableError",
			"DUPLICATE_TAG", "")}, provider.ClassProviderError, "conversionCustomVariableError DUPLICATE_TAG", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"cut connection", reply{err: errors.New("connection reset by peer")}, provider.ClassUnreachable,
			"outcome of the change is unknown", true},
		{"not json", reply{status: 200, body: "<html>", header: http.Header{"Content-Type": {"text/html"}}},
			provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	for id, arguments := range packageCalls() {
		for _, c := range cases {
			t.Run(id+" "+c.name, func(t *testing.T) {
				e := newEffectEnvironment(t, nil, nil)
				e.fake.routes["POST "+pathOfPackageTool(id)] = c.reply
				_, err := e.invokeConfirmed(id, arguments)
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
				if len(e.posts()) != 1 || len(e.fake.api()) != 1 || e.fake.tokenRequests() != 1 {
					t.Errorf("posts = %d of %d, tokens = %d, want one request and no repeat", len(e.posts()),
						len(e.fake.api()), e.fake.tokenRequests())
				}
			})
		}
	}
}

func TestConversionSummariesNameTheEffect(t *testing.T) {
	text := conversionVariablesSummary(true)(json.RawMessage(`{"customer_id":"` + clientID + `",` + variablesCreateArgs + `,"validate_only":true}`))
	for _, word := range []string{clientID, `"plan"`, `"Plan"`, "PAUSED", "Only validate"} {
		if !strings.Contains(text, word) {
			t.Errorf("summary %q lacks %q", text, word)
		}
	}
	text = conversionVariablesSummary(false)(json.RawMessage(`{"customer_id":"` + clientID + `",` + variablesUpdateArgs + `}`))
	for _, word := range []string{clientID, "variable 11", "Plan B", "status ENABLED"} {
		if !strings.Contains(text, word) {
			t.Errorf("summary %q lacks %q", text, word)
		}
	}
	text = remarketingActionsSummary(false)(json.RawMessage(`{"customer_id":"` + clientID + `",` + remarketingUpdateArgs + `}`))
	for _, word := range []string{clientID, "action 21", "All visitors"} {
		if !strings.Contains(text, word) {
			t.Errorf("summary %q lacks %q", text, word)
		}
	}
	if text = remarketingActionsSummary(true)(json.RawMessage(`{"customer_id":"` + clientID + `",` + remarketingCreateArgs + `}`)); !strings.Contains(text, "Shop visitors") {
		t.Errorf("summary %q", text)
	}
}

// conversionChangeToolIDs are the tools of this file; all of them are in notListedOnly.
var conversionChangeToolIDs = []string{conversionCustomVariablesCreateID, conversionCustomVariablesUpdateID,
	remarketingActionsCreateID, remarketingActionsUpdateID}

// newEffectEnvironment is a connection with the effects create and update and no tools list.
func newEffectEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{
		Effects: []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate}})
}

func TestConversionToolsAreConfirmedEffectToolsOutsideTheReadProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{conversionCustomVariablesCreateID: capability.EffectCreate,
		conversionCustomVariablesUpdateID: capability.EffectUpdate, remarketingActionsCreateID: capability.EffectCreate,
		remarketingActionsUpdateID: capability.EffectUpdate}
	for _, id := range conversionChangeToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.RequiresToolAllowList || d.Version != 1 || d.Risk.Effect != wantEffect[id] ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s = %+v", id, d)
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
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "login-customer-id", "partial_failure", "operations",
			"path", "url", "header"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
		if !strings.Contains(d.Description, "outcome is unknown") || !strings.Contains(d.Description, "never repeats") ||
			strings.Contains(d.Description, "tools list") {
			t.Errorf("%s: description = %s", id, d.Description)
		}
	}
	for _, tool := range metadata.Tools {
		if slices.Contains(conversionChangeToolIDs, tool.ID) && (tool.RequiresToolAllowList || tool.Effect == config.EffectRead ||
			tool.Effect != config.Effect(wantEffect[tool.ID])) {
			t.Errorf("tool %+v is not an effect-reachable change tool", tool)
		}
	}
}
