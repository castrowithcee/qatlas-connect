package googlecloud

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const projectAccounts = "/v1/projects/example-project-123/serviceAccounts"

// writeEnvironment is an environment whose connection may create and update.
func writeEnvironment(t *testing.T, targets []string) *environment {
	t.Helper()
	e := newEnvironment(t, targets)
	e.cfg.Connections["work"] = config.Connection{Service: "cloud", Credential: credential,
		Effects: []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate}, Targets: targets}
	if err := e.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	e.core = application.New(e.reg, e.cfg, e.secrets(), e.red)
	return e
}

func (e *environment) invokeConfirmed(operation, arguments string) (string, error) {
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: operation, Connection: "work", Arguments: json.RawMessage(arguments), Confirmed: true})
	return string(response.Result), err
}

func TestServiceAccountsCreateSendsTheBodyOnceAndHidesKeyFields(t *testing.T) {
	e := writeEnvironment(t, []string{"projects/example-project-123"})
	e.fake.routes[projectAccounts] = okReply(saRow("deploy", "example-project-123"))
	out, err := e.invokeConfirmed(serviceAccountsCreateID,
		`{"project_id":"example-project-123","account_id":"deploy","display_name":"Deploy","description":"CI"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != "POST" || !strings.HasSuffix(api[0].url, projectAccounts) {
		t.Fatalf("requests = %+v", api)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(api[0].body), &body); err != nil {
		t.Fatal(err)
	}
	account, _ := body["serviceAccount"].(map[string]any)
	if body["accountId"] != "deploy" || account["displayName"] != "Deploy" || account["description"] != "CI" || len(body) != 2 {
		t.Errorf("body = %s", api[0].body)
	}
	got := decode(t, out)
	if got["email"] != acctEmail || got["disabled"] != true {
		t.Errorf("result = %s", out)
	}
	for _, leaked := range []string{"privateKeyData", "c2VjcmV0", "etag", "AAA="} {
		if strings.Contains(out, leaked) {
			t.Errorf("result holds %q: %s", leaked, out)
		}
	}
}

func TestServiceAccountsCreateRefusesInputBeforeIO(t *testing.T) {
	long := strings.Repeat("a", 101)
	for name, args := range map[string]string{
		"short id":      `{"project_id":"example-project-123","account_id":"abc"}`,
		"upper id":      `{"project_id":"example-project-123","account_id":"Deploy1"}`,
		"hyphen end":    `{"project_id":"example-project-123","account_id":"deploy-"}`,
		"digit start":   `{"project_id":"example-project-123","account_id":"1deploy"}`,
		"long name":     `{"project_id":"example-project-123","account_id":"deploy","display_name":"` + long + `"}`,
		"control":       `{"project_id":"example-project-123","account_id":"deploy","description":"a\u0007b"}`,
		"other project": `{"project_id":"example-other-9","account_id":"deploy"}`,
		"unknown":       `{"project_id":"example-project-123","account_id":"deploy","keys":true}`,
	} {
		e := writeEnvironment(t, []string{"projects/example-project-123"})
		if _, err := e.invokeConfirmed(serviceAccountsCreateID, args); err == nil {
			t.Errorf("%s: accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestServiceAccountsCreateAlreadyExists(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes[projectAccounts] = reply{status: 409, body: errorObject(409, "ALREADY_EXISTS", "")}
	_, err := e.invokeConfirmed(serviceAccountsCreateID, `{"project_id":"example-project-123","account_id":"deploy"}`)
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "already exists") ||
		strings.Contains(err.Error(), detailCanary) {
		t.Errorf("err = %v", err)
	}
	if len(e.fake.api()) != 1 {
		t.Errorf("requests = %+v", e.fake.api())
	}
}

func TestServiceAccountsCreateRefusesAnAnswerOfAnotherProject(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes[projectAccounts] = okReply(saRow("deploy", "example-other-9"))
	_, err := e.invokeConfirmed(serviceAccountsCreateID, `{"project_id":"example-project-123","account_id":"deploy"}`)
	if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome") {
		t.Errorf("err = %v", err)
	}
}

func TestServiceAccountsPatchSendsTheMaskOfTheSetFields(t *testing.T) {
	for args, want := range map[string]string{
		`"display_name":"New"`:                   "displayName",
		`"description":""`:                       "description",
		`"display_name":"New","description":"x"`: "displayName,description",
	} {
		e := writeEnvironment(t, nil)
		// Google may leave members out of a patch answer.
		e.fake.routes[acctPath+acctEmail] = okReply(`{"name":"projects/example-project-123/serviceAccounts/` + acctEmail +
			`","displayName":"New","privateKeyData":"c2VjcmV0"}`)
		out, err := e.invokeConfirmed(serviceAccountsPatchID,
			`{"project_id":"example-project-123","account":"`+acctEmail+`",`+args+`}`)
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		api := e.fake.api()
		if len(api) != 1 || api[0].method != "PATCH" {
			t.Fatalf("requests = %+v", api)
		}
		var body struct {
			UpdateMask string `json:"updateMask"`
		}
		_ = json.Unmarshal([]byte(api[0].body), &body)
		if body.UpdateMask != want {
			t.Errorf("%s: mask = %q, want %q (%s)", args, body.UpdateMask, want, api[0].body)
		}
		if got := decode(t, out); got["projectId"] != "example-project-123" || strings.Contains(out, "c2VjcmV0") {
			t.Errorf("result = %s", out)
		}
	}
}

