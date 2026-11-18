package googlebusinessprofile

import (
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const followersJSON = `{"name":"` + v4Location + `/followers/metadata","count":"1234","extra":"dropped"}`

func followersEnvironment(t *testing.T) *environment {
	t.Helper()
	env, f := reviewsEnvironment(t)
	f.review = followersJSON
	env.fake.answer = func(r *http.Request, n int) (*http.Response, error) {
		if r.URL.Path == "/v4/"+v4Location+"/followers/metadata" {
			return respond(r, http.StatusOK, "application/json", followersJSON), nil
		}
		return f.answer(r, n)
	}
	return env
}

func TestFollowersGetReturnsTheCountThroughTheBinding(t *testing.T) {
	env := followersEnvironment(t)
	for _, connection := range []string{"all", "location", "account"} {
		out, err := env.invoke(t, followersGetID, connection, `{"location":"`+v4Location+`"}`)
		if err != nil || out["count"] != "1234" || out["name"] != v4Location+"/followers/metadata" {
			t.Fatalf("%s: result = %v, %v", connection, out, err)
		}
		if _, ok := out["extra"]; ok {
			t.Errorf("%s: undocumented member returned: %v", connection, out)
		}
	}
	calls, _ := env.fake.calls()
	if len(calls) != 4 || calls[2].path != "/v1/"+boundAccount+"/locations" ||
		calls[3].path != "/v4/"+v4Location+"/followers/metadata" {
		t.Errorf("requests = %+v", calls)
	}
}

func TestFollowersGetRefusesLocationsOutsideTheBindingBeforeIO(t *testing.T) {
	env := followersEnvironment(t)
	for name, c := range map[string]struct{ connection, location string }{
		"location outside": {"location", v4Other},
		"account outside":  {"account", v4ForeignAcct},
		"v1 form":          {"all", "locations/2000002"},
		"path step":        {"all", v4Location + "/../x"},
		"extra segment":    {"all", v4Location + "/followers"},
	} {
		_, err := env.invoke(t, followersGetID, c.connection, `{"location":"`+c.location+`"}`)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "2000003") {
			t.Errorf("%s: names the other object: %v", name, err)
		}
	}
	env.noRequests(t)
}

func TestFollowersGetRefusesALocationNotInTheBoundAccount(t *testing.T) {
	env := followersEnvironment(t)
	_, err := env.invoke(t, followersGetID, "account", `{"location":"`+v4ForeignLoc+`"}`)
	if !isInvalidRequest(err) {
		t.Fatalf("error = %v, want invalid request", err)
	}
	calls, _ := env.fake.calls()
	for _, c := range calls {
		if strings.Contains(c.path, "followers") {
			t.Fatal("followers were read")
		}
	}
}

func TestFollowersGetFailuresMapToTheClassesOfTheBase(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		class  provider.Class
	}{
		"not found":    {404, errorObject(404, "NOT_FOUND", "NOT_FOUND"), provider.ClassNotFound},
		"permission":   {403, errorObject(403, "PERMISSION_DENIED", "FORBIDDEN"), provider.ClassPermission},
		"not approved": {429, zeroQuotaObject(), ClassAccessNotApproved},
		"rate limited": {429, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"), provider.ClassRateLimited},
	} {
		env := followersEnvironment(t)
		env.fake.answer = reply(c.status, c.body)
		_, err := env.invoke(t, followersGetID, "all", `{"location":"`+v4Location+`"}`)
		if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) {
			t.Errorf("%s: error = %v, want class %s", name, err, c.class)
		}
	}
}

func TestFollowersGetIsAReadOnlyTool(t *testing.T) {
	if !contains(readTools, followersGetID) || contains(writeTools, followersGetID) ||
		len(toolScopes[followersGetID]) != 1 || toolScopes[followersGetID][0] != scopeManage {
		t.Errorf("profile or scope wrong: %v", toolScopes[followersGetID])
	}
	d := followersGet
	if d.Risk.Effect != capability.EffectRead || d.Risk.Idempotency != capability.IdempotencySafe ||
		d.Risk.Confirmation != capability.ConfirmationNone {
		t.Errorf("risk = %+v", d.Risk)
	}
}
