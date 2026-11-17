package googlecloud

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	vpnGatewaysPath = "/compute/v1/projects/my-project-123/regions/us-central1/vpnGateways"
	vpnTunnelsPath  = "/compute/v1/projects/my-project-123/regions/us-central1/vpnTunnels"

	// Canary values are synthetic and no credential. A result or an error that holds one has leaked a secret member.
	secretCanary     = "canary-shared-value-not-a-secret"
	secretHashCanary = "canary-hash-value-not-a-secret"
	nestedCanary     = "canary-nested-value-not-a-secret"
)

var vpnTools = []string{vpnGatewaysListID, vpnGatewaysGetID, vpnTunnelsListID, vpnTunnelsGetID}

func vpnGatewayBody(name string) string {
	return `{"kind":"compute#vpnGateway","id":"7100000000000000001","name":"` + name + `","description":"d",` +
		`"region":"https://www.googleapis.com/compute/v1/projects/my-project-123/regions/us-central1",` +
		`"network":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/networks/vpc-a",` +
		`"vpnInterfaces":[{"id":0,"ipAddress":"203.0.113.10"},` +
		`{"id":1,"ipAddress":"198.51.100.11","interconnectAttachment":"https://x/attachments/att-a"}],` +
		`"stackType":"IPV4_ONLY","gatewayIpVersion":"IPV4","labels":{"env":"test"},"labelFingerprint":"lfp",` +
		`"selfLink":"s","creationTimestamp":"2026-03-04T05:06:07.000-08:00","params":{"resourceManagerTags":{"k":"v"}},` +
		`"unknownMember":"x"}`
}

func vpnTunnelBody(name, status, detail string) string {
	return `{"kind":"compute#vpnTunnel","id":"7200000000000000001","name":"` + name + `",` +
		`"region":"https://www.googleapis.com/compute/v1/projects/my-project-123/regions/us-central1",` +
		`"vpnGateway":"https://x/vpnGateways/ha-vpn-a","vpnGatewayInterface":0,"peerExternalGateway":"https://x/ext/peer-a",` +
		`"peerExternalGatewayInterface":1,"peerIp":"192.0.2.20","ikeVersion":2,"status":"` + status + `",` +
		`"detailedStatus":"` + detail + `","router":"https://x/routers/rt-a","localTrafficSelector":["0.0.0.0/0"],` +
		`"remoteTrafficSelector":["10.0.0.0/8","172.16.0.0/12"],"labels":{"env":"test"},"labelFingerprint":"lfp",` +
		`"cipherSuite":{"phase1":{"encryption":["AES-GCM-16-256"]},"sharedSecret":"` + nestedCanary + `"},` +
		`"sharedSecret":"` + secretCanary + `","sharedSecretHash":"` + secretHashCanary + `","selfLink":"s",` +
		`"params":{"x":"y"},"unknownMember":"x"}`
}

func absentSecrets(t *testing.T, what, out string) {
	t.Helper()
	for _, canary := range []string{secretCanary, secretHashCanary, nestedCanary, "sharedSecret"} {
		if strings.Contains(out, canary) {
			t.Errorf("%s holds %s: %s", what, canary, out)
		}
	}
}

