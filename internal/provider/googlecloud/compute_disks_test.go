package googlecloud

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	disksPath         = "/compute/v1/projects/my-project-123/zones/us-central1-a/disks"
	disksAggregated   = "/compute/v1/projects/my-project-123/aggregated/disks"
	diskZoneURL       = "https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a"
	diskRawCanary     = "canary-raw-value-not-a-key"
	diskRSACanary     = "canary-rsa-value-not-a-key"
	diskNestedCanary  = "canary-nested-value-not-a-key"
	diskKMSName       = "projects/p/locations/l/keyRings/r/cryptoKeys/k"
	diskKMSSourceName = "projects/p/locations/l/keyRings/r/cryptoKeys/src"
)

var diskTools = []string{disksListID, disksAggregatedListID, disksGetID}

// diskBody is a disk with all three encryption keys holding key material next to the names that stay.
func diskBody(name string) string {
	return `{"kind":"compute#disk","id":"5100000000000000001","creationTimestamp":"2026-03-04T05:06:07.000-08:00",` +
		`"name":"` + name + `","description":"d","sizeGb":"100","zone":"` + diskZoneURL + `","status":"READY",` +
		`"sourceImage":"https://x/images/img-a","sourceSnapshot":"https://x/snapshots/snap-a",` +
		`"type":"` + diskZoneURL + `/diskTypes/pd-balanced","users":["` + diskZoneURL + `/instances/vm-a"],` +
		`"labels":{"env":"test"},"labelFingerprint":"lfp","physicalBlockSizeBytes":"4096",` +
		`"lastAttachTimestamp":"2026-03-05T05:06:07.000-08:00","lastDetachTimestamp":"2026-03-06T05:06:07.000-08:00",` +
		`"provisionedIops":"3000","resourcePolicies":["https://x/policies/p"],"selfLink":"s",` +
		`"diskEncryptionKey":{"kmsKeyName":"` + diskKMSName + `","sha256":"hash-a","rawKey":"` + diskRawCanary +
		`","rsaEncryptedKey":"` + diskRSACanary + `"},` +
		`"sourceImageEncryptionKey":{"kmsKeyName":"` + diskKMSSourceName + `","RawKey":"` + diskNestedCanary + `"},` +
		`"sourceSnapshotEncryptionKey":{"raw_key":"` + diskNestedCanary + `","RSAEncryptedKey":"` + diskRSACanary + `"},` +
		`"options":"x","params":{"resourceManagerTags":{"k":"v"}},"unknownMember":"x"}`
}

func absentDiskKeys(t *testing.T, what, out string) {
	t.Helper()
	for _, canary := range []string{diskRawCanary, diskRSACanary, diskNestedCanary} {
		if strings.Contains(out, canary) {
			t.Errorf("%s holds %s: %s", what, canary, out)
		}
	}
}

