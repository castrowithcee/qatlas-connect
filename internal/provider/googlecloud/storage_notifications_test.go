package googlecloud

import (
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const notificationsBody = `{"kind":"storage#notifications","items":[{"kind":"storage#notification","id":"7",` +
	`"selfLink":"https://www.googleapis.com/storage/v1/b/example-bucket/notificationConfigs/7",` +
	`"topic":"//pubsub.googleapis.com/projects/my-project-123/topics/events","event_types":["OBJECT_FINALIZE"],` +
	`"custom_attributes":{"k":"v"},"object_name_prefix":"in/","payload_format":"JSON_API_V1","etag":"CAc=",` +
	`"unknownMember":"x"}]}`

func TestNotificationsListReturnsTheConfigurationsAndNeverCallsTheTopic(t *testing.T) {
	e := newEnvironment(t, []string{"projects/" + storageProjectNumber})
	e.fake.routes[bucketsPath+"/example-bucket"] = okReply(`{"name":"example-bucket","projectNumber":"` + storageProjectNumber + `"}`)
	e.fake.routes[bucketsPath+"/example-bucket/notificationConfigs"] = okReply(notificationsBody)
	out, err := e.invoke(notificationsListID, `{"bucket":"example-bucket","user_project":"`+storageProjectNumber+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := listOf(t, out, "items")
	if len(rows) != 1 || rows[0]["id"] != "7" || rows[0]["topic"] != "//pubsub.googleapis.com/projects/my-project-123/topics/events" ||
		rows[0]["event_types"].([]any)[0] != "OBJECT_FINALIZE" || rows[0]["custom_attributes"].(map[string]any)["k"] != "v" ||
		rows[0]["object_name_prefix"] != "in/" || rows[0]["payload_format"] != "JSON_API_V1" || rows[0]["etag"] != "CAc=" ||
		rows[0]["unknownMember"] != nil {
		t.Errorf("out = %s", out)
	}
	// One proof read of the bucket and the list; the topic is never requested.
	paths := apiPaths(e)
	if len(paths) != 2 || paths[1] != "storage.googleapis.com/storage/v1/b/example-bucket/notificationConfigs" {
		t.Errorf("paths = %v", paths)
	}
	if q := queriesOf(e)[1]; q.Get("userProject") != storageProjectNumber {
		t.Errorf("query = %v", q)
	}
}

func TestNotificationsListRefusesABucketOutsideTheTargetsAndBadAnswers(t *testing.T) {
	e := newEnvironment(t, []string{"projects/" + storageProjectNumber})
	e.fake.routes[bucketsPath+"/foreign-bucket"] = okReply(`{"name":"foreign-bucket","projectNumber":"` + foreignNumber + `"}`)
	e.fake.routes[bucketsPath+"/foreign-bucket/notificationConfigs"] = okReply(notificationsBody)
	_, err := e.invoke(notificationsListID, `{"bucket":"foreign-bucket"}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "foreign-bucket") || strings.Contains(err.Error(), foreignNumber) {
		t.Errorf("err = %v", err)
	}
	if len(apiPaths(e)) != 1 {
		t.Errorf("paths = %v", apiPaths(e))
	}
	for _, body := range []string{`{"items":[{"topic":"t"}]}`, `{"items":[{"id":"1","event_types":"x"}]}`,
		`{"items":[{"id":"1","custom_attributes":{"a":1}}]}`} {
		e = newEnvironment(t, nil)
		e.fake.routes[bucketsPath+"/example-bucket/notificationConfigs"] = okReply(body)
		if _, err := e.invoke(notificationsListID, `{"bucket":"example-bucket"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", body, err)
		}
	}
	e = newEnvironment(t, nil)
	e.fake.routes[bucketsPath+"/example-bucket/notificationConfigs"] = okReply(`{"kind":"storage#notifications"}`)
	if out, err := e.invoke(notificationsListID, `{"bucket":"example-bucket"}`); err != nil || out != `{}` {
		t.Errorf("out = %s, err = %v", out, err)
	}
}

func managedFolderBody(name string) string {
	return `{"kind":"storage#managedFolder","id":"example-bucket/` + name + `","bucket":"example-bucket","name":"` + name +
		`","metageneration":"1","createTime":"2026-01-02T03:04:05.000Z","updateTime":"2026-02-03T04:05:06.000Z","unknownMember":"x"}`
}

func TestManagedFoldersListFollowsTwoPages(t *testing.T) {
	e := newEnvironment(t, []string{"projects/" + storageProjectNumber})
	e.fake.routes[bucketsPath+"/example-bucket"] = okReply(`{"name":"example-bucket","projectNumber":"` + storageProjectNumber + `"}`)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != bucketsPath+"/example-bucket/managedFolders" {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"kind":"storage#managedFolders","items":[` + managedFolderBody("logs/a/") +
				`],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"items":[` + managedFolderBody("logs/b/") + `]}`}
	}
	base := `{"bucket":"example-bucket","prefix":"logs/","page_size":1`
	out, err := e.invoke(managedFoldersListID, base+`}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "managed_folders")
	if len(rows) != 1 || rows[0]["name"] != "logs/a/" || rows[0]["metageneration"] != "1" || rows[0]["createTime"] == nil ||
		rows[0]["updateTime"] == nil || rows[0]["unknownMember"] != nil || got["has_more"] != true {
		t.Fatalf("page 1 = %s", out)
	}
	out, err = e.invoke(managedFoldersListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "managed_folders")
	if len(rows) != 1 || rows[0]["name"] != "logs/b/" || got["has_more"] != false || got["next_cursor"] != nil {
		t.Errorf("page 2 = %s", out)
	}
	queries := queriesOf(e)
	var lists []string
	for i, q := range queries {
		if q.Has("pageSize") {
			lists = append(lists, q.Encode())
			if q.Get("pageSize") != "1" || q.Get("prefix") != "logs/" || (q.Get("pageToken") == "p2") != (len(lists) == 2) {
				t.Errorf("request %d: %v", i, q)
			}
		}
	}
	if len(lists) != 2 {
		t.Errorf("list requests = %v", lists)
	}
	// A cursor of another bucket or prefix is refused before any request.
	before := len(e.fake.api())
	for _, arguments := range []string{`{"bucket":"other-bucket","prefix":"logs/","page_size":1,"cursor":"bogus"}`,
		`{"bucket":"example-bucket","cursor":"bogus"}`} {
		if _, err := e.invoke(managedFoldersListID, arguments); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", arguments, err)
		}
	}
	if len(e.fake.api()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestManagedFoldersListRefusesBadInputBeforeIOAndForeignBuckets(t *testing.T) {
	for name, arguments := range map[string]string{
		"path":           `{"bucket":"a/b"}`,
		"page size 0":    `{"bucket":"example-bucket","page_size":0}`,
		"page size 1001": `{"bucket":"example-bucket","page_size":1001}`,
		"long prefix":    `{"bucket":"example-bucket","prefix":"` + strings.Repeat("a", 1025) + `"}`,
		"control":        `{"bucket":"example-bucket","prefix":"a\u0007b"}`,
		"unknown":        `{"bucket":"example-bucket","project_id":"my-project-123"}`,
	} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(managedFoldersListID, arguments); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	e := newEnvironment(t, []string{"projects/" + storageProjectNumber})
	e.fake.routes[bucketsPath+"/foreign-bucket"] = okReply(`{"name":"foreign-bucket","projectNumber":"` + foreignNumber + `"}`)
	e.fake.routes[bucketsPath+"/foreign-bucket/managedFolders"] = okReply(`{"items":[` + managedFolderBody("x/") + `]}`)
	if _, err := e.invoke(managedFoldersListID, `{"bucket":"foreign-bucket"}`); !isInvalidRequest(err) || len(apiPaths(e)) != 1 {
		t.Errorf("err = %v, paths = %v", err, apiPaths(e))
	}
	// A row of another bucket is invalid.
	e = newEnvironment(t, nil)
	e.fake.routes[bucketsPath+"/asked-bucket/managedFolders"] = okReply(`{"items":[` + managedFolderBody("x/") + `]}`)
	if _, err := e.invoke(managedFoldersListID, `{"bucket":"asked-bucket"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestNotificationAndManagedFolderToolsMapErrors(t *testing.T) {
	for _, status := range []struct {
		code   int
		reason string
		want   provider.Class
	}{{403, "PERMISSION_DENIED", provider.ClassPermission}, {404, "NOT_FOUND", provider.ClassNotFound}} {
		for _, id := range []string{notificationsListID, managedFoldersListID} {
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply {
				return &reply{status: status.code, body: errorObject(status.code, status.reason, "")}
			}
			_, err := e.invoke(id, `{"bucket":"example-bucket"}`)
			if classOf(err) != status.want || err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s %d: err = %v", id, status.code, err)
			}
		}
	}
}
