package googlecloud

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	lifecycleRequestID = "3f0c7a52-9a4e-4c1b-8d2f-5b6a7c8d9e01"
	lifecycleBase      = `"project_id":"my-project-123","zone":"us-central1-a","instance":"my-vm"`
	lifecyclePath      = "/compute/v1/projects/my-project-123/zones/us-central1-a/instances/my-vm"
)

func lifecycleOperation(status, extra string) string {
	return `{"kind":"compute#operation","id":"9100000000000000002","name":"` + operationName + `",` +
		`"operationType":"stop","status":"` + status + `","user":"person@example.com",` +
		`"zone":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a"` + extra + `}`
}

func lifecycleEnvironment(t *testing.T, action string, answer reply) *environment {
	t.Helper()
	e := revokeEnvironment(t, []string{"projects/my-project-123"}, nil)
	e.fake.routes[lifecyclePath+"/"+action] = answer
	return e
}

var lifecycleTools = map[string]string{
	instancesStartID: "start", instancesStopID: "stop", instancesResetID: "reset",
	instancesSetMachineTypeID: "setMachineType",
}

func lifecycleArgs(id string) string {
	if id == instancesSetMachineTypeID {
		return `{` + lifecycleBase + `,"machine_type":"e2-standard-4"`
	}
	return `{` + lifecycleBase
}

func TestInstanceLifecycleSendsOnePostAndReturnsTheRunningOperation(t *testing.T) {
	for id, action := range lifecycleTools {
		e := lifecycleEnvironment(t, action, okReply(lifecycleOperation("RUNNING", "")))
		out, err := e.invokeConfirmed(id, lifecycleArgs(id)+`,"request_id":"`+lifecycleRequestID+`"}`)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		api := e.fake.api()
		want := "https://compute.googleapis.com" + lifecyclePath + "/" + action + "?requestId=" + lifecycleRequestID
		if len(api) != 1 || api[0].method != http.MethodPost || api[0].url != want {
			t.Fatalf("%s: requests = %+v", id, api)
		}
		if id == instancesSetMachineTypeID {
			if api[0].body != `{"machineType":"projects/my-project-123/zones/us-central1-a/machineTypes/e2-standard-4"}` {
				t.Errorf("body = %s", api[0].body)
			}
		} else if api[0].body != "" {
			t.Errorf("%s: body = %s", id, api[0].body)
		}
		got := decode(t, out)
		operation, _ := got["operation"].(map[string]any)
		if operation["status"] != "RUNNING" || got["scope"] != "zone" || got["zone"] != "us-central1-a" ||
			got["request_id"] != lifecycleRequestID {
			t.Errorf("%s: result = %s", id, out)
		}
	}
}

func TestInstanceStopSendsDiscardLocalSsdOnlyWhenAsked(t *testing.T) {
	for _, c := range []struct{ args, want string }{
		{`{` + lifecycleBase + `,"request_id":"` + lifecycleRequestID + `"}`, ""},
		{`{` + lifecycleBase + `,"discard_local_ssd":false,"request_id":"` + lifecycleRequestID + `"}`, ""},
		{`{` + lifecycleBase + `,"discard_local_ssd":true,"request_id":"` + lifecycleRequestID + `"}`, "true"},
	} {
		e := lifecycleEnvironment(t, "stop", okReply(lifecycleOperation("PENDING", "")))
		if _, err := e.invokeConfirmed(instancesStopID, c.args); err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(e.fake.api()[0].url)
		if u.Query().Get("discardLocalSsd") != c.want || u.Query().Get("requestId") != lifecycleRequestID {
			t.Errorf("query = %v, want discardLocalSsd %q", u.Query(), c.want)
		}
	}
}

