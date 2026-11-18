package googlecloud

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	snapshotsPath = "/compute/v1/projects/my-project-123/global/snapshots"
	imagesPath    = "/compute/v1/projects/my-project-123/global/images"

	// Canary values are synthetic and no key material. A result that holds one has leaked a key member.
	rawKeyCanary    = "canary-raw-value-not-key-material"
	rsaKeyCanary    = "canary-wrapped-value-not-key-material"
	nestedKeyCanary = "canary-nested-value-not-key-material"
	kmsKeyName      = "projects/my-project-123/locations/global/keyRings/ring-a/cryptoKeys/key-a"
)

var keyedTools = []string{snapshotsListID, snapshotsGetID, imagesListID, imagesGetID}

func absentKeyMaterial(t *testing.T, what, out string) {
	t.Helper()
	for _, canary := range []string{rawKeyCanary, rsaKeyCanary, nestedKeyCanary, "rawKey", "rsaEncryptedKey"} {
		if strings.Contains(out, canary) {
			t.Errorf("%s holds %s: %s", what, canary, out)
		}
	}
}

func snapshotBody(name string) string {
	return `{"kind":"compute#snapshot","id":"7300000000000000001","name":"` + name + `","description":"d",` +
		`"status":"READY","diskSizeGb":"20","storageBytes":"1024","sourceDisk":"https://x/disks/disk-a",` +
		`"snapshotType":"STANDARD","storageLocations":["us"],"labels":{"env":"test"},"autoCreated":true,` +
		`"creationTimestamp":"2026-03-04T05:06:07.000-08:00","selfLink":"s","storageBytesStatus":"UP_TO_DATE",` +
		`"snapshotEncryptionKey":{"kmsKeyName":"` + kmsKeyName + `","sha256":"h","rawKey":"` + rawKeyCanary + `",` +
		`"rsaEncryptedKey":"` + rsaKeyCanary + `"},"sourceDiskEncryptionKey":{"RawKey":"` + rawKeyCanary + `"},` +
		`"params":{"x":"y"},"guestFlush":true,"unknownMember":"x"}`
}

func imageBody(name string) string {
	return `{"kind":"compute#image","id":"7400000000000000001","name":"` + name + `","status":"READY",` +
		`"family":"web-base","archiveSizeBytes":"2048","diskSizeGb":"10","sourceDisk":"https://x/disks/disk-a",` +
		`"sourceImage":"https://x/images/img-a","sourceSnapshot":"https://x/snapshots/snap-a",` +
		`"licenses":["https://x/licenses/lic-a"],"guestOsFeatures":[{"type":"UEFI_COMPATIBLE"}],` +
		`"storageLocations":["us"],"labels":{"env":"test"},"creationTimestamp":"2026-03-04T05:06:07.000-08:00",` +
		`"deprecated":{"state":"DEPRECATED","replacement":"https://x/images/img-b"},` +
		`"imageEncryptionKey":{"kmsKeyName":"` + kmsKeyName + `","raw_key":"` + rawKeyCanary + `"},` +
		`"sourceImageEncryptionKey":{"rsaEncryptedKey":"` + rsaKeyCanary + `"},` +
		`"sourceSnapshotEncryptionKey":{"rawKey":"` + rawKeyCanary + `","kmsKeyName":"` + kmsKeyName + `"},` +
		`"sourceDiskEncryptionKey":{"RSAENCRYPTEDKEY":"` + rsaKeyCanary + `"},` +
		`"params":{"x":"y"},"unknownMember":"x"}`
}

