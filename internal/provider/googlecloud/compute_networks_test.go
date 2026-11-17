package googlecloud

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	networksPath    = "/compute/v1/projects/my-project-123/global/networks"
	subnetworksPath = "/compute/v1/projects/my-project-123/regions/us-central1/subnetworks"
)

var networkTools = []string{networksListID, networksGetID, subnetworksListID, subnetworksGetID}

func autoNetworkBody(name string) string {
	return `{"kind":"compute#network","id":"5100000000000000001","name":"` + name + `","autoCreateSubnetworks":true,` +
		`"IPv4Range":"10.0.0.0/8","gatewayIPv4":"10.128.0.1","mtu":1460,` +
		`"subnetworks":["https://www.googleapis.com/compute/v1/projects/my-project-123/regions/us-central1/subnetworks/` + name + `"],` +
		`"routingConfig":{"routingMode":"REGIONAL"},"networkFirewallPolicyEnforcementOrder":"AFTER_CLASSIC_FIREWALL",` +
		`"selfLink":"s","creationTimestamp":"2026-03-04T05:06:07.000-08:00","params":{"resourceManagerTags":{"k":"v"}},"unknownMember":"x"}`
}

func customNetworkBody(name string) string {
	return `{"kind":"compute#network","id":"5100000000000000002","name":"` + name + `","autoCreateSubnetworks":false,` +
		`"peerings":[{"name":"peer-a","network":"n","state":"ACTIVE","exportCustomRoutes":true}],"mtu":1500,` +
		`"routingConfig":{"routingMode":"GLOBAL"},"selfLink":"s"}`
}

func subnetworkBody(name string) string {
	return `{"kind":"compute#subnetwork","id":"6100000000000000001","name":"` + name + `","ipCidrRange":"10.10.0.0/20",` +
		`"network":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/networks/vpc-a",` +
		`"region":"https://www.googleapis.com/compute/v1/projects/my-project-123/regions/us-central1",` +
		`"gatewayAddress":"10.10.0.1","privateIpGoogleAccess":true,"purpose":"PRIVATE","role":"ACTIVE","stackType":"IPV4_ONLY",` +
		`"ipv6AccessType":"INTERNAL","secondaryIpRanges":[{"rangeName":"pods","ipCidrRange":"10.20.0.0/16"},` +
		`{"rangeName":"services","ipCidrRange":"10.30.0.0/20"}],"logConfig":{"enable":true,"aggregationInterval":"INTERVAL_5_SEC"},` +
		`"params":{"x":"y"},"selfLink":"s"}`
}

