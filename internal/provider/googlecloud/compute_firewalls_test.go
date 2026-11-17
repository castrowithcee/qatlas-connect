package googlecloud

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	firewallsPath = "/compute/v1/projects/my-project-123/global/firewalls"
	addressesPath = "/compute/v1/projects/my-project-123/regions/us-central1/addresses"
)

var firewallAddressTools = []string{firewallsListID, firewallsGetID, addressesListID, addressesGetID}

func ingressRuleBody(name string) string {
	return `{"kind":"compute#firewall","id":"7100000000000000001","name":"` + name + `","direction":"INGRESS","priority":1000,` +
		`"network":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/networks/vpc-a",` +
		`"allowed":[{"IPProtocol":"tcp","ports":["22","8000-8100"]},{"IPProtocol":"icmp"}],` +
		`"sourceRanges":["192.0.2.0/24","203.0.113.0/24"],"targetTags":["web"],"sourceTags":["ops"],` +
		`"sourceServiceAccounts":["deploy@example-project-123.iam.gserviceaccount.com"],"disabled":false,` +
		`"logConfig":{"enable":true,"metadata":"INCLUDE_ALL_METADATA"},"selfLink":"s",` +
		`"creationTimestamp":"2026-03-04T05:06:07.000-08:00","params":{"resourceManagerTags":{"k":"v"}},"unknownMember":"x"}`
}

func egressRuleBody(name string) string {
	return `{"kind":"compute#firewall","id":"7100000000000000002","name":"` + name + `","direction":"EGRESS","priority":900,` +
		`"denied":[{"IPProtocol":"all"}],"destinationRanges":["10.0.0.0/8"],"targetServiceAccounts":["deploy@example-other-9.iam.gserviceaccount.com"],` +
		`"disabled":true,"network":"n"}`
}

func addressBody(name, kind, status string) string {
	address := `"address":"10.128.0.5","subnetwork":"https://www.googleapis.com/compute/v1/projects/my-project-123/regions/us-central1/subnetworks/s"`
	if kind == "EXTERNAL" {
		address = `"address":"203.0.113.10","networkTier":"PREMIUM"`
	}
	return `{"kind":"compute#address","id":"8100000000000000001","name":"` + name + `","addressType":"` + kind + `","status":"` + status + `",` +
		address + `,"ipVersion":"IPV4","purpose":"GCE_ENDPOINT","users":["https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a/instances/vm-a"],` +
		`"labels":{"env":"test"},"labelFingerprint":"f","region":"https://www.googleapis.com/compute/v1/projects/my-project-123/regions/us-central1",` +
		`"selfLink":"s","unknownMember":"x"}`
}

