package googlecloud

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	deleteProject   = "my-project-123"
	deleteRequestID = "3f0c7a52-9a4e-4c1b-8d2f-5b6a7c8d9e01"
	firewallRule    = "allow-ssh"
	firewallPath    = firewallsPath + "/" + firewallRule
	addressName     = "web-ip"
	addressPath     = addressesPath + "/" + addressName
	operationName   = "operation-1700000000000-abc123-def456"
	firewallArgs    = `{"project_id":"my-project-123","firewall":"allow-ssh","expected_direction":"INGRESS",` +
		`"expected_action":"allow","expected_priority":1000`
	addressArgs = `{"project_id":"my-project-123","region":"us-central1","address":"web-ip"`
)

func deleteOperationBody(status, extra string) string {
	return `{"kind":"compute#operation","id":"9100000000000000001","name":"` + operationName + `",` +
		`"operationType":"delete","status":"` + status + `","user":"person@example.com","progress":0,` +
		`"targetLink":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/firewalls/allow-ssh",` +
		`"selfLink":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/operations/` + operationName + `"` +
		extra + `}`
}

func regionalDeleteOperationBody(status string) string {
	return `{"kind":"compute#operation","name":"` + operationName + `","operationType":"delete","status":"` + status + `",` +
		`"region":"https://www.googleapis.com/compute/v1/projects/my-project-123/regions/us-central1"}`
}

// deleteRoutes answers the read of the rule with the rule and the DELETE with deleted.
func deleteRoutes(e *environment, path, rule string, deleted reply) {
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != path {
			return nil
		}
		if req.Method == http.MethodGet {
			return &reply{status: 200, body: rule}
		}
		return &deleted
	}
}

func methodsOf(e *environment) []string {
	var out []string
	for _, r := range e.fake.api() {
		out = append(out, r.method)
	}
	return out
}

func TestFirewallsDeleteReadsTheRuleAndReturnsTheRunningOperation(t *testing.T) {
	e := revokeEnvironment(t, nil, nil)
	deleteRoutes(e, firewallPath, ingressRuleBody(firewallRule), okReply(deleteOperationBody("RUNNING", "")))
	out, err := e.invokeConfirmed(firewallsDeleteID, firewallArgs+`,"request_id":"`+deleteRequestID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].method != "GET" || api[1].method != "DELETE" || api[1].body != "" ||
		api[1].url != "https://compute.googleapis.com"+firewallPath+"?requestId="+deleteRequestID {
		t.Fatalf("requests = %+v", api)
	}
	got := decode(t, out)
	operation, _ := got["operation"].(map[string]any)
	if operation["status"] != "RUNNING" || operation["name"] != operationName || got["scope"] != "global" ||
		got["request_id"] != deleteRequestID {
		t.Errorf("result = %s", out)
	}
}

