package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	adminOne      = boundAccount + "/admins/3000001"
	adminTwo      = boundAccount + "/admins/3000002"
	adminPending  = boundAccount + "/admins/3000003"
	adminPrimary  = boundAccount + "/admins/3000004"
	locationAdmin = boundLocation + "/admins/3000005"
)

var pendingAddress = "invitee" + "@" + "sample.invalid"

// adminList is the account admin list: two admins, a pending invitation, and the primary owner.
var adminList = `{"accountAdmins":[` +
	`{"name":"` + adminOne + `","admin":"Sample Person","role":"OWNER"},` +
	`{"name":"` + adminTwo + `","admin":"Sample Other","role":"MANAGER","account":"accounts/1000050"},` +
	`{"name":"` + adminPending + `","admin":"` + pendingAddress + `","role":"MANAGER","pendingInvitation":true},` +
	`{"name":"` + adminPrimary + `","admin":"Sample Primary","role":"PRIMARY_OWNER"}]}`

var locationAdminList = `{"admins":[{"name":"` + locationAdmin + `","admin":"Sample Person","role":"MANAGER"}]}`

// writer routes the fake by method and path: lists answer, a PATCH answers patched, and every request and
// body is kept.
type writer struct {
	mu      sync.Mutex
	methods []string
	patched string
	status  int
}

func (w *writer) answer(r *http.Request, _ int) (*http.Response, error) {
	w.mu.Lock()
	w.methods = append(w.methods, r.Method)
	w.mu.Unlock()
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPatch && w.status != 0:
		return respond(r, w.status, "application/json", errorObject(w.status, "UNAVAILABLE", "BACKEND")), nil
	case r.Method == http.MethodPatch:
		return respond(r, http.StatusOK, "application/json", w.patched), nil
	case path == "/v1/"+boundAccount+"/admins":
		return respond(r, http.StatusOK, "application/json", adminList), nil
	case path == "/v1/"+boundLocation+"/admins":
		return respond(r, http.StatusOK, "application/json", locationAdminList), nil
	case path == "/v1/"+boundAccount+"/locations":
		return respond(r, http.StatusOK, "application/json", `{"locations":[{"name":"`+boundLocation+`"}]}`), nil
	case path == "/v1/"+boundAccount:
		return respond(r, http.StatusOK, "application/json",
			`{"name":"`+boundAccount+`","accountName":"Old group","type":"LOCATION_GROUP","role":"OWNER"}`), nil
	case path == "/v1/"+otherAccount:
		return respond(r, http.StatusOK, "application/json",
			`{"name":"`+otherAccount+`","accountName":"Sample Person","type":"PERSONAL","role":"PRIMARY_OWNER"}`), nil
	}
	return respond(r, http.StatusNotFound, "application/json", errorObject(404, "NOT_FOUND", "NOT_FOUND")), nil
}

// writeEnvironment adds connections that may change to the environment and routes the fake.
func writeEnvironment(t *testing.T) (*environment, *writer) {
	t.Helper()
	env := newEnvironment(t, nil)
	w := &writer{patched: `{"name":"` + adminOne + `","admin":"Sample Person","role":"MANAGER"}`}
	env.fake.answer = w.answer
	effects := []config.Effect{config.EffectRead, config.EffectUpdate}
	for name, target := range map[string]string{"w-all": "", "w-account": boundAccount, "w-location": boundLocation} {
		env.cfg.Connections[name] = config.Connection{Service: "profile", Credential: "google", Effects: effects,
			Target: target}
	}
	return env, w
}

func (e *environment) change(t *testing.T, operation, connection, arguments string) (map[string]any, error) {
	t.Helper()
	return e.changeWith(t, context.Background(), operation, connection, arguments)
}

func (e *environment) changeWith(t *testing.T, ctx context.Context, operation, connection, arguments string) (map[string]any, error) {
	t.Helper()
	response, err := e.core.Invoke(ctx, application.InvokeRequest{Operation: operation, Connection: connection,
		Arguments: json.RawMessage(arguments), Confirmed: true})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(response.Result, &out); err != nil {
		t.Fatalf("result %s: %v", response.Result, err)
	}
	return out, nil
}

