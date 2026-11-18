package googlecloud

import (
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	dnsChangesPath = dnsZonesPath + "/example-zone/changes"
	dnsKeysPath    = dnsZonesPath + "/example-zone/dnsKeys"
)

const doneChangeBody = `{"kind":"dns#change","id":"2","startTime":"2026-03-04T05:06:07.890Z","status":"done","isServing":true,` +
	`"additions":[{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.10"]}],` +
	`"deletions":[{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.9"]}],"unknownMember":1}`

const pendingChangeBody = `{"kind":"dns#change","id":"1","startTime":"2026-03-03T05:06:07Z","status":"pending","isServing":false,` +
	`"additions":[{"name":"example.com.","type":"TXT","ttl":60,"rrdatas":["\"synthetic-text-value\""]}]}`

func TestChangesListSortsAndFollowsTwoPages(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != dnsChangesPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"kind":"dns#changesListResponse","changes":[` + doneChangeBody +
				`],"nextPageToken":"c2"}`}
		}
		return &reply{status: 200, body: `{"changes":[` + pendingChangeBody + `]}`}
	}
	for _, order := range []string{"ascending", "descending"} {
		e.fake.requests = nil
		base := `{"project_id":"my-project-123","managed_zone":"example-zone","sort_by":"changeSequence","sort_order":"` +
			order + `","max_results":1`
		out, err := e.invoke(changesListID, base+`}`)
		if err != nil {
			t.Fatal(err)
		}
		rows, got := listOf(t, out, "changes")
		if len(rows) != 1 || got["has_more"] != true || rows[0]["id"] != "2" || rows[0]["status"] != "done" ||
			rows[0]["isServing"] != true || rows[0]["startTime"] == nil || rows[0]["kind"] != "dns#change" ||
			len(rows[0]["additions"].([]any)) != 1 || len(rows[0]["deletions"].([]any)) != 1 ||
			rows[0]["unknownMember"] != nil {
			t.Fatalf("page 1 = %s", out)
		}
		out, err = e.invoke(changesListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		rows, got = listOf(t, out, "changes")
		if len(rows) != 1 || got["has_more"] != false || rows[0]["status"] != "pending" || rows[0]["isServing"] != false ||
			rows[0]["deletions"] != nil {
			t.Fatalf("page 2 = %s", out)
		}
		queries := queriesOf(e)
		if len(queries) != 2 || queries[0].Get("sortOrder") != order || queries[0].Get("sortBy") != "changeSequence" ||
			queries[0].Get("maxResults") != "1" || queries[1].Get("pageToken") != "c2" ||
			queries[1].Get("sortOrder") != order {
			t.Errorf("queries = %v", queries)
		}
	}
	// A cursor of another order is refused before any request.
	before := len(e.fake.api())
	if _, err := e.invoke(changesListID, `{"project_id":"my-project-123","managed_zone":"example-zone","sort_order":"ascending","cursor":"bogus"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	if len(e.fake.api()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestChangesGetReadsPendingAndDone(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes[dnsChangesPath+"/1"] = okReply(pendingChangeBody)
	e.fake.routes[dnsChangesPath+"/2"] = okReply(doneChangeBody)
	for id, status := range map[string]string{"1": "pending", "2": "done"} {
		out, err := e.invoke(changesGetID, `{"project_id":"my-project-123","managed_zone":"example-zone","change_id":"`+id+`"}`)
		if err != nil || !strings.Contains(out, `"status":"`+status+`"`) || strings.Contains(out, "unknownMember") {
			t.Errorf("%s: out = %s, err = %v", id, out, err)
		}
	}
	// An answer for another change than the one asked for is invalid.
	e.fake.routes[dnsChangesPath+"/3"] = okReply(doneChangeBody)
	if _, err := e.invoke(changesGetID, `{"project_id":"my-project-123","managed_zone":"example-zone","change_id":"3"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestChangesBoundRecordsAndRefuseBadRows(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	records := strings.Repeat(`{"name":"a.example.com.","type":"A","rrdatas":["192.0.2.1"]},`, 150)
	long := strings.Repeat("x", 5000)
	e.fake.routes[dnsChangesPath+"/5"] = okReply(`{"id":"5","status":"done","additions":[` + records +
		`{"name":"t.example.com.","type":"TXT","rrdatas":["` + long + `"]}]}`)
	out, err := e.invoke(changesGetID, `{"project_id":"my-project-123","managed_zone":"example-zone","change_id":"5"}`)
	if err != nil || strings.Count(out, `"a.example.com."`) != 100 || strings.Contains(out, long) {
		t.Errorf("out length = %d, err = %v", len(out), err)
	}
	for name, body := range map[string]string{
		"status type":    `{"id":"6","status":1}`,
		"no id":          `{"status":"done"}`,
		"record no type": `{"id":"6","additions":[{"name":"a.example.com."}]}`,
		"additions type": `{"id":"6","additions":"x"}`,
	} {
		e.fake.routes[dnsChangesPath+"/6"] = okReply(body)
		if _, err := e.invoke(changesGetID, `{"project_id":"my-project-123","managed_zone":"example-zone","change_id":"6"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

const keySigningBody = `{"kind":"dns#dnsKey","id":"0","keyTag":12345,"algorithm":"ecdsap256sha256","type":"keySigning",` +
	`"isActive":true,"creationTime":"2026-01-02T03:04:05.678Z","publicKey":"synthetic-public-key",` +
	`"digests":[{"type":"sha256","digest":"synthetic-digest"}],"keyLength":256,"description":"signing key","unknownMember":1}`

const zoneSigningBody = `{"kind":"dns#dnsKey","id":"1","keyTag":54321,"algorithm":"ecdsap256sha256","type":"zoneSigning",` +
	`"isActive":false,"creationTime":"2026-01-02T03:04:06Z","publicKey":"synthetic-public-key-two","keyLength":256}`

func TestDNSKeysListPassesDigestTypeAndFollowsPages(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != dnsKeysPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"kind":"dns#dnsKeysListResponse","dnsKeys":[` + keySigningBody +
				`],"nextPageToken":"k2"}`}
		}
		return &reply{status: 200, body: `{"dnsKeys":[` + zoneSigningBody + `]}`}
	}
	base := `{"project_id":"my-project-123","managed_zone":"example-zone","digest_type":"sha256,sha384","max_results":1`
	out, err := e.invoke(dnsKeysListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "dns_keys")
	if len(rows) != 1 || got["has_more"] != true || rows[0]["id"] != "0" || rows[0]["keyTag"] != float64(12345) ||
		rows[0]["algorithm"] != "ecdsap256sha256" || rows[0]["type"] != "keySigning" || rows[0]["isActive"] != true ||
		rows[0]["publicKey"] != "synthetic-public-key" || rows[0]["keyLength"] != float64(256) ||
		len(rows[0]["digests"].([]any)) != 1 || rows[0]["creationTime"] == nil || rows[0]["unknownMember"] != nil {
		t.Fatalf("page 1 = %s", out)
	}
	out, err = e.invoke(dnsKeysListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "dns_keys")
	if len(rows) != 1 || got["has_more"] != false || rows[0]["type"] != "zoneSigning" || rows[0]["isActive"] != false {
		t.Fatalf("page 2 = %s", out)
	}
	queries := queriesOf(e)
	if len(queries) != 2 || queries[0].Get("digestType") != "sha256,sha384" || queries[1].Get("pageToken") != "k2" ||
		queries[1].Get("digestType") != "sha256,sha384" {
		t.Errorf("queries = %v", queries)
	}
}

func TestDNSKeysListOfAZoneWithoutDNSSECIsEmpty(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes[dnsKeysPath] = okReply(`{"kind":"dns#dnsKeysListResponse"}`)
	out, err := e.invoke(dnsKeysListID, `{"project_id":"my-project-123","managed_zone":"example-zone"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "dns_keys")
	if len(rows) != 0 || got["has_more"] != false {
		t.Errorf("out = %s", out)
	}
}

func TestDNSKeysGetReadsOneKeyWithDigestType(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes[dnsKeysPath+"/0"] = okReply(keySigningBody)
	e.fake.routes[dnsKeysPath+"/1"] = okReply(keySigningBody)
	out, err := e.invoke(dnsKeysGetID, `{"project_id":"my-project-123","managed_zone":"example-zone","dns_key_id":"0","digest_type":"sha1"}`)
	if err != nil || !strings.Contains(out, `"publicKey":"synthetic-public-key"`) || strings.Contains(out, "unknownMember") {
		t.Errorf("out = %s, err = %v", out, err)
	}
	if q := queriesOf(e); len(q) != 1 || q[0].Get("digestType") != "sha1" {
		t.Errorf("queries = %v", q)
	}
	// An answer for another key than the one asked for is invalid.
	if _, err := e.invoke(dnsKeysGetID, `{"project_id":"my-project-123","managed_zone":"example-zone","dns_key_id":"1"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestChangesAndKeysRefuseBadInputBeforeIO(t *testing.T) {
	const base = `"project_id":"my-project-123","managed_zone":"example-zone"`
	for name, call := range map[string][2]string{
		"zone path":        {changesListID, `{"project_id":"my-project-123","managed_zone":"a/b"}`},
		"zone invalid":     {dnsKeysListID, `{"project_id":"my-project-123","managed_zone":"Example"}`},
		"sort order":       {changesListID, `{` + base + `,"sort_order":"sideways"}`},
		"sort by":          {changesListID, `{` + base + `,"sort_by":"name"}`},
		"max results":      {changesListID, `{` + base + `,"max_results":0}`},
		"change id path":   {changesGetID, `{` + base + `,"change_id":"1/../2"}`},
		"change id query":  {changesGetID, `{` + base + `,"change_id":"1?x=1"}`},
		"change id text":   {changesGetID, `{` + base + `,"change_id":"abc"}`},
		"key id path":      {dnsKeysGetID, `{` + base + `,"dns_key_id":"0/x"}`},
		"digest type":      {dnsKeysListID, `{` + base + `,"digest_type":"md5"}`},
		"digest type list": {dnsKeysGetID, `{` + base + `,"dns_key_id":"0","digest_type":"sha1,"}`},
		"cursor":           {dnsKeysListID, `{` + base + `,"cursor":"bogus"}`},
	} {
		e := newEnvironment(t, []string{"projects/my-project-123"})
		if _, err := e.invoke(call[0], call[1]); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
}