func TestDeleteWithTheSameRequestIDSendsItUnchangedEachTime(t *testing.T) {
	e := revokeEnvironment(t, nil, nil)
	deleteRoutes(e, addressPath, "", okReply(regionalDeleteOperationBody("RUNNING")))
	for i := 0; i < 2; i++ {
		out, err := e.invokeConfirmed(addressesDeleteID, addressArgs+`,"request_id":"`+deleteRequestID+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		if got := decode(t, out); got["request_id"] != deleteRequestID || got["scope"] != "region" ||
			got["region"] != "us-central1" {
			t.Errorf("result = %s", out)
		}
	}
	api := e.fake.api()
	if len(api) != 2 {
		t.Fatalf("requests = %+v", api)
	}
	for _, r := range api {
		u, _ := url.Parse(r.url)
		if r.method != "DELETE" || u.Path != addressPath || u.Query().Get("requestId") != deleteRequestID ||
			len(u.Query()) != 1 {
			t.Errorf("request = %+v", r)
		}
	}
}

func TestDeleteCreatesARequestIDWhenNoneIsGiven(t *testing.T) {
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	e := revokeEnvironment(t, nil, nil)
	deleteRoutes(e, addressPath, "", okReply(regionalDeleteOperationBody("PENDING")))
	out, err := e.invokeConfirmed(addressesDeleteID, addressArgs+`}`)
	if err != nil {
		t.Fatal(err)
	}
	sent, _ := url.Parse(e.fake.api()[0].url)
	id, _ := decode(t, out)["request_id"].(string)
	if !uuid.MatchString(id) || sent.Query().Get("requestId") != id {
		t.Errorf("request_id = %q, sent %q", id, sent.Query().Get("requestId"))
	}
}

func TestFirewallsDeleteDeletesOnlyWhenTheRuleStillMatches(t *testing.T) {
	// ingressRuleBody is an INGRESS allow rule with priority 1000; egressRuleBody an EGRESS deny rule with 900.
	for name, c := range map[string]struct{ rule, args string }{
		"action":    {ingressRuleBody(firewallRule), `"expected_direction":"INGRESS","expected_action":"deny","expected_priority":1000`},
		"direction": {ingressRuleBody(firewallRule), `"expected_direction":"EGRESS","expected_action":"allow","expected_priority":1000`},
		"priority":  {ingressRuleBody(firewallRule), `"expected_direction":"INGRESS","expected_action":"allow","expected_priority":999`},
		"deny rule": {egressRuleBody(firewallRule), `"expected_direction":"EGRESS","expected_action":"allow","expected_priority":900`},
	} {
		e := revokeEnvironment(t, nil, nil)
		deleteRoutes(e, firewallPath, c.rule, okReply(deleteOperationBody("RUNNING", "")))
		_, err := e.invokeConfirmed(firewallsDeleteID, `{"project_id":"my-project-123","firewall":"allow-ssh",`+c.args+`}`)
		var invalid *provider.InvalidRequestError
		if err == nil || !strings.Contains(err.Error(), "firewalls.get") || !asInvalid(err, &invalid) {
			t.Errorf("%s: err = %v", name, err)
		}
		if got := methodsOf(e); !slices.Equal(got, []string{"GET"}) {
			t.Errorf("%s: methods = %v, want only the read", name, got)
		}
	}
	e := revokeEnvironment(t, nil, nil)
	deleteRoutes(e, firewallPath, egressRuleBody(firewallRule), okReply(deleteOperationBody("RUNNING", "")))
	if _, err := e.invokeConfirmed(firewallsDeleteID, `{"project_id":"my-project-123","firewall":"allow-ssh",`+
		`"expected_direction":"EGRESS","expected_action":"deny","expected_priority":900}`); err != nil {
		t.Fatal(err)
	}
	if got := methodsOf(e); !slices.Equal(got, []string{"GET", "DELETE"}) {
		t.Errorf("methods = %v", got)
	}
}

func asInvalid(err error, target **provider.InvalidRequestError) bool {
	e, ok := err.(*provider.InvalidRequestError)
	*target = e
	return ok
}

func TestFirewallsDeleteSummaryNamesTheThreeValuesAndTheDenyRisk(t *testing.T) {
	e := newEnvironment(t, nil)
	summary := e.reg.ConfirmationSummary(firewallsDeleteID)
	if summary == nil {
		t.Fatal("no confirmation summary")
	}
	deny := summary(json.RawMessage(`{"project_id":"my-project-123","firewall":"block-db","expected_direction":"EGRESS",` +
		`"expected_action":"deny","expected_priority":900}`))
	for _, part := range []string{"my-project-123", "firewall rule", "block-db", "EGRESS", "deny", "900", "open access"} {
		if !strings.Contains(deny, part) {
			t.Errorf("deny summary %q lacks %q", deny, part)
		}
	}
	allow := summary(json.RawMessage(firewallArgs + `}`))
	if strings.Contains(allow, "open access") || !strings.Contains(allow, "allow") || !strings.Contains(allow, "1000") {
		t.Errorf("allow summary = %q", allow)
	}
	text := e.reg.ConfirmationSummary(addressesDeleteID)(json.RawMessage(addressArgs + `}`))
	for _, part := range []string{"my-project-123", "address", "us-central1", "web-ip"} {
		if !strings.Contains(text, part) {
			t.Errorf("address summary %q lacks %q", text, part)
		}
	}
}

func TestAddressInUseIsTheErrorOfGoogleAndNothingIsRepeated(t *testing.T) {
	e := revokeEnvironment(t, nil, nil)
	deleteRoutes(e, addressPath, "", reply{status: 400, body: `{"error":{"code":400,"message":"` + detailCanary +
		`","errors":[{"message":"` + detailCanary + `","domain":"global","reason":"resourceInUseByAnotherResource"}]}}`})
	_, err := e.invokeConfirmed(addressesDeleteID, addressArgs+`}`)
	if err == nil || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("err = %v", err)
	}
	if got := methodsOf(e); !slices.Equal(got, []string{"DELETE"}) {
		t.Errorf("methods = %v", got)
	}
}