func (e *environment) noRequests(t *testing.T) {
	t.Helper()
	if calls, grants := e.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func update(admin, previousAdmin, previousRole, role string) string {
	return `{"admin":"` + admin + `","previous_admin":"` + previousAdmin + `","previous_role":"` + previousRole +
		`","role":"` + role + `"}`
}

func TestAdminListsAnswerEveryDocumentedMember(t *testing.T) {
	env, _ := writeEnvironment(t)
	out, err := env.invoke(t, accountAdminsListID, "all", `{"account":"`+boundAccount+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	admins := out["admins"].([]any)
	if len(admins) != 4 {
		t.Fatalf("admins = %v", admins)
	}
	pending := admins[2].(map[string]any)
	if pending["pendingInvitation"] != true || pending["admin"] != pendingAddress || pending["role"] != "MANAGER" {
		t.Errorf("pending admin = %v", pending)
	}
	if admins[1].(map[string]any)["account"] != "accounts/1000050" {
		t.Errorf("account member missing: %v", admins[1])
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].path != "/v1/"+boundAccount+"/admins" || calls[0].query != "" {
		t.Errorf("requests = %+v", calls)
	}
	// A location target proves the location through its account first.
	out, err = env.invoke(t, locationAdminsListID, "account", `{"location":"`+boundLocation+`"}`)
	if err != nil || len(out["admins"].([]any)) != 1 {
		t.Fatalf("location admins = %v, %v", out, err)
	}
	calls, _ = env.fake.calls()
	if len(calls) != 3 || calls[1].path != "/v1/"+boundAccount+"/locations" || calls[2].path != "/v1/"+boundLocation+"/admins" {
		t.Errorf("requests = %+v", calls)
	}
}

func TestAdminListsRefuseWhatLiesOutsideTheTarget(t *testing.T) {
	env, _ := writeEnvironment(t)
	for name, c := range map[string]struct{ op, connection, args string }{
		"account list other account":  {accountAdminsListID, "account", `{"account":"` + otherAccount + `"}`},
		"account list location bound": {accountAdminsListID, "location", `{"account":"` + boundAccount + `"}`},
		"location list other":         {locationAdminsListID, "location", `{"location":"` + otherLocation + `"}`},
		"bad location":                {locationAdminsListID, "all", `{"location":"locations/x"}`},
	} {
		if _, err := env.invoke(t, c.op, c.connection, c.args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noRequests(t)
}

func TestRoleChangeShowsOldAndNewValuesAndPatchesOnlyTheRole(t *testing.T) {
	env, w := writeEnvironment(t)
	args := update(adminOne, "Sample Person", "OWNER", "MANAGER")
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: accountAdminsUpdateID,
		Connection: "w-all", Arguments: json.RawMessage(args)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) {
		t.Fatalf("error = %v, want confirmation required", err)
	}
	for _, want := range []string{adminOne, "Sample Person", "OWNER", "MANAGER"} {
		if !strings.Contains(needs.Summary, want) {
			t.Errorf("summary %q lacks %q", needs.Summary, want)
		}
	}
	env.noRequests(t)

	out, err := env.change(t, accountAdminsUpdateID, "w-all", args)
	if err != nil || out["role"] != "MANAGER" {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || w.methods[0] != http.MethodGet || w.methods[1] != http.MethodPatch ||
		calls[1].path != "/v1/"+adminOne || calls[1].host != "mybusinessaccountmanagement.googleapis.com" ||
		calls[1].values().Get("updateMask") != "role" || len(calls[1].values()) != 1 {
		t.Fatalf("requests = %+v %v", calls, w.methods)
	}
	if calls[1].body != `{"role":"MANAGER"}` {
		t.Errorf("body = %s, want only the role", calls[1].body)
	}
}

func TestLocationRoleChange(t *testing.T) {
	env, w := writeEnvironment(t)
	w.patched = `{"name":"` + locationAdmin + `","admin":"Sample Person","role":"SITE_MANAGER"}`
	out, err := env.change(t, locationAdminsUpdateID, "w-account",
		update(locationAdmin, "Sample Person", "MANAGER", "SITE_MANAGER"))
	if err != nil || out["role"] != "SITE_MANAGER" {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	// The account target proves the location, then the list is read, then the change is sent once.
	if len(calls) != 3 || calls[0].path != "/v1/"+boundAccount+"/locations" ||
		calls[2].path != "/v1/"+locationAdmin || w.methods[2] != http.MethodPatch || calls[2].body != `{"role":"SITE_MANAGER"}` {
		t.Fatalf("requests = %+v", calls)
	}
}

func TestPrimaryOwnerIsNeverATargetAndNeverChanged(t *testing.T) {
	env, _ := writeEnvironment(t)
	for name, args := range map[string]string{
		"target role":      update(adminOne, "Sample Person", "OWNER", "PRIMARY_OWNER"),
		"previous primary": update(adminPrimary, "Sample Primary", "PRIMARY_OWNER", "OWNER"),
		"unspecified role": update(adminOne, "Sample Person", "OWNER", "ADMIN_ROLE_UNSPECIFIED"),
		"same role":        update(adminOne, "Sample Person", "OWNER", "OWNER"),
		"unknown mask":     strings.TrimSuffix(update(adminOne, "Sample Person", "OWNER", "MANAGER"), "}") + `,"update_mask":"admin"}`,
	} {
		if _, err := env.change(t, accountAdminsUpdateID, "w-all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid request", name, err)
		}
	}
	env.noRequests(t)
}

func TestHeldPrimaryOwnerAndPendingInvitationAreRefusedWithoutAChange(t *testing.T) {
	env, w := writeEnvironment(t)
	// previous_role says OWNER, but the list holds PRIMARY_OWNER: no change.
	for name, args := range map[string]string{
		"holds primary owner": update(adminPrimary, "Sample Primary", "OWNER", "MANAGER"),
		"pending invitation":  update(adminPending, pendingAddress, "MANAGER", "OWNER"),
		"unknown admin":       update(boundAccount+"/admins/3999999", "Sample Person", "OWNER", "MANAGER"),
	} {
		_, err := env.change(t, accountAdminsUpdateID, "w-all", args)
		if !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid request", name, err)
		} else if strings.Contains(err.Error(), pendingAddress) || strings.Contains(err.Error(), "Sample Primary") {
			t.Errorf("%s: the message names a person: %v", name, err)
		}
	}
	for _, method := range w.methods {
		if method == http.MethodPatch {
			t.Fatal("a change was sent")
		}
	}
}

func TestPreviousValuesMustMatchWhatGoogleHolds(t *testing.T) {
	env, w := writeEnvironment(t)
	for name, args := range map[string]string{
		"role":  update(adminOne, "Sample Person", "MANAGER", "SITE_MANAGER"),
		"admin": update(adminOne, "Sample Someone", "OWNER", "MANAGER"),
	} {
		if _, err := env.change(t, accountAdminsUpdateID, "w-all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid request", name, err)
		}
	}
	for _, method := range w.methods {
		if method == http.MethodPatch {
			t.Fatal("a change was sent")
		}
	}
}

func TestAdminPathsAreCheckedAgainstFormAndTargetBeforeIO(t *testing.T) {
	env, _ := writeEnvironment(t)
	other := otherAccount + "/admins/3000001"
	for name, c := range map[string]struct{ op, connection, args string }{
		"other account":            {accountAdminsUpdateID, "w-account", update(other, "Sample Person", "OWNER", "MANAGER")},
		"location target":          {accountAdminsUpdateID, "w-location", update(adminOne, "Sample Person", "OWNER", "MANAGER")},
		"other location":           {locationAdminsUpdateID, "w-location", update(otherLocation+"/admins/3000001", "Sample Person", "OWNER", "MANAGER")},
		"path step":                {accountAdminsUpdateID, "w-all", update(boundAccount+"/admins/..", "Sample Person", "OWNER", "MANAGER")},
		"extra segment":            {accountAdminsUpdateID, "w-all", update(adminOne+"/x", "Sample Person", "OWNER", "MANAGER")},
		"location in account tool": {accountAdminsUpdateID, "w-all", update(locationAdmin, "Sample Person", "OWNER", "MANAGER")},
		"account in location tool": {locationAdminsUpdateID, "w-all", update(adminOne, "Sample Person", "OWNER", "MANAGER")},
	} {
		_, err := env.change(t, c.op, c.connection, c.args)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "1000009") || strings.Contains(err.Error(), "2000003") {
			t.Errorf("%s: the message names the other object: %v", name, err)
		}
	}
	env.noRequests(t)
}

func TestNoToolCreatesAdminsOrAccounts(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range reg.Provider(Provider) {
		ids = append(ids, d.ID)
	}
	want := map[string]bool{}
	for _, name := range []string{"account-admins.list", "account-admins.update", "accounts.get", "accounts.list",
		"accounts.update", "location-admins.list", "location-admins.update", "locations.get", "locations.list",
		"performance.fetch-daily-metrics", "performance.get-daily-metric", "performance.list-search-keywords",
		"notifications.get", "verifications.fetch-options", "verifications.get-merchant-state", "verifications.list"} {
		want[Provider+"."+name] = true
	}
	if len(ids) != len(want) {
		t.Errorf("tools = %v", ids)
	}
	for _, id := range ids {
		if !want[id] || strings.Contains(id, "create") || strings.Contains(id, "delete") || strings.Contains(id, "transfer") {
			t.Errorf("unexpected tool %s", id)
		}
	}
	for _, d := range reg.Provider(Provider) {
		for _, a := range d.Arguments {
			if strings.Contains(a.Name, "email") || strings.Contains(a.Name, "address") || a.Name == "primary_owner" {
				t.Errorf("%s takes %s", d.ID, a.Name)
			}
		}
	}
}

func TestChangesNeedConfirmation(t *testing.T) {
	env, _ := writeEnvironment(t)
	for op, args := range map[string]string{
		accountAdminsUpdateID:  update(adminOne, "Sample Person", "OWNER", "MANAGER"),
		locationAdminsUpdateID: update(locationAdmin, "Sample Person", "MANAGER", "OWNER"),
		accountsUpdateID:       `{"account":"` + boundAccount + `","previous_account_name":"Old group","account_name":"New group"}`,
	} {
		_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: op, Connection: "w-all",
			Arguments: json.RawMessage(args)})
		var needs *application.ConfirmationRequiredError
		if !errors.As(err, &needs) || needs.Summary == "" {
			t.Errorf("%s: error = %v, want confirmation required with a summary", op, err)
		}
	}
	env.noRequests(t)
}