func TestVpnGatewaysListFollowsPagesAndKeepsBothInterfaces(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != vpnGatewaysPath {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"items":[` + vpnGatewayBody("gw-a") + `],"nextPageToken":"p2"}`}
		case "p2":
			return &reply{status: 200, body: `{"items":[` + vpnGatewayBody("gw-b") + `],"nextPageToken":"p3",` +
				`"warning":{"code":"X","message":"m"}}`}
		}
		return &reply{status: 200, body: `{"items":[` + vpnGatewayBody("gw-c") + `]}`}
	}
	var names []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		arguments := `{"project_id":"my-project-123","region":"us-central1","filter":"name != x","max_results":1,` +
			`"order_by":"name desc","return_partial_success":true`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(vpnGatewaysListID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		rows, got := listOf(t, out, "vpn_gateways")
		for _, row := range rows {
			names = append(names, row["name"].(string))
			for _, dropped := range []string{"params", "unknownMember"} {
				if _, found := row[dropped]; found {
					t.Errorf("page %d returns %s", page, dropped)
				}
			}
			interfaces, _ := row["vpnInterfaces"].([]any)
			if len(interfaces) != 2 || interfaces[0].(map[string]any)["ipAddress"] != "203.0.113.10" ||
				interfaces[1].(map[string]any)["ipAddress"] != "198.51.100.11" ||
				interfaces[1].(map[string]any)["interconnectAttachment"] != "https://x/attachments/att-a" ||
				row["stackType"] != "IPV4_ONLY" || row["gatewayIpVersion"] != "IPV4" ||
				row["labels"].(map[string]any)["env"] != "test" || row["labelFingerprint"] != "lfp" {
				t.Errorf("row = %v", row)
			}
		}
		if page == 2 && got["warning"] == nil {
			t.Error("warning not passed on")
		}
		cursor, _ = got["next_cursor"].(string)
		if got["has_more"].(bool) != (page < 3) {
			t.Fatalf("page %d: has_more = %v", page, got["has_more"])
		}
	}
	if !slices.Equal(names, []string{"gw-a", "gw-b", "gw-c"}) {
		t.Errorf("names = %v", names)
	}
	for i, q := range queriesOf(e) {
		if q.Get("filter") != "name != x" || q.Get("maxResults") != "1" || q.Get("orderBy") != "name desc" ||
			q.Get("returnPartialSuccess") != "true" {
			t.Errorf("request %d: %v", i, q)
		}
	}
}

func TestVpnGatewaysGetByNameAndID(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[vpnGatewaysPath+"/gw-a"] = okReply(vpnGatewayBody("gw-a"))
	e.fake.routes[vpnGatewaysPath+"/7100000000000000001"] = okReply(vpnGatewayBody("gw-a"))
	for _, target := range []string{"gw-a", "7100000000000000001"} {
		out, err := e.invoke(vpnGatewaysGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_gateway":"`+target+`"}`)
		if err != nil || !strings.Contains(out, "198.51.100.11") || strings.Contains(out, "params") {
			t.Errorf("%s: %s, %v", target, out, err)
		}
	}
	e.fake.routes[vpnGatewaysPath+"/gw-z"] = okReply(vpnGatewayBody("gw-a"))
	if _, err := e.invoke(vpnGatewaysGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_gateway":"gw-z"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("answer for another gateway: %v", err)
	}
}

func TestVpnTunnelsListKeepsStatusAndFollowsPages(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != vpnTunnelsPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"items":[` + vpnTunnelBody("tun-a", "ESTABLISHED", "Tunnel is up and running.") +
				`],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"items":[` + vpnTunnelBody("tun-b", "NEGOTIATION_FAILURE", "Phase 1 failed.") + `]}`}
	}
	base := `{"project_id":"my-project-123","region":"us-central1","max_results":1`
	out, err := e.invoke(vpnTunnelsListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "vpn_tunnels")
	absentSecrets(t, "list page 1", out)
	if len(rows) != 1 || rows[0]["status"] != "ESTABLISHED" || rows[0]["peerIp"] != "192.0.2.20" ||
		rows[0]["ikeVersion"] != float64(2) || rows[0]["vpnGatewayInterface"] != float64(0) ||
		rows[0]["peerExternalGatewayInterface"] != float64(1) || len(rows[0]["remoteTrafficSelector"].([]any)) != 2 ||
		rows[0]["router"] != "https://x/routers/rt-a" || rows[0]["vpnGateway"] != "https://x/vpnGateways/ha-vpn-a" ||
		rows[0]["peerExternalGateway"] != "https://x/ext/peer-a" || rows[0]["cipherSuite"] == nil {
		t.Errorf("row = %v", rows)
	}
	if _, found := rows[0]["params"]; found {
		t.Error("input-only member returned")
	}
	if got["has_more"] != true {
		t.Fatal("no second page")
	}
	out, err = e.invoke(vpnTunnelsListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "vpn_tunnels")
	absentSecrets(t, "list page 2", out)
	if len(rows) != 1 || rows[0]["status"] != "NEGOTIATION_FAILURE" || rows[0]["detailedStatus"] != "Phase 1 failed." ||
		got["has_more"] != false {
		t.Errorf("row = %v, %v", rows, got)
	}
}

func TestVpnTunnelsGetKeepsStatus(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[vpnTunnelsPath+"/tun-a"] = okReply(vpnTunnelBody("tun-a", "ESTABLISHED", "up"))
	e.fake.routes[vpnTunnelsPath+"/tun-b"] = okReply(vpnTunnelBody("tun-b", "FAILED", "down"))
	out, err := e.invoke(vpnTunnelsGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_tunnel":"tun-a"}`)
	if err != nil || !strings.Contains(out, `"status":"ESTABLISHED"`) {
		t.Errorf("established: %s, %v", out, err)
	}
	out, err = e.invoke(vpnTunnelsGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_tunnel":"tun-b"}`)
	if err != nil || !strings.Contains(out, `"status":"FAILED"`) || !strings.Contains(out, `"detailedStatus":"down"`) {
		t.Errorf("failed: %s, %v", out, err)
	}
}