func TestDeleteOfAnUnknownNameIsNotFound(t *testing.T) {
	e := revokeEnvironment(t, nil, nil)
	if _, err := e.invokeConfirmed(firewallsDeleteID, firewallArgs+`}`); classOf(err) != provider.ClassNotFound {
		t.Errorf("firewall: class = %q, err = %v", classOf(err), err)
	}
	if got := methodsOf(e); !slices.Equal(got, []string{"GET"}) {
		t.Errorf("firewall methods = %v", got)
	}
	e = revokeEnvironment(t, nil, nil)
	if _, err := e.invokeConfirmed(addressesDeleteID, addressArgs+`}`); classOf(err) != provider.ClassNotFound {
		t.Errorf("address: class = %q, err = %v", classOf(err), err)
	}
}

func TestDeleteReportsAnOperationThatIsDoneWithAnError(t *testing.T) {
	e := revokeEnvironment(t, nil, nil)
	deleteRoutes(e, addressPath, "", okReply(regionalDeleteOperationBody("DONE")[:len(regionalDeleteOperationBody("DONE"))-1]+
		`,"httpErrorStatusCode":400,"error":{"errors":[{"code":"RESOURCE_IN_USE_BY_ANOTHER_RESOURCE",`+
		`"message":"`+detailCanary+`"}]}}`))
	_, err := e.invokeConfirmed(addressesDeleteID, addressArgs+`}`)
	if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
}

func TestDeleteOfAnUnclearOutcomeIsSentOnceAndSaysSo(t *testing.T) {
	e := revokeEnvironment(t, nil, nil)
	deleteRoutes(e, addressPath, "", reply{status: 503, body: errorObject(503, "UNAVAILABLE", "")})
	_, err := e.invokeConfirmed(addressesDeleteID, addressArgs+`}`)
	if err == nil || !strings.Contains(err.Error(), "outcome") {
		t.Errorf("err = %v", err)
	}
	if got := methodsOf(e); !slices.Equal(got, []string{"DELETE"}) {
		t.Errorf("methods = %v", got)
	}
	e = revokeEnvironment(t, nil, nil)
	deleteRoutes(e, addressPath, "", okReply(`{"kind":"compute#operation"}`))
	if _, err := e.invokeConfirmed(addressesDeleteID, addressArgs+`}`); classOf(err) != provider.ClassInvalidResponse ||
		!strings.Contains(err.Error(), "outcome") {
		t.Errorf("no operation: class = %q, err = %v", classOf(err), err)
	}
}

func TestDeleteRefusesBeforeAnyIO(t *testing.T) {
	args := map[string][]string{
		firewallsDeleteID: {
			firewallArgs + `,"request_id":"not-a-uuid"}`,
			firewallArgs + `,"request_id":"00000000-0000-0000-0000-000000000000"}`,
			`{"project_id":"my-project-123","firewall":"../x","expected_direction":"INGRESS","expected_action":"allow","expected_priority":1}`,
			`{"project_id":"projects/my-project-123","firewall":"allow-ssh","expected_direction":"INGRESS","expected_action":"allow","expected_priority":1}`,
			`{"project_id":"my-project-123","firewall":"allow-ssh","expected_direction":"INGRESS","expected_action":"allow"}`,
			`{"project_id":"my-project-123","firewall":"allow-ssh","expected_direction":"INGRESS","expected_action":"allow","expected_priority":65536}`,
			`{"project_id":"my-project-123","firewall":"allow-ssh","expected_direction":"in","expected_action":"allow","expected_priority":1}`,
			`{"project_id":"my-project-123","firewall":"allow-ssh","expected_direction":"INGRESS","expected_action":"permit","expected_priority":1}`,
			firewallArgs + `,"path":"/x"}`,
			// A project outside the targets of the connection.
			`{"project_id":"example-other-9","firewall":"allow-ssh","expected_direction":"INGRESS","expected_action":"allow","expected_priority":1}`,
		},
		addressesDeleteID: {
			addressArgs + `,"request_id":"nope"}`,
			`{"project_id":"my-project-123","region":"us-central1/x","address":"web-ip"}`,
			`{"project_id":"my-project-123","region":"us-central1","address":"203.0.113.10"}`,
			`{"project_id":"my-project-123","address":"web-ip"}`,
			`{"project_id":"example-other-9","region":"us-central1","address":"web-ip"}`,
		},
	}
	for id, list := range args {
		for _, a := range list {
			e := revokeEnvironment(t, []string{"projects/my-project-123"}, nil)
			if _, err := e.invokeConfirmed(id, a); err == nil {
				t.Errorf("%s accepted %s", id, a)
			}
			noIO(t, e, id+" "+a)
		}
	}
}

