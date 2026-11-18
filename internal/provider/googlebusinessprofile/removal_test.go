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
	invitationOne = boundAccount + "/invitations/4000001"
	invitationTwo = boundAccount + "/invitations/4000002"
)

const invitationList = `{"invitations":[` +
	`{"name":"` + invitationOne + `","role":"MANAGER","targetType":"ACCOUNTS_ONLY","targetAccount":` +
	`{"name":"accounts/1000050","accountName":"Sample Group","type":"LOCATION_GROUP"}},` +
	`{"name":"` + invitationTwo + `","role":"OWNER","targetType":"LOCATIONS_ONLY","targetLocation":` +
	`{"locationName":"Sample Shop","address":"1 Sample Street","placeId":"sample-place"}}]}`

// remover adds DELETE, POST, and the invitation list to the fake of writeEnvironment and keeps what was sent.
type remover struct {
	mu      sync.Mutex
	inner   func(*http.Request, int) (*http.Response, error)
	methods []string
	status  int
	list    string
}

func (m *remover) answer(r *http.Request, n int) (*http.Response, error) {
	m.mu.Lock()
	m.methods = append(m.methods, r.Method)
	m.mu.Unlock()
	switch {
	case r.Method == http.MethodDelete || r.Method == http.MethodPost:
		if m.status != 0 {
			return respond(r, m.status, "application/json", errorObject(m.status, "UNAVAILABLE", "BACKEND")), nil
		}
		return respond(r, http.StatusOK, "application/json", `{}`), nil
	case r.URL.Path == "/v1/"+boundAccount+"/invitations":
		return respond(r, http.StatusOK, "application/json", m.list), nil
	}
	return m.inner(r, n)
}

func (m *remover) count(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, got := range m.methods {
		if got == method {
			total++
		}
	}
	return total
}

func removeEnvironment(t *testing.T) (*environment, *remover) {
	t.Helper()
	env, _ := writeEnvironment(t)
	m := &remover{inner: env.fake.answer, list: invitationList}
	env.fake.answer = m.answer
	for name, c := range env.cfg.Connections {
		if strings.HasPrefix(name, "w-") {
			c.Effects = append(c.Effects, config.EffectDelete)
			env.cfg.Connections[name] = c
		}
	}
	return env, m
}

func remove(admin, previousAdmin, previousRole string) string {
	return `{"admin":"` + admin + `","previous_admin":"` + previousAdmin + `","previous_role":"` + previousRole + `"}`
}

func decline(invitation, role, target string) string {
	return `{"invitation":"` + invitation + `","previous_role":"` + role + `","previous_target":"` + target + `"}`
}

func TestAdminRemovalShowsWhoIsRemovedAndDeletesOnce(t *testing.T) {
	env, m := removeEnvironment(t)
	args := remove(adminTwo, "Sample Other", "MANAGER")
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: accountAdminsDeleteID,
		Connection: "w-all", Arguments: json.RawMessage(args)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) {
		t.Fatalf("error = %v, want confirmation required", err)
	}
	for _, want := range []string{adminTwo, "Sample Other", "MANAGER", boundAccount} {
		if !strings.Contains(needs.Summary, want) {
			t.Errorf("summary %q lacks %q", needs.Summary, want)
		}
	}
	env.noRequests(t)

	out, err := env.change(t, accountAdminsDeleteID, "w-all", args)
	if err != nil || out["removed"] != true || out["name"] != adminTwo {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || m.methods[0] != http.MethodGet || m.methods[1] != http.MethodDelete ||
		calls[1].path != "/v1/"+adminTwo || calls[1].host != "mybusinessaccountmanagement.googleapis.com" ||
		calls[1].query != "" || calls[1].body != "" {
		t.Fatalf("requests = %+v %v", calls, m.methods)
	}
}