func TestServiceAccountsPatchWithoutAFieldIsRefusedBeforeIO(t *testing.T) {
	e := writeEnvironment(t, nil)
	if _, err := e.invokeConfirmed(serviceAccountsPatchID,
		`{"project_id":"example-project-123","account":"`+acctEmail+`"}`); err == nil {
		t.Fatal("a patch without a field was accepted")
	}
	noIO(t, e, "empty patch")
}

func TestServiceAccountsDisableAndEnableAreRepeatable(t *testing.T) {
	for _, c := range []struct {
		id, suffix string
		disabled   bool
	}{{serviceAccountsDisableID, ":disable", true}, {serviceAccountsEnableID, ":enable", false}} {
		e := writeEnvironment(t, nil)
		e.fake.routes[acctPath+acctEmail+c.suffix] = okReply(`{}`)
		for i := 0; i < 2; i++ {
			out, err := e.invokeConfirmed(c.id, `{"project_id":"example-project-123","account":"`+acctEmail+`"}`)
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			if got["account"] != acctEmail || (got["disabled"] == true) != c.disabled {
				t.Errorf("%s result = %s", c.id, out)
			}
		}
		api := e.fake.api()
		if len(api) != 2 || api[0].method != "POST" || api[0].body != "{}" {
			t.Errorf("%s requests = %+v", c.id, api)
		}
	}
}

func TestServiceAccountChangesOfAForeignAccountAreRefusedBeforeIO(t *testing.T) {
	foreign := `"account":"deploy@example-other-9.iam.gserviceaccount.com"`
	for id, args := range map[string]string{
		serviceAccountsPatchID:   `{"project_id":"example-project-123",` + foreign + `,"description":"x"}`,
		serviceAccountsDisableID: `{"project_id":"example-project-123",` + foreign + `}`,
		serviceAccountsEnableID:  `{"project_id":"example-project-123",` + foreign + `}`,
	} {
		e := writeEnvironment(t, nil)
		if _, err := e.invokeConfirmed(id, args); err == nil {
			t.Errorf("%s: accepted", id)
		}
		noIO(t, e, id)
		e = writeEnvironment(t, []string{"projects/example-project-123"})
		if _, err := e.invokeConfirmed(id, strings.Replace(args, "example-project-123", "example-other-9", 1)); err == nil {
			t.Errorf("%s: project outside the targets accepted", id)
		}
		noIO(t, e, id+" outside")
	}
}

