package googlecloud

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const dnsZonesPath = "/dns/v1/projects/my-project-123/managedZones"

const publicZoneBody = `{"kind":"dns#managedZone","name":"example-zone","id":"1234567890123456789","dnsName":"example.com.",` +
	`"description":"public zone","nameServers":["ns-cloud-a1.example-dns.test.","ns-cloud-a2.example-dns.test."],` +
	`"visibility":"public","dnssecConfig":{"kind":"dns#managedZoneDnsSecConfig","state":"on","nonExistence":"nsec3",` +
	`"defaultKeySpecs":[{"kind":"dns#dnsKeySpec","algorithm":"ecdsap256sha256","keyLength":256,"keyType":"keySigning"}]},` +
	`"cloudLoggingConfig":{"kind":"dns#managedZoneCloudLoggingConfig","enableLogging":true},` +
	`"labels":{"env":"test"},"creationTime":"2026-01-02T03:04:05.678Z","unknownMember":"x"}`

const privateZoneBody = `{"kind":"dns#managedZone","name":"internal-zone","id":"987654321","dnsName":"internal.example.",` +
	`"visibility":"private","privateVisibilityConfig":{"kind":"dns#managedZonePrivateVisibilityConfig",` +
	`"networks":[{"kind":"dns#managedZonePrivateVisibilityConfigNetwork","networkUrl":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/networks/default"}],` +
	`"gkeClusters":[{"gkeClusterName":"projects/my-project-123/locations/us-central1/clusters/example-cluster"}]},` +
	`"forwardingConfig":{"targetNameServers":[{"ipv4Address":"192.0.2.53","forwardingPath":"private"}],` +
	`"outboundEndpoints":[{"name":"projects/my-project-123/regions/us-central1/outboundEndpoints/example"}]},` +
	`"serviceDirectoryConfig":{"namespace":{"namespaceUrl":"https://servicedirectory.googleapis.com/v1/projects/my-project-123/locations/us-central1/namespaces/example"}},` +
	`"nameServerSet":"example-set","creationTime":"2026-02-03T04:05:06Z"}`

