package googlecloud

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	dnsPoliciesPath         = "/dns/v1/projects/my-project-123/policies"
	dnsResponsePoliciesPath = "/dns/v1/projects/my-project-123/responsePolicies"
)

const forwardingPolicyBody = `{"kind":"dns#policy","name":"forwarding-policy","id":"1234567890123456789",` +
	`"description":"forward to on-premises","enableInboundForwarding":true,"enableLogging":false,` +
	`"alternativeNameServerConfig":{"kind":"dns#policyAlternativeNameServerConfig","targetNameServers":[` +
	`{"kind":"dns#policyAlternativeNameServerConfigTargetNameServer","ipv4Address":"192.0.2.53","forwardingPath":"private"},` +
	`{"ipv6Address":"2001:db8::53","forwardingPath":"default"}],` +
	`"outboundEndpoints":[{"name":"projects/my-project-123/locations/us-central1/outboundEndpoints/example"}]},` +
	`"unknownMember":"x"}`

const networkPolicyBody = `{"kind":"dns#policy","name":"network-policy","id":"42","enableLogging":true,` +
	`"networks":[{"kind":"dns#policyNetwork","networkUrl":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/networks/default"},` +
	`{"networkUrl":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/networks/second"}],` +
	`"dns64Config":{"kind":"dns#policyDns64Config","scope":{"allQueries":true}}}`

const clusterResponsePolicyBody = `{"kind":"dns#responsePolicy","responsePolicyName":"cluster-policy","id":"77",` +
	`"description":"cluster overrides","gkeClusters":[{"kind":"dns#responsePolicyGKECluster",` +
	`"gkeClusterName":"projects/my-project-123/locations/us-central1/clusters/example-cluster"}],` +
	`"labels":{"env":"test"},"unknownMember":1}`

const networkResponsePolicyBody = `{"responsePolicyName":"network-response-policy","id":"78",` +
	`"networks":[{"kind":"dns#responsePolicyNetwork","networkUrl":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/networks/default"}]}`