func TestDeleteWithoutConfirmationMakesNoProviderCall(t *testing.T) {
	for id, args := range map[string]string{firewallsDeleteID: firewallArgs + `}`, addressesDeleteID: addressArgs + `}`} {
		e := revokeEnvironment(t, nil, nil)
		if _, err := e.invoke(id, args); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		if d.Risk.Confirmation != "required" || string(d.Risk.Effect) != "delete" || !d.Risk.OpenWorld ||
			string(d.Risk.Idempotency) != "idempotent" || d.RequiresToolAllowList {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if e.reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
}

func TestDeleteToolsRunWithTheProfileAlone(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	var tools []string
	for _, profile := range metadata.Profiles {
		if profile.ID == "compute-network-delete" {
			tools = profile.Tools
		}
	}
	if len(tools) == 0 {
		t.Fatal("no profile compute-network-delete")
	}
	e = revokeEnvironment(t, []string{"projects/my-project-123"}, tools)
	deleteRoutes(e, addressPath, "", okReply(regionalDeleteOperationBody("RUNNING")))
	if _, err := e.invokeConfirmed(addressesDeleteID, addressArgs+`}`); err != nil {
		t.Fatal(err)
	}
	deleteRoutes(e, firewallPath, ingressRuleBody(firewallRule), okReply(deleteOperationBody("RUNNING", "")))
	if _, err := e.invokeConfirmed(firewallsDeleteID, firewallArgs+`}`); err != nil {
		t.Fatal(err)
	}
}

// TestOnlyDeletionsOfFirewallsAddressesAndVPNExist proves decision 4 for the writing side: no registered tool
// creates, changes, or adds an access config to a firewall rule, address, VPN tunnel, or VPN gateway, and the
// only tools of these groups that change anything are deletions that need a confirmation.
func TestOnlyDeletionsOfFirewallsAddressesAndVPNExist(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	groups := regexp.MustCompile(`\.(firewalls|addresses|global-addresses|vpn-tunnels|vpn-gateways|external-vpn-gateways|vpn-tunnel|vpn-gateway)\.`)
	verbs := regexp.MustCompile(`\.(insert|create|patch|update|set-[a-z-]+|add-[a-z-]+)$`)
	for _, tool := range metadata.Tools {
		if strings.Contains(tool.ID, "access-config") {
			t.Errorf("tool %s changes an access config", tool.ID)
		}
		if !groups.MatchString(tool.ID) {
			continue
		}
		if verbs.MatchString(tool.ID) {
			t.Errorf("tool %s creates or changes a firewall rule, address, or VPN resource", tool.ID)
		}
		switch {
		case strings.HasSuffix(tool.ID, ".list"), strings.HasSuffix(tool.ID, ".get"):
		case strings.HasSuffix(tool.ID, ".delete"):
			d, _, _ := e.reg.Lookup(tool.ID)
			if string(d.Risk.Effect) != "delete" || d.Risk.Confirmation != "required" || !d.Risk.OpenWorld ||
				d.RequiresToolAllowList {
				t.Errorf("%s risk = %+v", tool.ID, d.Risk)
			}
		default:
			t.Errorf("tool %s is neither a read nor a deletion", tool.ID)
		}
	}
}