func TestVpnTunnelsNeverReturnSharedSecrets(t *testing.T) {
	e := newEnvironment(t, nil)
	nested := `{"name":"tun-a","region":"https://x/compute/v1/projects/p/regions/us-central1","labels":{"SharedSecret":"` +
		nestedCanary + `"},"cipherSuite":{"phase2":[{"sharedSecretHash":"` + secretHashCanary + `"}]}}`
	e.fake.routes[vpnTunnelsPath] = okReply(`{"items":[` + vpnTunnelBody("tun-a", "ESTABLISHED", "up") + `,` + nested + `],` +
		`"warning":{"code":"X","data":[{"key":"k","value":"v","sharedSecret":"` + secretCanary + `"}]}}`)
	e.fake.routes[vpnTunnelsPath+"/tun-a"] = okReply(vpnTunnelBody("tun-a", "ESTABLISHED", "up"))
	for id, arguments := range map[string]string{
		vpnTunnelsListID: `{"project_id":"my-project-123","region":"us-central1"}`,
		vpnTunnelsGetID:  `{"project_id":"my-project-123","region":"us-central1","vpn_tunnel":"tun-a"}`,
	} {
		out, err := e.invoke(id, arguments)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		absentSecrets(t, id, out)
		if !strings.Contains(out, `"AES-GCM-16-256"`) {
			t.Errorf("%s: the rest of cipherSuite was dropped: %s", id, out)
		}
	}
	// Neither the schemas nor the descriptions of any VPN tool name a secret member as returned.
	for _, id := range vpnTools {
		d, _, _ := e.reg.Lookup(id)
		schema := string(d.OutputSchema)
		if strings.Contains(strings.ToLower(schema), "sharedsecret") {
			t.Errorf("%s: output schema names a secret member", id)
		}
	}
	for _, member := range [][]computeMember{vpnGatewayMembers, vpnTunnelMembers} {
		for _, m := range member {
			if vpnSecretKey(m.name) {
				t.Errorf("member %s is a secret", m.name)
			}
		}
	}
}