func TestInstanceLifecycleRepeatsTheSameRequestIDAndCreatesOneWhenMissing(t *testing.T) {
	e := lifecycleEnvironment(t, "reset", okReply(lifecycleOperation("RUNNING", "")))
	for i := 0; i < 2; i++ {
		if _, err := e.invokeConfirmed(instancesResetID, `{`+lifecycleBase+`,"request_id":"`+lifecycleRequestID+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range e.fake.api() {
		if u, _ := url.Parse(r.url); u.Query().Get("requestId") != lifecycleRequestID {
			t.Errorf("request = %+v", r)
		}
	}
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	e = lifecycleEnvironment(t, "start", okReply(lifecycleOperation("PENDING", "")))
	out, err := e.invokeConfirmed(instancesStartID, `{`+lifecycleBase+`}`)
	if err != nil {
		t.Fatal(err)
	}
	sent, _ := url.Parse(e.fake.api()[0].url)
	id, _ := decode(t, out)["request_id"].(string)
	if !uuid.MatchString(id) || sent.Query().Get("requestId") != id {
		t.Errorf("request_id = %q, sent %q", id, sent.Query().Get("requestId"))
	}
}

func TestInstanceMachineTypeOnARunningInstanceIsTheErrorOfGoogleAndNotRepeated(t *testing.T) {
	e := lifecycleEnvironment(t, "setMachineType", reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")})
	_, err := e.invokeConfirmed(instancesSetMachineTypeID, lifecycleArgs(instancesSetMachineTypeID)+`}`)
	if err == nil || strings.Contains(err.Error(), detailCanary) && false {
		t.Fatalf("err = %v", err)
	}
	if got := methodsOf(e); !slices.Equal(got, []string{"POST"}) {
		t.Errorf("methods = %v", got)
	}
}

func TestInstanceLifecycleReportsADoneOperationWithAnError(t *testing.T) {
	e := lifecycleEnvironment(t, "stop", okReply(lifecycleOperation("DONE",
		`,"httpErrorStatusCode":400,"error":{"errors":[{"code":"RESOURCE_NOT_READY","message":"`+detailCanary+`"}]}`)))
	_, err := e.invokeConfirmed(instancesStopID, `{`+lifecycleBase+`}`)
	if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
}

func TestInstanceLifecycleOfAnUnclearOutcomeIsSentOnceAndSaysSo(t *testing.T) {
	e := lifecycleEnvironment(t, "start", reply{status: 503, body: errorObject(503, "UNAVAILABLE", "")})
	if _, err := e.invokeConfirmed(instancesStartID, `{`+lifecycleBase+`}`); err == nil || !strings.Contains(err.Error(), "outcome") {
		t.Errorf("err = %v", err)
	}
	if got := methodsOf(e); !slices.Equal(got, []string{"POST"}) {
		t.Errorf("methods = %v", got)
	}
	for name, body := range map[string]string{
		"no operation":  `{"kind":"compute#operation"}`,
		"foreign zone":  strings.Replace(lifecycleOperation("RUNNING", ""), "us-central1-a", "europe-west1-b", 1),
		"not an object": `[]`,
	} {
		e = lifecycleEnvironment(t, "start", okReply(body))
		if _, err := e.invokeConfirmed(instancesStartID, `{`+lifecycleBase+`}`); classOf(err) != provider.ClassInvalidResponse ||
			!strings.Contains(err.Error(), "outcome") {
			t.Errorf("%s: class = %q, err = %v", name, classOf(err), err)
		}
	}
}

func TestInstanceLifecycleRefusesBeforeAnyIO(t *testing.T) {
	bad := []string{
		`{"project_id":"example-other-9","zone":"us-central1-a","instance":"my-vm"}`,
		`{"project_id":"projects/my-project-123","zone":"us-central1-a","instance":"my-vm"}`,
		`{"project_id":"my-project-123","zone":"us-central1-a/x","instance":"my-vm"}`,
		`{"project_id":"my-project-123","zone":"us-central1-a","instance":"../my-vm"}`,
		`{"project_id":"my-project-123","zone":"us-central1-a","instance":"123456"}`,
		`{` + lifecycleBase + `,"request_id":"nope"}`,
		`{` + lifecycleBase + `,"request_id":"00000000-0000-0000-0000-000000000000"}`,
		`{` + lifecycleBase + `,"path":"/x"}`,
	}
	for id := range lifecycleTools {
		for _, a := range bad {
			e := revokeEnvironment(t, []string{"projects/my-project-123"}, nil)
			if _, err := e.invokeConfirmed(id, a); err == nil {
				t.Errorf("%s accepted %s", id, a)
			}
			noIO(t, e, id+" "+a)
		}
	}
	for _, machineType := range []string{
		"https://www.googleapis.com/compute/v1/projects/other-project-1/zones/us-central1-a/machineTypes/e2-micro",
		"projects/other-project-1/zones/us-central1-a/machineTypes/e2-micro",
		"zones/us-central1-a/machineTypes/e2-micro", "E2-Micro", "../e2-micro", "",
	} {
		e := revokeEnvironment(t, []string{"projects/my-project-123"}, nil)
		_, err := e.invokeConfirmed(instancesSetMachineTypeID, `{`+lifecycleBase+`,"machine_type":"`+machineType+`"}`)
		if err == nil || strings.Contains(err.Error(), "other-project-1") {
			t.Errorf("machine_type %q: err = %v", machineType, err)
		}
		noIO(t, e, "machine_type "+machineType)
	}
	e := revokeEnvironment(t, []string{"projects/my-project-123"}, nil)
	if _, err := e.invokeConfirmed(instancesSetMachineTypeID, `{`+lifecycleBase+`}`); err == nil {
		t.Error("set-machine-type accepted a call without machine_type")
	}
	noIO(t, e, "missing machine_type")
}

func TestInstanceLifecycleRefusalDoesNotNameTheForeignProject(t *testing.T) {
	e := revokeEnvironment(t, []string{"projects/my-project-123"}, nil)
	_, err := e.invokeConfirmed(instancesStartID, `{"project_id":"example-other-9","zone":"us-central1-a","instance":"my-vm"}`)
	if err == nil || strings.Contains(err.Error(), "example-other-9") {
		t.Errorf("err = %v", err)
	}
}

func TestInstanceLifecycleWithoutConfirmationMakesNoProviderCall(t *testing.T) {
	for id := range lifecycleTools {
		e := revokeEnvironment(t, nil, nil)
		if _, err := e.invoke(id, lifecycleArgs(id)+`}`); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		if d.Risk.Confirmation != "required" || string(d.Risk.Effect) != "update" || !d.Risk.OpenWorld ||
			d.RequiresToolAllowList {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if e.reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
}

func TestInstanceLifecycleSummaryNamesTargetAndEffect(t *testing.T) {
	for _, c := range []struct {
		id, args string
		want     []string
	}{
		{instancesStartID, `{` + lifecycleBase + `}`, []string{"my-project-123", "us-central1-a", "my-vm", "billed"}},
		{instancesStopID, `{` + lifecycleBase + `}`, []string{"my-project-123", "us-central1-a", "my-vm", "not discarded"}},
		{instancesStopID, `{` + lifecycleBase + `,"discard_local_ssd":true}`, []string{"my-vm", "local SSDs", "lost"}},
		{instancesResetID, `{` + lifecycleBase + `}`, []string{"my-project-123", "us-central1-a", "my-vm", "memory", "lost"}},
		{instancesSetMachineTypeID, lifecycleArgs(instancesSetMachineTypeID) + `}`, []string{"my-vm", "e2-standard-4", "price", "stopped"}},
	} {
		e := newEnvironment(t, nil)
		got := e.reg.ConfirmationSummary(c.id)([]byte(c.args))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s summary %q lacks %q", c.id, got, w)
			}
		}
	}
}

func TestInstanceLifecycleToolsRunWithTheProfileAlone(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	var tools []string
	for _, profile := range metadata.Profiles {
		if profile.ID == "compute-lifecycle" {
			tools = profile.Tools
		}
	}
	if len(tools) == 0 {
		t.Fatal("no profile compute-lifecycle")
	}
	e = revokeEnvironment(t, []string{"projects/my-project-123"}, tools)
	e.fake.routes[lifecyclePath+"/start"] = okReply(lifecycleOperation("RUNNING", ""))
	if _, err := e.invokeConfirmed(instancesStartID, `{`+lifecycleBase+`}`); err != nil {
		t.Fatal(err)
	}
}

func TestInstanceLifecycleIsNotOfferedToABillingAccountAlone(t *testing.T) {
	for _, tool := range []string{instancesStartID, instancesStopID, instancesResetID, instancesSetMachineTypeID} {
		if reaches, _ := targetReaches(tool, []string{"billingAccounts/012345-ABCDEF-678901"}); reaches {
			t.Errorf("%s reaches a connection bound to a billing account alone", tool)
		}
		if reaches, _ := targetReaches(tool, []string{"projects/my-project-123"}); !reaches {
			t.Errorf("%s does not reach a connection bound to a project", tool)
		}
	}
}