func TestNetworksListFollowsThreePagesAndBuildsTheQuery(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != networksPath {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"items":[` + autoNetworkBody("vpc-a") + `],"nextPageToken":"p2"}`}
		case "p2":
			return &reply{status: 200, body: `{"items":[` + customNetworkBody("vpc-b") + `],"nextPageToken":"p3",` +
				`"warning":{"code":"X","message":"m"}}`}
		}
		return &reply{status: 200, body: `{"items":[` + customNetworkBody("vpc-c") + `]}`}
	}
	var names []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		arguments := `{"project_id":"my-project-123","filter":"name != x","max_results":1,"order_by":"name desc",` +
			`"return_partial_success":true`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(networksListID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		rows, got := listOf(t, out, "networks")
		for _, row := range rows {
			names = append(names, row["name"].(string))
			for _, dropped := range []string{"IPv4Range", "params", "unknownMember"} {
				if _, found := row[dropped]; found {
					t.Errorf("page %d returns %s", page, dropped)
				}
			}
			if page == 1 && (row["autoCreateSubnetworks"] != true || row["gatewayIPv4"] != "10.128.0.1" ||
				row["mtu"] != float64(1460) || len(row["subnetworks"].([]any)) != 1 ||
				row["routingConfig"].(map[string]any)["routingMode"] != "REGIONAL" ||
				row["networkFirewallPolicyEnforcementOrder"] != "AFTER_CLASSIC_FIREWALL") {
				t.Errorf("auto network = %v", row)
			}
			if page == 2 && (row["autoCreateSubnetworks"] != false || len(row["peerings"].([]any)) != 1) {
				t.Errorf("custom network = %v", row)
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
	if !slices.Equal(names, []string{"vpc-a", "vpc-b", "vpc-c"}) {
		t.Errorf("names = %v", names)
	}
	for i, q := range queriesOf(e) {
		if q.Get("filter") != "name != x" || q.Get("maxResults") != "1" || q.Get("orderBy") != "name desc" ||
			q.Get("returnPartialSuccess") != "true" || q.Has("views") {
			t.Errorf("request %d: %v", i, q)
		}
	}
}

func TestNetworksGetAutoAndCustomMode(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[networksPath+"/vpc-a"] = okReply(autoNetworkBody("vpc-a"))
	e.fake.routes[networksPath+"/5100000000000000002"] = okReply(customNetworkBody("vpc-b"))
	out, err := e.invoke(networksGetID, `{"project_id":"my-project-123","network":"vpc-a"}`)
	if err != nil || !strings.Contains(out, `"autoCreateSubnetworks":true`) || strings.Contains(out, "IPv4Range") {
		t.Errorf("auto: %s, %v", out, err)
	}
	out, err = e.invoke(networksGetID, `{"project_id":"my-project-123","network":"5100000000000000002"}`)
	if err != nil || !strings.Contains(out, `"autoCreateSubnetworks":false`) || !strings.Contains(out, "peer-a") {
		t.Errorf("custom by id: %s, %v", out, err)
	}
	// An answer for another network is refused.
	e.fake.routes[networksPath+"/vpc-c"] = okReply(customNetworkBody("vpc-b"))
	if _, err := e.invoke(networksGetID, `{"project_id":"my-project-123","network":"vpc-c"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestSubnetworksListKeepsSecondaryRangesAndBindsTheRegion(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != subnetworksPath {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"items":[` + subnetworkBody("sub-a") + `],"nextPageToken":"p2"}`}
		case "p2":
			return &reply{status: 200, body: `{"items":[` + subnetworkBody("sub-b") + `],"nextPageToken":"p3"}`}
		}
		return &reply{status: 200, body: `{"items":[` + subnetworkBody("sub-c") + `]}`}
	}
	base := `{"project_id":"my-project-123","region":"us-central1","filter":"purpose = PRIVATE","max_results":1,"views":"WITH_UTILIZATION"`
	cursor := ""
	var names []string
	var first string
	for page := 1; page <= 3; page++ {
		arguments := base
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(subnetworksListID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		rows, got := listOf(t, out, "subnetworks")
		for _, row := range rows {
			names = append(names, row["name"].(string))
			if _, found := row["params"]; found {
				t.Error("input-only member returned")
			}
			ranges, _ := row["secondaryIpRanges"].([]any)
			if row["ipCidrRange"] != "10.10.0.0/20" || len(ranges) != 2 || ranges[0].(map[string]any)["rangeName"] != "pods" ||
				row["gatewayAddress"] != "10.10.0.1" || row["privateIpGoogleAccess"] != true || row["purpose"] != "PRIVATE" ||
				row["role"] != "ACTIVE" || row["stackType"] != "IPV4_ONLY" || row["ipv6AccessType"] != "INTERNAL" ||
				row["logConfig"].(map[string]any)["enable"] != true {
				t.Errorf("row = %v", row)
			}
		}
		if page == 1 {
			first, _ = got["next_cursor"].(string)
		}
		cursor, _ = got["next_cursor"].(string)
	}
	if !slices.Equal(names, []string{"sub-a", "sub-b", "sub-c"}) {
		t.Errorf("names = %v", names)
	}
	for _, q := range queriesOf(e) {
		if q.Get("filter") != "purpose = PRIVATE" || q.Get("maxResults") != "1" || q.Get("views") != "WITH_UTILIZATION" {
			t.Errorf("query = %v", q)
		}
	}
	before := len(e.fake.api())
	for name, arguments := range map[string]string{
		"other region": `{"project_id":"my-project-123","region":"europe-west1","filter":"purpose = PRIVATE","max_results":1,` +
			`"views":"WITH_UTILIZATION","cursor":"` + first + `"}`,
		"other views": `{"project_id":"my-project-123","region":"us-central1","filter":"purpose = PRIVATE","max_results":1,` +
			`"cursor":"` + first + `"}`,
		"other filter": `{"project_id":"my-project-123","region":"us-central1","cursor":"` + first + `"}`,
		"bogus":        `{"project_id":"my-project-123","region":"us-central1","cursor":"bogus"}`,
	} {
		if _, err := e.invoke(subnetworksListID, arguments); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
	if _, err := e.invoke(networksListID, `{"project_id":"my-project-123","cursor":"`+first+`"}`); err == nil {
		t.Error("cursor of another tool accepted")
	}
	if len(e.fake.api()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestSubnetworksGetWithSecondaryRanges(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[subnetworksPath+"/sub-a"] = okReply(subnetworkBody("sub-a"))
	out, err := e.invoke(subnetworksGetID, `{"project_id":"my-project-123","region":"us-central1","subnetwork":"sub-a"}`)
	if err != nil || !strings.Contains(out, `"rangeName":"services"`) || strings.Contains(out, `"params"`) {
		t.Errorf("out = %s, %v", out, err)
	}
	if q := queriesOf(e)[0]; len(q) != 0 {
		t.Errorf("query = %v", q)
	}
	e.fake.routes[subnetworksPath+"/6100000000000000001"] = okReply(subnetworkBody("sub-a"))
	if _, err := e.invoke(subnetworksGetID, `{"project_id":"my-project-123","region":"us-central1","subnetwork":"6100000000000000001","views":"WITH_UTILIZATION"}`); err != nil {
		t.Fatal(err)
	}
	if q := queriesOf(e); q[1].Get("views") != "WITH_UTILIZATION" {
		t.Errorf("query = %v", q[1])
	}
	// A subnetwork of another region or another name is refused.
	e.fake.routes[subnetworksPath+"/sub-x"] = okReply(subnetworkBody("sub-a"))
	if _, err := e.invoke(subnetworksGetID, `{"project_id":"my-project-123","region":"us-central1","subnetwork":"sub-x"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("name mismatch: %v", err)
	}
	e.fake.routes["/compute/v1/projects/my-project-123/regions/europe-west1/subnetworks/sub-a"] = okReply(subnetworkBody("sub-a"))
	if _, err := e.invoke(subnetworksGetID, `{"project_id":"my-project-123","region":"europe-west1","subnetwork":"sub-a"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("region mismatch: %v", err)
	}
}

func TestNetworkToolsRefuseInvalidInputBeforeIO(t *testing.T) {
	long := strings.Repeat("a", 64)
	for name, c := range map[string]struct{ tool, arguments string }{
		"region path":          {subnetworksListID, `{"project_id":"my-project-123","region":"regions/us-central1"}`},
		"region traversal":     {subnetworksListID, `{"project_id":"my-project-123","region":"../x"}`},
		"region query":         {subnetworksListID, `{"project_id":"my-project-123","region":"a?b=1"}`},
		"region upper":         {subnetworksListID, `{"project_id":"my-project-123","region":"US-central1"}`},
		"region long":          {subnetworksListID, `{"project_id":"my-project-123","region":"` + long + `"}`},
		"region missing":       {subnetworksListID, `{"project_id":"my-project-123"}`},
		"region on get":        {subnetworksGetID, `{"project_id":"my-project-123","region":"a/b","subnetwork":"s"}`},
		"region missing get":   {subnetworksGetID, `{"project_id":"my-project-123","subnetwork":"s"}`},
		"subnetwork path":      {subnetworksGetID, `{"project_id":"my-project-123","region":"us-central1","subnetwork":"a/b"}`},
		"subnetwork traversal": {subnetworksGetID, `{"project_id":"my-project-123","region":"us-central1","subnetwork":".."}`},
		"subnetwork zero id":   {subnetworksGetID, `{"project_id":"my-project-123","region":"us-central1","subnetwork":"0"}`},
		"views":                {subnetworksListID, `{"project_id":"my-project-123","region":"us-central1","views":"FULL"}`},
		"views get":            {subnetworksGetID, `{"project_id":"my-project-123","region":"us-central1","subnetwork":"s","views":"x"}`},
		"zone on subnetworks":  {subnetworksListID, `{"project_id":"my-project-123","region":"us-central1","zone":"us-central1-a"}`},
		"scopes on subnetwork": {subnetworksListID, `{"project_id":"my-project-123","region":"us-central1","include_all_scopes":true}`},
		"network path":         {networksGetID, `{"project_id":"my-project-123","network":"global/networks/x"}`},
		"network traversal":    {networksGetID, `{"project_id":"my-project-123","network":".."}`},
		"network query":        {networksGetID, `{"project_id":"my-project-123","network":"a?b"}`},
		"network missing":      {networksGetID, `{"project_id":"my-project-123"}`},
		"project path":         {networksListID, `{"project_id":"projects/x"}`},
		"project traversal":    {networksGetID, `{"project_id":"../x","network":"n"}`},
		"max results 501":      {networksListID, `{"project_id":"my-project-123","max_results":501}`},
		"max results 0":        {subnetworksListID, `{"project_id":"my-project-123","region":"us-central1","max_results":0}`},
		"order by":             {networksListID, `{"project_id":"my-project-123","order_by":"name; drop"}`},
		"filter control":       {networksListID, `{"project_id":"my-project-123","filter":"a\nb"}`},
		"cursor":               {networksListID, `{"project_id":"my-project-123","cursor":"bogus"}`},
		"scopes on networks":   {networksListID, `{"project_id":"my-project-123","include_all_scopes":true}`},
		"unknown":              {networksGetID, `{"project_id":"my-project-123","network":"n","path":"/x"}`},
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
		"subnetworks list": func() error {
			_, err := handleSubnetworksList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","region":"a/b"}`))
			return err
		},
		"subnetworks get": func() error {
			_, err := handleSubnetworksGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","region":"us-central1","subnetwork":"a/b"}`))
			return err
		},
		"networks list": func() error {
			_, err := handleNetworksList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","max_results":501}`))
			return err
		},
		"networks get": func() error {
			_, err := handleNetworksGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","network":"a/b"}`))
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

func TestNetworkToolsRefuseProjectsOutsideTheTargetsBeforeIO(t *testing.T) {
	calls := map[string]string{
		networksListID:    `{"project_id":"other-project-1"}`,
		networksGetID:     `{"project_id":"other-project-1","network":"n"}`,
		subnetworksListID: `{"project_id":"other-project-1","region":"us-central1"}`,
		subnetworksGetID:  `{"project_id":"other-project-1","region":"us-central1","subnetwork":"s"}`,
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

func TestNetworkToolsRefuseInvalidAnswers(t *testing.T) {
	for name, c := range map[string]struct{ tool, arguments, path, body string }{
		"network without name":  {networksListID, `{"project_id":"my-project-123"}`, networksPath, `{"items":[{"id":"1"}]}`},
		"network not an object": {networksListID, `{"project_id":"my-project-123"}`, networksPath, `{"items":[1]}`},
		"subnetwork other region": {subnetworksListID, `{"project_id":"my-project-123","region":"us-central1"}`, subnetworksPath,
			`{"items":[{"name":"s","region":"https://x/compute/v1/projects/p/regions/europe-west1"}]}`},
		"subnetwork bad name": {subnetworksListID, `{"project_id":"my-project-123","region":"us-central1"}`, subnetworksPath,
			`{"items":[{"name":"../s"}]}`},
		"token too long": {networksListID, `{"project_id":"my-project-123"}`, networksPath,
			`{"items":[],"nextPageToken":"` + strings.Repeat("a", maxPageTokenBytes+1) + `"}`},
	} {
		e := newEnvironment(t, nil)
		e.fake.routes[c.path] = okReply(c.body)
		if _, err := e.invoke(c.tool, c.arguments); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestNetworkToolsBoundProviderText(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[subnetworksPath] = okReply(`{"items":[{"name":"s","description":"` + strings.Repeat("x", 1<<20) + `"}]}`)
	out, err := e.invoke(subnetworksListID, `{"project_id":"my-project-123","region":"us-central1"}`)
	if err != nil || len(out) > 1<<19 {
		t.Errorf("text was not bounded: %d bytes, %v", len(out), err)
	}
}

func TestNetworkToolsFailures(t *testing.T) {
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
			networksListID:    `{"project_id":"my-project-123"}`,
			networksGetID:     `{"project_id":"my-project-123","network":"vpc-a"}`,
			subnetworksListID: `{"project_id":"my-project-123","region":"us-central1"}`,
			subnetworksGetID:  `{"project_id":"my-project-123","region":"us-central1","subnetwork":"sub-a"}`,
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
	e.fake.routes[networksPath] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}
	if _, err := e.invoke(networksListID, `{"project_id":"my-project-123"}`); err == nil || !strings.Contains(err.Error(), "Compute Engine") {
		t.Errorf("err = %v, want a setup message that names Compute Engine", err)
	}
}

func TestNetworkToolsAreCompleteReadTools(t *testing.T) {
	e := newEnvironment(t, nil)
	for _, id := range networkTools {
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
}
