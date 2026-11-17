package googlecloud

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	usageProject = "my-project-123"
	enablePath   = "/v1/projects/" + usageProject + "/services/compute.googleapis.com:enable"
	batchPath    = "/v1/projects/" + usageProject + "/services:batchEnable"
	disablePath  = "/v1/projects/" + usageProject + "/services/compute.googleapis.com:disable"
	runningOp    = `{"name":"operations/acf.p2-1","metadata":{"@type":"type.googleapis.com/x.OperationMetadata"}}`
	enableArgs   = `{"project_id":"` + usageProject + `","service":"compute.googleapis.com"}`
)

func TestServicesEnableReturnsTheOperationAndOperationsGetReadsIt(t *testing.T) {
	e := revokeEnvironment(t, nil, nil)
	e.fake.routes[enablePath] = okReply(runningOp)
	e.fake.routes["/v1/operations/acf.p2-1"] = okReply(`{"name":"operations/acf.p2-1","done":true,` +
		`"response":{"@type":"type.googleapis.com/google.protobuf.Empty"}}`)
	out, err := e.invokeConfirmed(servicesEnableID, enableArgs)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	if got["service"] != "service-usage" || got["name"] != "operations/acf.p2-1" || got["done"] != false {
		t.Errorf("result = %s", out)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != "POST" || api[0].body != "{}" ||
		api[0].url != "https://serviceusage.googleapis.com"+enablePath {
		t.Fatalf("requests = %+v", api)
	}
	out, err = e.invoke(operationsGetID, `{"service":"service-usage","name":"operations/acf.p2-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	got = decode(t, out)
	api = e.fake.api()
	if got["done"] != true || got["service"] != "service-usage" || len(api) != 2 || api[1].method != "GET" ||
		api[1].url != "https://serviceusage.googleapis.com/v1/operations/acf.p2-1" {
		t.Errorf("result = %s, requests = %+v", out, api)
	}
}

func TestOperationsGetOfServiceUsageReportsTheErrorOfTheOperation(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v1/operations/acf.p2-2"] = okReply(`{"name":"operations/acf.p2-2","done":true,` +
		`"error":{"code":7,"message":"` + detailCanary + `"}}`)
	_, err := e.invoke(operationsGetID, `{"service":"service-usage","name":"operations/acf.p2-2"}`)
	if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
}

func TestServicesEnableOfADoneOperationWithAnErrorIsAnError(t *testing.T) {
	e := revokeEnvironment(t, nil, nil)
	e.fake.routes[enablePath] = okReply(`{"name":"operations/acf.p2-3","done":true,"error":{"code":9,"message":"m"}}`)
	if _, err := e.invokeConfirmed(servicesEnableID, enableArgs); classOf(err) != provider.ClassProviderError {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
}

func TestServicesBatchEnableSendsTheIDsAndRefusesMoreThanTwenty(t *testing.T) {
	e := revokeEnvironment(t, nil, nil)
	e.fake.routes[batchPath] = okReply(runningOp)
	out, err := e.invokeConfirmed(servicesBatchEnableID,
		`{"project_id":"`+usageProject+`","service_ids":["compute.googleapis.com","storage.googleapis.com"]}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != "POST" ||
		api[0].body != `{"serviceIds":["compute.googleapis.com","storage.googleapis.com"]}` {
		t.Fatalf("requests = %+v", api)
	}
	if got := decode(t, out); got["name"] != "operations/acf.p2-1" || got["service"] != "service-usage" {
		t.Errorf("result = %s", out)
	}
	names := make([]string, 21)
	for i := range names {
		names[i] = `"s` + string(rune('a'+i)) + `.googleapis.com"`
	}
	for name, ids := range map[string]string{
		"21 services": strings.Join(names, ","),
		"none":        "",
		"duplicate":   `"a.googleapis.com","a.googleapis.com"`,
		"bad name":    `"a.googleapis.com","projects/x/services/b"`,
	} {
		e := revokeEnvironment(t, nil, nil)
		if _, err := e.invokeConfirmed(servicesBatchEnableID,
			`{"project_id":"`+usageProject+`","service_ids":[`+ids+`]}`); err == nil {
			t.Errorf("%s accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestServicesDisableSendsTheDocumentedMembers(t *testing.T) {
	for args, body := range map[string]string{
		`{"project_id":"` + usageProject + `","service":"compute.googleapis.com"}`: `{}`,
		`{"project_id":"` + usageProject + `","service":"compute.googleapis.com","disable_dependent_services":true,` +
			`"check_if_service_has_usage":"CHECK"}`: `{"disableDependentServices":true,"checkIfServiceHasUsage":"CHECK"}`,
		`{"project_id":"` + usageProject + `","service":"compute.googleapis.com","check_if_service_has_usage":"SKIP"}`: `{"checkIfServiceHasUsage":"SKIP"}`,
	} {
		e := revokeEnvironment(t, nil, []string{servicesDisableID})
		e.fake.routes[disablePath] = okReply(runningOp)
		out, err := e.invokeConfirmed(servicesDisableID, args)
		if err != nil {
			t.Fatal(err)
		}
		api := e.fake.api()
		if len(api) != 1 || api[0].method != "POST" || api[0].body != body {
			t.Errorf("requests = %+v, want body %s", api, body)
		}
		if got := decode(t, out); got["done"] != false || got["service"] != "service-usage" {
			t.Errorf("result = %s", out)
		}
	}
}

func TestServicesDisableIsOnlyReachableByName(t *testing.T) {
	for name, tools := range map[string][]string{
		"no tools list":    nil,
		"other tool named": {servicesEnableID},
		"namespace prefix": {"google.cloud.services.*"},
	} {
		e := revokeEnvironment(t, nil, tools)
		if _, err := e.invokeConfirmed(servicesDisableID, enableArgs); err == nil {
			t.Errorf("%s: services.disable was reachable", name)
		}
		noIO(t, e, name)
	}
	d, _, _ := newEnvironment(t, nil).reg.Lookup(servicesDisableID)
	if !d.RequiresToolAllowList {
		t.Error("services.disable is not listed-only")
	}
	for _, id := range []string{servicesEnableID, servicesBatchEnableID} {
		if d, _, _ := newEnvironment(t, nil).reg.Lookup(id); d.RequiresToolAllowList {
			t.Errorf("%s is listed-only", id)
		}
	}
}

func TestServicesProfileHoldsTheEnablesAndNotDisable(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		for _, tool := range profile.Tools {
			if tool == servicesDisableID {
				t.Errorf("profile %s holds %s", profile.ID, tool)
			}
		}
	}
	for _, id := range []string{servicesEnableID, servicesBatchEnableID, servicesDisableID} {
		if scopes := tokenMetadata.Scopes[id]; len(scopes) != 1 || scopes[0] != scopeCloudPlatform {
			t.Errorf("%s scopes = %v", id, scopes)
		}
	}
}

func TestServiceChangesNeedConfirmationAndAreIdempotentUpdates(t *testing.T) {
	for id, args := range map[string]string{
		servicesEnableID: enableArgs, servicesDisableID: enableArgs,
		servicesBatchEnableID: `{"project_id":"` + usageProject + `","service_ids":["compute.googleapis.com"]}`,
	} {
		e := revokeEnvironment(t, nil, []string{id})
		if _, err := e.invoke(id, args); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		if d.Risk.Confirmation != "required" || !d.Risk.OpenWorld || string(d.Risk.Effect) != "update" ||
			string(d.Risk.Idempotency) != "idempotent" {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if e.reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
}

func TestServiceChangeSummariesNameProjectAndServices(t *testing.T) {
	e := newEnvironment(t, nil)
	batch := `{"project_id":"` + usageProject + `","service_ids":["compute.googleapis.com","storage.googleapis.com"]}`
	disable := `{"project_id":"` + usageProject + `","service":"compute.googleapis.com","disable_dependent_services":true}`
	for _, c := range []struct {
		id, args string
		want     []string
	}{
		{servicesEnableID, enableArgs, []string{"Enable", "compute.googleapis.com", "billed"}},
		{servicesBatchEnableID, batch, []string{"Enable 2", "compute.googleapis.com", "storage.googleapis.com", "billed"}},
		{servicesDisableID, disable, []string{"Disable", "compute.googleapis.com", "depend on it are disabled as well"}},
		{servicesDisableID, enableArgs, []string{"Disable", "compute.googleapis.com"}},
	} {
		s := e.reg.ConfirmationSummary(c.id)(json.RawMessage(c.args))
		for _, part := range append(c.want, usageProject) {
			if !strings.Contains(s, part) {
				t.Errorf("%s summary %q lacks %q", c.id, s, part)
			}
		}
	}
	if s := e.reg.ConfirmationSummary(servicesDisableID)(json.RawMessage(enableArgs)); strings.Contains(s, "depend") {
		t.Errorf("summary %q names dependents without the argument", s)
	}
}

func TestServiceChangesRefuseBadInputBeforeIO(t *testing.T) {
	for _, id := range []string{servicesEnableID, servicesDisableID} {
		for name, args := range map[string]string{
			"other project":   `{"project_id":"example-other-9","service":"compute.googleapis.com"}`,
			"path service":    `{"project_id":"` + usageProject + `","service":"compute.googleapis.com/../x"}`,
			"projects name":   `{"project_id":"` + usageProject + `","service":"projects/1/services/a.googleapis.com"}`,
			"no dot":          `{"project_id":"` + usageProject + `","service":"compute"}`,
			"upper case":      `{"project_id":"` + usageProject + `","service":"Compute.googleapis.com"}`,
			"projects/ id":    `{"project_id":"projects/` + usageProject + `","service":"compute.googleapis.com"}`,
			"extra":           `{"project_id":"` + usageProject + `","service":"compute.googleapis.com","body":"x"}`,
			"no service":      `{"project_id":"` + usageProject + `"}`,
			"bad usage check": `{"project_id":"` + usageProject + `","service":"compute.googleapis.com","check_if_service_has_usage":"MAYBE"}`,
		} {
			e := revokeEnvironment(t, []string{"projects/" + usageProject}, []string{id})
			if _, err := e.invokeConfirmed(id, args); err == nil {
				t.Errorf("%s %s: accepted", id, name)
			}
			noIO(t, e, id+" "+name)
		}
	}
	e := revokeEnvironment(t, []string{"projects/" + usageProject}, nil)
	if _, err := e.invokeConfirmed(servicesBatchEnableID,
		`{"project_id":"example-other-9","service_ids":["compute.googleapis.com"]}`); err == nil {
		t.Error("batch-enable of another project accepted")
	}
	noIO(t, e, "batch-enable other project")
}

func TestServicesDisableUsageCheckFailureIsAnErrorOfGoogle(t *testing.T) {
	e := revokeEnvironment(t, nil, []string{servicesDisableID})
	e.fake.routes[disablePath] = reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")}
	_, err := e.invokeConfirmed(servicesDisableID, `{"project_id":"`+usageProject+`","service":"compute.googleapis.com",`+
		`"check_if_service_has_usage":"CHECK"}`)
	if err == nil || classOf(err) == "" || strings.Contains(err.Error(), detailCanary) || len(e.fake.api()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
}

func TestServiceChangeFailureNamesAnUnknownOutcomeAndIsNotRepeated(t *testing.T) {
	e := revokeEnvironment(t, nil, nil)
	e.fake.routes[enablePath] = reply{status: 503, body: `{}`}
	_, err := e.invokeConfirmed(servicesEnableID, enableArgs)
	if err == nil || !strings.Contains(err.Error(), "outcome") || len(e.fake.api()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
	e = revokeEnvironment(t, nil, nil)
	e.fake.routes[enablePath] = okReply(`{}`)
	_, err = e.invokeConfirmed(servicesEnableID, enableArgs)
	if err == nil || !strings.Contains(err.Error(), "outcome") || len(e.fake.api()) != 1 {
		t.Errorf("answer without operation: err = %v, requests = %d", err, len(e.fake.api()))
	}
}

func TestServiceChangeOfAProjectBelowAFolderIsProvedByReadingIt(t *testing.T) {
	e := revokeEnvironment(t, []string{"folders/2001"}, nil)
	e.fake.routes["/v3/projects/"+usageProject] = okReply(projectBody)
	e.fake.routes[enablePath] = okReply(runningOp)
	if _, err := e.invokeConfirmed(servicesEnableID, enableArgs); err != nil {
		t.Fatal(err)
	}
	if api := e.fake.api(); len(api) != 2 || api[0].method != "GET" || api[1].method != "POST" {
		t.Errorf("requests = %+v", api)
	}
}
