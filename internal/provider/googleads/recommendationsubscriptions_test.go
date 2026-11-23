package googleads

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func subscriptionsPath(id string) string {
	return "/v26/customers/" + id + "/recommendationSubscriptions:mutate"
}

func subscriptionResourceName(kind string) string {
	return "customers/" + queryNumber(clientID) + "/recommendationSubscriptions/" + kind
}

const subscriptionEntries = `"recommendation_subscriptions":[{"type":"KEYWORD","status":"ENABLED"},` +
	`{"type":"CAMPAIGN_BUDGET","status":"PAUSED"}]`

func subscriptionCalls() map[string]string {
	call := `{"customer_id":"` + clientID + `",` + subscriptionEntries + `}`
	return map[string]string{recommendationSubscriptionsCreateID: call, recommendationSubscriptionsUpdateID: call}
}

func TestRecommendationSubscriptionToolsAreListedOnlyAndNameTheOngoingEffect(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for id := range subscriptionCalls() {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Version != 1 || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Fatalf("%s = %+v", id, d)
		}
		for _, word := range []string{"automatically", "from then on", "spending", "without a further call", "atomic",
			"never repeats", "outcome is unknown", "per entry", "never deleted"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", id, word)
			}
		}
		for _, forbidden := range []string{"partial_failure", "operations", "login_customer_id"} {
			if strings.Contains(string(d.InputSchema), `"`+forbidden+`"`) {
				t.Errorf("%s offers %s", id, forbidden)
			}
		}
		if !strings.Contains(string(d.InputSchema), `"validate_only"`) || !strings.Contains(string(d.InputSchema), `"maxItems":10`) {
			t.Errorf("%s: input schema %s", id, d.InputSchema)
		}
		if strings.Contains(string(d.InputSchema), "UNSPECIFIED") || strings.Contains(string(d.InputSchema), "UNKNOWN") {
			t.Errorf("%s offers a placeholder value", id)
		}
		if _, ok := metadata.Token.Scopes[id]; !ok || slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("%s: scopes or profile", id)
		}
	}
}

func TestRecommendationSubscriptionsNeedTheToolsListAndTheConfirmation(t *testing.T) {
	for id, arguments := range subscriptionCalls() {
		t.Run(id+" with every effect but without the tools list", func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
				config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute}})
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a connection without the tools list ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" without confirmation", func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			_, err := e.invoke(id, arguments)
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) ||
				!strings.Contains(needed.Summary, "automatically") {
				t.Fatalf("err = %v, want a confirmation that names the account and the automatic effect", err)
			}
			e.untouched(t)
		})
		t.Run(id+" target is checked before secret and IO", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + otherID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), clientID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestRecommendationSubscriptionArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	entry := func(kind string) string { return `{"type":"` + kind + `","status":"ENABLED"}` }
	var eleven []string
	for _, kind := range recommendationSubscriptionTypes[:11] {
		eleven = append(eleven, entry(kind))
	}
	bad := map[string]string{
		`"recommendation_subscriptions":[]`:                                                          "empty list",
		`"recommendation_subscriptions":[` + strings.Join(eleven, ",") + `]`:                         "too many",
		`"recommendation_subscriptions":[{"type":"KEYWORD"}]`:                                        "no status",
		`"recommendation_subscriptions":[{"status":"ENABLED"}]`:                                      "no type",
		`"recommendation_subscriptions":[{"type":"UNSPECIFIED","status":"ENABLED"}]`:                 "placeholder type",
		`"recommendation_subscriptions":[{"type":"KEYWORD","status":"REMOVED"}]`:                     "removed status",
		`"recommendation_subscriptions":[{"type":"keyword","status":"ENABLED"}]`:                     "lower case type",
		`"recommendation_subscriptions":[` + entry("KEYWORD") + `,` + entry("KEYWORD") + `]`:         "type twice",
		`"recommendation_subscriptions":[{"type":"KEYWORD","status":"ENABLED","resource_name":"x"}]`: "resource name",
		subscriptionEntries + `,"partial_failure":true`:                                              "partial failure",
		subscriptionEntries + `,"login_customer_id":"` + loginID + `"`:                               "login customer",
		subscriptionEntries + `,"response_content_type":"ALL"`:                                       "content type",
	}
	for id := range subscriptionCalls() {
		for tail, name := range bad {
			t.Run(id+" "+name, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				if _, err := e.invokeConfirmed(id, `{"customer_id":"`+clientID+`",`+tail+`}`); err == nil {
					t.Fatal("accepted")
				}
				e.untouched(t)
			})
		}
	}
}