func TestAccountRename(t *testing.T) {
	env, w := writeEnvironment(t)
	w.patched = `{"name":"` + boundAccount + `","accountName":"New group","type":"LOCATION_GROUP"}`
	out, err := env.change(t, accountsUpdateID, "w-all",
		`{"account":"`+boundAccount+`","previous_account_name":"Old group","account_name":"New group","validate_only":true}`)
	if err != nil || out["accountName"] != "New group" {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || w.methods[1] != http.MethodPatch || calls[1].path != "/v1/"+boundAccount ||
		calls[1].values().Get("updateMask") != "accountName" || calls[1].values().Get("validateOnly") != "true" ||
		calls[1].body != `{"accountName":"New group"}` {
		t.Fatalf("requests = %+v", calls)
	}
}

// A connection bound to an account may leave the account out.
func TestAccountRenameOnABoundAccount(t *testing.T) {
	env, _ := writeEnvironment(t)
	if _, err := env.change(t, accountsUpdateID, "w-account",
		`{"previous_account_name":"Old group","account_name":"New group"}`); err != nil {
		t.Errorf("bound account left out: %v", err)
	}
}

func TestAccountRenameRefusals(t *testing.T) {
	env, w := writeEnvironment(t)
	base := `"previous_account_name":"Old group","account_name":"New group"`
	for name, c := range map[string]struct{ connection, args string }{
		"unknown mask":    {"w-all", `{"account":"` + boundAccount + `",` + base + `,"update_mask":"type"}`},
		"other account":   {"w-account", `{"account":"` + otherAccount + `",` + base + `}`},
		"location target": {"w-location", `{"account":"` + boundAccount + `",` + base + `}`},
		"control chars":   {"w-all", `{"account":"` + boundAccount + `","previous_account_name":"Old group","account_name":"New\ngroup"}`},
		"blank":           {"w-all", `{"account":"` + boundAccount + `","previous_account_name":"Old group","account_name":"   "}`},
		"same name":       {"w-all", `{"account":"` + boundAccount + `","previous_account_name":"Old group","account_name":"Old group"}`},
		"account missing": {"w-all", `{` + base + `}`},
	} {
		if _, err := env.change(t, accountsUpdateID, c.connection, c.args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noRequests(t)
	// After the read: another name, and a personal account.
	for name, args := range map[string]string{
		"stale name": `{"account":"` + boundAccount + `","previous_account_name":"Older group","account_name":"New group"}`,
		"personal":   `{"account":"` + otherAccount + `","previous_account_name":"Sample Person","account_name":"New group"}`,
	} {
		if _, err := env.change(t, accountsUpdateID, "w-all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid request", name, err)
		}
	}
	for _, method := range w.methods {
		if method == http.MethodPatch {
			t.Fatal("a change was sent")
		}
	}
}

func TestOneAccountSharesTheChangeLimiter(t *testing.T) {
	env, _ := writeEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := env.changeWith(t, ctx, accountAdminsUpdateID, "w-all",
		update(adminOne, "Sample Person", "OWNER", "MANAGER")); err != nil {
		t.Fatal(err)
	}
	_, err := env.changeWith(t, ctx, accountsUpdateID, "w-all",
		`{"account":"`+boundAccount+`","previous_account_name":"Old group","account_name":"New group"}`)
	if classOf(err) != provider.ClassRateLimited {
		t.Errorf("second change of the account = %v, want rate-limited", err)
	}
	// Another parent has its own count.
	if _, err := env.changeWith(t, ctx, locationAdminsUpdateID, "w-all",
		update(locationAdmin, "Sample Person", "MANAGER", "OWNER")); err != nil {
		t.Errorf("a change of a location waited: %v", err)
	}
}

func TestChangeFailuresKeepGoogleTextOutAndSayWhenUnsure(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		class  provider.Class
		unsure bool
	}{
		"403": {http.StatusForbidden, provider.ClassPermission, false},
		"429": {http.StatusTooManyRequests, provider.ClassRateLimited, false},
		"503": {http.StatusServiceUnavailable, provider.ClassUnreachable, true},
	} {
		t.Run(name, func(t *testing.T) {
			env, w := writeEnvironment(t)
			w.status = c.status
			_, err := env.change(t, accountAdminsUpdateID, "w-all", update(adminOne, "Sample Person", "OWNER", "MANAGER"))
			if err == nil || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), accessPrefix) {
				t.Fatalf("error = %v", err)
			}
			var failed *provider.Error
			if !errors.As(err, &failed) {
				t.Fatalf("error = %v", err)
			}
			if c.class != "" && failed.Class != c.class && !(c.status == 403 || c.status == 429) {
				t.Errorf("class = %s, want %s", failed.Class, c.class)
			}
			if strings.Contains(failed.Message, "outcome of the change is unknown") != c.unsure {
				t.Errorf("message = %q, unsure = %v", failed.Message, c.unsure)
			}
			patches := 0
			for _, method := range w.methods {
				if method == http.MethodPatch {
					patches++
				}
			}
			if patches != 1 {
				t.Errorf("%d changes sent, want exactly one", patches)
			}
		})
	}
}

func TestAccountToolsAreWithheldFromALocationTarget(t *testing.T) {
	for _, id := range []string{accountAdminsListID, accountAdminsUpdateID, accountsUpdateID} {
		if ok, _ := targetReaches(id, []string{boundLocation}); ok {
			t.Errorf("%s is offered to a location target", id)
		}
		if ok, _ := targetReaches(id, []string{boundAccount}); !ok {
			t.Errorf("%s is withheld from an account target", id)
		}
	}
	for _, id := range []string{locationAdminsListID, locationAdminsUpdateID} {
		if ok, _ := targetReaches(id, []string{boundLocation}); !ok {
			t.Errorf("%s is withheld from a location target", id)
		}
	}
}
