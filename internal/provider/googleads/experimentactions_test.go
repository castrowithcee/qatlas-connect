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

var experimentActionIDs = []string{experimentsScheduleID, experimentsEndID, experimentsGraduateID, experimentsPromoteID}

var experimentActionMethods = map[string]string{experimentsScheduleID: "scheduleExperiment",
	experimentsEndID: "endExperiment", experimentsGraduateID: "graduateExperiment", experimentsPromoteID: "promoteExperiment"}

func experimentActionRoute(id string) string {
	return "POST /v26/" + experimentsResource(clientID, "111") + ":" + experimentActionMethods[id]
}

func newExperimentActionsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: experimentActionIDs})
}

const experimentOperationAnswer = `{"name":"customers/` + clientID + `/operations/abc","done":false,"metadata":"x"}`

var experimentActionCalls = map[string]string{
	experimentsScheduleID: `{"customer_id":"` + clientID + `","experiment_id":"111"}`,
	experimentsEndID:      `{"customer_id":"` + clientID + `","experiment_id":"111"}`,
	experimentsGraduateID: `{"customer_id":"` + clientID + `","experiment_id":"111","campaign_budget_mappings":` +
		`[{"experiment_campaign_id":"2001","campaign_budget_id":"3001"}]}`,
	experimentsPromoteID: `{"customer_id":"` + clientID + `","experiment_id":"111"}`,
}

var experimentActionAnswers = map[string]string{experimentsScheduleID: experimentOperationAnswer,
	experimentsEndID: `{}`, experimentsGraduateID: `{}`, experimentsPromoteID: experimentOperationAnswer}

func withValidateOnly(call string) string {
	return strings.TrimSuffix(call, "}") + `,"validate_only":true}`
}

func experimentActionPosts(e *environment) []seen {
	var out []seen
	for _, s := range e.fake.api() {
		if s.method == http.MethodPost && !strings.HasSuffix(s.path, ":search") {
			out = append(out, s)
		}
	}
	return out
}

func TestExperimentActionsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range experimentActionIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != capability.EffectUpdate || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld || !d.RequiresToolAllowList ||
			d.Risk.Idempotency != capability.IdempotencyNonIdempotent || !strings.HasSuffix(d.Description, mutateNote) {
			t.Fatalf("%s = %+v", id, d)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) || !reflect.DeepEqual(metadata.Token.Scopes[id], []string{scopeAdwords}) {
			t.Errorf("%s: profile or scope wrong", id)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "resource_name", "experiment", "path", "url", "operations"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
	}
	graduate, _, _ := reg.Lookup(experimentsGraduateID)
	if !strings.Contains(string(graduate.InputSchema), `"campaign_budget_mappings"`) ||
		!strings.Contains(string(graduate.InputSchema), `"maxItems":1`) {
		t.Errorf("graduate schema = %s", graduate.InputSchema)
	}
	for _, id := range []string{experimentsScheduleID, experimentsPromoteID} {
		if d, _, _ := reg.Lookup(id); !strings.Contains(string(d.OutputSchema), `"operation"`) {
			t.Errorf("%s has no operation in its output", id)
		}
	}
}

func TestExperimentActionsAreReachedOnlyByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute}
	for _, id := range experimentActionIDs {
		for name, conn := range map[string]config.Connection{"all effects": {Effects: all}, "listed": {Tools: []string{id}}} {
			t.Run(name+" "+id, func(t *testing.T) {
				e := newEnvironmentWith(t, nil, nil, conn)
				e.fake.routes[experimentActionRoute(id)] = okReply(experimentActionAnswers[id])
				_, err := e.invokeConfirmed(id, experimentActionCalls[id])
				if (err == nil) != (name == "listed") {
					t.Fatalf("err = %v", err)
				}
				if err != nil {
					e.untouched(t)
				}
			})
		}
	}
}

func TestExperimentActionsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range experimentActionIDs {
		t.Run(id, func(t *testing.T) {
			e := newExperimentActionsEnvironment(t, nil, nil)
			_, err := e.invoke(id, experimentActionCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) || !strings.Contains(needed.Summary, "111") {
				t.Fatalf("err = %v, want a confirmation that names the account and the experiment", err)
			}
			e.untouched(t)
		})
	}
}

func TestExperimentActionsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"resource_name":"customers/` + otherID + `/experiments/1"`,
		`"experiment":"customers/` + otherID + `/experiments/1"`, `"path":"/x"`} {
		for _, id := range experimentActionIDs {
			e := newExperimentActionsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, strings.TrimSuffix(experimentActionCalls[id], "}")+","+extra+"}"); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestExperimentActionsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for id, call := range experimentActionCalls {
		t.Run(id, func(t *testing.T) {
			e := newExperimentActionsEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invokeConfirmed(id, strings.ReplaceAll(call, clientID, otherID))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestExperimentActionsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	mapping := func(m string) string {
		return head + `"experiment_id":"111","campaign_budget_mappings":` + m + `}`
	}
	bad := map[string][2]string{}
	for _, id := range experimentActionIDs {
		bad["no experiment "+id] = [2]string{id, strings.Replace(experimentActionCalls[id], `"experiment_id":"111"`, `"x":1`, 1)}
		for name, v := range map[string]string{"zero": "0", "leading zero": "011", "negative": "-1", "name": "customers/" + clientID + "/experiments/1",
			"overflow": "99999999999999999999", "injected": "111:endExperiment", "number": "1.5"} {
			bad[name+" experiment "+id] = [2]string{id, strings.Replace(experimentActionCalls[id], `"experiment_id":"111"`, `"experiment_id":"`+v+`"`, 1)}
		}
		bad["bad customer "+id] = [2]string{id, strings.Replace(experimentActionCalls[id], clientID, "12-34", 1)}
	}
	for name, m := range map[string]string{
		"none": `[]`, "missing": `null`, "two": `[{"experiment_campaign_id":"1","campaign_budget_id":"2"},{"experiment_campaign_id":"3","campaign_budget_id":"4"}]`,
		"no budget":        `[{"experiment_campaign_id":"1"}]`,
		"no campaign":      `[{"campaign_budget_id":"1"}]`,
		"foreign campaign": `[{"experiment_campaign_id":"customers/` + otherID + `/campaigns/1","campaign_budget_id":"2"}]`,
		"foreign budget":   `[{"experiment_campaign_id":"1","campaign_budget_id":"customers/` + otherID + `/campaignBudgets/2"}]`,
		"own budget name":  `[{"experiment_campaign_id":"1","campaign_budget_id":"customers/` + clientID + `/campaignBudgets/2"}]`,
		"zero budget":      `[{"experiment_campaign_id":"1","campaign_budget_id":"0"}]`,
		"extra field":      `[{"experiment_campaign_id":"1","campaign_budget_id":"2","x":"y"}]`,
		"object":           `{"experiment_campaign_id":"1","campaign_budget_id":"2"}`,
	} {
		bad["graduate "+name] = [2]string{experimentsGraduateID, mapping(m)}
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newExperimentActionsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestExperimentActionsSendOneRequestBoundToTheAccount(t *testing.T) {
	for _, id := range experimentActionIDs {
		t.Run(id, func(t *testing.T) {
			e := newExperimentActionsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes[experimentActionRoute(id)] = okReply(experimentActionAnswers[id])
			out, err := e.invokeConfirmed(id, experimentActionCalls[id])
			if err != nil {
				t.Fatal(err)
			}
			posts := experimentActionPosts(e)
			if len(posts) != 1 || len(e.fake.api()) != 1 || posts[0].login != loginID {
				t.Fatalf("requests = %+v", e.fake.api())
			}
			want := map[string]any{}
			if id == experimentsGraduateID {
				want["campaignBudgetMappings"] = []any{map[string]any{
					"experimentCampaign": "customers/" + clientID + "/campaigns/2001",
					"campaignBudget":     "customers/" + clientID + "/campaignBudgets/3001"}}
			}
			if body := bodyOf(t, posts[0]); !reflect.DeepEqual(body, want) {
				t.Errorf("body = %s", posts[0].body)
			}
			got := decode(t, out)
			if got["customer_id"] != clientID || got["experiment_id"] != "111" || got["validate_only"] != false {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestExperimentActionsReturnTheOperationOrAnEmptyResult(t *testing.T) {
	for _, id := range []string{experimentsScheduleID, experimentsPromoteID} {
		t.Run(id, func(t *testing.T) {
			e := newExperimentActionsEnvironment(t, nil, nil)
			e.fake.routes[experimentActionRoute(id)] = okReply(experimentOperationAnswer)
			out, err := e.invokeConfirmed(id, experimentActionCalls[id])
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			if got["operation"] != "customers/"+clientID+"/operations/abc" || got["done"] != false || got["error"] != nil {
				t.Errorf("out = %s", out)
			}
			e = newExperimentActionsEnvironment(t, nil, nil)
			e.fake.routes[experimentActionRoute(id)] = okReply(`{"name":"` + strings.Repeat("o", 5000) +
				`\u0007x","done":true,"error":{"code":9,"message":"` + strings.Repeat("m", 5000) +
				`","details":[{"note":"` + detailCanary + `"}]}}`)
			out, err = e.invokeConfirmed(id, experimentActionCalls[id])
			if err != nil {
				t.Fatal(err)
			}
			got = decode(t, out)
			failed := got["error"].(map[string]any)
			if len(got["operation"].(string)) > maxText || len(failed["message"].(string)) > maxText ||
				failed["code"] != float64(9) || got["done"] != true || strings.Contains(out, detailCanary) ||
				strings.ContainsRune(out, 7) {
				t.Errorf("out = %.300s", out)
			}
		})
	}
	for _, id := range []string{experimentsEndID, experimentsGraduateID} {
		t.Run(id, func(t *testing.T) {
			e := newExperimentActionsEnvironment(t, nil, nil)
			e.fake.routes[experimentActionRoute(id)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, experimentActionCalls[id])
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(t, out); got["operation"] != nil || got["done"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestExperimentActionsValidateOnly(t *testing.T) {
	for _, id := range experimentActionIDs {
		t.Run(id, func(t *testing.T) {
			e := newExperimentActionsEnvironment(t, nil, nil)
			e.fake.routes[experimentActionRoute(id)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, withValidateOnly(experimentActionCalls[id]))
			if err != nil {
				t.Fatal(err)
			}
			if body := bodyOf(t, experimentActionPosts(e)[0]); body["validateOnly"] != true || decode(t, out)["validate_only"] != true {
				t.Errorf("body = %s, out = %s", experimentActionPosts(e)[0].body, out)
			}
		})
	}
	for id, summary := range map[string]capability.SummaryFunc{experimentsScheduleID: experimentsScheduleSummary,
		experimentsEndID: experimentsEndSummary, experimentsGraduateID: experimentsGraduateSummary,
		experimentsPromoteID: experimentsPromoteSummary} {
		if text := summary(json.RawMessage(withValidateOnly(experimentActionCalls[id]))); !strings.Contains(text, "Only validate") {
			t.Errorf("%s: summary %q", id, text)
		}
		if text := summary(json.RawMessage(experimentActionCalls[id])); strings.Contains(text, "validate") ||
			!strings.Contains(text, clientID) || !strings.Contains(text, "111") {
			t.Errorf("%s: summary %q", id, text)
		}
	}
	if text := experimentsGraduateSummary(json.RawMessage(experimentActionCalls[experimentsGraduateID])); !strings.Contains(text, "2001") ||
		!strings.Contains(text, "3001") {
		t.Errorf("graduate summary %q", text)
	}
}

func TestExperimentActionsManagerAccountsNeedAManagerTarget(t *testing.T) {
	call := `{"customer_id":"` + managerID + `","experiment_id":"111"}`
	route := "POST /v26/" + experimentsResource(managerID, "111") + ":endExperiment"
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newExperimentActionsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes[route] = okReply(`{}`)
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(experimentsEndID, call)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(experimentActionPosts(e)) != 0 {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newExperimentActionsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes[route] = okReply(`{}`)
		if _, err := e.invokeConfirmed(experimentsEndID, call); err != nil {
			t.Fatal(err)
		}
	})
}

func TestExperimentActionsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"experiment error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "experimentError", "CANNOT_SCHEDULE", "")},
			provider.ClassProviderError, "rejected the change (experimentError CANNOT_SCHEDULE), nothing was changed", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"answer is not json", reply{status: 200, body: `<html>`}, provider.ClassInvalidResponse,
			"outcome of the change is unknown", true},
	}
	for _, c := range cases {
		for _, id := range experimentActionIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newExperimentActionsEnvironment(t, nil, nil)
				e.fake.routes[experimentActionRoute(id)] = c.reply
				_, err := e.invokeConfirmed(id, experimentActionCalls[id])
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
				if len(experimentActionPosts(e)) != 1 || len(e.fake.api()) != 1 {
					t.Errorf("requests = %d, want one and no repeat", len(e.fake.api()))
				}
			})
		}
	}
}
