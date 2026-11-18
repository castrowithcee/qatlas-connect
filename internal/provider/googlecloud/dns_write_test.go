package googlecloud

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	dnsZonePathOne = dnsZonesPath + "/example-zone"
	dnsHead        = `{"project_id":"my-project-123","managed_zone":"example-zone"`
)

// dnsWriteEnvironment is an environment whose connection may read, create, update, and delete.
func dnsWriteEnvironment(t *testing.T, targets []string) *environment {
	t.Helper()
	e := newEnvironment(t, targets)
	e.cfg.Connections["work"] = config.Connection{Service: "cloud", Credential: credential, Targets: targets,
		Effects: []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}}
	if err := e.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	e.core = application.New(e.reg, e.cfg, e.secrets(), e.red)
	return e
}

// dnsRoutes answers the zone read, the record read, and every write with the given replies.
func dnsRoutes(e *environment, zone, record string, write reply) {
	e.fake.hook = func(_ int, req *http.Request) *reply {
		switch {
		case req.URL.Path == dnsZonePathOne && req.Method == http.MethodGet:
			return &reply{status: 200, body: zone}
		case strings.HasPrefix(req.URL.Path, dnsZonePathOne+"/rrsets/") && req.Method == http.MethodGet:
			return &reply{status: 200, body: record}
		case strings.HasPrefix(req.URL.Path, dnsZonePathOne+"/"):
			return &write
		}
		return nil
	}
}

const wwwRecord = `{"kind":"dns#resourceRecordSet","name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.10"]}`

