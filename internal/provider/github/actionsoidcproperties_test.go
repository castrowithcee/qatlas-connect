package github

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const oidcPropertyCanary = "oidc-property-text-canary-3c9a"

const oidcPropertiesBase = "/api/v3/orgs/octo-org/actions/oidc/customization/properties/repo"

// fakeOIDCProperties answers the OIDC property routes of octo-org through the failure hook of fakeGitHub.
type fakeOIDCProperties struct {
	mu     sync.Mutex
	status int
}

func (o *fakeOIDCProperties) route(w http.ResponseWriter, req *http.Request) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !strings.HasPrefix(req.URL.EscapedPath(), oidcPropertiesBase) {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if o.status != 0 {
		w.WriteHeader(o.status)
		fmt.Fprintf(w, `{"message":%q}`, oidcPropertyCanary)
		return true
	}
	switch req.Method {
	case http.MethodGet:
		fmt.Fprint(w, `[{"custom_property_name":"team","inclusion_source":"organization"},`+
			`{"custom_property_name":"env","inclusion_source":"enterprise"},{"inclusion_source":"organization"}]`)
	case http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"custom_property_name":"team","inclusion_source":"organization"}`)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
	return true
}

func oidcPropertyCore(t *testing.T, reads *int) (*fakeGitHub, *fakeOIDCProperties, *application.Core) {
	t.Helper()
	o := &fakeOIDCProperties{}
	f := &fakeGitHub{}
	f.failure = o.route
	base := serve(t, f)
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["oidc-listed"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: all, Tools: actionsOIDCPropertyToolIDs}
	cfg.Connections["oidc-unlisted"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: all}
	cfg.Connections["oidc-repo-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/example"}, Effects: all, Tools: actionsOIDCPropertyToolIDs}
	cfg.Connections["oidc-excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: all, Tools: actionsOIDCPropertyToolIDs}
	cfg.Connections["oidc-user"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"users/octocat"}, Effects: all, Tools: actionsOIDCPropertyToolIDs}
	red := &redact.Redactor{}
	return f, o, application.New(registry(t), cfg, resolver(red, reads), red)
}

var oidcPropertyCalls = []struct {
	id, arguments, method, path string
	want                        []string
}{
	{actionsOIDCPropertiesList.ID, `{` + teamOwner + `}`, http.MethodGet, oidcPropertiesBase,
		[]string{`"custom_property_name":"team"`, `"inclusion_source":"enterprise"`, `"owner":"orgs/octo-org"`}},
	{actionsOIDCPropertiesAdd.ID, `{` + teamOwner + `,"custom_property_name":"team"}`, http.MethodPost,
		oidcPropertiesBase, []string{`"custom_property_name":"team"`, `"inclusion_source":"organization"`}},
	{actionsOIDCPropertiesRemove.ID, `{` + teamOwner + `,"custom_property_name":"team"}`, http.MethodDelete,
		oidcPropertiesBase + "/team", []string{`"removed":true`, `"custom_property_name":"team"`}},
}

// Every tool satisfies its output contract and sends exactly one documented request.
func TestOIDCPropertyToolsSatisfyTheirContract(t *testing.T) {
	f, _, core := oidcPropertyCore(t, nil)
	for _, call := range oidcPropertyCalls {
		before := len(f.recorded())
		result, err := invoke(t, core, call.id, "oidc-listed", call.arguments, true)
		if err != nil {
			t.Fatalf("%s = %v", call.id, err)
		}
		for _, want := range call.want {
			if !strings.Contains(string(result), want) {
				t.Errorf("%s = %s, want %s", call.id, result, want)
			}
		}
		requests := f.recorded()[before:]
		if len(requests) != 1 || requests[0].method != call.method || requests[0].path != call.path {
			t.Errorf("%s sent %+v, want one %s %s", call.id, requests, call.method, call.path)
		}
		if call.method == http.MethodPost && requests[0].body["custom_property_name"] != "team" {
			t.Errorf("%s body = %v", call.id, requests[0].body)
		}
	}
}

// The organization and the name are settled before a credential is read or GitHub is contacted.
func TestOIDCPropertyRefusalsBeforeIO(t *testing.T) {
	reads := 0
	f, _, core := oidcPropertyCore(t, &reads)
	for _, tt := range []struct{ name, id, connection, arguments, message string }{
		{"foreign organization", actionsOIDCPropertiesList.ID, "oidc-listed", `{"owner":"orgs/other"}`, "outside the targets"},
		{"foreign organization for add", actionsOIDCPropertiesAdd.ID, "oidc-listed",
			`{"owner":"orgs/other","custom_property_name":"team"}`, "outside the targets"},
		{"foreign organization for remove", actionsOIDCPropertiesRemove.ID, "oidc-listed",
			`{"owner":"orgs/other","custom_property_name":"team"}`, "outside the targets"},
		{"excluded organization", actionsOIDCPropertiesList.ID, "oidc-excluded", `{` + teamOwner + `}`, ""},
		{"excluded organization for add", actionsOIDCPropertiesAdd.ID, "oidc-excluded",
			`{` + teamOwner + `,"custom_property_name":"team"}`, ""},
		{"user owner", actionsOIDCPropertiesList.ID, "oidc-user", `{"owner":"users/octocat"}`, "organization"},
		{"user owner for remove", actionsOIDCPropertiesRemove.ID, "oidc-user",
			`{"owner":"users/octocat","custom_property_name":"team"}`, "organization"},
		{"repository target only", actionsOIDCPropertiesList.ID, "oidc-repo-only", `{}`, ""},
		{"missing name", actionsOIDCPropertiesAdd.ID, "oidc-listed", `{` + teamOwner + `}`, ""},
		{"empty name", actionsOIDCPropertiesAdd.ID, "oidc-listed", `{` + teamOwner + `,"custom_property_name":""}`, ""},
		{"path injection in the name", actionsOIDCPropertiesRemove.ID, "oidc-listed",
			`{` + teamOwner + `,"custom_property_name":"a/../b"}`, ""},
		{"query in the name", actionsOIDCPropertiesRemove.ID, "oidc-listed",
			`{` + teamOwner + `,"custom_property_name":"a?x=1"}`, ""},
		{"encoded slash in the name", actionsOIDCPropertiesRemove.ID, "oidc-listed",
			`{` + teamOwner + `,"custom_property_name":"a%2Fb"}`, ""},
		{"control character in the name", actionsOIDCPropertiesAdd.ID, "oidc-listed",
			`{` + teamOwner + `,"custom_property_name":"a\nb"}`, ""},
		{"too long name", actionsOIDCPropertiesAdd.ID, "oidc-listed",
			`{` + teamOwner + `,"custom_property_name":"` + strings.Repeat("x", 101) + `"}`, ""},
		{"unknown argument", actionsOIDCPropertiesList.ID, "oidc-listed",
			`{` + teamOwner + `,"custom_property_name":"team"}`, ""},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		var unsupported *capability.UnsupportedError
		if err == nil || errors.As(err, &unsupported) || (tt.message != "" && !strings.Contains(err.Error(), tt.message)) {
			t.Errorf("%s: err = %v", tt.name, err)
		}
		if reads != 0 {
			t.Errorf("%s reached the credential (%d)", tt.name, reads)
		}
		if len(f.recorded()) != before {
			t.Errorf("%s reached GitHub", tt.name)
		}
	}
}

// Failures are named without GitHub's text, a change is never repeated, and a lost answer is uncertain.
func TestOIDCPropertyFailuresAreNamed(t *testing.T) {
	f, o, core := oidcPropertyCore(t, nil)
	for _, call := range oidcPropertyCalls {
		change := call.method != http.MethodGet
		for _, tt := range []struct {
			status int
			class  provider.Class
			unsure bool
		}{
			{http.StatusForbidden, provider.ClassPermission, false},
			{http.StatusNotFound, provider.ClassNotFound, false},
			{http.StatusUnprocessableEntity, provider.ClassProviderError, false},
			{http.StatusBadGateway, provider.ClassProviderError, change},
			{http.StatusGatewayTimeout, provider.ClassTimeout, change},
		} {
			o.status = tt.status
			before := len(f.recorded())
			_, err := invoke(t, core, call.id, "oidc-listed", call.arguments, true)
			if err == nil || classOf(err) != tt.class || strings.Contains(err.Error(), uncertain) != tt.unsure ||
				strings.Contains(err.Error(), oidcPropertyCanary) {
				t.Errorf("%s answered %d = %v, want %s without GitHub's text", call.id, tt.status, err, tt.class)
				continue
			}
			if change && len(f.recorded()[before:]) != 1 {
				t.Errorf("%s answered %d sent %d requests, want one without a retry", call.id, tt.status,
					len(f.recorded()[before:]))
			}
			if tt.status == http.StatusForbidden && !strings.Contains(err.Error(), ":org") {
				t.Errorf("%s forbidden = %v, want the token right named", call.id, err)
			}
		}
		o.status = 0
	}
}

// The add and remove tools are offered only where the connection's tools list names them and need
// confirmation; the list tool is offered without a list.
func TestOIDCPropertyChangesAreListedOnlyAndNeedConfirmation(t *testing.T) {
	reads := 0
	f, _, core := oidcPropertyCore(t, &reads)
	var unsupported *capability.UnsupportedError
	for _, call := range oidcPropertyCalls {
		_, err := invoke(t, core, call.id, "oidc-unlisted", call.arguments, true)
		if call.method == http.MethodGet {
			if err != nil {
				t.Errorf("%s without a tools list = %v, want offered", call.id, err)
			}
			continue
		}
		if !errors.As(err, &unsupported) {
			t.Errorf("invoke %s without a tools list = %v, want unsupported", call.id, err)
		}
		if _, err := core.Describe(application.DescribeRequest{Operation: call.id, Connection: "oidc-unlisted"}); !errors.As(err, &unsupported) {
			t.Errorf("describe %s without a tools list = %v, want unsupported", call.id, err)
		}
		unconfirmed := &application.ConfirmationRequiredError{}
		before := len(f.recorded())
		if _, err := invoke(t, core, call.id, "oidc-listed", call.arguments, false); !errors.As(err, &unconfirmed) {
			t.Errorf("invoke %s without confirmation = %v, want confirmation-required", call.id, err)
		}
		if len(f.recorded()) != before {
			t.Errorf("%s without confirmation reached GitHub", call.id)
		}
	}
}
