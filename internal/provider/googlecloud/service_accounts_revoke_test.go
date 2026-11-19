package googlecloud

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	testKeyID  = "0123456789abcdef0123456789abcdef01234567"
	keyURLPath = acctPath + acctEmail + "/keys/" + testKeyID
)

// revokeEnvironment is an environment whose connection may read, update, and delete; tools is its tools list.
func revokeEnvironment(t *testing.T, targets, tools []string) *environment {
	t.Helper()
	e := newEnvironment(t, targets)
	connection := config.Connection{Service: "cloud", Credential: credential, Targets: targets, Tools: tools}
	if tools == nil {
		// A connection offers by effect or by tools list, never both.
		connection.Effects = []config.Effect{config.EffectRead, config.EffectUpdate, config.EffectDelete}
	}
	e.cfg.Connections["work"] = connection
	if err := e.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	e.core = application.New(e.reg, e.cfg, e.secrets(), e.red)
	return e
}

const keyArgs = `{"project_id":"example-project-123","account":"` + acctEmail + `","key_id":"` + testKeyID + `"}`

func TestKeyDisableAndEnableAreRepeatable(t *testing.T) {
	for _, c := range []struct {
		id, suffix string
		disabled   bool
	}{{serviceAccountKeysDisableID, ":disable", true}, {serviceAccountKeysEnableID, ":enable", false}} {
		e := revokeEnvironment(t, nil, nil)
		e.fake.routes[keyURLPath+c.suffix] = okReply(`{}`)
		for i := 0; i < 2; i++ {
			out, err := e.invokeConfirmed(c.id, keyArgs)
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			if got["keyId"] != testKeyID || got["account"] != acctEmail || (got["disabled"] == true) != c.disabled {
				t.Errorf("%s result = %s", c.id, out)
			}
		}
		api := e.fake.api()
		if len(api) != 2 || api[0].method != "POST" || api[0].body != "{}" {
			t.Errorf("%s requests = %+v", c.id, api)
		}
	}
}

func TestKeyDeleteSendsOneDeleteWithoutBody(t *testing.T) {
	e := revokeEnvironment(t, nil, []string{serviceAccountKeysDeleteID})
	e.fake.routes[keyURLPath] = okReply(`{}`)
	out, err := e.invokeConfirmed(serviceAccountKeysDeleteID, keyArgs)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != "DELETE" || api[0].body != "" || !strings.HasSuffix(api[0].url, "/keys/"+testKeyID) {
		t.Fatalf("requests = %+v", api)
	}
	if got := decode(t, out); got["deleted"] != true || got["keyId"] != testKeyID {
		t.Errorf("result = %s", out)
	}
}