func TestDNSPoliciesListFollowsTwoPages(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != dnsPoliciesPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"kind":"dns#policiesListResponse","policies":[` + forwardingPolicyBody +
				`],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"policies":[` + networkPolicyBody + `]}`}
	}
	base := `{"project_id":"my-project-123","max_results":1`
	out, err := e.invoke(dnsPoliciesListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "policies")
	if len(rows) != 1 || got["has_more"] != true || rows[0]["name"] != "forwarding-policy" ||
		rows[0]["id"] != "1234567890123456789" || rows[0]["description"] != "forward to on-premises" ||
		rows[0]["enableInboundForwarding"] != true || rows[0]["enableLogging"] != false ||
		rows[0]["kind"] != "dns#policy" || rows[0]["unknownMember"] != nil {
		t.Fatalf("page 1 = %s", out)
	}
	alt := rows[0]["alternativeNameServerConfig"].(map[string]any)
	targets := alt["targetNameServers"].([]any)
	if len(targets) != 2 || targets[0].(map[string]any)["ipv4Address"] != "192.0.2.53" ||
		targets[0].(map[string]any)["forwardingPath"] != "private" ||
		targets[1].(map[string]any)["ipv6Address"] != "2001:db8::53" || len(alt["outboundEndpoints"].([]any)) != 1 {
		t.Errorf("forwarding = %v", alt)
	}
	cursor := got["next_cursor"].(string)
	out, err = e.invoke(dnsPoliciesListID, base+`,"cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "policies")
	if len(rows) != 1 || got["has_more"] != false || got["next_cursor"] != nil || rows[0]["name"] != "network-policy" ||
		len(rows[0]["networks"].([]any)) != 2 ||
		rows[0]["dns64Config"].(map[string]any)["scope"].(map[string]any)["allQueries"] != true {
		t.Fatalf("page 2 = %s", out)
	}
	queries := queriesOf(e)
	if len(queries) != 2 || queries[0].Get("maxResults") != "1" || queries[0].Has("pageToken") ||
		queries[1].Get("pageToken") != "p2" || queries[1].Get("maxResults") != "1" {
		t.Errorf("queries = %v", queries)
	}
	for _, p := range apiPaths(e) {
		if p != "dns.googleapis.com"+dnsPoliciesPath {
			t.Errorf("path = %s", p)
		}
	}
	// A cursor of another list is refused before any request.
	before := len(e.fake.api())
	if _, err := e.invoke(responsePoliciesListID, `{"project_id":"my-project-123","max_results":1,"cursor":"`+
		cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	if len(e.fake.api()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestResponsePoliciesListFollowsTwoPages(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != dnsResponsePoliciesPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"responsePolicies":[` + clusterResponsePolicyBody + `],"nextPageToken":"r2"}`}
		}
		return &reply{status: 200, body: `{"responsePolicies":[` + networkResponsePolicyBody + `]}`}
	}
	out, err := e.invoke(responsePoliciesListID, `{"project_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "response_policies")
	if len(rows) != 1 || got["has_more"] != true || rows[0]["responsePolicyName"] != "cluster-policy" ||
		rows[0]["id"] != "77" || rows[0]["description"] != "cluster overrides" ||
		rows[0]["labels"].(map[string]any)["env"] != "test" || rows[0]["kind"] != "dns#responsePolicy" ||
		rows[0]["unknownMember"] != nil {
		t.Fatalf("page 1 = %s", out)
	}
	clusters := rows[0]["gkeClusters"].([]any)
	if len(clusters) != 1 || clusters[0].(map[string]any)["gkeClusterName"] !=
		"projects/my-project-123/locations/us-central1/clusters/example-cluster" {
		t.Errorf("clusters = %v", clusters)
	}
	out, err = e.invoke(responsePoliciesListID, `{"project_id":"my-project-123","cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "response_policies")
	if len(rows) != 1 || got["has_more"] != false || rows[0]["responsePolicyName"] != "network-response-policy" ||
		len(rows[0]["networks"].([]any)) != 1 {
		t.Fatalf("page 2 = %s", out)
	}
	queries := queriesOf(e)
	if len(queries) != 2 || queries[0].Has("maxResults") || queries[0].Has("pageToken") || queries[1].Get("pageToken") != "r2" {
		t.Errorf("queries = %v", queries)
	}
}

func TestDNSPolicyListsAcceptAnEmptyAnswer(t *testing.T) {
	for id, path := range map[string]string{dnsPoliciesListID: dnsPoliciesPath, responsePoliciesListID: dnsResponsePoliciesPath} {
		e := newEnvironment(t, nil)
		e.fake.routes[path] = okReply(`{}`)
		out, err := e.invoke(id, `{"project_id":"my-project-123"}`)
		if err != nil || !strings.Contains(out, `"has_more":false`) {
			t.Errorf("%s: out = %s, err = %v", id, out, err)
		}
	}
}

func TestDNSPoliciesGetReadOneByName(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes[dnsPoliciesPath+"/forwarding-policy"] = okReply(forwardingPolicyBody)
	e.fake.routes[dnsPoliciesPath+"/network-policy"] = okReply(networkPolicyBody)
	e.fake.routes[dnsPoliciesPath+"/other-policy"] = okReply(networkPolicyBody)
	e.fake.routes[dnsResponsePoliciesPath+"/cluster-policy"] = okReply(clusterResponsePolicyBody)
	e.fake.routes[dnsResponsePoliciesPath+"/other-policy"] = okReply(clusterResponsePolicyBody)
	out, err := e.invoke(dnsPoliciesGetID, `{"project_id":"my-project-123","policy":"forwarding-policy"}`)
	if err != nil || !strings.Contains(out, `"ipv4Address":"192.0.2.53"`) || !strings.Contains(out, `"enableInboundForwarding":true`) ||
		strings.Contains(out, "unknownMember") {
		t.Errorf("forwarding: out = %s, err = %v", out, err)
	}
	out, err = e.invoke(dnsPoliciesGetID, `{"project_id":"my-project-123","policy":"network-policy"}`)
	if err != nil || !strings.Contains(out, `networks/second"`) || !strings.Contains(out, `"allQueries":true`) {
		t.Errorf("networks: out = %s, err = %v", out, err)
	}
	out, err = e.invoke(responsePoliciesGetID, `{"project_id":"my-project-123","response_policy":"cluster-policy"}`)
	if err != nil || !strings.Contains(out, `clusters/example-cluster"`) || strings.Contains(out, "unknownMember") {
		t.Errorf("response policy: out = %s, err = %v", out, err)
	}
	for _, r := range e.fake.api() {
		u, _ := url.Parse(r.url)
		if r.method != http.MethodGet || u.RawQuery != "" || u.Host != "dns.googleapis.com" {
			t.Errorf("request = %s %s", r.method, r.url)
		}
	}
	// An answer for another policy than the one asked for is invalid.
	for id, arguments := range map[string]string{
		dnsPoliciesGetID:      `{"project_id":"my-project-123","policy":"other-policy"}`,
		responsePoliciesGetID: `{"project_id":"my-project-123","response_policy":"other-policy"}`,
	} {
		if _, err := e.invoke(id, arguments); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", id, err)
		}
	}
}

func TestDNSPolicyToolsRefuseBadInputBeforeIO(t *testing.T) {
	const project = `"project_id":"my-project-123"`
	for name, call := range map[string][2]string{
		"policy path":        {dnsPoliciesGetID, `{` + project + `,"policy":"a/b"}`},
		"policy uppercase":   {dnsPoliciesGetID, `{` + project + `,"policy":"Example"}`},
		"policy id":          {dnsPoliciesGetID, `{` + project + `,"policy":"1234"}`},
		"policy escape":      {dnsPoliciesGetID, `{` + project + `,"policy":"%2e%2e"}`},
		"policy long":        {dnsPoliciesGetID, `{` + project + `,"policy":"` + strings.Repeat("a", 64) + `"}`},
		"policy missing":     {dnsPoliciesGetID, `{` + project + `}`},
		"response path":      {responsePoliciesGetID, `{` + project + `,"response_policy":"a/b"}`},
		"response query":     {responsePoliciesGetID, `{` + project + `,"response_policy":"p?x=1"}`},
		"response dash last": {responsePoliciesGetID, `{` + project + `,"response_policy":"policy-"}`},
		"project path":       {dnsPoliciesListID, `{"project_id":"projects/my-project-123"}`},
		"max zero":           {dnsPoliciesListID, `{` + project + `,"max_results":0}`},
		"max large":          {responsePoliciesListID, `{` + project + `,"max_results":1001}`},
		"unknown argument":   {responsePoliciesListID, `{` + project + `,"path":"/x"}`},
		"bad cursor":         {dnsPoliciesListID, `{` + project + `,"cursor":"bogus"}`},
	} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(call[0], call[1]); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
}

func TestDNSPolicyToolsRefuseAProjectOutsideTheTargetsBeforeIO(t *testing.T) {
	for _, call := range [][2]string{
		{dnsPoliciesListID, `{"project_id":"foreign-project-1"}`},
		{dnsPoliciesGetID, `{"project_id":"foreign-project-1","policy":"example-policy"}`},
		{responsePoliciesListID, `{"project_id":"foreign-project-1"}`},
		{responsePoliciesGetID, `{"project_id":"foreign-project-1","response_policy":"example-policy"}`},
	} {
		e := newEnvironment(t, []string{"projects/my-project-123"})
		_, err := e.invoke(call[0], call[1])
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "foreign-project-1") {
			t.Errorf("%s: err = %v", call[0], err)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", call[0], len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
}

func TestDNSPolicyToolsBoundTextAndRefuseBadRows(t *testing.T) {
	long := strings.Repeat("x", 3000)
	e := newEnvironment(t, nil)
	e.fake.routes[dnsPoliciesPath+"/example-policy"] = okReply(`{"name":"example-policy","description":"` + long + `\u0007"}`)
	out, err := e.invoke(dnsPoliciesGetID, `{"project_id":"my-project-123","policy":"example-policy"}`)
	if err != nil || strings.Contains(out, long) || !strings.Contains(out, "…") || strings.Contains(out, `\u0007`) {
		t.Errorf("out length = %d, err = %v", len(out), err)
	}
	for _, body := range []string{`{"id":"1"}`, `{"name":"Bad Name"}`, `{"name":"p","enableLogging":"yes"}`,
		`{"name":"p","networks":{}}`, `{"name":"p","alternativeNameServerConfig":[]}`, `{"name":"p","id":5}`} {
		e = newEnvironment(t, nil)
		e.fake.routes[dnsPoliciesPath] = okReply(`{"policies":[` + body + `]}`)
		if _, err := e.invoke(dnsPoliciesListID, `{"project_id":"my-project-123"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("policy %s: err = %v", body, err)
		}
	}
	for _, body := range []string{`{"name":"p"}`, `{"responsePolicyName":"p","gkeClusters":"x"}`,
		`{"responsePolicyName":"p","labels":[]}`} {
		e = newEnvironment(t, nil)
		e.fake.routes[dnsResponsePoliciesPath] = okReply(`{"responsePolicies":[` + body + `]}`)
		if _, err := e.invoke(responsePoliciesListID, `{"project_id":"my-project-123"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("response policy %s: err = %v", body, err)
		}
	}
	for _, answer := range []string{`{"policies":{}}`, `{"policies":[],"nextPageToken":5}`} {
		e = newEnvironment(t, nil)
		e.fake.routes[dnsPoliciesPath] = okReply(answer)
		if _, err := e.invoke(dnsPoliciesListID, `{"project_id":"my-project-123"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("answer %s: err = %v", answer, err)
		}
	}
}

func TestDNSPolicyToolsMapErrors(t *testing.T) {
	calls := map[string]string{
		dnsPoliciesListID:      `{"project_id":"my-project-123"}`,
		dnsPoliciesGetID:       `{"project_id":"my-project-123","policy":"example-policy"}`,
		responsePoliciesListID: `{"project_id":"my-project-123"}`,
		responsePoliciesGetID:  `{"project_id":"my-project-123","response_policy":"example-policy"}`,
	}
	for _, status := range []struct {
		code   int
		reason string
		want   provider.Class
	}{{403, "PERMISSION_DENIED", provider.ClassPermission}, {404, "NOT_FOUND", provider.ClassNotFound}} {
		for id, arguments := range calls {
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply {
				return &reply{status: status.code, body: errorObject(status.code, status.reason, "")}
			}
			_, err := e.invoke(id, arguments)
			if classOf(err) != status.want || err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s %d: err = %v", id, status.code, err)
			}
		}
	}
}