func TestVpnToolsKeepSecretsOutOfErrors(t *testing.T) {
	body := `{"error":{"code":400,"message":"` + secretCanary + `","status":"FAILED_PRECONDITION","errors":[{"message":"` +
		secretHashCanary + `","reason":"badRequest"}],"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",` +
		`"reason":"R","domain":"d","metadata":{"sharedSecret":"` + nestedCanary + `"}}]}}`
	for _, status := range []int{400, 403, 404, 500} {
		for id, arguments := range map[string]string{
			vpnGatewaysListID: `{"project_id":"my-project-123","region":"us-central1"}`,
			vpnGatewaysGetID:  `{"project_id":"my-project-123","region":"us-central1","vpn_gateway":"gw-a"}`,
			vpnTunnelsListID:  `{"project_id":"my-project-123","region":"us-central1"}`,
			vpnTunnelsGetID:   `{"project_id":"my-project-123","region":"us-central1","vpn_tunnel":"tun-a"}`,
		} {
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply { return &reply{status: status, body: body} }
			_, err := e.invoke(id, arguments)
			if err == nil {
				t.Fatalf("%s %d: want an error", id, status)
			}
			text := err.Error()
			var failed *provider.Error
			if errors.As(err, &failed) {
				original, _ := json.Marshal(failed.Original)
				text += string(original)
			}
			absentSecrets(t, id+" error", text)
		}
	}
}