func TestDisksListFollowsPagesAndKeepsKeyNames(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != disksPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"items":[` + diskBody("disk-a") + `],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"items":[` + diskBody("disk-b") + `],"warning":{"code":"X","message":"m"}}`}
	}
	base := `{"project_id":"my-project-123","zone":"us-central1-a","filter":"name != x","max_results":1,` +
		`"order_by":"name desc","return_partial_success":true`
	out, err := e.invoke(disksListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	absentDiskKeys(t, "page 1", out)
	rows, got := listOf(t, out, "disks")
	if len(rows) != 1 || got["has_more"] != true {
		t.Fatalf("page 1 = %s", out)
	}
	row := rows[0]
	key, _ := row["diskEncryptionKey"].(map[string]any)
	if row["sizeGb"] != "100" || row["status"] != "READY" || row["sourceImage"] != "https://x/images/img-a" ||
		row["sourceSnapshot"] != "https://x/snapshots/snap-a" || len(row["users"].([]any)) != 1 ||
		row["labels"].(map[string]any)["env"] != "test" || row["physicalBlockSizeBytes"] != "4096" ||
		row["provisionedIops"] != "3000" || row["lastAttachTimestamp"] == nil || row["lastDetachTimestamp"] == nil ||
		len(row["resourcePolicies"].([]any)) != 1 || key["kmsKeyName"] != diskKMSName || key["sha256"] != "hash-a" ||
		row["sourceImageEncryptionKey"].(map[string]any)["kmsKeyName"] != diskKMSSourceName {
		t.Errorf("row = %v", row)
	}
	for _, dropped := range []string{"options", "params", "unknownMember"} {
		if _, found := row[dropped]; found {
			t.Errorf("row returns %s", dropped)
		}
	}
	for _, name := range []string{"rawKey", "rsaEncryptedKey"} {
		if _, found := key[name]; found {
			t.Errorf("diskEncryptionKey returns %s", name)
		}
	}
	out, err = e.invoke(disksListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "disks")
	if len(rows) != 1 || rows[0]["name"] != "disk-b" || got["has_more"] != false || got["warning"] == nil {
		t.Errorf("page 2 = %s", out)
	}
	for i, q := range queriesOf(e) {
		if q.Get("filter") != "name != x" || q.Get("maxResults") != "1" || q.Get("orderBy") != "name desc" ||
			q.Get("returnPartialSuccess") != "true" {
			t.Errorf("request %d: %v", i, q)
		}
	}
}

func TestDisksAggregatedListGroupsByScopeAndKeepsUnreachables(t *testing.T) {
	e := newEnvironment(t, nil)
	regional := strings.Replace(diskBody("disk-r"), `"zone":"`+diskZoneURL+`"`,
		`"region":"https://x/compute/v1/projects/my-project-123/regions/us-central1"`, 1)
	e.fake.routes[disksAggregated] = okReply(`{"items":{` +
		`"zones/us-central1-b":{"warning":{"code":"NO_RESULTS_ON_PAGE","message":"empty"}},` +
		`"zones/us-central1-a":{"disks":[` + diskBody("disk-a") + `]},` +
		`"regions/us-central1":{"disks":[` + regional + `]},` +
		`"zones/europe-west1-b":{"warning":{"code":"UNREACHABLE","message":"down","data":[{"key":"scope","rawKey":"` +
		diskNestedCanary + `"}]}}},` +
		`"unreachables":["zones/europe-west1-b"],"nextPageToken":"p2","warning":{"code":"UNREACHABLE","message":"partial"}}`)
	out, err := e.invoke(disksAggregatedListID,
		`{"project_id":"my-project-123","return_partial_success":true,"include_all_scopes":true,"max_results":50}`)
	if err != nil {
		t.Fatal(err)
	}
	absentDiskKeys(t, "aggregated list", out)
	var got struct {
		Scopes       []map[string]any `json:"scopes"`
		Unreachables []string         `json:"unreachables"`
		Warning      map[string]any   `json:"warning"`
		HasMore      bool             `json:"has_more"`
		NextCursor   string           `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	var scopes []any
	for _, scope := range got.Scopes {
		scopes = append(scopes, scope["scope"])
	}
	if !slices.Equal(scopes, []any{"regions/us-central1", "zones/europe-west1-b", "zones/us-central1-a"}) {
		t.Errorf("scopes = %v", scopes)
	}
	if _, has := got.Scopes[1]["disks"]; has || got.Scopes[1]["warning"] == nil {
		t.Errorf("unreachable scope = %v", got.Scopes[1])
	}
	if !slices.Equal(got.Unreachables, []string{"zones/europe-west1-b"}) || got.Warning["code"] != "UNREACHABLE" ||
		!got.HasMore || got.NextCursor == "" {
		t.Errorf("out = %s", out)
	}
	q := queriesOf(e)[0]
	if q.Get("returnPartialSuccess") != "true" || q.Get("includeAllScopes") != "true" || q.Get("maxResults") != "50" {
		t.Errorf("query = %v", q)
	}
	// A disk in a scope other than its own is an invalid response.
	e = newEnvironment(t, nil)
	e.fake.routes[disksAggregated] = okReply(`{"items":{"zones/us-central1-b":{"disks":[` + diskBody("disk-a") + `]}}}`)
	if _, err := e.invoke(disksAggregatedListID, `{"project_id":"my-project-123"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("disk of another scope: %v", err)
	}
}

func TestDisksGetByNameAndIDChecksTheAnswer(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[disksPath+"/disk-a"] = okReply(diskBody("disk-a"))
	e.fake.routes[disksPath+"/5100000000000000001"] = okReply(diskBody("disk-a"))
	e.fake.routes[disksPath+"/disk-z"] = okReply(diskBody("disk-a"))
	for _, target := range []string{"disk-a", "5100000000000000001"} {
		out, err := e.invoke(disksGetID, `{"project_id":"my-project-123","zone":"us-central1-a","disk":"`+target+`"}`)
		if err != nil || !strings.Contains(out, diskKMSName) || !strings.Contains(out, "hash-a") {
			t.Errorf("%s: %s, %v", target, out, err)
		}
		absentDiskKeys(t, target, out)
	}
	if _, err := e.invoke(disksGetID, `{"project_id":"my-project-123","zone":"us-central1-a","disk":"disk-z"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("answer for another disk: %v", err)
	}
	e.fake.routes[disksPath+"/disk-y"] = okReply(strings.Replace(diskBody("disk-y"), "zones/us-central1-a", "zones/us-central1-b", 1))
	if _, err := e.invoke(disksGetID, `{"project_id":"my-project-123","zone":"us-central1-a","disk":"disk-y"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("answer for another zone: %v", err)
	}
}

func TestDisksNeverReturnKeyMaterial(t *testing.T) {
	e := newEnvironment(t, nil)
	nested := `{"name":"disk-n","zone":"` + diskZoneURL + `","labels":{"RAWKEY":"` + diskNestedCanary +
		`"},"guestOsFeatures":[{"rsaEncryptedKey":"` + diskRSACanary + `"}]}`
	warning := `{"code":"X","data":[{"key":"k","value":"v","rawKey":"` + diskRawCanary + `"}]}`
	e.fake.routes[disksPath] = okReply(`{"items":[` + diskBody("disk-a") + `,` + nested + `],"warning":` + warning + `}`)
	e.fake.routes[disksAggregated] = okReply(`{"items":{"zones/us-central1-a":{"disks":[` + diskBody("disk-a") +
		`],"warning":` + warning + `}},"warning":` + warning + `}`)
	e.fake.routes[disksPath+"/disk-a"] = okReply(diskBody("disk-a"))
	for id, arguments := range map[string]string{
		disksListID:           `{"project_id":"my-project-123","zone":"us-central1-a"}`,
		disksAggregatedListID: `{"project_id":"my-project-123"}`,
		disksGetID:            `{"project_id":"my-project-123","zone":"us-central1-a","disk":"disk-a"}`,
	} {
		out, err := e.invoke(id, arguments)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		absentDiskKeys(t, id, out)
		if !strings.Contains(out, diskKMSName) {
			t.Errorf("%s: the KMS key name was dropped: %s", id, out)
		}
	}
	for _, id := range diskTools {
		d, _, _ := e.reg.Lookup(id)
		if strings.Contains(strings.ToLower(string(d.OutputSchema)), "rawkey") ||
			strings.Contains(strings.ToLower(string(d.OutputSchema)), "rsaencryptedkey") {
			t.Errorf("%s: output schema names key material", id)
		}
		if !strings.Contains(d.Description, "never returned") || !strings.Contains(d.Description, "untrusted") {
			t.Errorf("%s: description lacks the key or untrusted-data rule", id)
		}
	}
	for _, m := range diskMembers {
		if diskKeyMaterial(m.name) {
			t.Errorf("member %s is key material", m.name)
		}
	}
}

func TestDisksRefuseInvalidInputBeforeIO(t *testing.T) {
	for name, c := range map[string]struct{ tool, arguments string }{
		"zone path":       {disksListID, `{"project_id":"my-project-123","zone":"zones/us-central1-a"}`},
		"zone traversal":  {disksListID, `{"project_id":"my-project-123","zone":"../x"}`},
		"zone upper":      {disksGetID, `{"project_id":"my-project-123","zone":"US-central1-a","disk":"d"}`},
		"zone long":       {disksListID, `{"project_id":"my-project-123","zone":"` + strings.Repeat("a", 64) + `"}`},
		"zone missing":    {disksListID, `{"project_id":"my-project-123"}`},
		"zone on aggr":    {disksAggregatedListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`},
		"disk path":       {disksGetID, `{"project_id":"my-project-123","zone":"us-central1-a","disk":"a/b"}`},
		"disk traversal":  {disksGetID, `{"project_id":"my-project-123","zone":"us-central1-a","disk":".."}`},
		"disk zero id":    {disksGetID, `{"project_id":"my-project-123","zone":"us-central1-a","disk":"0"}`},
		"disk missing":    {disksGetID, `{"project_id":"my-project-123","zone":"us-central1-a"}`},
		"project path":    {disksListID, `{"project_id":"projects/x","zone":"us-central1-a"}`},
		"max results 501": {disksListID, `{"project_id":"my-project-123","zone":"us-central1-a","max_results":501}`},
		"order by":        {disksListID, `{"project_id":"my-project-123","zone":"us-central1-a","order_by":"name; x"}`},
		"filter control":  {disksAggregatedListID, `{"project_id":"my-project-123","filter":"a\nb"}`},
		"cursor":          {disksListID, `{"project_id":"my-project-123","zone":"us-central1-a","cursor":"bogus"}`},
		"scopes on list":  {disksListID, `{"project_id":"my-project-123","zone":"us-central1-a","include_all_scopes":true}`},
		"unknown":         {disksGetID, `{"project_id":"my-project-123","zone":"us-central1-a","disk":"d","path":"/x"}`},
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
		"list": func() error {
			_, err := handleDisksList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","zone":"a/b"}`))
			return err
		},
		"aggregated": func() error {
			_, err := handleDisksAggregatedList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","max_results":501}`))
			return err
		},
		"get": func() error {
			_, err := handleDisksGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","zone":"us-central1-a","disk":"a/b"}`))
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

func TestDisksRefuseProjectsOutsideTheTargetsBeforeIO(t *testing.T) {
	for id, arguments := range map[string]string{
		disksListID:           `{"project_id":"other-project-1","zone":"us-central1-a"}`,
		disksAggregatedListID: `{"project_id":"other-project-1"}`,
		disksGetID:            `{"project_id":"other-project-1","zone":"us-central1-a","disk":"d"}`,
	} {
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

func TestDisksFailures(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"403", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"403 API disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission},
		{"404", reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
		{"429", reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")}, provider.ClassRateLimited},
		{"not an object", okReply(`{"items":[1]}`), provider.ClassInvalidResponse},
	}
	for _, c := range cases {
		for id, arguments := range map[string]string{
			disksListID:           `{"project_id":"my-project-123","zone":"us-central1-a"}`,
			disksAggregatedListID: `{"project_id":"my-project-123"}`,
			disksGetID:            `{"project_id":"my-project-123","zone":"us-central1-a","disk":"disk-a"}`,
		} {
			if c.name == "not an object" && id == disksGetID {
				continue
			}
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply { return &c.reply }
			_, err := e.invoke(id, arguments)
			if classOf(err) != c.want {
				t.Errorf("%s %s: err = %v (%s), want %s", c.name, id, err, classOf(err), c.want)
			}
		}
	}
}

func TestDiskToolsAreReadToolsWithTheComputeReadScope(t *testing.T) {
	e := newEnvironment(t, nil)
	for _, id := range diskTools {
		if got := tokenMetadata.Scopes[id]; len(got) != 1 || got[0] != scopeComputeReadOnly {
			t.Errorf("%s scopes = %v", id, got)
		}
		if !slices.Contains(readTools, id) {
			t.Errorf("%s is not in the profile read", id)
		}
		d, _, found := e.reg.Lookup(id)
		if !found || d.Risk != readRisk {
			t.Errorf("%s: found = %v, risk = %+v", id, found, d.Risk)
		}
	}
}

func TestDiskToolsAreOfferedOnlyToBindingsThatReachProjects(t *testing.T) {
	for _, id := range diskTools {
		if reaches, _ := targetReaches(id, []string{"billingAccounts/012345-ABCDEF-678901"}); reaches {
			t.Errorf("%s is offered to a billing-only binding", id)
		}
		if reaches, _ := targetReaches(id, []string{"projects/my-project-123"}); !reaches {
			t.Errorf("%s is hidden from a project binding", id)
		}
	}
}
