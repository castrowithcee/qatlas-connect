package googlecloud

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

const (
	revokeAccessID = "EXAMPLEACCESSID0001"
	revokeKeyPath  = hmacKeysPathForT + "/" + revokeAccessID
	revokeNotePath = bucketsPath + "/example-bucket/notificationConfigs/7"
	revokeTopic    = "//pubsub.googleapis.com/projects/my-project-123/topics/events"
)

// keyRoutes answers the project, the reading of the key (state and project as given), and the change by method.
func keyRoutes(e *environment, project, state string, change reply) {
	row := hmacRow(revokeAccessID, project, hmacAccount("deploy-account", "my-project-123"), state)
	e.fake.routes[hmacProjectPath] = okReply(projectBody)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != revokeKeyPath {
			return nil
		}
		if req.Method == http.MethodGet {
			return &reply{status: 200, body: row}
		}
		return &change
	}
}

func changesOf(e *environment) []seen {
	var out []seen
	for _, r := range e.fake.api() {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

// revokeTools are the tools of the profile storage-revoke and the listed-only HMAC key deletion.
var revokeTools = []string{projectsGetID, hmacKeysListID, hmacKeysGetID, notificationsListID, hmacKeysDeactivateID,
	hmacKeysDeleteID, notificationsDeleteID}

func TestHMACKeysDeactivateSendsOneFixedInactiveBodyWithTheEtag(t *testing.T) {
	e := revokeEnvironment(t, nil, revokeTools)
	keyRoutes(e, "my-project-123", "ACTIVE", okReply(hmacRow(revokeAccessID, "my-project-123",
		hmacAccount("deploy-account", "my-project-123"), "INACTIVE")))
	out, err := e.invokeConfirmed(hmacKeysDeactivateID,
		`{"project_id":"my-project-123","access_id":"`+revokeAccessID+`","etag":"CAE="}`)
	if err != nil {
		t.Fatal(err)
	}
	changes := changesOf(e)
	if len(changes) != 1 || changes[0].method != http.MethodPut || changes[0].body != `{"state":"INACTIVE","etag":"CAE="}` ||
		!strings.HasSuffix(changes[0].url, revokeKeyPath) {
		t.Fatalf("changes = %+v", changes)
	}
	if got := decode(t, out); got["state"] != "INACTIVE" || got["accessId"] != revokeAccessID {
		t.Errorf("out = %s", out)
	}
	if strings.Contains(out, hmacCanaryMaterial) || strings.Contains(strings.ToLower(out), "secret") {
		t.Errorf("a secret reached the result: %s", out)
	}
}

func TestHMACKeysCannotBeActivated(t *testing.T) {
	// No argument names a state, and the body holds INACTIVE whatever the key and the arguments are.
	for _, extra := range []string{`,"state":"ACTIVE"`, `,"body":"{}"`, `,"active":true`} {
		e := revokeEnvironment(t, nil, revokeTools)
		keyRoutes(e, "my-project-123", "INACTIVE", okReply(hmacRow(revokeAccessID, "my-project-123",
			hmacAccount("deploy-account", "my-project-123"), "INACTIVE")))
		if _, err := e.invokeConfirmed(hmacKeysDeactivateID,
			`{"project_id":"my-project-123","access_id":"`+revokeAccessID+`"`+extra+`}`); err == nil {
			t.Errorf("%s accepted", extra)
		}
		noIO(t, e, extra)
	}
	e := revokeEnvironment(t, nil, revokeTools)
	keyRoutes(e, "my-project-123", "INACTIVE", okReply(hmacRow(revokeAccessID, "my-project-123",
		hmacAccount("deploy-account", "my-project-123"), "INACTIVE")))
	if _, err := e.invokeConfirmed(hmacKeysDeactivateID,
		`{"project_id":"my-project-123","access_id":"`+revokeAccessID+`"}`); err != nil {
		t.Fatal(err)
	}
	if changes := changesOf(e); len(changes) != 1 || changes[0].body != `{"state":"INACTIVE"}` {
		t.Errorf("changes = %+v", changes)
	}
	for _, id := range []string{hmacKeysDeactivateID, hmacKeysDeleteID} {
		d, _, _ := e.reg.Lookup(id)
		if strings.Contains(string(d.InputSchema), `"state"`) || strings.Contains(string(d.InputSchema), "ACTIVE") {
			t.Errorf("%s takes a state: %s", id, d.InputSchema)
		}
	}
	metadata, _ := e.reg.ProviderMetadata(Provider)
	for _, tool := range metadata.Tools {
		if strings.Contains(tool.ID, "hmac-keys.") && !strings.HasSuffix(tool.ID, ".list") &&
			!strings.HasSuffix(tool.ID, ".get") && !strings.HasSuffix(tool.ID, ".deactivate") &&
			!strings.HasSuffix(tool.ID, ".delete") {
			t.Errorf("tool %s changes a key in a way that is not offered", tool.ID)
		}
	}
}

func TestHMACKeysDeactivateRefusesAnEtagThatDiffersWithoutAChange(t *testing.T) {
	e := revokeEnvironment(t, nil, revokeTools)
	keyRoutes(e, "my-project-123", "ACTIVE", okReply(`{}`))
	_, err := e.invokeConfirmed(hmacKeysDeactivateID,
		`{"project_id":"my-project-123","access_id":"`+revokeAccessID+`","etag":"OTHER="}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "OTHER=") || len(changesOf(e)) != 0 {
		t.Errorf("err = %v, changes = %+v", err, changesOf(e))
	}
}

func TestHMACKeyOfAnotherProjectIsRefusedWithoutAChange(t *testing.T) {
	for _, id := range []string{hmacKeysDeactivateID, hmacKeysDeleteID} {
		for _, project := range []string{"other-project-1", foreignNumber} {
			e := revokeEnvironment(t, nil, revokeTools)
			keyRoutes(e, project, "INACTIVE", okReply(`{}`))
			_, err := e.invokeConfirmed(id, `{"project_id":"my-project-123","access_id":"`+revokeAccessID+`"}`)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), project) || len(changesOf(e)) != 0 {
				t.Errorf("%s %s: err = %v, changes = %+v", id, project, err, changesOf(e))
			}
		}
	}
	// A project outside the targets ends before any request about the key.
	e := revokeEnvironment(t, []string{"projects/other-project-1"}, revokeTools)
	keyRoutes(e, "my-project-123", "INACTIVE", okReply(`{}`))
	if _, err := e.invokeConfirmed(hmacKeysDeleteID,
		`{"project_id":"my-project-123","access_id":"`+revokeAccessID+`"}`); err == nil {
		t.Error("project outside the targets accepted")
	}
	for _, p := range apiPaths(e) {
		if strings.Contains(p, "hmacKeys") {
			t.Errorf("the key was requested: %v", apiPaths(e))
		}
	}
}

func TestHMACKeysDeleteOfAnActiveKeyIsAnErrorOfGoogle(t *testing.T) {
	e := revokeEnvironment(t, nil, revokeTools)
	keyRoutes(e, "my-project-123", "ACTIVE", reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")})
	_, err := e.invokeConfirmed(hmacKeysDeleteID, `{"project_id":"my-project-123","access_id":"`+revokeAccessID+`"}`)
	changes := changesOf(e)
	if err == nil || classOf(err) == "" || strings.Contains(err.Error(), detailCanary) || len(changes) != 1 ||
		changes[0].method != http.MethodDelete {
		t.Errorf("err = %v, changes = %+v", err, changes)
	}
}

func TestHMACKeysDeleteOfAnInactiveKeySendsOneDeleteWithoutBody(t *testing.T) {
	e := revokeEnvironment(t, nil, revokeTools)
	keyRoutes(e, "my-project-123", "INACTIVE", reply{status: 204})
	out, err := e.invokeConfirmed(hmacKeysDeleteID, `{"project_id":"my-project-123","access_id":"`+revokeAccessID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	changes := changesOf(e)
	if len(changes) != 1 || changes[0].method != http.MethodDelete || changes[0].body != "" ||
		!strings.HasSuffix(changes[0].url, revokeKeyPath) {
		t.Fatalf("changes = %+v", changes)
	}
	if got := decode(t, out); got["deleted"] != true || got["accessId"] != revokeAccessID || got["projectId"] != "my-project-123" ||
		strings.Contains(strings.ToLower(out), "secret") {
		t.Errorf("out = %s", out)
	}
}

func TestHMACKeyChangeTextsNeverHoldAKeyValue(t *testing.T) {
	for _, status := range []int{400, 403, 404, 500} {
		for _, id := range []string{hmacKeysDeactivateID, hmacKeysDeleteID} {
			e := revokeEnvironment(t, nil, revokeTools)
			keyRoutes(e, "my-project-123", "ACTIVE", reply{status: status, body: `{"error":{"code":` +
				`400,"message":"` + hmacCanaryMaterial + `","status":"X"},"secret":"` + hmacCanaryMaterial + `"}`})
			_, err := e.invokeConfirmed(id, `{"project_id":"my-project-123","access_id":"`+revokeAccessID+`"}`)
			if err == nil || strings.Contains(err.Error(), hmacCanaryMaterial) {
				t.Errorf("%s %d: err = %v", id, status, err)
			}
		}
	}
}

func TestNotificationsDeleteSendsOneDeleteAfterReadingTheConfiguration(t *testing.T) {
	e := revokeEnvironment(t, []string{"projects/" + storageProjectNumber}, revokeTools)
	e.fake.routes[bucketsPath+"/example-bucket"] = okReply(`{"name":"example-bucket","projectNumber":"` + storageProjectNumber + `"}`)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != revokeNotePath {
			return nil
		}
		if req.Method == http.MethodGet {
			return &reply{status: 200, body: `{"id":"7","topic":"` + revokeTopic + `","event_types":["OBJECT_FINALIZE"]}`}
		}
		return &reply{status: 204}
	}
	// The short form of the topic names the same topic.
	out, err := e.invokeConfirmed(notificationsDeleteID, `{"bucket":"example-bucket","notification_id":"7",`+
		`"topic":"projects/my-project-123/topics/events","user_project":"`+storageProjectNumber+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	changes := changesOf(e)
	if len(changes) != 1 || changes[0].method != http.MethodDelete || changes[0].body != "" ||
		!strings.HasSuffix(strings.Split(changes[0].url, "?")[0], revokeNotePath) {
		t.Fatalf("changes = %+v", changes)
	}
	if got := decode(t, out); got["deleted"] != true || got["notificationId"] != "7" || got["bucket"] != "example-bucket" {
		t.Errorf("out = %s", out)
	}
	if !strings.Contains(changes[0].url, "userProject="+storageProjectNumber) {
		t.Errorf("url = %s", changes[0].url)
	}
	for _, p := range apiPaths(e) {
		if strings.Contains(p, "pubsub") {
			t.Errorf("the topic was requested: %v", apiPaths(e))
		}
	}
}

func TestNotificationsDeleteWithAnotherTopicMakesNoChange(t *testing.T) {
	e := revokeEnvironment(t, nil, revokeTools)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path == revokeNotePath {
			return &reply{status: 200, body: `{"id":"7","topic":"` + revokeTopic + `"}`}
		}
		return nil
	}
	_, err := e.invokeConfirmed(notificationsDeleteID, `{"bucket":"example-bucket","notification_id":"7",`+
		`"topic":"//pubsub.googleapis.com/projects/my-project-123/topics/other"}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "topics/") || len(changesOf(e)) != 0 {
		t.Errorf("err = %v, changes = %+v", err, changesOf(e))
	}
	// A read that answers another configuration is invalid and ends without a change.
	e = revokeEnvironment(t, nil, revokeTools)
	e.fake.routes[revokeNotePath] = okReply(`{"id":"8","topic":"` + revokeTopic + `"}`)
	if _, err := e.invokeConfirmed(notificationsDeleteID, `{"bucket":"example-bucket","notification_id":"7",`+
		`"topic":"`+revokeTopic+`"}`); err == nil || len(changesOf(e)) != 0 {
		t.Errorf("err = %v, changes = %+v", err, changesOf(e))
	}
}

func TestNotificationsDeleteRefusesABucketOutsideTheTargetsBeforeAnyOtherRequest(t *testing.T) {
	e := revokeEnvironment(t, []string{"projects/" + storageProjectNumber}, revokeTools)
	e.fake.routes[bucketsPath+"/foreign-bucket"] = okReply(`{"name":"foreign-bucket","projectNumber":"` + foreignNumber + `"}`)
	e.fake.routes[bucketsPath+"/foreign-bucket/notificationConfigs/7"] = okReply(`{"id":"7","topic":"` + revokeTopic + `"}`)
	_, err := e.invokeConfirmed(notificationsDeleteID, `{"bucket":"foreign-bucket","notification_id":"7","topic":"`+revokeTopic+`"}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "foreign-bucket") || strings.Contains(err.Error(), foreignNumber) {
		t.Errorf("err = %v", err)
	}
	if paths := apiPaths(e); len(paths) != 1 || !strings.HasSuffix(paths[0], "/b/foreign-bucket") {
		t.Errorf("paths = %v", paths)
	}
}

func TestStorageRevokeToolsRefuseBadInputBeforeIO(t *testing.T) {
	cases := map[string]map[string]string{
		hmacKeysDeactivateID: {
			"path access id": `{"project_id":"my-project-123","access_id":"../abcdefgh"}`,
			"short id":       `{"project_id":"my-project-123","access_id":"abc"}`,
			"bad etag":       `{"project_id":"my-project-123","access_id":"` + revokeAccessID + `","etag":"a b"}`,
			"projects name":  `{"project_id":"projects/x","access_id":"` + revokeAccessID + `"}`,
			"no access":      `{"project_id":"my-project-123"}`,
		},
		hmacKeysDeleteID: {
			"path access id": `{"project_id":"my-project-123","access_id":"a/../bcdefgh"}`,
			"etag":           `{"project_id":"my-project-123","access_id":"` + revokeAccessID + `","etag":"CAE="}`,
			"no project":     `{"access_id":"` + revokeAccessID + `"}`,
		},
		notificationsDeleteID: {
			"path bucket": `{"bucket":"../x","notification_id":"7","topic":"` + revokeTopic + `"}`,
			"path id":     `{"bucket":"example-bucket","notification_id":"7/../x","topic":"` + revokeTopic + `"}`,
			"no topic":    `{"bucket":"example-bucket","notification_id":"7"}`,
			"control":     `{"bucket":"example-bucket","notification_id":"7","topic":"a\nb"}`,
			"long topic":  `{"bucket":"example-bucket","notification_id":"7","topic":"` + strings.Repeat("a", 300) + `"}`,
			"extra":       `{"bucket":"example-bucket","notification_id":"7","topic":"t","force":true}`,
		},
	}
	for id, byName := range cases {
		for name, args := range byName {
			e := revokeEnvironment(t, nil, revokeTools)
			if _, err := e.invokeConfirmed(id, args); err == nil {
				t.Errorf("%s %s accepted", id, name)
			}
			noIO(t, e, id+" "+name)
		}
	}
}

func TestStorageRevokeWithoutConfirmationMakesNoProviderCall(t *testing.T) {
	args := map[string]string{
		hmacKeysDeactivateID:  `{"project_id":"my-project-123","access_id":"` + revokeAccessID + `"}`,
		hmacKeysDeleteID:      `{"project_id":"my-project-123","access_id":"` + revokeAccessID + `"}`,
		notificationsDeleteID: `{"bucket":"example-bucket","notification_id":"7","topic":"` + revokeTopic + `"}`,
	}
	for id, a := range args {
		e := revokeEnvironment(t, nil, revokeTools)
		keyRoutes(e, "my-project-123", "INACTIVE", reply{status: 204})
		if _, err := e.invoke(id, a); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		effect := "delete"
		if id == hmacKeysDeactivateID {
			effect = "update"
		}
		if d.Risk.Confirmation != "required" || string(d.Risk.Effect) != effect || !d.Risk.OpenWorld ||
			d.RequiresToolAllowList != (id == hmacKeysDeleteID) {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if e.reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
}

func TestStorageRevokeConfirmationNamesWhatIsChanged(t *testing.T) {
	e := newEnvironment(t, nil)
	text := e.reg.ConfirmationSummary(hmacKeysDeactivateID)(json.RawMessage(
		`{"project_id":"my-project-123","access_id":"` + revokeAccessID + `","etag":"CAE="}`))
	for _, want := range []string{"my-project-123", revokeAccessID, "CAE="} {
		if !strings.Contains(text, want) {
			t.Errorf("deactivate summary %q lacks %q", text, want)
		}
	}
	text = e.reg.ConfirmationSummary(hmacKeysDeleteID)(json.RawMessage(
		`{"project_id":"my-project-123","access_id":"` + revokeAccessID + `"}`))
	for _, want := range []string{"my-project-123", revokeAccessID, "INACTIVE"} {
		if !strings.Contains(text, want) {
			t.Errorf("delete summary %q lacks %q", text, want)
		}
	}
	text = e.reg.ConfirmationSummary(notificationsDeleteID)(json.RawMessage(
		`{"bucket":"example-bucket","notification_id":"7","topic":"` + revokeTopic + `"}`))
	for _, want := range []string{"example-bucket", `"7"`, revokeTopic} {
		if !strings.Contains(text, want) {
			t.Errorf("notification summary %q lacks %q", text, want)
		}
	}
}

func TestStorageRevokeToolsRunWithTheProfileAlone(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	var tools []string
	var recommended bool
	for _, profile := range metadata.Profiles {
		if profile.ID == "storage-revoke" {
			tools, recommended = profile.Tools, profile.Recommended
		}
	}
	if len(tools) == 0 || recommended {
		t.Fatalf("profile storage-revoke tools = %v, recommended = %v", tools, recommended)
	}
	if slices.Contains(tools, hmacKeysDeleteID) {
		t.Errorf("profile storage-revoke holds %s", hmacKeysDeleteID)
	}
	e = revokeEnvironment(t, []string{"projects/my-project-123"}, tools)
	keyRoutes(e, "my-project-123", "INACTIVE", reply{status: 204})
	if _, err := e.invokeConfirmed(hmacKeysDeleteID,
		`{"project_id":"my-project-123","access_id":"`+revokeAccessID+`"}`); err == nil {
		t.Error("hmac-keys.delete ran with the profile alone")
	}
}