func TestVpnToolsRefuseInvalidInputBeforeIO(t *testing.T) {
	long := strings.Repeat("a", 64)
	for name, c := range map[string]struct{ tool, arguments string }{
		"region path":        {vpnGatewaysListID, `{"project_id":"my-project-123","region":"regions/us-central1"}`},
		"region traversal":   {vpnTunnelsListID, `{"project_id":"my-project-123","region":"../x"}`},
		"region query":       {vpnGatewaysListID, `{"project_id":"my-project-123","region":"a?b=1"}`},
		"region upper":       {vpnTunnelsListID, `{"project_id":"my-project-123","region":"US-central1"}`},
		"region long":        {vpnGatewaysListID, `{"project_id":"my-project-123","region":"` + long + `"}`},
		"region missing":     {vpnTunnelsListID, `{"project_id":"my-project-123"}`},
		"region on get":      {vpnGatewaysGetID, `{"project_id":"my-project-123","region":"a/b","vpn_gateway":"g"}`},
		"region on tunnel":   {vpnTunnelsGetID, `{"project_id":"my-project-123","region":"a/b","vpn_tunnel":"t"}`},
		"region missing get": {vpnTunnelsGetID, `{"project_id":"my-project-123","vpn_tunnel":"t"}`},
		"gateway path":       {vpnGatewaysGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_gateway":"a/b"}`},
		"gateway traversal":  {vpnGatewaysGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_gateway":".."}`},
		"gateway zero id":    {vpnGatewaysGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_gateway":"0"}`},
		"gateway missing":    {vpnGatewaysGetID, `{"project_id":"my-project-123","region":"us-central1"}`},
		"tunnel path":        {vpnTunnelsGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_tunnel":"a/b"}`},
		"tunnel query":       {vpnTunnelsGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_tunnel":"a?b"}`},
		"tunnel gateway arg": {vpnTunnelsGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_gateway":"g"}`},
		"project path":       {vpnGatewaysListID, `{"project_id":"projects/x","region":"us-central1"}`},
		"project traversal":  {vpnTunnelsGetID, `{"project_id":"../x","region":"us-central1","vpn_tunnel":"t"}`},
		"max results 501":    {vpnGatewaysListID, `{"project_id":"my-project-123","region":"us-central1","max_results":501}`},
		"order by":           {vpnTunnelsListID, `{"project_id":"my-project-123","region":"us-central1","order_by":"name; drop"}`},
		"filter control":     {vpnTunnelsListID, `{"project_id":"my-project-123","region":"us-central1","filter":"a\nb"}`},
		"cursor":             {vpnGatewaysListID, `{"project_id":"my-project-123","region":"us-central1","cursor":"bogus"}`},
		"scopes":             {vpnGatewaysListID, `{"project_id":"my-project-123","region":"us-central1","include_all_scopes":true}`},
		"zone":               {vpnTunnelsListID, `{"project_id":"my-project-123","region":"us-central1","zone":"us-central1-a"}`},
		"views":              {vpnTunnelsListID, `{"project_id":"my-project-123","region":"us-central1","views":"DEFAULT"}`},
		"unknown":            {vpnGatewaysGetID, `{"project_id":"my-project-123","region":"us-central1","vpn_gateway":"g","path":"/x"}`},
	} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(c.tool, c.arguments); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	// The handlers refuse what the schema would not have let through.
	e := newEnvironment(t, nil)
	for name, call := range map[string]func() error{
		"gateways list": func() error {
			_, err := handleVpnGatewaysList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","region":"a/b"}`))
			return err
		},
		"gateways get": func() error {
			_, err := handleVpnGatewaysGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","region":"us-central1","vpn_gateway":"a/b"}`))
			return err
		},
		"tunnels list": func() error {
			_, err := handleVpnTunnelsList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","region":"us-central1","max_results":501}`))
			return err
		},
		"tunnels get": func() error {
			_, err := handleVpnTunnelsGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","region":"..","vpn_tunnel":"t"}`))
			return err
		},
	} {
		if !isInvalidRequest(call()) {
			t.Errorf("%s: want a refusal of the handler", name)
		}
	}
	if len(e.fake.api()) != 0 {
		t.Error("a handler refusal reached Google")
	}
}

func TestVpnToolsRefuseProjectsOutsideTheTargetsBeforeIO(t *testing.T) {
	calls := map[string]string{
		vpnGatewaysListID: `{"project_id":"other-project-1","region":"us-central1"}`,
		vpnGatewaysGetID:  `{"project_id":"other-project-1","region":"us-central1","vpn_gateway":"g"}`,
		vpnTunnelsListID:  `{"project_id":"other-project-1","region":"us-central1"}`,
		vpnTunnelsGetID:   `{"project_id":"other-project-1","region":"us-central1","vpn_tunnel":"t"}`,
	}
	for id, arguments := range calls {
		e := newEnvironment(t, []string{"projects/my-project-123"})
		_, err := e.invoke(id, arguments)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "my-project-123") {
			t.Errorf("%s: err = %v", id, err)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", id, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
}

func TestVpnCursorIsBoundToTheRegionAndTheTool(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[vpnGatewaysPath] = okReply(`{"items":[` + vpnGatewayBody("gw-a") + `],"nextPageToken":"p2"}`)
	out, err := e.invoke(vpnGatewaysListID, `{"project_id":"my-project-123","region":"us-central1"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := listOf(t, out, "vpn_gateways")
	cursor := got["next_cursor"].(string)
	before := len(e.fake.api())
	for name, c := range map[string]struct{ tool, arguments string }{
		"other region": {vpnGatewaysListID, `{"project_id":"my-project-123","region":"europe-west1","cursor":"` + cursor + `"}`},
		"other tool":   {vpnTunnelsListID, `{"project_id":"my-project-123","region":"us-central1","cursor":"` + cursor + `"}`},
		"other filter": {vpnGatewaysListID, `{"project_id":"my-project-123","region":"us-central1","filter":"x","cursor":"` + cursor + `"}`},
	} {
		if _, err := e.invoke(c.tool, c.arguments); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
	if len(e.fake.api()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestVpnToolsRefuseInvalidAnswers(t *testing.T) {
	for name, c := range map[string]struct{ tool, arguments, path, body string }{
		"gateway without name": {vpnGatewaysListID, `{"project_id":"my-project-123","region":"us-central1"}`, vpnGatewaysPath, `{"items":[{"id":"1"}]}`},
		"tunnel not an object": {vpnTunnelsListID, `{"project_id":"my-project-123","region":"us-central1"}`, vpnTunnelsPath, `{"items":[1]}`},
		"tunnel other region": {vpnTunnelsListID, `{"project_id":"my-project-123","region":"us-central1"}`, vpnTunnelsPath,
			`{"items":[{"name":"t","region":"https://x/compute/v1/projects/p/regions/europe-west1"}]}`},
		"gateway bad name": {vpnGatewaysListID, `{"project_id":"my-project-123","region":"us-central1"}`, vpnGatewaysPath,
			`{"items":[{"name":"../g"}]}`},
		"token too long": {vpnGatewaysListID, `{"project_id":"my-project-123","region":"us-central1"}`, vpnGatewaysPath,
			`{"items":[],"nextPageToken":"` + strings.Repeat("a", maxPageTokenBytes+1) + `"}`},
	} {
		e := newEnvironment(t, nil)
		e.fake.routes[c.path] = okReply(c.body)
		if _, err := e.invoke(c.tool, c.arguments); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestVpnToolsBoundProviderText(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[vpnTunnelsPath] = okReply(`{"items":[{"name":"t","detailedStatus":"` + strings.Repeat("x", 1<<20) + `"}]}`)
	out, err := e.invoke(vpnTunnelsListID, `{"project_id":"my-project-123","region":"us-central1"}`)
	if err != nil || len(out) > 1<<19 {
		t.Errorf("text was not bounded: %d bytes, %v", len(out), err)
	}
}

func TestVpnToolsFailures(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"403 permission denied", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"403 API disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission},
		{"403 legacy form", reply{status: 403, body: `{"error":{"code":403,"message":"` + detailCanary +
			`","errors":[{"message":"m","domain":"global","reason":"forbidden"}]}}`}, provider.ClassPermission},
		{"404 not found", reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
		{"404 legacy form", reply{status: 404, body: `{"error":{"code":404,"message":"` + detailCanary +
			`","errors":[{"message":"m","domain":"global","reason":"notFound"}]}}`}, provider.ClassNotFound},
		{"429", reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")}, provider.ClassRateLimited},
	}
	for _, c := range cases {
		for id, arguments := range map[string]string{
			vpnGatewaysListID: `{"project_id":"my-project-123","region":"us-central1"}`,
			vpnGatewaysGetID:  `{"project_id":"my-project-123","region":"us-central1","vpn_gateway":"gw-a"}`,
			vpnTunnelsListID:  `{"project_id":"my-project-123","region":"us-central1"}`,
			vpnTunnelsGetID:   `{"project_id":"my-project-123","region":"us-central1","vpn_tunnel":"tun-a"}`,
		} {
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply { return &c.reply }
			_, err := e.invoke(id, arguments)
			if classOf(err) != c.want {
				t.Errorf("%s %s: err = %v (%s), want %s", c.name, id, err, classOf(err), c.want)
			}
			if err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s %s: error echoes provider text", c.name, id)
			}
		}
	}
	e := newEnvironment(t, nil)
	e.fake.routes[vpnGatewaysPath] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}
	if _, err := e.invoke(vpnGatewaysListID, `{"project_id":"my-project-123","region":"us-central1"}`); err == nil || !strings.Contains(err.Error(), "Compute Engine") {
		t.Errorf("err = %v, want a setup message that names Compute Engine", err)
	}
}

func TestVpnToolsAreCompleteReadToolsWithoutAWriter(t *testing.T) {
	e := newEnvironment(t, nil)
	for _, id := range vpnTools {
		if got := tokenMetadata.Scopes[id]; len(got) != 1 || got[0] != scopeComputeReadOnly {
			t.Errorf("%s scopes = %v", id, got)
		}
		if !slices.Contains(readTools, id) {
			t.Errorf("%s is not in the profile read", id)
		}
		d, _, found := e.reg.Lookup(id)
		if !found {
			t.Fatalf("%s is not registered", id)
		}
		if d.Risk != readRisk || !strings.Contains(d.Description, "untrusted") {
			t.Errorf("%s risk = %+v, description = %q", id, d.Risk, d.Description)
		}
	}
	for _, id := range []string{vpnTunnelsListID, vpnTunnelsGetID} {
		d, _, _ := e.reg.Lookup(id)
		if !strings.Contains(d.Description, "never returned") {
			t.Errorf("%s description does not name the secret rule", id)
		}
	}
	for _, d := range e.reg.All() {
		if strings.Contains(d.ID, ".vpn-") && !slices.Contains(vpnTools, d.ID) &&
			d.ID != vpnTunnelsDeleteID && d.ID != vpnGatewaysDeleteID {
			// The two deletions are covered by compute_vpn_delete_test.go.
			t.Errorf("unexpected VPN tool %s", d.ID)
		}
	}
}