func TestManagedZonesListFollowsTwoPagesWithDNSNameFilter(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != dnsZonesPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"kind":"dns#managedZonesListResponse","managedZones":[` + publicZoneBody +
				`],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"managedZones":[` + privateZoneBody + `]}`}
	}
	base := `{"project_id":"my-project-123","dns_name":"example.com.","max_results":1`
	out, err := e.invoke(managedZonesListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "managed_zones")
	if len(rows) != 1 || got["has_more"] != true || rows[0]["name"] != "example-zone" ||
		rows[0]["id"] != "1234567890123456789" || rows[0]["dnsName"] != "example.com." ||
		rows[0]["visibility"] != "public" || rows[0]["description"] != "public zone" ||
		len(rows[0]["nameServers"].([]any)) != 2 || rows[0]["labels"].(map[string]any)["env"] != "test" ||
		rows[0]["creationTime"] == nil || rows[0]["dnssecConfig"].(map[string]any)["state"] != "on" ||
		rows[0]["cloudLoggingConfig"].(map[string]any)["enableLogging"] != true || rows[0]["unknownMember"] != nil {
		t.Fatalf("page 1 = %s", out)
	}
	out, err = e.invoke(managedZonesListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "managed_zones")
	if len(rows) != 1 || got["has_more"] != false || got["next_cursor"] != nil || rows[0]["visibility"] != "private" ||
		rows[0]["nameServerSet"] != "example-set" || rows[0]["serviceDirectoryConfig"] == nil ||
		rows[0]["forwardingConfig"].(map[string]any)["outboundEndpoints"] == nil {
		t.Fatalf("page 2 = %s", out)
	}
	pv := rows[0]["privateVisibilityConfig"].(map[string]any)
	if len(pv["networks"].([]any)) != 1 || len(pv["gkeClusters"].([]any)) != 1 {
		t.Errorf("private visibility = %v", pv)
	}
	queries := queriesOf(e)
	if len(queries) != 2 || queries[0].Get("dnsName") != "example.com." || queries[0].Get("maxResults") != "1" ||
		queries[0].Has("pageToken") || queries[1].Get("pageToken") != "p2" || queries[1].Get("dnsName") != "example.com." {
		t.Errorf("queries = %v", queries)
	}
	for _, p := range apiPaths(e) {
		if p != "dns.googleapis.com"+dnsZonesPath {
			t.Errorf("path = %s", p)
		}
	}
	// A cursor of another filter or project is refused before any request.
	before := len(e.fake.api())
	for _, arguments := range []string{`{"project_id":"my-project-123","dns_name":"other.example.","cursor":"bogus"}`,
		`{"project_id":"my-project-123","cursor":"bogus"}`} {
		if _, err := e.invoke(managedZonesListID, arguments); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", arguments, err)
		}
	}
	if len(e.fake.api()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestManagedZonesGetReadsByNameAndByID(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes[dnsZonesPath+"/example-zone"] = okReply(publicZoneBody)
	e.fake.routes[dnsZonesPath+"/1234567890123456789"] = okReply(publicZoneBody)
	e.fake.routes[dnsZonesPath+"/internal-zone"] = okReply(publicZoneBody)
	for _, zone := range []string{"example-zone", "1234567890123456789"} {
		out, err := e.invoke(managedZonesGetID, `{"project_id":"my-project-123","managed_zone":"`+zone+`"}`)
		if err != nil || !strings.Contains(out, `"name":"example-zone"`) || strings.Contains(out, "unknownMember") {
			t.Errorf("%s: out = %s, err = %v", zone, out, err)
		}
	}
	// An answer for another zone than the one asked for is invalid.
	if _, err := e.invoke(managedZonesGetID, `{"project_id":"my-project-123","managed_zone":"internal-zone"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestDNSToolsRefuseBadInputBeforeIO(t *testing.T) {
	const project = `"project_id":"my-project-123"`
	for name, call := range map[string][2]string{
		"zone path":           {managedZonesGetID, `{` + project + `,"managed_zone":"a/b"}`},
		"zone uppercase":      {managedZonesGetID, `{` + project + `,"managed_zone":"Example"}`},
		"zone query":          {managedZonesGetID, `{` + project + `,"managed_zone":"zone?x=1"}`},
		"zone escape":         {managedZonesGetID, `{` + project + `,"managed_zone":"%2e%2e"}`},
		"zone long":           {recordSetsListID, `{` + project + `,"managed_zone":"` + strings.Repeat("a", 64) + `"}`},
		"project path":        {managedZonesListID, `{"project_id":"projects/my-project-123"}`},
		"dns name no dot":     {managedZonesListID, `{` + project + `,"dns_name":"example.com"}`},
		"dns name wildcard":   {managedZonesListID, `{` + project + `,"dns_name":"*.example.com."}`},
		"max zero":            {managedZonesListID, `{` + project + `,"max_results":0}`},
		"max large":           {recordSetsListID, `{` + project + `,"managed_zone":"z","max_results":1001}`},
		"record path":         {recordSetsGetID, `{` + project + `,"managed_zone":"z","name":"a/b.example.com.","type":"A"}`},
		"record no dot":       {recordSetsGetID, `{` + project + `,"managed_zone":"z","name":"www.example.com","type":"A"}`},
		"record dotdot":       {recordSetsGetID, `{` + project + `,"managed_zone":"z","name":"www..example.com.","type":"A"}`},
		"record query":        {recordSetsGetID, `{` + project + `,"managed_zone":"z","name":"a.example.com.?x","type":"A"}`},
		"record wildcard mid": {recordSetsGetID, `{` + project + `,"managed_zone":"z","name":"a.*.example.com.","type":"A"}`},
		"record long label":   {recordSetsGetID, `{` + project + `,"managed_zone":"z","name":"` + strings.Repeat("a", 64) + `.example.com.","type":"A"}`},
		"record long name":    {recordSetsGetID, `{` + project + `,"managed_zone":"z","name":"` + strings.Repeat("abcdefghi.", 26) + `","type":"A"}`},
		"type lowercase":      {recordSetsGetID, `{` + project + `,"managed_zone":"z","name":"a.example.com.","type":"a"}`},
		"type path":           {recordSetsGetID, `{` + project + `,"managed_zone":"z","name":"a.example.com.","type":"A/x"}`},
		"type without name":   {recordSetsListID, `{` + project + `,"managed_zone":"z","type":"A"}`},
		"filter and name":     {recordSetsListID, `{` + project + `,"managed_zone":"z","name":"a.example.com.","filter":"name=\"a.example.com.\""}`},
		"filter control":      {recordSetsListID, `{` + project + `,"managed_zone":"z","filter":"a\u0007b"}`},
		"unknown argument":    {recordSetsListID, `{` + project + `,"managed_zone":"z","path":"/x"}`},
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

func TestDNSToolsRefuseAProjectOutsideTheTargetsBeforeIO(t *testing.T) {
	for _, call := range [][2]string{
		{managedZonesListID, `{"project_id":"foreign-project-1"}`},
		{managedZonesGetID, `{"project_id":"foreign-project-1","managed_zone":"example-zone"}`},
		{recordSetsListID, `{"project_id":"foreign-project-1","managed_zone":"example-zone"}`},
		{recordSetsGetID, `{"project_id":"foreign-project-1","managed_zone":"example-zone","name":"www.example.com.","type":"A"}`},
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

const dnsRecordsPath = dnsZonesPath + "/example-zone/rrsets"

func TestRecordSetsListFiltersByNameAndTypeAndFollowsPages(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != dnsRecordsPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"kind":"dns#resourceRecordSetsListResponse","rrsets":[{"kind":"dns#resourceRecordSet",` +
				`"name":"example.com.","type":"TXT","ttl":300,"rrdatas":["\"v=spf1 -all\"","\"synthetic-text-value\""],"unknownMember":1}],` +
				`"nextPageToken":"r2"}`}
		}
		return &reply{status: 200, body: `{"rrsets":[{"name":"example.com.","type":"TXT","ttl":60,"rrdatas":["\"second\""]}]}`}
	}
	base := `{"project_id":"my-project-123","managed_zone":"example-zone","name":"example.com.","type":"TXT"`
	out, err := e.invoke(recordSetsListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "record_sets")
	if len(rows) != 1 || got["has_more"] != true || rows[0]["name"] != "example.com." || rows[0]["type"] != "TXT" ||
		rows[0]["ttl"] != float64(300) || len(rows[0]["rrdatas"].([]any)) != 2 || rows[0]["kind"] != "dns#resourceRecordSet" ||
		rows[0]["unknownMember"] != nil {
		t.Fatalf("page 1 = %s", out)
	}
	cursor := got["next_cursor"].(string)
	out, err = e.invoke(recordSetsListID, base+`,"cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "record_sets")
	if len(rows) != 1 || got["has_more"] != false || rows[0]["ttl"] != float64(60) {
		t.Fatalf("page 2 = %s", out)
	}
	queries := queriesOf(e)
	if len(queries) != 2 || queries[0].Get("name") != "example.com." || queries[0].Get("type") != "TXT" ||
		queries[1].Get("pageToken") != "r2" || queries[1].Get("type") != "TXT" {
		t.Errorf("queries = %v", queries)
	}
	// A cursor of another type is refused before any request.
	before := len(e.fake.api())
	if _, err := e.invoke(recordSetsListID, `{"project_id":"my-project-123","managed_zone":"example-zone","name":"example.com.","type":"A","cursor":"`+
		cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	if len(e.fake.api()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestRecordSetsListPassesTheFilterAlone(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[dnsRecordsPath] = okReply(`{"rrsets":[]}`)
	filter := `name=has_suffix("example.com.") AND type="A"`
	out, err := e.invoke(recordSetsListID, `{"project_id":"my-project-123","managed_zone":"example-zone","filter":`+
		`"name=has_suffix(\"example.com.\") AND type=\"A\""}`)
	if err != nil || out != `{"has_more":false}` {
		t.Fatalf("out = %s, err = %v", out, err)
	}
	q := queriesOf(e)[0]
	if q.Get("filter") != filter || q.Has("name") || q.Has("type") {
		t.Errorf("query = %v", q)
	}
}

func TestRecordSetsGetReturnsTheRoutingPolicyAndPathEscapesTheName(t *testing.T) {
	const body = `{"kind":"dns#resourceRecordSet","name":"www.example.com.","type":"A","ttl":30,` +
		`"routingPolicy":{"kind":"dns#rRSetRoutingPolicy","healthCheck":"projects/my-project-123/global/healthChecks/example",` +
		`"wrr":{"items":[{"weight":0.7,"rrdatas":["192.0.2.1"],"healthCheckedTargets":{"externalEndpoints":["192.0.2.9"]}},` +
		`{"weight":0.3,"rrdatas":["192.0.2.2"],"signatureRrdatas":["synthetic-signature"]}]},` +
		`"geo":{"enableFencing":true,"items":[{"location":"us-east1","rrdatas":["192.0.2.3"],` +
		`"healthCheckedTargets":{"internalLoadBalancers":[{"loadBalancerType":"regionalL4ilb","ipAddress":"192.0.2.4",` +
		`"port":"80","ipProtocol":"tcp","networkUrl":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/networks/default",` +
		`"project":"my-project-123","region":"us-east1"}]}}]},` +
		`"primaryBackup":{"trickleTraffic":0.1,"primaryTargets":{"externalEndpoints":["192.0.2.5"]},` +
		`"backupGeoTargets":{"items":[{"location":"europe-west1","rrdatas":["192.0.2.6"],` +
		`"healthCheckedTargets":{"internalLoadBalancers":[{"ipAddress":"192.0.2.7","loadBalancerType":"globalL7ilb"}]}}]}}}}`
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes[dnsRecordsPath+"/www.example.com./A"] = okReply(body)
	e.fake.routes[dnsRecordsPath+"/*.example.com./A"] = okReply(`{"name":"*.example.com.","type":"A","ttl":5,"rrdatas":["192.0.2.8"]}`)
	out, err := e.invoke(recordSetsGetID, `{"project_id":"my-project-123","managed_zone":"example-zone","name":"www.example.com.","type":"A"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"weight":0.7`, `"externalEndpoints":["192.0.2.9"]`, `"signatureRrdatas":["synthetic-signature"]`,
		`"enableFencing":true`, `"location":"us-east1"`, `"loadBalancerType":"regionalL4ilb"`, `"ipProtocol":"tcp"`,
		`"trickleTraffic":0.1`, `"location":"europe-west1"`, `"loadBalancerType":"globalL7ilb"`, `"healthCheck":"projects/`,
		`"ttl":30`} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %s: %s", want, out)
		}
	}
	out, err = e.invoke(recordSetsGetID, `{"project_id":"my-project-123","managed_zone":"example-zone","name":"*.example.com.","type":"A"}`)
	if err != nil || !strings.Contains(out, `"name":"*.example.com."`) {
		t.Errorf("wildcard: out = %s, err = %v", out, err)
	}
	for _, r := range e.fake.api() {
		u, _ := url.Parse(r.url)
		if r.method != http.MethodGet || u.RawQuery != "" || u.Host != "dns.googleapis.com" {
			t.Errorf("request = %s %s", r.method, r.url)
		}
	}
}

func TestRecordSetsBoundTextValuesAndRefuseBadRows(t *testing.T) {
	long := strings.Repeat("x", 3000)
	e := newEnvironment(t, nil)
	e.fake.routes[dnsRecordsPath+"/example.com./TXT"] = okReply(`{"name":"example.com.","type":"TXT","rrdatas":["` + long + `","ctl\u0007x"]}`)
	out, err := e.invoke(recordSetsGetID, `{"project_id":"my-project-123","managed_zone":"example-zone","name":"example.com.","type":"TXT"}`)
	if err != nil || strings.Contains(out, long) || !strings.Contains(out, "…") || strings.Contains(out, `\u0007`) {
		t.Errorf("out length = %d, err = %v", len(out), err)
	}
	for _, body := range []string{`{"type":"A"}`, `{"name":"example.com."}`, `{"name":"example.com.","type":"A","ttl":"x"}`,
		`{"name":"example.com.","type":"A","rrdatas":"x"}`, `{"name":"example.com.","type":"AAAA"}`} {
		e = newEnvironment(t, nil)
		e.fake.routes[dnsRecordsPath+"/example.com./A"] = okReply(body)
		if _, err := e.invoke(recordSetsGetID, `{"project_id":"my-project-123","managed_zone":"example-zone","name":"example.com.","type":"A"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", body, err)
		}
	}
	for _, body := range []string{`{"name":"example-zone","dnsName":5}`, `{"name":"Bad Name"}`, `{"id":"1"}`, `{"name":"z","nameServers":"x"}`} {
		e = newEnvironment(t, nil)
		e.fake.routes[dnsZonesPath] = okReply(`{"managedZones":[` + body + `]}`)
		if _, err := e.invoke(managedZonesListID, `{"project_id":"my-project-123"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", body, err)
		}
	}
}

func TestDNSToolsMapErrors(t *testing.T) {
	calls := map[string]string{
		managedZonesListID: `{"project_id":"my-project-123"}`,
		managedZonesGetID:  `{"project_id":"my-project-123","managed_zone":"example-zone"}`,
		recordSetsListID:   `{"project_id":"my-project-123","managed_zone":"example-zone"}`,
		recordSetsGetID:    `{"project_id":"my-project-123","managed_zone":"example-zone","name":"www.example.com.","type":"A"}`,
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