func TestRecommendationSubscriptionBodiesAreOneAtomicRequestWithoutPartialFailure(t *testing.T) {
	cases := map[string]string{
		recommendationSubscriptionsCreateID: `[{"create":{"type":"KEYWORD","status":"ENABLED"}},` +
			`{"create":{"type":"CAMPAIGN_BUDGET","status":"PAUSED"}}]`,
		recommendationSubscriptionsUpdateID: `[{"update":{"resourceName":"` + subscriptionResourceName("KEYWORD") +
			`","status":"ENABLED"},"updateMask":"status"},{"update":{"resourceName":"` +
			subscriptionResourceName("CAMPAIGN_BUDGET") + `","status":"PAUSED"},"updateMask":"status"}]`,
	}
	answer := `{"results":[{"resourceName":"` + subscriptionResourceName("KEYWORD") + `","recommendationSubscription":` +
		`{"resourceName":"` + subscriptionResourceName("KEYWORD") + `","type":"KEYWORD","status":"ENABLED",` +
		`"createDateTime":"2026-11-01 10:00:00","modifyDateTime":"2026-11-02 10:00:00.000000"}},` +
		`{"resourceName":"` + subscriptionResourceName("CAMPAIGN_BUDGET") + `"}]}`
	for id, operations := range cases {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+subscriptionsPath(clientID)] = okReply(answer)
			arguments := strings.TrimSuffix(subscriptionCalls()[id], "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
			out, err := e.invokeConfirmed(id, arguments)
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			results, _ := got["results"].([]any)
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 {
				t.Fatalf("out = %s", out)
			}
			first := results[0].(map[string]any)
			sub, _ := first["recommendation_subscription"].(map[string]any)
			if first["type"] != "KEYWORD" || first["resource_name"] != subscriptionResourceName("KEYWORD") ||
				sub["status"] != "ENABLED" || sub["create_date_time"] != "2026-11-01 10:00:00" ||
				sub["modify_date_time"] != "2026-11-02 10:00:00.000000" ||
				results[1].(map[string]any)["recommendation_subscription"] != nil {
				t.Errorf("results = %v", results)
			}
			posts := e.posts()
			if len(posts) != 1 || posts[0].auth != "Bearer synthetic-access-1" || posts[0].login != loginID ||
				posts[0].developerToken != "" {
				t.Fatalf("posts = %+v", posts)
			}
			body := bodyOf(t, posts[0])
			if len(body) != 2 || body["responseContentType"] != "MUTABLE_RESOURCE" ||
				!reflect.DeepEqual(body["operations"], wantWire(t, operations)) || strings.Contains(posts[0].body, "partialFailure") {
				t.Errorf("body = %s", posts[0].body)
			}
		})
	}
}

func TestRecommendationSubscriptionValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range subscriptionCalls() {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+subscriptionsPath(clientID)] = okReply(`{}`)
		out, err := e.invokeConfirmed(id, strings.TrimSuffix(arguments, "}")+`,"validate_only":true}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		if results, _ := got["results"].([]any); got["validate_only"] != true || len(results) != 0 {
			t.Errorf("out = %s", out)
		}
		if posts := e.posts(); len(posts) != 1 || bodyOf(t, posts[0])["validateOnly"] != true {
			t.Errorf("posts = %+v", posts)
		}
	}
}

// An answer that is not exactly the one asked for never passes: the request was sent, so the outcome is open.
func TestRecommendationSubscriptionAnswersAreBoundToTheRequest(t *testing.T) {
	good := `{"resourceName":"` + subscriptionResourceName("KEYWORD") + `"}`
	second := `{"resourceName":"` + subscriptionResourceName("CAMPAIGN_BUDGET") + `"}`
	foreign := "customers/" + queryNumber(otherID) + "/recommendationSubscriptions/KEYWORD"
	cases := map[string]string{
		"foreign account":  `{"results":[{"resourceName":"` + foreign + `"},` + second + `]}`,
		"other type":       `{"results":[` + good + `,{"resourceName":"` + subscriptionResourceName("KEYWORD_MATCH_TYPE") + `"}]}`,
		"swapped":          `{"results":[` + second + `,` + good + `]}`,
		"placeholder type": `{"results":[` + good + `,{"resourceName":"` + subscriptionResourceName("UNKNOWN") + `"}]}`,
		"too few":          `{"results":[` + good + `]}`,
		"no name":          `{"results":[{},{}]}`,
		"partial failure":  `{"results":[` + good + `,` + second + `],"partialFailureError":{"code":3}}`,
		"other subscription inside": `{"results":[{"resourceName":"` + subscriptionResourceName("KEYWORD") +
			`","recommendationSubscription":{"resourceName":"` + subscriptionResourceName("CAMPAIGN_BUDGET") + `"}},` + second + `]}`,
		"other type inside": `{"results":[{"resourceName":"` + subscriptionResourceName("KEYWORD") +
			`","recommendationSubscription":{"resourceName":"` + subscriptionResourceName("KEYWORD") + `","type":"TEXT_AD"}},` + second + `]}`,
	}
	for id, arguments := range subscriptionCalls() {
		for name, body := range cases {
			t.Run(id+" "+name, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+subscriptionsPath(clientID)] = okReply(body)
				_, err := e.invokeConfirmed(id, arguments)
				if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
					len(e.posts()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestRecommendationSubscriptionFailuresMapWithoutProviderText(t *testing.T) {
	cases := map[string]struct {
		reply   reply
		class   provider.Class
		unknown bool
	}{
		"validation": {reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "recommendationSubscriptionError",
			"UNKNOWN", "")}, provider.ClassProviderError, false},
		"permission": {reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, false},
		"quota": {reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"rateScope":"ACCOUNT","retryDelay":"3s"}`)}, provider.ClassRateLimited, false},
		"server":     {reply{status: 503, body: googleError(503, "UNAVAILABLE", "")}, provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, true},
		"cut socket": {reply{err: errors.New("connection reset")}, "", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+subscriptionsPath(clientID)] = c.reply
			_, err := e.invokeConfirmed(recommendationSubscriptionsCreateID, subscriptionCalls()[recommendationSubscriptionsCreateID])
			if err == nil || (c.class != "" && classOf(err) != c.class) || strings.Contains(err.Error(), detailCanary) ||
				strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != c.unknown || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}