func TestKeyDeleteOfASystemManagedKeyIsAnErrorOfGoogle(t *testing.T) {
	e := revokeEnvironment(t, nil, []string{serviceAccountKeysDeleteID})
	e.fake.routes[keyURLPath] = reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")}
	_, err := e.invokeConfirmed(serviceAccountKeysDeleteID, keyArgs)
	if err == nil || classOf(err) == "" || strings.Contains(err.Error(), detailCanary) || len(e.fake.api()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
}

func TestKeyChangesOfAnUnknownKeyAreNotFound(t *testing.T) {
	for _, id := range []string{serviceAccountKeysDisableID, serviceAccountKeysEnableID, serviceAccountKeysDeleteID} {
		e := revokeEnvironment(t, nil, []string{serviceAccountKeysDisableID, serviceAccountKeysEnableID, serviceAccountKeysDeleteID})
		_, err := e.invokeConfirmed(id, keyArgs)
		if classOf(err) != provider.ClassNotFound || len(e.fake.api()) != 1 {
			t.Errorf("%s: class = %q, err = %v", id, classOf(err), err)
		}
	}
}

func TestRevokeToolsRefuseBadInputBeforeIO(t *testing.T) {
	foreign := `"account":"deploy@example-other-9.iam.gserviceaccount.com"`
	for _, id := range []string{serviceAccountKeysDisableID, serviceAccountKeysEnableID, serviceAccountKeysDeleteID} {
		for name, args := range map[string]string{
			"foreign account": `{"project_id":"example-project-123",` + foreign + `,"key_id":"` + testKeyID + `"}`,
			"path key":        `{"project_id":"example-project-123","account":"1034","key_id":"../x"}`,
			"no key":          `{"project_id":"example-project-123","account":"1034"}`,
			"extra":           `{"project_id":"example-project-123","account":"1034","key_id":"k1","reason":"x"}`,
			"other project":   `{"project_id":"example-other-9","account":"1034","key_id":"k1"}`,
		} {
			e := revokeEnvironment(t, []string{"projects/example-project-123"}, []string{serviceAccountKeysDisableID, serviceAccountKeysEnableID, serviceAccountKeysDeleteID})
			if _, err := e.invokeConfirmed(id, args); err == nil {
				t.Errorf("%s %s: accepted", id, name)
			}
			noIO(t, e, id+" "+name)
		}
	}
	for name, args := range map[string]string{
		"foreign account": `{"project_id":"example-project-123",` + foreign + `}`,
		"other project":   `{"project_id":"example-other-9","account":"1034"}`,
	} {
		e := revokeEnvironment(t, []string{"projects/example-project-123"}, []string{serviceAccountsDeleteID})
		if _, err := e.invokeConfirmed(serviceAccountsDeleteID, args); err == nil {
			t.Errorf("delete %s: accepted", name)
		}
		noIO(t, e, "delete "+name)
	}
}

func TestRevokeToolsNeedConfirmation(t *testing.T) {
	for id, args := range map[string]string{
		serviceAccountKeysDisableID: keyArgs, serviceAccountKeysEnableID: keyArgs, serviceAccountKeysDeleteID: keyArgs,
		serviceAccountsDeleteID: `{"project_id":"example-project-123","account":"1034"}`,
	} {
		e := revokeEnvironment(t, nil, []string{id})
		if _, err := e.invoke(id, args); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		if d.Risk.Confirmation != "required" || !d.Risk.OpenWorld {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		wantEffect := "update"
		if strings.HasSuffix(id, ".delete") {
			wantEffect = "delete"
		}
		if string(d.Risk.Effect) != wantEffect || d.RequiresToolAllowList != (id == serviceAccountsDeleteID || id == serviceAccountKeysDeleteID) {
			t.Errorf("%s effect = %s, listed-only = %v", id, d.Risk.Effect, d.RequiresToolAllowList)
		}
		if e.reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
}

func TestRevokeSummariesNameProjectAccountAndKey(t *testing.T) {
	e := newEnvironment(t, nil)
	for id, want := range map[string][]string{
		serviceAccountKeysDisableID: {"Disable", "again"},
		serviceAccountKeysEnableID:  {"Enable"},
		serviceAccountKeysDeleteID:  {"Delete", "cannot be undone"},
		serviceAccountsDeleteID:     {"Delete", "fail afterwards", "role bindings"},
	} {
		s := e.reg.ConfirmationSummary(id)(json.RawMessage(keyArgs))
		for _, part := range append(want, "example-project-123", acctEmail) {
			if !strings.Contains(s, part) {
				t.Errorf("%s summary %q lacks %q", id, s, part)
			}
		}
		if id != serviceAccountsDeleteID && !strings.Contains(s, testKeyID) {
			t.Errorf("%s summary %q lacks the key", id, s)
		}
	}
}

func TestServiceAccountsDeleteIsOnlyReachableByName(t *testing.T) {
	args := `{"project_id":"example-project-123","account":"` + acctEmail + `"}`
	for name, tools := range map[string][]string{
		"no tools list":    nil,
		"other tool named": {serviceAccountsDisableID},
		"namespace prefix": {"google.cloud.service-accounts.*"},
		"prefix and a key": {"google.cloud.service-accounts.*", serviceAccountKeysDeleteID},
	} {
		e := revokeEnvironment(t, nil, tools)
		if _, err := e.invokeConfirmed(serviceAccountsDeleteID, args); err == nil {
			t.Errorf("%s: service-accounts.delete was reachable", name)
		}
		noIO(t, e, name)
	}
	e := revokeEnvironment(t, nil, []string{serviceAccountsDeleteID})
	e.fake.routes[acctPath+acctEmail] = okReply(`{}`)
	out, err := e.invokeConfirmed(serviceAccountsDeleteID, args)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != "DELETE" || api[0].body != "" {
		t.Fatalf("requests = %+v", api)
	}
	if got := decode(t, out); got["deleted"] != true || got["account"] != acctEmail {
		t.Errorf("result = %s", out)
	}
}

func TestServiceAccountDeletionsAreInNoProfile(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	found := false
	for _, profile := range metadata.Profiles {
		for _, tool := range profile.Tools {
			if tool == serviceAccountsDeleteID || tool == serviceAccountKeysDeleteID {
				t.Errorf("profile %s holds %s", profile.ID, tool)
			}
		}
		if profile.ID == "service-accounts-revoke" {
			found = true
			if profile.Recommended || len(profile.Tools) != 2 {
				t.Errorf("profile = %+v", profile)
			}
		}
	}
	if !found {
		t.Error("profile service-accounts-revoke is missing")
	}
	for _, id := range []string{serviceAccountKeysDisableID, serviceAccountKeysEnableID, serviceAccountKeysDeleteID,
		serviceAccountsDeleteID} {
		if scopes := tokenMetadata.Scopes[id]; len(scopes) != 1 || scopes[0] != scopeCloudPlatform {
			t.Errorf("%s scopes = %v", id, scopes)
		}
	}
}

func TestRevokeFailureNamesAnUnknownOutcome(t *testing.T) {
	e := revokeEnvironment(t, nil, []string{serviceAccountKeysDeleteID})
	e.fake.routes[keyURLPath] = reply{status: 503, body: `{}`}
	_, err := e.invokeConfirmed(serviceAccountKeysDeleteID, keyArgs)
	if err == nil || !strings.Contains(err.Error(), "outcome") || len(e.fake.api()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
}