func TestLocationAdminRemoval(t *testing.T) {
	env, m := removeEnvironment(t)
	out, err := env.change(t, locationAdminsDeleteID, "w-account", remove(locationAdmin, "Sample Person", "MANAGER"))
	if err != nil || out["removed"] != true {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	// The account target proves the location, then the list is read, then the delete is sent once.
	if len(calls) != 3 || calls[0].path != "/v1/"+boundAccount+"/locations" ||
		calls[2].path != "/v1/"+locationAdmin || m.methods[2] != http.MethodDelete {
		t.Fatalf("requests = %+v %v", calls, m.methods)
	}
	if _, err := env.change(t, locationAdminsDeleteID, "w-location", remove(locationAdmin, "Sample Person", "MANAGER")); err != nil {
		t.Errorf("location target: %v", err)
	}
}

func TestPendingInvitationOfAnAdminMayBeRevoked(t *testing.T) {
	env, m := removeEnvironment(t)
	if _, err := env.change(t, accountAdminsDeleteID, "w-all", remove(adminPending, pendingAddress, "MANAGER")); err != nil {
		t.Fatal(err)
	}
	if m.count(http.MethodDelete) != 1 {
		t.Errorf("%d deletes, want 1", m.count(http.MethodDelete))
	}
}

func TestPrimaryOwnerIsNeverRemoved(t *testing.T) {
	env, m := removeEnvironment(t)
	_, err := env.change(t, accountAdminsDeleteID, "w-all", remove(adminPrimary, "Sample Primary", "PRIMARY_OWNER"))
	if !isInvalidRequest(err) {
		t.Errorf("stated primary owner: error = %v, want invalid request", err)
	}
	env.noRequests(t)
	// previous_role says OWNER, but the list holds PRIMARY_OWNER: read, refused, nothing deleted.
	_, err = env.change(t, accountAdminsDeleteID, "w-all", remove(adminPrimary, "Sample Primary", "OWNER"))
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "Sample Primary") {
		t.Errorf("held primary owner: error = %v", err)
	}
	if m.count(http.MethodDelete) != 0 {
		t.Fatal("a delete was sent")
	}
}