func TestSnapshotsListFollowsPagesAndKeepsKmsKeyName(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != snapshotsPath {
			return nil
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return &reply{status: 200, body: `{"items":[` + snapshotBody("snap-a") + `],"nextPageToken":"p2"}`}
		case "p2":
			return &reply{status: 200, body: `{"items":[` + snapshotBody("snap-b") + `],"nextPageToken":"p3",` +
				`"warning":{"code":"X","message":"m"}}`}
		}
		return &reply{status: 200, body: `{"items":[` + snapshotBody("snap-c") + `]}`}
	}
	var names []string
	cursor := ""
	for page := 1; page <= 3; page++ {
		arguments := `{"project_id":"my-project-123","filter":"status = READY","max_results":1,` +
			`"order_by":"name desc","return_partial_success":true`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		out, err := e.invoke(snapshotsListID, arguments+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		absentKeyMaterial(t, "list", out)
		rows, got := listOf(t, out, "snapshots")
		for _, row := range rows {
			names = append(names, row["name"].(string))
			for _, dropped := range []string{"params", "guestFlush", "storageBytesStatus", "unknownMember"} {
				if _, found := row[dropped]; found {
					t.Errorf("page %d returns %s", page, dropped)
				}
			}
			key, _ := row["snapshotEncryptionKey"].(map[string]any)
			if key["kmsKeyName"] != kmsKeyName || key["sha256"] != "h" || len(key) != 2 ||
				row["status"] != "READY" || row["diskSizeGb"] != "20" || row["storageBytes"] != "1024" ||
				row["sourceDisk"] != "https://x/disks/disk-a" || row["snapshotType"] != "STANDARD" ||
				row["autoCreated"] != true || row["labels"].(map[string]any)["env"] != "test" {
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
	if !slices.Equal(names, []string{"snap-a", "snap-b", "snap-c"}) {
		t.Errorf("names = %v", names)
	}
	for i, q := range queriesOf(e) {
		if q.Get("filter") != "status = READY" || q.Get("maxResults") != "1" || q.Get("orderBy") != "name desc" ||
			q.Get("returnPartialSuccess") != "true" {
			t.Errorf("request %d: %v", i, q)
		}
	}
}

func TestSnapshotsGetByNameAndID(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[snapshotsPath+"/snap-a"] = okReply(snapshotBody("snap-a"))
	e.fake.routes[snapshotsPath+"/7300000000000000001"] = okReply(snapshotBody("snap-a"))
	for _, target := range []string{"snap-a", "7300000000000000001"} {
		out, err := e.invoke(snapshotsGetID, `{"project_id":"my-project-123","snapshot":"`+target+`"}`)
		if err != nil || !strings.Contains(out, kmsKeyName) || strings.Contains(out, "params") {
			t.Errorf("%s: %s, %v", target, out, err)
		}
		absentKeyMaterial(t, "get "+target, out)
	}
	e.fake.routes[snapshotsPath+"/snap-z"] = okReply(snapshotBody("snap-a"))
	if _, err := e.invoke(snapshotsGetID, `{"project_id":"my-project-123","snapshot":"snap-z"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("answer for another snapshot: %v", err)
	}
}

func TestImagesListFollowsPagesAndKeepsDeprecation(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != imagesPath {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"items":[` + imageBody("img-a") + `],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"items":[` + imageBody("img-b") + `]}`}
	}
	base := `{"project_id":"my-project-123","max_results":1`
	out, err := e.invoke(imagesListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	absentKeyMaterial(t, "list page 1", out)
	rows, got := listOf(t, out, "images")
	if len(rows) != 1 || got["has_more"] != true {
		t.Fatalf("page 1: %v, %v", rows, got)
	}
	row := rows[0]
	deprecated, _ := row["deprecated"].(map[string]any)
	key, _ := row["imageEncryptionKey"].(map[string]any)
	if deprecated["state"] != "DEPRECATED" || deprecated["replacement"] != "https://x/images/img-b" ||
		key["kmsKeyName"] != kmsKeyName || len(key) != 1 || row["family"] != "web-base" ||
		row["archiveSizeBytes"] != "2048" || row["sourceImage"] != "https://x/images/img-a" ||
		row["sourceSnapshot"] != "https://x/snapshots/snap-a" || len(row["guestOsFeatures"].([]any)) != 1 {
		t.Errorf("row = %v", row)
	}
	for _, dropped := range []string{"params", "unknownMember"} {
		if _, found := row[dropped]; found {
			t.Errorf("returns %s", dropped)
		}
	}
	out, err = e.invoke(imagesListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	absentKeyMaterial(t, "list page 2", out)
	rows, got = listOf(t, out, "images")
	if len(rows) != 1 || rows[0]["name"] != "img-b" || got["has_more"] != false {
		t.Errorf("page 2: %v, %v", rows, got)
	}
	// Only the project itself is read: no public image project appears in any request.
	for _, req := range e.fake.api() {
		if !strings.Contains(req.url, "/projects/my-project-123/global/images") {
			t.Errorf("request to %s", req.url)
		}
	}
}

func TestImagesGetByNameAndID(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[imagesPath+"/img-a"] = okReply(imageBody("img-a"))
	e.fake.routes[imagesPath+"/7400000000000000001"] = okReply(imageBody("img-a"))
	for _, target := range []string{"img-a", "7400000000000000001"} {
		out, err := e.invoke(imagesGetID, `{"project_id":"my-project-123","image":"`+target+`"}`)
		if err != nil || !strings.Contains(out, `"state":"DEPRECATED"`) || !strings.Contains(out, kmsKeyName) {
			t.Errorf("%s: %s, %v", target, out, err)
		}
		absentKeyMaterial(t, "get "+target, out)
	}
	e.fake.routes[imagesPath+"/img-z"] = okReply(imageBody("img-a"))
	if _, err := e.invoke(imagesGetID, `{"project_id":"my-project-123","image":"img-z"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("answer for another image: %v", err)
	}
}

func TestKeyedToolsNeverReturnKeyMaterial(t *testing.T) {
	e := newEnvironment(t, nil)
	nested := `{"name":"snap-n","labels":{"RawKey":"` + nestedKeyCanary + `"},` +
		`"guestOsFeatures":[{"rsaEncryptedKey":"` + nestedKeyCanary + `","type":"X"}],` +
		`"snapshotEncryptionKey":{"kmsKeyName":"` + kmsKeyName + `","kmsKeyServiceAccount":"sa","rawKey":"` + rawKeyCanary + `"}}`
	warning := `"warning":{"code":"X","data":[{"key":"k","value":"v","rawKey":"` + rawKeyCanary + `"}]}`
	e.fake.routes[snapshotsPath] = okReply(`{"items":[` + snapshotBody("snap-a") + `,` + nested + `],` + warning + `}`)
	e.fake.routes[snapshotsPath+"/snap-a"] = okReply(snapshotBody("snap-a"))
	e.fake.routes[imagesPath] = okReply(`{"items":[` + imageBody("img-a") + `],` + warning + `}`)
	e.fake.routes[imagesPath+"/img-a"] = okReply(imageBody("img-a"))
	for id, arguments := range map[string]string{
		snapshotsListID: `{"project_id":"my-project-123"}`,
		snapshotsGetID:  `{"project_id":"my-project-123","snapshot":"snap-a"}`,
		imagesListID:    `{"project_id":"my-project-123"}`,
		imagesGetID:     `{"project_id":"my-project-123","image":"img-a"}`,
	} {
		out, err := e.invoke(id, arguments)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		absentKeyMaterial(t, id, out)
		if !strings.Contains(out, kmsKeyName) {
			t.Errorf("%s: kmsKeyName was dropped: %s", id, out)
		}
	}
	for _, id := range keyedTools {
		d, _, _ := e.reg.Lookup(id)
		if schema := strings.ToLower(string(d.OutputSchema)); strings.Contains(schema, "rawkey") ||
			strings.Contains(schema, "rsaencryptedkey") {
			t.Errorf("%s: output schema names a key material member", id)
		}
	}
	for _, members := range [][]computeMember{snapshotMembers, imageMembers} {
		for _, m := range members {
			if keyMaterialKey(m.name) {
				t.Errorf("member %s is key material", m.name)
			}
		}
	}
}

func TestKeyedToolsRefuseInvalidInputBeforeIO(t *testing.T) {
	for name, c := range map[string]struct{ tool, arguments string }{
		"snapshot path":      {snapshotsGetID, `{"project_id":"my-project-123","snapshot":"a/b"}`},
		"snapshot traversal": {snapshotsGetID, `{"project_id":"my-project-123","snapshot":".."}`},
		"snapshot query":     {snapshotsGetID, `{"project_id":"my-project-123","snapshot":"a?b"}`},
		"snapshot upper":     {snapshotsGetID, `{"project_id":"my-project-123","snapshot":"Snap"}`},
		"snapshot zero id":   {snapshotsGetID, `{"project_id":"my-project-123","snapshot":"0"}`},
		"snapshot missing":   {snapshotsGetID, `{"project_id":"my-project-123"}`},
		"image path":         {imagesGetID, `{"project_id":"my-project-123","image":"a/b"}`},
		"image family":       {imagesGetID, `{"project_id":"my-project-123","image":"family/web-base"}`},
		"image project":      {imagesGetID, `{"project_id":"my-project-123","image":"x","image_project":"debian-cloud"}`},
		"image missing":      {imagesGetID, `{"project_id":"my-project-123"}`},
		"project path":       {snapshotsListID, `{"project_id":"projects/x"}`},
		"project traversal":  {imagesGetID, `{"project_id":"../x","image":"i"}`},
		"max results 501":    {imagesListID, `{"project_id":"my-project-123","max_results":501}`},
		"order by":           {snapshotsListID, `{"project_id":"my-project-123","order_by":"name; drop"}`},
		"filter control":     {imagesListID, `{"project_id":"my-project-123","filter":"a\nb"}`},
		"cursor":             {snapshotsListID, `{"project_id":"my-project-123","cursor":"bogus"}`},
		"scopes":             {imagesListID, `{"project_id":"my-project-123","include_all_scopes":true}`},
		"zone":               {snapshotsListID, `{"project_id":"my-project-123","zone":"us-central1-a"}`},
		"unknown":            {snapshotsGetID, `{"project_id":"my-project-123","snapshot":"s","path":"/x"}`},
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
		"snapshots get": func() error {
			_, err := handleSnapshotsGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","snapshot":"a/b"}`))
			return err
		},
		"images get": func() error {
			_, err := handleImagesGet(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","image":"a/b"}`))
			return err
		},
		"snapshots list": func() error {
			_, err := handleSnapshotsList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","max_results":501}`))
			return err
		},
		"images list": func() error {
			_, err := handleImagesList(t.Context(), e.resolved(), e.secrets(), e.red,
				[]byte(`{"project_id":"my-project-123","include_all_scopes":true}`))
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

func TestKeyedToolsRefuseProjectsOutsideTheTargetsBeforeIO(t *testing.T) {
	calls := map[string]string{
		snapshotsListID: `{"project_id":"other-project-1"}`,
		snapshotsGetID:  `{"project_id":"other-project-1","snapshot":"s"}`,
		imagesListID:    `{"project_id":"other-project-1"}`,
		imagesGetID:     `{"project_id":"other-project-1","image":"i"}`,
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

func TestKeyedCursorIsBoundToTheToolAndTheList(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[snapshotsPath] = okReply(`{"items":[` + snapshotBody("snap-a") + `],"nextPageToken":"p2"}`)
	out, err := e.invoke(snapshotsListID, `{"project_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := listOf(t, out, "snapshots")
	cursor := got["next_cursor"].(string)
	before := len(e.fake.api())
	for name, c := range map[string]struct{ tool, arguments string }{
		"other tool":   {imagesListID, `{"project_id":"my-project-123","cursor":"` + cursor + `"}`},
		"other filter": {snapshotsListID, `{"project_id":"my-project-123","filter":"x","cursor":"` + cursor + `"}`},
	} {
		if _, err := e.invoke(c.tool, c.arguments); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
	if len(e.fake.api()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestKeyedToolsRefuseInvalidAnswers(t *testing.T) {
	for name, c := range map[string]struct{ tool, arguments, path, body string }{
		"snapshot without name": {snapshotsListID, `{"project_id":"my-project-123"}`, snapshotsPath, `{"items":[{"id":"1"}]}`},
		"image not an object":   {imagesListID, `{"project_id":"my-project-123"}`, imagesPath, `{"items":[1]}`},
		"image bad name":        {imagesListID, `{"project_id":"my-project-123"}`, imagesPath, `{"items":[{"name":"../i"}]}`},
		"token too long": {snapshotsListID, `{"project_id":"my-project-123"}`, snapshotsPath,
			`{"items":[],"nextPageToken":"` + strings.Repeat("a", maxPageTokenBytes+1) + `"}`},
	} {
		e := newEnvironment(t, nil)
		e.fake.routes[c.path] = okReply(c.body)
		if _, err := e.invoke(c.tool, c.arguments); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestKeyedToolsMapForbiddenAndNotFound(t *testing.T) {
	for _, c := range []struct {
		status  int
		status2 string
		class   provider.Class
	}{{403, "PERMISSION_DENIED", provider.ClassPermission}, {404, "NOT_FOUND", provider.ClassNotFound}} {
		for id, arguments := range map[string]string{
			snapshotsListID: `{"project_id":"my-project-123"}`,
			snapshotsGetID:  `{"project_id":"my-project-123","snapshot":"snap-a"}`,
			imagesListID:    `{"project_id":"my-project-123"}`,
			imagesGetID:     `{"project_id":"my-project-123","image":"img-a"}`,
		} {
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply {
				return &reply{status: c.status, body: errorObject(c.status, c.status2, "")}
			}
			if _, err := e.invoke(id, arguments); classOf(err) != c.class {
				t.Errorf("%s %d: err = %v", id, c.status, err)
			}
		}
	}
}

func TestKeyedToolsBoundProviderText(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[imagesPath] = okReply(`{"items":[{"name":"i","description":"` + strings.Repeat("x", 1<<20) + `"}]}`)
	out, err := e.invoke(imagesListID, `{"project_id":"my-project-123"}`)
	if err != nil || len(out) > 1<<19 {
		t.Errorf("text was not bounded: %d bytes, %v", len(out), err)
	}
}
