package googlecloud

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	vpnRegionPath = "/compute/v1/projects/my-project-123/regions/us-central1"
	vpnSecret     = "synthetic-shared-secret-value"
)

var vpnDeleteCases = []struct {
	id, argument, segment string
}{
	{vpnTunnelsDeleteID, "vpn_tunnel", "vpnTunnels"},
	{vpnGatewaysDeleteID, "vpn_gateway", "vpnGateways"},
}

func vpnDeleteArgs(argument, extra string) string {
	return `{"project_id":"my-project-123","region":"us-central1","` + argument + `":"example-vpn"` + extra + `}`
}

func TestVPNDeleteReturnsTheRunningOperationAndRepeatsWithTheSameRequestID(t *testing.T) {
	for _, c := range vpnDeleteCases {
		path := vpnRegionPath + "/" + c.segment + "/example-vpn"
		e := revokeEnvironment(t, nil, networkDeleteTools)
		deleteRoutes(e, path, "", okReply(regionalDeleteOperationBody("RUNNING")))
		for i := 0; i < 2; i++ {
			out, err := e.invokeConfirmed(c.id, vpnDeleteArgs(c.argument, `,"request_id":"`+deleteRequestID+`"`))
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			operation, _ := got["operation"].(map[string]any)
			if operation["status"] != "RUNNING" || got["scope"] != "region" || got["region"] != "us-central1" ||
				got["request_id"] != deleteRequestID {
				t.Errorf("%s result = %s", c.id, out)
			}
		}
		api := e.fake.api()
		if len(api) != 2 {
			t.Fatalf("%s requests = %+v", c.id, api)
		}
		for _, r := range api {
			u, _ := url.Parse(r.url)
			if r.method != "DELETE" || u.Path != path || u.Query().Get("requestId") != deleteRequestID ||
				len(u.Query()) != 1 || r.body != "" {
				t.Errorf("%s request = %+v", c.id, r)
			}
		}
	}
}

func TestVPNDeleteOfAnUnknownNameIsNotFound(t *testing.T) {
	for _, c := range vpnDeleteCases {
		e := revokeEnvironment(t, nil, networkDeleteTools)
		if _, err := e.invokeConfirmed(c.id, vpnDeleteArgs(c.argument, "")); classOf(err) != provider.ClassNotFound {
			t.Errorf("%s: class = %q, err = %v", c.id, classOf(err), err)
		}
		if got := methodsOf(e); !slices.Equal(got, []string{"DELETE"}) {
			t.Errorf("%s methods = %v", c.id, got)
		}
	}
}

func TestVPNDeleteRefusesBeforeAnyIOAndWithoutConfirmation(t *testing.T) {
	for _, c := range vpnDeleteCases {
		for _, a := range []string{
			`{"project_id":"my-project-123","region":"us-central1/x","` + c.argument + `":"example-vpn"}`,
			`{"project_id":"my-project-123","region":"US","` + c.argument + `":"example-vpn"}`,
			vpnDeleteArgs(c.argument, `,"request_id":"nope"`),
			`{"project_id":"my-project-123","region":"us-central1","` + c.argument + `":"../x"}`,
			`{"project_id":"example-other-9","region":"us-central1","` + c.argument + `":"example-vpn"}`,
		} {
			e := revokeEnvironment(t, []string{"projects/my-project-123"}, networkDeleteTools)
			if _, err := e.invokeConfirmed(c.id, a); err == nil {
				t.Errorf("%s accepted %s", c.id, a)
			}
			noIO(t, e, c.id+" "+a)
		}
		e := revokeEnvironment(t, nil, networkDeleteTools)
		if _, err := e.invoke(c.id, vpnDeleteArgs(c.argument, "")); err == nil {
			t.Errorf("%s ran without confirmation", c.id)
		}
		noIO(t, e, c.id)
		d, _, _ := e.reg.Lookup(c.id)
		if d.Risk.Confirmation != "required" || string(d.Risk.Effect) != "delete" || !d.Risk.OpenWorld ||
			string(d.Risk.Idempotency) != "idempotent" || d.RequiresToolAllowList != listedNetworkDelete(d.ID) {
			t.Errorf("%s risk = %+v", c.id, d.Risk)
		}
		text := e.reg.ConfirmationSummary(c.id)([]byte(vpnDeleteArgs(c.argument, "")))
		for _, part := range []string{"my-project-123", "us-central1", "example-vpn"} {
			if !strings.Contains(text, part) {
				t.Errorf("%s summary %q lacks %q", c.id, text, part)
			}
		}
	}
}

func TestVPNDeleteNeverPassesASecretOrProviderText(t *testing.T) {
	for _, c := range vpnDeleteCases {
		path := vpnRegionPath + "/" + c.segment + "/example-vpn"
		e := revokeEnvironment(t, nil, networkDeleteTools)
		deleteRoutes(e, path, "", okReply(regionalDeleteOperationBody("RUNNING")[:len(regionalDeleteOperationBody("RUNNING"))-1]+
			`,"sharedSecret":"`+vpnSecret+`","statusMessage":"ok"}`))
		out, err := e.invokeConfirmed(c.id, vpnDeleteArgs(c.argument, ""))
		if err != nil || strings.Contains(out, vpnSecret) || strings.Contains(out, "sharedSecret") {
			t.Errorf("%s result = %s, err = %v", c.id, out, err)
		}
		for _, r := range []reply{
			{status: 400, body: `{"error":{"code":400,"message":"` + vpnSecret + `","errors":[{"message":"` + vpnSecret +
				`","reason":"resourceInUseByAnotherResource"}]}}`},
			okReply(regionalDeleteOperationBody("DONE")[:len(regionalDeleteOperationBody("DONE"))-1] +
				`,"httpErrorStatusCode":400,"error":{"errors":[{"code":"X","message":"` + vpnSecret + `"}]}}`),
		} {
			e := revokeEnvironment(t, nil, networkDeleteTools)
			deleteRoutes(e, path, "", r)
			_, err := e.invokeConfirmed(c.id, vpnDeleteArgs(c.argument, ""))
			if err == nil || strings.Contains(err.Error(), vpnSecret) {
				t.Fatalf("%s err = %v", c.id, err)
			}
			var failed *provider.Error
			if asProviderError(err, &failed) && failed.Original != nil {
				for _, entry := range failed.Original.Entries {
					if entry.Message != "" {
						t.Errorf("%s keeps provider text %q", c.id, entry.Message)
					}
				}
			}
		}
	}
}

func asProviderError(err error, target **provider.Error) bool {
	e, ok := err.(*provider.Error)
	*target = e
	return ok
}