func TestRemovalRefusesWhatGoogleHoldsDifferently(t *testing.T) {
	env, m := removeEnvironment(t)
	for name, args := range map[string]string{
		"role":    remove(adminOne, "Sample Person", "MANAGER"),
		"admin":   remove(adminOne, "Sample Someone", "OWNER"),
		"unknown": remove(boundAccount+"/admins/3999999", "Sample Person", "OWNER"),
	} {
		if _, err := env.change(t, accountAdminsDeleteID, "w-all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid request", name, err)
		}
	}
	for name, args := range map[string]string{
		"unspecified role": remove(adminOne, "Sample Person", "ADMIN_ROLE_UNSPECIFIED"),
		"blank admin":      remove(adminOne, "  ", "OWNER"),
	} {
		if _, err := env.change(t, accountAdminsDeleteID, "w-all", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if m.count(http.MethodDelete) != 0 {
		t.Fatal("a delete was sent")
	}
}

func TestRemovalAndDeclinePathsAreCheckedAgainstFormAndTargetBeforeIO(t *testing.T) {
	env, _ := removeEnvironment(t)
	other := otherAccount + "/admins/3000001"
	for name, c := range map[string]struct{ op, connection, args string }{
		"other account":            {accountAdminsDeleteID, "w-account", remove(other, "Sample Person", "OWNER")},
		"location target":          {accountAdminsDeleteID, "w-location", remove(adminOne, "Sample Person", "OWNER")},
		"other location":           {locationAdminsDeleteID, "w-location", remove(otherLocation+"/admins/3000001", "Sample Person", "OWNER")},
		"path step":                {accountAdminsDeleteID, "w-all", remove(boundAccount+"/admins/..", "Sample Person", "OWNER")},
		"extra segment":            {accountAdminsDeleteID, "w-all", remove(adminOne+"/x", "Sample Person", "OWNER")},
		"location in account tool": {accountAdminsDeleteID, "w-all", remove(locationAdmin, "Sample Person", "OWNER")},
		"account in location tool": {locationAdminsDeleteID, "w-all", remove(adminOne, "Sample Person", "OWNER")},
		"decline other account":    {invitationsDeclineID, "w-account", decline(otherAccount+"/invitations/4000001", "MANAGER", "accounts/1000050")},
		"decline location target":  {invitationsDeclineID, "w-location", decline(invitationOne, "MANAGER", "accounts/1000050")},
		"decline path step":        {invitationsDeclineID, "w-all", decline(boundAccount+"/invitations/..", "MANAGER", "accounts/1000050")},
		"decline extra segment":    {invitationsDeclineID, "w-all", decline(invitationOne+":accept", "MANAGER", "accounts/1000050")},
		"decline admin path":       {invitationsDeclineID, "w-all", decline(adminOne, "MANAGER", "accounts/1000050")},
		"decline blank target":     {invitationsDeclineID, "w-all", decline(invitationOne, "MANAGER", " ")},
		"decline unknown role":     {invitationsDeclineID, "w-all", decline(invitationOne, "ADMIN_ROLE_UNSPECIFIED", "accounts/1000050")},
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

func TestRemovalsAndDeclineNeedConfirmation(t *testing.T) {
	env, _ := removeEnvironment(t)
	for op, args := range map[string]string{
		accountAdminsDeleteID:  remove(adminOne, "Sample Person", "OWNER"),
		locationAdminsDeleteID: remove(locationAdmin, "Sample Person", "MANAGER"),
		invitationsDeclineID:   decline(invitationOne, "MANAGER", "accounts/1000050"),
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

func TestRemovalNeedsTheDeleteEffect(t *testing.T) {
	env, _ := removeEnvironment(t)
	// w-all has the effect; a connection that only reads or updates does not offer the tool.
	c := env.cfg.Connections["w-all"]
	c.Effects = []config.Effect{config.EffectRead, config.EffectUpdate}
	env.cfg.Connections["w-all"] = c
	if _, err := env.change(t, accountAdminsDeleteID, "w-all", remove(adminOne, "Sample Person", "OWNER")); err == nil {
		t.Error("removal accepted without the delete effect")
	}
	env.noRequests(t)
}

func TestDeclineShowsTheInvitationAndPostsAnEmptyBodyOnce(t *testing.T) {
	env, m := removeEnvironment(t)
	args := decline(invitationOne, "MANAGER", "accounts/1000050")
	_, err := env.core.Invoke(context.Background(), application.InvokeRequest{Operation: invitationsDeclineID,
		Connection: "w-all", Arguments: json.RawMessage(args)})
	var needs *application.ConfirmationRequiredError
	if !errors.As(err, &needs) {
		t.Fatalf("error = %v, want confirmation required", err)
	}
	for _, want := range []string{invitationOne, "accounts/1000050", "MANAGER"} {
		if !strings.Contains(needs.Summary, want) {
			t.Errorf("summary %q lacks %q", needs.Summary, want)
		}
	}
	env.noRequests(t)

	out, err := env.change(t, invitationsDeclineID, "w-all", args)
	if err != nil || out["declined"] != true || out["name"] != invitationOne {
		t.Fatalf("result = %v, %v", out, err)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 2 || m.methods[0] != http.MethodGet || m.methods[1] != http.MethodPost ||
		calls[0].path != "/v1/"+boundAccount+"/invitations" || calls[1].path != "/v1/"+invitationOne+":decline" ||
		calls[1].query != "" || calls[1].body != `{}` {
		t.Fatalf("requests = %+v %v", calls, m.methods)
	}
	// A location is named by its location name.
	if _, err := env.change(t, invitationsDeclineID, "w-account", decline(invitationTwo, "OWNER", "Sample Shop")); err != nil {
		t.Errorf("location invitation: %v", err)
	}
}

func TestDeclineRefusesWhatGoogleHoldsDifferently(t *testing.T) {
	env, m := removeEnvironment(t)
	for name, args := range map[string]string{
		"role":    decline(invitationOne, "OWNER", "accounts/1000050"),
		"target":  decline(invitationOne, "MANAGER", "accounts/1000099"),
		"unknown": decline(boundAccount+"/invitations/4999999", "MANAGER", "accounts/1000050"),
	} {
		if _, err := env.change(t, invitationsDeclineID, "w-all", args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid request", name, err)
		}
	}
	if m.count(http.MethodPost) != 0 {
		t.Fatal("a decline was sent")
	}
}

func TestRemovalAndDeclineFailuresKeepGoogleTextOutAndSayWhenUnsure(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		unsure bool
	}{
		"403": {http.StatusForbidden, false},
		"429": {http.StatusTooManyRequests, false},
		"503": {http.StatusServiceUnavailable, true},
	} {
		for tool, send := range map[string]struct {
			op, args, method string
		}{
			"delete":  {accountAdminsDeleteID, remove(adminOne, "Sample Person", "OWNER"), http.MethodDelete},
			"decline": {invitationsDeclineID, decline(invitationOne, "MANAGER", "accounts/1000050"), http.MethodPost},
		} {
			t.Run(tool+" "+name, func(t *testing.T) {
				env, m := removeEnvironment(t)
				m.status = c.status
				_, err := env.change(t, send.op, "w-all", send.args)
				var failed *provider.Error
				if !errors.As(err, &failed) || strings.Contains(err.Error(), detailCanary) ||
					strings.Contains(err.Error(), accessPrefix) {
					t.Fatalf("error = %v", err)
				}
				if strings.Contains(failed.Message, "outcome of the change is unknown") != c.unsure {
					t.Errorf("message = %q, unsure = %v", failed.Message, c.unsure)
				}
				if m.count(send.method) != 1 {
					t.Errorf("%d changes sent, want exactly one", m.count(send.method))
				}
			})
		}
	}
}

func TestRemovalSharesTheChangeLimiterOfItsAccount(t *testing.T) {
	env, _ := removeEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := env.changeWith(t, ctx, accountAdminsDeleteID, "w-all", remove(adminTwo, "Sample Other", "MANAGER")); err != nil {
		t.Fatal(err)
	}
	_, err := env.changeWith(t, ctx, invitationsDeclineID, "w-all", decline(invitationOne, "MANAGER", "accounts/1000050"))
	if classOf(err) != provider.ClassRateLimited {
		t.Errorf("second change of the account = %v, want rate-limited", err)
	}
}

func TestInvitationListAnswersEveryDocumentedMemberAndFilters(t *testing.T) {
	env, _ := removeEnvironment(t)
	out, err := env.invoke(t, invitationsListID, "all", `{"account":"`+boundAccount+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	invitations := out["invitations"].([]any)
	if len(invitations) != 2 {
		t.Fatalf("invitations = %v", invitations)
	}
	first, second := invitations[0].(map[string]any), invitations[1].(map[string]any)
	account := first["targetAccount"].(map[string]any)
	location := second["targetLocation"].(map[string]any)
	if first["name"] != invitationOne || first["role"] != "MANAGER" || first["targetType"] != "ACCOUNTS_ONLY" ||
		account["name"] != "accounts/1000050" || account["accountName"] != "Sample Group" ||
		location["locationName"] != "Sample Shop" || location["address"] != "1 Sample Street" ||
		location["placeId"] != "sample-place" {
		t.Errorf("invitations = %v", invitations)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].path != "/v1/"+boundAccount+"/invitations" || calls[0].query != "" {
		t.Errorf("requests = %+v", calls)
	}

	out, err = env.invoke(t, invitationsListID, "account", `{"filter":"LOCATIONS_ONLY"}`)
	if err != nil {
		t.Fatal(err)
	}
	// Google answered both entries although it was asked for locations: the client drops the other one.
	if got := out["invitations"].([]any); len(got) != 1 || got[0].(map[string]any)["name"] != invitationTwo {
		t.Errorf("filtered invitations = %v", got)
	}
	calls, _ = env.fake.calls()
	if len(calls) != 2 || calls[1].values().Get("filter") != "targetType=LOCATIONS_ONLY" {
		t.Errorf("requests = %+v", calls)
	}
}

func TestEmptyInvitationListAnswersAnEmptyList(t *testing.T) {
	env, m := removeEnvironment(t)
	m.list = `{}`
	out, err := env.invoke(t, invitationsListID, "all", `{"account":"`+boundAccount+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	// The core leaves an empty list out of the result.
	if got, _ := out["invitations"].([]any); len(got) != 0 {
		t.Errorf("invitations = %v", out["invitations"])
	}
}

func TestInvitationListRefusesWhatLiesOutsideTheTarget(t *testing.T) {
	env, _ := removeEnvironment(t)
	for name, c := range map[string]struct{ connection, args string }{
		"other account":   {"account", `{"account":"` + otherAccount + `"}`},
		"location target": {"location", `{"account":"` + boundAccount + `"}`},
		"no account":      {"all", `{}`},
		"bad account":     {"all", `{"account":"accounts/-"}`},
		"bad filter":      {"all", `{"account":"` + boundAccount + `","filter":"state=PENDING"}`},
	} {
		if _, err := env.invoke(t, invitationsListID, c.connection, c.args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noRequests(t)
}

func TestAccountLevelRemovalAndInvitationToolsAreWithheldFromALocationTarget(t *testing.T) {
	for _, id := range []string{accountAdminsDeleteID, invitationsListID, invitationsDeclineID} {
		if ok, _ := targetReaches(id, []string{boundLocation}); ok {
			t.Errorf("%s is offered to a location target", id)
		}
		if ok, _ := targetReaches(id, []string{boundAccount}); !ok {
			t.Errorf("%s is withheld from an account target", id)
		}
	}
	if ok, _ := targetReaches(locationAdminsDeleteID, []string{boundLocation}); !ok {
		t.Error("location-admins.delete is withheld from a location target")
	}
}

func TestInvitationsCanBeDeclinedButNotAccepted(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, d := range reg.Provider(Provider) {
		links := strings.HasPrefix(d.ID, Provider+".place-action-links.") || strings.HasPrefix(d.ID, Provider+".media.")
		if strings.Contains(d.ID, "accept") || !links && strings.Contains(d.ID, "create") {
			t.Errorf("unexpected tool %s", d.ID)
		}
		for _, a := range d.Arguments {
			if strings.Contains(a.Name, "accept") || strings.Contains(a.Name, "email") || a.Name == "role" && d.ID == invitationsDeclineID {
				t.Errorf("%s takes %s", d.ID, a.Name)
			}
		}
		switch d.ID {
		case accountAdminsDeleteID, locationAdminsDeleteID:
			if d.Risk.Effect != capability.EffectDelete || d.Risk.Confirmation != capability.ConfirmationRequired {
				t.Errorf("%s risk = %+v", d.ID, d.Risk)
			}
		case invitationsDeclineID:
			if d.Risk.Effect != capability.EffectUpdate || d.Risk.Confirmation != capability.ConfirmationRequired {
				t.Errorf("%s risk = %+v", d.ID, d.Risk)
			}
		}
	}
}