func TestFirewallsListFollowsPagesAndKeepsRules(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != firewallsPath {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"items":[` + ingressRuleBody("allow-ssh") + `],"nextPageToken":"p2"}`}
		case "p2":
			return &reply{status: 200, body: `{"items":[` + egressRuleBody("deny-all-out") + `],"nextPageToken":"p3",` +
				`"warning":{"code":"X","message":"m"}}`}
		}
		return &reply{status: 200, body: `{"items":[` + ingressRuleBody("allow-web") + `]}`}
	}
	var names []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		arguments := `{"project_id":"my-project-123","filter":"direction = INGRESS","max_results":1,"order_by":"name desc",` +
			`"return_partial_success":true`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(firewallsListID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		rows, got := listOf(t, out, "firewalls")
		for _, row := range rows {
			names = append(names, row["name"].(string))
			for _, dropped := range []string{"params", "unknownMember"} {
				if _, found := row[dropped]; found {
					t.Errorf("page %d returns %s", page, dropped)
				}
			}
			switch page {
			case 1:
				if row["direction"] != "INGRESS" || row["priority"] != float64(1000) || len(row["allowed"].([]any)) != 2 ||
					row["sourceRanges"].([]any)[0] != "192.0.2.0/24" || row["disabled"] != false ||
					row["logConfig"].(map[string]any)["enable"] != true || len(row["targetTags"].([]any)) != 1 ||
					len(row["sourceServiceAccounts"].([]any)) != 1 || len(row["sourceTags"].([]any)) != 1 {
					t.Errorf("ingress rule = %v", row)
				}
			case 2:
				if row["direction"] != "EGRESS" || row["disabled"] != true || len(row["denied"].([]any)) != 1 ||
					row["destinationRanges"].([]any)[0] != "10.0.0.0/8" || len(row["targetServiceAccounts"].([]any)) != 1 {
					t.Errorf("egress rule = %v", row)
				}
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
	if !slices.Equal(names, []string{"allow-ssh", "deny-all-out", "allow-web"}) {
		t.Errorf("names = %v", names)
	}
	for i, q := range queriesOf(e) {
		if q.Get("filter") != "direction = INGRESS" || q.Get("maxResults") != "1" || q.Get("orderBy") != "name desc" ||
			q.Get("returnPartialSuccess") != "true" {
			t.Errorf("request %d: %v", i, q)
		}
	}
}

func TestFirewallsGetByNameAndID(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[firewallsPath+"/allow-ssh"] = okReply(ingressRuleBody("allow-ssh"))
	e.fake.routes[firewallsPath+"/7100000000000000002"] = okReply(egressRuleBody("deny-all-out"))
	out, err := e.invoke(firewallsGetID, `{"project_id":"my-project-123","firewall":"allow-ssh"}`)
	if err != nil || !strings.Contains(out, `"direction":"INGRESS"`) || !strings.Contains(out, "203.0.113.0/24") ||
		strings.Contains(out, "resourceManagerTags") {
		t.Errorf("ingress: %s, %v", out, err)
	}
	out, err = e.invoke(firewallsGetID, `{"project_id":"my-project-123","firewall":"7100000000000000002"}`)
	if err != nil || !strings.Contains(out, `"disabled":true`) || !strings.Contains(out, `"denied"`) {
		t.Errorf("egress by id: %s, %v", out, err)
	}
	e.fake.routes[firewallsPath+"/other"] = okReply(ingressRuleBody("allow-ssh"))
	if _, err := e.invoke(firewallsGetID, `{"project_id":"my-project-123","firewall":"other"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestAddressesListInternalAndExternalAcrossPages(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != addressesPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"items":[` + addressBody("int-ip", "INTERNAL", "RESERVED") + `],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"items":[` + addressBody("ext-ip", "EXTERNAL", "IN_USE") + `]}`}
	}
	base := `{"project_id":"my-project-123","region":"us-central1","filter":"status = RESERVED","max_results":1`
	out, err := e.invoke(addressesListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "addresses")
	if len(rows) != 1 || rows[0]["addressType"] != "INTERNAL" || rows[0]["address"] != "10.128.0.5" ||
		rows[0]["status"] != "RESERVED" || rows[0]["subnetwork"] == nil || rows[0]["purpose"] != "GCE_ENDPOINT" ||
		rows[0]["ipVersion"] != "IPV4" || len(rows[0]["users"].([]any)) != 1 || rows[0]["unknownMember"] != nil {
		t.Errorf("internal = %v", rows)
	}
	if got["has_more"] != true {
		t.Fatalf("got = %v", got)
	}
	out, err = e.invoke(addressesListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "addresses")
	if len(rows) != 1 || rows[0]["addressType"] != "EXTERNAL" || rows[0]["address"] != "203.0.113.10" ||
		rows[0]["networkTier"] != "PREMIUM" || rows[0]["status"] != "IN_USE" || got["has_more"] != false {
		t.Errorf("external = %v", rows)
	}
	// A cursor of this region does not continue the list of another region.
	if _, err := e.invoke(addressesListID, `{"project_id":"my-project-123","region":"europe-west1","cursor":"`+
		e.mustCursor(t, base)+`"}`); !isInvalidRequest(err) {
		t.Errorf("cursor across regions: %v", err)
	}
	for i, q := range queriesOf(e) {
		if q.Get("filter") != "status = RESERVED" || q.Get("maxResults") != "1" || q.Has("views") {
			t.Errorf("request %d: %v", i, q)
		}
	}
}

// mustCursor returns the next_cursor of the first page of a list of addresses.
func (e *environment) mustCursor(t *testing.T, base string) string {
	t.Helper()
	out, err := e.invoke(addressesListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := listOf(t, out, "addresses")
	cursor, _ := got["next_cursor"].(string)
	return cursor
}

func TestAddressesGetByNameAndID(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[addressesPath+"/ext-ip"] = okReply(addressBody("ext-ip", "EXTERNAL", "IN_USE"))
	e.fake.routes[addressesPath+"/8100000000000000001"] = okReply(addressBody("int-ip", "INTERNAL", "RESERVED"))
	out, err := e.invoke(addressesGetID, `{"project_id":"my-project-123","region":"us-central1","address":"ext-ip"}`)
	if err != nil || !strings.Contains(out, `"addressType":"EXTERNAL"`) || !strings.Contains(out, "203.0.113.10") {
		t.Errorf("external: %s, %v", out, err)
	}
	out, err = e.invoke(addressesGetID, `{"project_id":"my-project-123","region":"us-central1","address":"8100000000000000001"}`)
	if err != nil || !strings.Contains(out, `"addressType":"INTERNAL"`) {
		t.Errorf("internal by id: %s, %v", out, err)
	}
	e.fake.routes[addressesPath+"/ext-ip"] = okReply(`{"name":"ext-ip","region":"https://x/compute/v1/projects/p/regions/europe-west1"}`)
	if _, err := e.invoke(addressesGetID, `{"project_id":"my-project-123","region":"us-central1","address":"ext-ip"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("other region: %v", err)
	}
}

func TestFirewallAddressToolsRefuseInvalidInputBeforeIO(t *testing.T) {
	const p = `"project_id":"my-project-123"`
	for name, c := range map[string]struct{ tool, arguments string }{
		"region path":        {addressesListID, `{` + p + `,"region":"regions/us-central1"}`},
		"region traversal":   {addressesListID, `{` + p + `,"region":"../x"}`},
		"region query":       {addressesListID, `{` + p + `,"region":"a?b=1"}`},
		"region upper":       {addressesListID, `{` + p + `,"region":"US-central1"}`},
		"region long":        {addressesListID, `{` + p + `,"region":"` + strings.Repeat("a", 64) + `"}`},
		"region missing":     {addressesListID, `{` + p + `}`},
		"region on get":      {addressesGetID, `{` + p + `,"region":"a/b","address":"a"}`},
		"region missing get": {addressesGetID, `{` + p + `,"address":"a"}`},
		"address path":       {addressesGetID, `{` + p + `,"region":"us-central1","address":"a/b"}`},
		"address traversal":  {addressesGetID, `{` + p + `,"region":"us-central1","address":".."}`},
		"address is an IP":   {addressesGetID, `{` + p + `,"region":"us-central1","address":"192.0.2.1"}`},
		"address missing":    {addressesGetID, `{` + p + `,"region":"us-central1"}`},
		"views":              {addressesListID, `{` + p + `,"region":"us-central1","views":"DEFAULT"}`},
		"zone":               {addressesListID, `{` + p + `,"region":"us-central1","zone":"us-central1-a"}`},
		"all scopes":         {addressesListID, `{` + p + `,"region":"us-central1","include_all_scopes":true}`},
		"firewall path":      {firewallsGetID, `{` + p + `,"firewall":"global/firewalls/x"}`},
		"firewall traversal": {firewallsGetID, `{` + p + `,"firewall":".."}`},
		"firewall query":     {firewallsGetID, `{` + p + `,"firewall":"a?b"}`},
		"firewall missing":   {firewallsGetID, `{` + p + `}`},
		"project path":       {firewallsListID, `{"project_id":"projects/x"}`},
		"project traversal":  {firewallsGetID, `{"project_id":"../x","firewall":"f"}`},
		"max results 501":    {firewallsListID, `{` + p + `,"max_results":501}`},
		"max results 0":      {addressesListID, `{` + p + `,"region":"us-central1","max_results":0}`},
		"order by":           {firewallsListID, `{` + p + `,"order_by":"name; drop"}`},
		"filter control":     {firewallsListID, `{` + p + `,"filter":"a\nb"}`},
		"cursor":             {firewallsListID, `{` + p + `,"cursor":"bogus"}`},
		"region on firewall": {firewallsListID, `{` + p + `,"region":"us-central1"}`},
		"all scopes fw":      {firewallsListID, `{` + p + `,"include_all_scopes":true}`},
		"unknown":            {firewallsGetID, `{` + p + `,"firewall":"f","path":"/x"}`},
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
		"addresses list": func() error {
			_, err := handleAddressesList(t.Context(), e.resolved(), e.secrets(), e.red, []byte(`{`+p+`,"region":"a/b"}`))
			return err
		},
		"addresses list views": func() error {
			_, err := handleAddressesList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{`+p+`,"region":"us-central1","views":"DEFAULT"}`))
			return err
		},
		"addresses get": func() error {
			_, err := handleAddressesGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{`+p+`,"region":"us-central1","address":"a/b"}`))
			return err
		},
		"firewalls list": func() error {
			_, err := handleFirewallsList(t.Context(), e.resolved(), e.secrets(), e.red, []byte(`{`+p+`,"max_results":501}`))
			return err
		},
		"firewalls get": func() error {
			_, err := handleFirewallsGet(t.Context(), e.resolved(), e.secrets(), e.red, []byte(`{`+p+`,"firewall":"a/b"}`))
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

func TestFirewallAddressToolsRefuseProjectsOutsideTheTargetsBeforeIO(t *testing.T) {
	calls := map[string]string{
		firewallsListID: `{"project_id":"other-project-1"}`,
		firewallsGetID:  `{"project_id":"other-project-1","firewall":"f"}`,
		addressesListID: `{"project_id":"other-project-1","region":"us-central1"}`,
		addressesGetID:  `{"project_id":"other-project-1","region":"us-central1","address":"a"}`,
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

func TestFirewallAddressToolsRefuseInvalidAnswers(t *testing.T) {
	const fw, ad = `{"project_id":"my-project-123"}`, `{"project_id":"my-project-123","region":"us-central1"}`
	for name, c := range map[string]struct{ tool, arguments, path, body string }{
		"rule without name": {firewallsListID, fw, firewallsPath, `{"items":[{"id":"1"}]}`},
		"rule not object":   {firewallsListID, fw, firewallsPath, `{"items":[1]}`},
		"rule bad name":     {firewallsListID, fw, firewallsPath, `{"items":[{"name":"../x"}]}`},
		"address other region": {addressesListID, ad, addressesPath,
			`{"items":[{"name":"a","region":"https://x/compute/v1/projects/p/regions/europe-west1"}]}`},
		"address bad name": {addressesListID, ad, addressesPath, `{"items":[{"name":"../a"}]}`},
		"token too long": {firewallsListID, fw, firewallsPath,
			`{"items":[],"nextPageToken":"` + strings.Repeat("a", maxPageTokenBytes+1) + `"}`},
	} {
		e := newEnvironment(t, nil)
		e.fake.routes[c.path] = okReply(c.body)
		if _, err := e.invoke(c.tool, c.arguments); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestFirewallAddressToolsBoundProviderText(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[firewallsPath] = okReply(`{"items":[{"name":"f","description":"` + strings.Repeat("x", 1<<20) + `"}]}`)
	out, err := e.invoke(firewallsListID, `{"project_id":"my-project-123"}`)
	if err != nil || len(out) > 1<<19 {
		t.Errorf("text was not bounded: %d bytes, %v", len(out), err)
	}
}

func TestFirewallAddressToolsFailures(t *testing.T) {
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
			firewallsListID: `{"project_id":"my-project-123"}`,
			firewallsGetID:  `{"project_id":"my-project-123","firewall":"allow-ssh"}`,
			addressesListID: `{"project_id":"my-project-123","region":"us-central1"}`,
			addressesGetID:  `{"project_id":"my-project-123","region":"us-central1","address":"web-ip"}`,
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
	e.fake.routes[firewallsPath] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}
	if _, err := e.invoke(firewallsListID, `{"project_id":"my-project-123"}`); err == nil || !strings.Contains(err.Error(), "Compute Engine") {
		t.Errorf("err = %v, want a setup message that names Compute Engine", err)
	}
}

func TestFirewallAddressToolsAreCompleteReadTools(t *testing.T) {
	e := newEnvironment(t, nil)
	for _, id := range firewallAddressTools {
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

// TestNoToolInsertsFirewallsOrAddressesOrAddsAccessConfigs proves the exclusion of decision 4: no registered
// tool of the provider is named for a Google method that creates or changes a firewall rule, an address, or an
// access config, and the four tools of this group send nothing but GET requests to the one list or get path of
// their resource.
func TestNoToolInsertsFirewallsOrAddressesOrAddsAccessConfigs(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, found := e.reg.ProviderMetadata(Provider)
	if !found {
		t.Fatal("provider not registered")
	}
	forbidden := regexp.MustCompile(`\.(firewalls|addresses|global-addresses|instances)\.(insert|patch|update|add-access-config|delete-access-config|set-)|access-config|add-access`)
	for _, tool := range metadata.Tools {
		// instances.set-machine-type changes the machine type, not network access, and is in scope.
		if forbidden.MatchString(tool.ID) && tool.ID != instancesSetMachineTypeID {
			t.Errorf("tool %s names a method that creates or changes a firewall rule or an address", tool.ID)
		}
		if strings.Contains(tool.ID, ".firewalls.") || strings.Contains(tool.ID, ".addresses.") {
			if tool.ID == firewallsDeleteID || tool.ID == addressesDeleteID {
				// The two deletions are covered by compute_delete_test.go.
				continue
			}
			if tool.Effect != config.EffectRead || !slices.Contains(firewallAddressTools, tool.ID) {
				t.Errorf("tool %s is not one of the four read tools (effect %v)", tool.ID, tool.Effect)
			}
		}
	}
	if len(firewallAddressTools) != 4 {
		t.Fatalf("tools = %v", firewallAddressTools)
	}
	paths := regexp.MustCompile(`^/compute/v1/projects/my-project-123/(global/firewalls|regions/us-central1/addresses)(/[a-z0-9-]+)?$`)
	for id, arguments := range map[string]string{
		firewallsListID: `{"project_id":"my-project-123","filter":"disabled = false"}`,
		firewallsGetID:  `{"project_id":"my-project-123","firewall":"allow-ssh"}`,
		addressesListID: `{"project_id":"my-project-123","region":"us-central1"}`,
		addressesGetID:  `{"project_id":"my-project-123","region":"us-central1","address":"web-ip"}`,
	} {
		e := newEnvironment(t, nil)
		e.fake.hook = func(_ int, req *http.Request) *reply {
			if strings.Contains(req.URL.Path, "/firewalls/") || strings.Contains(req.URL.Path, "/addresses/") {
				return &reply{status: 200, body: `{"name":"` + req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:] + `"}`}
			}
			return &reply{status: 200, body: `{"items":[]}`}
		}
		if _, err := e.invoke(id, arguments); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		requests := e.fake.api()
		if len(requests) == 0 {
			t.Fatalf("%s sent no request", id)
		}
		for _, r := range requests {
			u, _ := url.Parse(r.url)
			if r.method != http.MethodGet || r.body != "" || !paths.MatchString(u.Path) {
				t.Errorf("%s sent %s %s with body %q", id, r.method, u.Path, r.body)
			}
		}
	}
}