func TestRecordSetsCreateReadsTheZoneThenPostsTheRecordOnce(t *testing.T) {
	e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
	dnsRoutes(e, publicZoneBody, "", okReply(wwwRecord))
	out, err := e.invokeConfirmed(recordSetsCreateID, dnsHead+`,"name":"www.example.com.","type":"A","ttl":300,`+
		`"rrdatas":["192.0.2.10"],"client_operation_id":"op-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	record, _ := got["record_set"].(map[string]any)
	if record["name"] != "www.example.com." || record["ttl"] != float64(300) || got["visibility"] != "public" {
		t.Errorf("result = %s", out)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].method != "GET" || api[1].method != "POST" ||
		!strings.HasPrefix(api[1].url, "https://dns.googleapis.com"+dnsZonePathOne+"/rrsets?") {
		t.Fatalf("requests = %+v", api)
	}
	u, _ := url.Parse(api[1].url)
	body := bodyOf(t, api[1])
	if u.Query().Get("clientOperationId") != "op-1" || body["name"] != "www.example.com." || body["type"] != "A" ||
		body["ttl"] != float64(300) || len(body) != 4 {
		t.Errorf("url = %s, body = %s", api[1].url, api[1].body)
	}
}

func TestRecordSetsCreateChecksTypesAndValuesBeforeAnyIO(t *testing.T) {
	long := strings.Repeat("a", 256)
	bad := map[string]string{
		"A with text":         `"type":"A","ttl":60,"rrdatas":["not-an-address"]`,
		"A with IPv6":         `"type":"A","ttl":60,"rrdatas":["2001:db8::1"]`,
		"AAAA with IPv4":      `"type":"AAAA","ttl":60,"rrdatas":["192.0.2.1"]`,
		"CNAME without dot":   `"type":"CNAME","ttl":60,"rrdatas":["target.example.com"]`,
		"CNAME with two":      `"type":"CNAME","ttl":60,"rrdatas":["a.example.com.","b.example.com."]`,
		"ALIAS no dot":        `"type":"ALIAS","ttl":60,"rrdatas":["target.example.com"]`,
		"TXT bare long":       `"type":"TXT","ttl":60,"rrdatas":["` + long + `"]`,
		"TXT unquoted":        `"type":"TXT","ttl":60,"rrdatas":["v=spf1 -all"]`,
		"TXT long string":     `"type":"TXT","ttl":60,"rrdatas":["\"` + long + `\""]`,
		"MX without priority": `"type":"MX","ttl":60,"rrdatas":["mail.example.com."]`,
		"MX priority too big": `"type":"MX","ttl":60,"rrdatas":["70000 mail.example.com."]`,
		"unknown type":        `"type":"BOGUS","ttl":60,"rrdatas":["x"]`,
		"SOA":                 `"type":"SOA","ttl":60,"rrdatas":["ns.example.com. h.example.com. 1 2 3 4 5"]`,
		"no data":             `"type":"A"`,
		"no ttl":              `"type":"A","rrdatas":["192.0.2.1"]`,
		"data and policy":     `"type":"A","ttl":60,"rrdatas":["192.0.2.1"],"routingPolicy":{"geo":{"items":[{"location":"us-east1","rrdatas":["192.0.2.2"]}]}}`,
		"policy of two kinds": `"type":"A","routingPolicy":{"geo":{"items":[{"location":"us-east1","rrdatas":["192.0.2.2"]}]},"wrr":{"items":[{"weight":1,"rrdatas":["192.0.2.3"]}]}}`,
		"policy bad value":    `"type":"A","routingPolicy":{"wrr":{"items":[{"weight":1,"rrdatas":["nope"]}]}}`,
		"policy item no data": `"type":"A","routingPolicy":{"wrr":{"items":[{"weight":1}]}}`,
		"negative ttl":        `"type":"A","ttl":-1,"rrdatas":["192.0.2.1"]`,
	}
	for name, record := range bad {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		_, err := e.invokeConfirmed(recordSetsCreateID, dnsHead+`,"name":"www.example.com.",`+record+`}`)
		if err == nil || (!isInvalidRequest(err) && !strings.Contains(err.Error(), "invalid")) {
			t.Errorf("%s: err = %v", name, err)
		}
		noIO(t, e, name)
	}
	good := map[string]string{
		"A":        `"type":"A","ttl":60,"rrdatas":["192.0.2.1","192.0.2.2"]`,
		"AAAA":     `"type":"AAAA","ttl":60,"rrdatas":["2001:db8::1"]`,
		"CNAME":    `"type":"CNAME","ttl":60,"rrdatas":["target.example.com."]`,
		"TXT":      `"type":"TXT","ttl":60,"rrdatas":["\"v=spf1 -all\"","\"a\" \"b\""]`,
		"ALIAS":    `"type":"ALIAS","ttl":60,"rrdatas":["target.example.com."]`,
		"TXT bare": `"type":"TXT","ttl":60,"rrdatas":["synthetic-token"]`,
		"MX":       `"type":"MX","ttl":60,"rrdatas":["10 mail.example.com.","0 ."]`,
		"policy":   `"type":"A","routingPolicy":{"wrr":{"items":[{"weight":1,"rrdatas":["192.0.2.3"]},{"weight":0.5,"rrdatas":["192.0.2.4"]}]}}`,
		"geo":      `"type":"A","ttl":60,"routingPolicy":{"geo":{"items":[{"location":"us-east1","rrdatas":["192.0.2.2"]}],"enableFencing":true}}`,
		"failover": `"type":"A","routingPolicy":{"primaryBackup":{"primaryTargets":{"externalEndpoints":["192.0.2.1"]},"backupGeoTargets":{"items":[{"location":"us-east1","rrdatas":["192.0.2.2"]}]},"trickleTraffic":0.1},"healthCheck":"https://www.googleapis.com/compute/v1/projects/my-project-123/global/healthChecks/hc"}`,
	}
	for name, record := range good {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		typ := strings.Split(strings.Split(record, `"type":"`)[1], `"`)[0]
		dnsRoutes(e, publicZoneBody, "", okReply(`{"name":"www.example.com.","type":"`+typ+`"}`))
		if _, err := e.invokeConfirmed(recordSetsCreateID, dnsHead+`,"name":"www.example.com.",`+record+`}`); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestApexSOAAndNSAreRefusedButSubzoneNSIsAllowed(t *testing.T) {
	ns := `"type":"NS","ttl":300,"rrdatas":["ns1.example.net."]`
	for name, call := range map[string][2]string{
		"create apex NS":               {recordSetsCreateID, dnsHead + `,"name":"example.com.",` + ns + `}`},
		"create apex NS in other case": {recordSetsCreateID, dnsHead + `,"name":"Example.COM.",` + ns + `}`},
		"patch apex NS":                {recordSetsPatchID, dnsHead + `,"name":"example.com.","type":"NS","ttl":60}`},
		"delete apex NS":               {recordSetsDeleteID, dnsHead + `,"name":"example.com.","type":"NS"}`},
		"change adds":                  {changesCreateID, dnsHead + `,"additions":[{"name":"example.com.",` + ns + `}]}`},
		"change deletes":               {changesCreateID, dnsHead + `,"deletions":[{"name":"example.com.",` + ns + `}]}`},
	} {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		dnsRoutes(e, publicZoneBody, wwwRecord, okReply(`{}`))
		_, err := e.invokeConfirmed(call[0], call[1])
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "apex") {
			t.Errorf("%s: err = %v", name, err)
		}
		for _, r := range e.fake.api() {
			if r.method != "GET" {
				t.Errorf("%s: %s reached Google", name, r.method)
			}
		}
	}
	for name, call := range map[string][2]string{
		"soa create": {recordSetsCreateID, dnsHead + `,"name":"example.com.","type":"SOA","ttl":60,"rrdatas":["x"]}`},
		"soa patch":  {recordSetsPatchID, dnsHead + `,"name":"example.com.","type":"SOA","ttl":60}`},
		"soa delete": {recordSetsDeleteID, dnsHead + `,"name":"example.com.","type":"SOA"}`},
		"soa change": {changesCreateID, dnsHead + `,"deletions":[{"name":"example.com.","type":"SOA","ttl":1,"rrdatas":["x"]}]}`},
	} {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		if _, err := e.invokeConfirmed(call[0], call[1]); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
		noIO(t, e, name)
	}
	e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
	dnsRoutes(e, publicZoneBody, "", okReply(`{"name":"sub.example.com.","type":"NS","ttl":300,"rrdatas":["ns1.example.net."]}`))
	if _, err := e.invokeConfirmed(recordSetsCreateID, dnsHead+`,"name":"sub.example.com.",`+ns+`}`); err != nil {
		t.Errorf("NS of a subzone: %v", err)
	}
}

func TestRecordSetsPatchReadsThePreviousRecordAndSendsOnlyWhatChanges(t *testing.T) {
	e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
	dnsRoutes(e, privateZoneBody0(), wwwRecord, okReply(`{"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.10"]}`))
	out, err := e.invokeConfirmed(recordSetsPatchID, dnsHead+`,"name":"www.example.com.","type":"A","ttl":60}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	previous, _ := got["previous"].(map[string]any)
	record, _ := got["record_set"].(map[string]any)
	if previous["ttl"] != float64(300) || record["ttl"] != float64(60) || got["visibility"] != "private" {
		t.Errorf("result = %s", out)
	}
	if m := methodsOf(e); strings.Join(m, " ") != "GET GET PATCH" {
		t.Errorf("methods = %v", m)
	}
	api := e.fake.api()
	body := bodyOf(t, api[2])
	if !strings.HasSuffix(api[2].url, "/rrsets/www.example.com./A") || body["ttl"] != float64(60) || body["rrdatas"] != nil ||
		len(body) != 3 {
		t.Errorf("url = %s, body = %s", api[2].url, api[2].body)
	}
	// A patch that changes nothing is refused before any request.
	e = dnsWriteEnvironment(t, []string{"projects/my-project-123"})
	if _, err := e.invokeConfirmed(recordSetsPatchID, dnsHead+`,"name":"www.example.com.","type":"A"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	noIO(t, e, "empty patch")
}

// privateZoneBody0 is the example zone, described as a private one.
func privateZoneBody0() string {
	return strings.Replace(publicZoneBody, `"visibility":"public"`, `"visibility":"private"`, 1)
}

func TestRecordSetsDeleteOfAnExistingAndAMissingRecord(t *testing.T) {
	e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
	dnsRoutes(e, publicZoneBody, wwwRecord, okReply(`{}`))
	out, err := e.invokeConfirmed(recordSetsDeleteID, dnsHead+`,"name":"www.example.com.","type":"A"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	previous, _ := got["previous"].(map[string]any)
	if got["deleted"] != true || previous["name"] != "www.example.com." || got["visibility"] != "public" {
		t.Errorf("result = %s", out)
	}
	api := e.fake.api()
	if len(api) != 3 || api[2].method != "DELETE" || api[2].body != "" ||
		!strings.HasSuffix(api[2].url, "/rrsets/www.example.com./A") {
		t.Fatalf("requests = %+v", api)
	}
	// A record that does not exist ends at the read; nothing is deleted.
	e = dnsWriteEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes[dnsZonePathOne] = okReply(publicZoneBody)
	e.fake.routes[dnsZonePathOne+"/rrsets/missing.example.com./A"] = reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}
	_, err = e.invokeConfirmed(recordSetsDeleteID, dnsHead+`,"name":"missing.example.com.","type":"A"}`)
	if classOf(err) != provider.ClassNotFound {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
	if m := methodsOf(e); strings.Join(m, " ") != "GET GET" {
		t.Errorf("methods = %v", m)
	}
}