func TestServiceAccountChangesNeedConfirmation(t *testing.T) {
	for id, args := range map[string]string{
		serviceAccountsCreateID:  `{"project_id":"example-project-123","account_id":"deploy"}`,
		serviceAccountsPatchID:   `{"project_id":"example-project-123","account":"1034","description":"x"}`,
		serviceAccountsDisableID: `{"project_id":"example-project-123","account":"1034"}`,
		serviceAccountsEnableID:  `{"project_id":"example-project-123","account":"1034"}`,
	} {
		e := writeEnvironment(t, nil)
		if _, err := e.invoke(id, args); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		if d.Risk.Confirmation != "required" || !d.Risk.OpenWorld {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if e.reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
}

func TestServiceAccountSummariesNameProjectAccountAndChangeAndCutText(t *testing.T) {
	e := writeEnvironment(t, nil)
	long := strings.Repeat("x", 500)
	patch := e.reg.ConfirmationSummary(serviceAccountsPatchID)(json.RawMessage(
		`{"project_id":"example-project-123","account":"` + acctEmail + `","display_name":"` + long + `"}`))
	if !strings.Contains(patch, "example-project-123") || !strings.Contains(patch, acctEmail) ||
		!strings.Contains(patch, "display name") || len(patch) > 400 {
		t.Errorf("patch summary = %q", patch)
	}
	create := e.reg.ConfirmationSummary(serviceAccountsCreateID)(json.RawMessage(
		`{"project_id":"example-project-123","account_id":"deploy","description":"CI"}`))
	if !strings.Contains(create, "deploy") || !strings.Contains(create, "example-project-123") ||
		!strings.Contains(create, "without roles or keys") {
		t.Errorf("create summary = %q", create)
	}
	for id, word := range map[string]string{serviceAccountsDisableID: "Disable", serviceAccountsEnableID: "Enable"} {
		s := e.reg.ConfirmationSummary(id)(json.RawMessage(`{"project_id":"example-project-123","account":"1034"}`))
		if !strings.HasPrefix(s, word) || !strings.Contains(s, "1034") || !strings.Contains(s, "example-project-123") {
			t.Errorf("%s summary = %q", id, s)
		}
	}
}

func TestServiceAccountChangeTextIsCutInTheResult(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes[acctPath+acctEmail] = okReply(`{"name":"projects/example-project-123/serviceAccounts/` + acctEmail +
		`","projectId":"example-project-123","displayName":"` + strings.Repeat("d", 900) + `","description":"a\u0007b"}`)
	out, err := e.invokeConfirmed(serviceAccountsPatchID,
		`{"project_id":"example-project-123","account":"`+acctEmail+`","description":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	if name, _ := got["displayName"].(string); len(name) > maxNameText || strings.Contains(out, `\u0007`) {
		t.Errorf("result = %s", out)
	}
}

func TestServiceAccountChangeFailureNamesAnUnknownOutcome(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes[acctPath+acctEmail+":disable"] = reply{status: 500, body: `{}`}
	_, err := e.invokeConfirmed(serviceAccountsDisableID, `{"project_id":"example-project-123","account":"`+acctEmail+`"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome") || len(e.fake.api()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
}

func TestServiceAccountWireTypeHasNoKeyFields(t *testing.T) {
	var row serviceAccountRow
	if err := json.Unmarshal([]byte(saRow("deploy", "example-project-123")), &row); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(row.result())
	for _, leaked := range []string{"privateKey", "c2VjcmV0", "etag"} {
		if strings.Contains(string(out), leaked) {
			t.Errorf("result holds %q: %s", leaked, out)
		}
	}
}

func TestServiceAccountChangeToolsNeedTheBroadScopeAndTheirOwnProfile(t *testing.T) {
	for _, id := range []string{serviceAccountsCreateID, serviceAccountsPatchID, serviceAccountsDisableID, serviceAccountsEnableID} {
		if scopes := tokenMetadata.Scopes[id]; len(scopes) != 1 || scopes[0] != scopeCloudPlatform {
			t.Errorf("%s scopes = %v", id, scopes)
		}
		for _, tool := range readTools {
			if tool == id {
				t.Errorf("%s is in the read profile", id)
			}
		}
	}
}