func TestChangesCreateAddsAndDeletesAtomicallyAndReportsTheStatus(t *testing.T) {
	for _, status := range []string{"pending", "done"} {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		dnsRoutes(e, publicZoneBody, "", okReply(`{"kind":"dns#change","id":"7","status":"`+status+`","startTime":"2026-03-04T05:06:07Z",`+
			`"additions":[{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.10"]}],`+
			`"deletions":[{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.9"]}]}`))
		out, err := e.invokeConfirmed(changesCreateID, dnsHead+`,"client_operation_id":"c1",`+
			`"deletions":[{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.9"]}],`+
			`"additions":[{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.10"]}]}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		change, _ := got["change"].(map[string]any)
		if change["status"] != status || change["id"] != "7" || got["visibility"] != "public" {
			t.Errorf("result = %s", out)
		}
		api := e.fake.api()
		body := bodyOf(t, api[1])
		if len(api) != 2 || api[1].method != "POST" || !strings.Contains(api[1].url, dnsZonePathOne+"/changes?clientOperationId=c1") ||
			len(body["additions"].([]any)) != 1 || len(body["deletions"].([]any)) != 1 {
			t.Errorf("requests = %+v", api)
		}
	}
	// An empty change is refused before any request, like one without a record in either list.
	for name, args := range map[string]string{"empty": dnsHead + `}`,
		"empty lists": dnsHead + `,"additions":[],"deletions":[]}`} {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		if _, err := e.invokeConfirmed(changesCreateID, args); err == nil {
			t.Errorf("%s: no error", name)
		}
		noIO(t, e, name)
	}
}

func TestChangesCreateConflictIsReportedAndNotRepeated(t *testing.T) {
	for name, status := range map[string]reply{
		"aborted": {status: 409, body: errorObject(409, "ABORTED", "")},
		"exists":  {status: 409, body: errorObject(409, "ALREADY_EXISTS", "")},
	} {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		dnsRoutes(e, publicZoneBody, "", status)
		_, err := e.invokeConfirmed(changesCreateID, dnsHead+`,"additions":[{"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.1"]}]}`)
		if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "conflict") ||
			strings.Contains(err.Error(), detailCanary) {
			t.Errorf("%s: class = %q, err = %v", name, classOf(err), err)
		}
		if m := methodsOf(e); strings.Join(m, " ") != "GET POST" {
			t.Errorf("%s: methods = %v", name, m)
		}
	}
}

func TestAServerErrorOfAWriteLeavesTheOutcomeOpenAndIsNotRepeated(t *testing.T) {
	for id, args := range map[string]string{
		recordSetsCreateID: dnsHead + `,"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.1"]}`,
		changesCreateID:    dnsHead + `,"additions":[{"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.1"]}]}`,
	} {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		dnsRoutes(e, publicZoneBody, "", reply{status: 503, body: errorObject(503, "UNAVAILABLE", "")})
		_, err := e.invokeConfirmed(id, args)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("%s: err = %v", id, err)
		}
		if m := methodsOf(e); strings.Join(m, " ") != "GET POST" {
			t.Errorf("%s: methods = %v", id, m)
		}
	}
	// An answer that is not the record asked for also leaves the outcome open.
	e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
	dnsRoutes(e, publicZoneBody, "", okReply(`{"name":"other.example.com.","type":"A"}`))
	_, err := e.invokeConfirmed(recordSetsCreateID, dnsHead+`,"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.1"]}`)
	if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
}

func TestDNSWritesRefuseAProjectOutsideTheTargetsAndABadZoneBeforeIO(t *testing.T) {
	record := `,"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.1"]}`
	for _, call := range [][2]string{
		{recordSetsCreateID, `{"project_id":"foreign-project-1","managed_zone":"example-zone"` + record},
		{recordSetsPatchID, `{"project_id":"foreign-project-1","managed_zone":"example-zone"` + record},
		{recordSetsDeleteID, `{"project_id":"foreign-project-1","managed_zone":"example-zone","name":"www.example.com.","type":"A"}`},
		{changesCreateID, `{"project_id":"foreign-project-1","managed_zone":"example-zone","additions":[{"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.1"]}]}`},
		{recordSetsCreateID, `{"project_id":"my-project-123","managed_zone":"../other"` + record},
		{recordSetsCreateID, `{"project_id":"projects/my-project-123","managed_zone":"example-zone"` + record},
		{recordSetsDeleteID, dnsHead + `,"name":"www.example.com.","type":"A","client_operation_id":"a b"}`},
	} {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		_, err := e.invokeConfirmed(call[0], call[1])
		if err == nil || strings.Contains(err.Error(), "foreign-project-1") {
			t.Errorf("%s: err = %v", call[0], err)
		}
		noIO(t, e, call[0])
	}
}

func TestDNSWritesDoNotTrustAZoneThatIsNotTheOneAskedFor(t *testing.T) {
	for name, zone := range map[string]string{
		"other name":        strings.Replace(publicZoneBody, `"name":"example-zone"`, `"name":"another-zone"`, 1),
		"no visibility":     strings.Replace(publicZoneBody, `"visibility":"public"`, `"visibility":"other"`, 1),
		"bad dns name":      strings.Replace(publicZoneBody, `"dnsName":"example.com."`, `"dnsName":"example.com"`, 1),
		"zone is no object": `[]`,
	} {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		dnsRoutes(e, zone, "", okReply(wwwRecord))
		_, err := e.invokeConfirmed(recordSetsCreateID, dnsHead+`,"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.1"]}`)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: class = %q, err = %v", name, classOf(err), err)
		}
		if m := methodsOf(e); strings.Join(m, " ") != "GET" {
			t.Errorf("%s: methods = %v", name, m)
		}
	}
}

func TestDNSWritesNeedConfirmationAndDescribeTheChange(t *testing.T) {
	args := map[string]string{
		recordSetsCreateID: dnsHead + `,"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.1"]}`,
		recordSetsPatchID:  dnsHead + `,"name":"www.example.com.","type":"A","ttl":60}`,
		recordSetsDeleteID: dnsHead + `,"name":"www.example.com.","type":"A"}`,
		changesCreateID: dnsHead + `,"additions":[{"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.1"]}],` +
			`"deletions":[{"name":"old.example.com.","type":"TXT","ttl":60,"rrdatas":["\"x\""]}]}`,
	}
	effects := map[string]string{recordSetsCreateID: "create", changesCreateID: "create", recordSetsPatchID: "update",
		recordSetsDeleteID: "delete"}
	for id, call := range args {
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		if _, err := e.invoke(id, call); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		if d.Risk.Confirmation != "required" || string(d.Risk.Effect) != effects[id] || !d.Risk.OpenWorld ||
			d.RequiresToolAllowList || !strings.Contains(d.Description, "apex") {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if (id == recordSetsCreateID || id == changesCreateID) && string(d.Risk.Idempotency) != "non_idempotent" {
			t.Errorf("%s idempotency = %s", id, d.Risk.Idempotency)
		}
		summary := e.reg.ConfirmationSummary(id)
		if summary == nil {
			t.Fatalf("%s has no confirmation summary", id)
		}
		text := summary(json.RawMessage(call))
		for _, part := range []string{"my-project-123", "example-zone", "example.com."} {
			if !strings.Contains(text, part) {
				t.Errorf("%s summary %q lacks %q", id, text, part)
			}
		}
	}
	e := dnsWriteEnvironment(t, nil)
	text := e.reg.ConfirmationSummary(changesCreateID)(json.RawMessage(args[changesCreateID]))
	for _, part := range []string{"192.0.2.1", "old.example.com.", "TXT", "atomic"} {
		if !strings.Contains(text, part) {
			t.Errorf("change summary %q lacks %q", text, part)
		}
	}
}

func TestDNSWriteToolsRunWithTheProfileAlone(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		if profile.ID != "dns-write" {
			continue
		}
		var scopes []string
		for _, tool := range profile.Tools {
			scopes = append(scopes, metadata.Token.Scopes[tool]...)
		}
		for _, want := range []string{scopeDNSReadWrite, scopeDNSReadOnly, scopeReadOnly} {
			if !strings.Contains(strings.Join(scopes, " "), want) {
				t.Errorf("dns-write lacks scope %s in %v", want, scopes)
			}
		}
		return
	}
	t.Error("no dns-write profile")
}

func TestPatchAndDeleteStopBeforeTheWriteWhenTheExpectationDiffers(t *testing.T) {
	two := `{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.10","192.0.2.11"]}`
	for _, id := range []string{recordSetsPatchID, recordSetsDeleteID} {
		call := dnsHead + `,"name":"www.example.com.","type":"A"`
		if id == recordSetsPatchID {
			call += `,"ttl":60`
		}
		for name, expected := range map[string]string{
			"ttl differs":    `,"expected_ttl":301`,
			"values differ":  `,"expected_rrdatas":["192.0.2.10"]`,
			"other value":    `,"expected_rrdatas":["192.0.2.10","192.0.2.99"]`,
			"ttl ok, values": `,"expected_ttl":300,"expected_rrdatas":["192.0.2.12"]`,
		} {
			e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
			dnsRoutes(e, publicZoneBody, two, okReply(`{}`))
			_, err := e.invokeConfirmed(id, call+expected+`}`)
			if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "nothing was changed") {
				t.Errorf("%s %s: class = %q, err = %v", id, name, classOf(err), err)
			}
			if m := methodsOf(e); strings.Join(m, " ") != "GET GET" {
				t.Errorf("%s %s: methods = %v", id, name, m)
			}
		}
		// A matching expectation, in another order and with a repeat, lets the change through.
		e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
		dnsRoutes(e, publicZoneBody, two, okReply(`{"name":"www.example.com.","type":"A","ttl":60,"rrdatas":["192.0.2.10"]}`))
		if _, err := e.invokeConfirmed(id, call+`,"expected_ttl":300,"expected_rrdatas":["192.0.2.11","192.0.2.10","192.0.2.11"]}`); err != nil {
			t.Errorf("%s: %v", id, err)
		}
		if m := methodsOf(e); len(m) != 3 || m[2] == "GET" {
			t.Errorf("%s: methods = %v", id, m)
		}
		// The confirmation names the expectation.
		e = dnsWriteEnvironment(t, nil)
		text := e.reg.ConfirmationSummary(id)(json.RawMessage(call + `,"expected_ttl":300,"expected_rrdatas":["192.0.2.10"]}`))
		if !strings.Contains(text, "only if the record set now has") || !strings.Contains(text, "ttl 300") ||
			!strings.Contains(text, "192.0.2.10") {
			t.Errorf("%s summary = %q", id, text)
		}
	}
	// Expectations out of form are refused before any request.
	e := dnsWriteEnvironment(t, []string{"projects/my-project-123"})
	if _, err := e.invokeConfirmed(recordSetsDeleteID, dnsHead+`,"name":"www.example.com.","type":"A","expected_ttl":-1}`); err == nil {
		t.Error("negative expected_ttl accepted")
	}
	noIO(t, e, "bad expectation")
}
