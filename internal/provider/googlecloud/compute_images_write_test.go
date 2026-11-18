package googlecloud

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	imageInsertArgs     = `{"project_id":"my-project-123","name":"web-base-v2"`
	imageDeleteArgs     = `{"project_id":"my-project-123","image":"img-a"`
	snapshotDeleteArgs  = `{"project_id":"my-project-123","snapshot":"snap-a"`
	globalOperationsURL = "https://www.googleapis.com/compute/v1/projects/my-project-123/global"
	// keyFileContent is synthetic base64 and no key.
	keyFileContent = "c3ludGhldGljLXRlc3QtY29udGVudA=="
)

var imageTools = []string{imagesInsertID, snapshotsDeleteID, imagesDeleteID}

func globalOperationBody(kind, status, extra string) string {
	return `{"kind":"compute#operation","id":"9100000000000000003","name":"` + operationName + `",` +
		`"operationType":"` + kind + `","status":"` + status + `","user":"person@example.com",` +
		`"targetLink":"` + globalOperationsURL + `/images/web-base-v2",` +
		`"selfLink":"` + globalOperationsURL + `/operations/` + operationName + `"` + extra + `}`
}

// imageEnvironment is an environment whose connection offers the tools of the profile compute-images-create
// and the listed-only deletions of snapshots and images, or the tools listed.
func imageEnvironment(t *testing.T, tools []string) *environment {
	t.Helper()
	if tools == nil {
		tools = []string{projectsGetID, imagesInsertID, snapshotsDeleteID, imagesDeleteID}
	}
	return createEnvironment(t, []string{"projects/my-project-123"}, tools)
}

// answerAll answers every request of the change with reply.
func answerAll(e *environment, r reply) {
	e.fake.hook = func(int, *http.Request) *reply { return &r }
}

func TestImagesInsertFromADiskSendsOnePostWithBuiltURLs(t *testing.T) {
	e := imageEnvironment(t, nil)
	answerAll(e, okReply(globalOperationBody("insert", "RUNNING", "")))
	out, err := e.invokeConfirmed(imagesInsertID, imageInsertArgs+`,"source_disk":"data-disk-a",`+
		`"source_disk_zone":"us-central1-a","family":"web-base","description":"golden image",`+
		`"labels":{"env":"test"},"storage_locations":["us"],"request_id":"`+deleteRequestID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != http.MethodPost ||
		api[0].url != "https://compute.googleapis.com"+imagesPath+"?requestId="+deleteRequestID {
		t.Fatalf("requests = %+v", api)
	}
	want := map[string]any{"name": "web-base-v2", "description": "golden image", "family": "web-base",
		"sourceDisk": diskRefBase + "my-project-123/zones/us-central1-a/disks/data-disk-a",
		"labels":     map[string]any{"env": "test"}, "storageLocations": []any{"us"}}
	if got := sentBody(t, api[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
	got := decode(t, out)
	operation, _ := got["operation"].(map[string]any)
	if operation["status"] != "RUNNING" || got["scope"] != "global" || got["request_id"] != deleteRequestID {
		t.Errorf("result = %s", out)
	}
	if _, zonal := got["zone"]; zonal {
		t.Errorf("result names a zone: %s", out)
	}
}

func TestImagesInsertBuildsEverySourceAndFieldInTheProject(t *testing.T) {
	cases := map[string]struct {
		args string
		want map[string]any
	}{
		"snapshot": {`,"source_snapshot":"snap-a"`, map[string]any{
			"sourceSnapshot": diskRefBase + "my-project-123/global/snapshots/snap-a"}},
		"image": {`,"source_image":"img-a"`, map[string]any{
			"sourceImage": diskRefBase + "my-project-123/global/images/img-a"}},
		"everything else": {`,"source_snapshot":"snap-a","guest_os_features":["UEFI_COMPATIBLE","GVNIC"],` +
			`"licenses":[{"license":"lic-a"},{"license":"debian-12-bookworm","project":"debian-cloud"}],` +
			`"kms_key_name":"` + diskWriteKMSName + `","kms_key_service_account":"encrypter-a@example-project-123.iam.gserviceaccount.com",` +
			`"architecture":"X86_64","shielded_instance_initial_state":{"pk":{"content":"` + keyFileContent + `","file_type":"X509"},` +
			`"dbs":[{"content":"` + keyFileContent + `"}]}`,
			map[string]any{"sourceSnapshot": diskRefBase + "my-project-123/global/snapshots/snap-a",
				"guestOsFeatures": []any{map[string]any{"type": "UEFI_COMPATIBLE"}, map[string]any{"type": "GVNIC"}},
				"licenses": []any{diskRefBase + "my-project-123/global/licenses/lic-a",
					diskRefBase + "debian-cloud/global/licenses/debian-12-bookworm"},
				"imageEncryptionKey": map[string]any{"kmsKeyName": diskWriteKMSName,
					"kmsKeyServiceAccount": "encrypter-a@example-project-123.iam.gserviceaccount.com"},
				"architecture": "X86_64",
				"shieldedInstanceInitialState": map[string]any{
					"pk":  map[string]any{"content": keyFileContent, "fileType": "X509"},
					"dbs": []any{map[string]any{"content": keyFileContent}}}}},
	}
	for name, c := range cases {
		e := imageEnvironment(t, nil)
		answerAll(e, okReply(globalOperationBody("insert", "PENDING", "")))
		if _, err := e.invokeConfirmed(imagesInsertID, imageInsertArgs+c.args+`}`); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		api := e.fake.api()
		if len(api) != 1 {
			t.Fatalf("%s: requests = %+v", name, api)
		}
		if u, _ := url.Parse(api[0].url); len(u.Query()) != 1 {
			t.Errorf("%s: url = %s", name, api[0].url)
		}
		c.want["name"] = "web-base-v2"
		if got := sentBody(t, api[0]); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: body = %v, want %v", name, got, c.want)
		}
	}
}

func TestImagesInsertSendsForceCreate(t *testing.T) {
	e := imageEnvironment(t, nil)
	answerAll(e, okReply(globalOperationBody("insert", "RUNNING", "")))
	if _, err := e.invokeConfirmed(imagesInsertID, imageInsertArgs+`,"source_disk":"data-disk-a",`+
		`"source_disk_zone":"us-central1-a","force_create":true,"request_id":"`+deleteRequestID+`"}`); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(e.fake.api()[0].url)
	if u.Path != imagesPath || u.Query().Get("forceCreate") != "true" || u.Query().Get("requestId") != deleteRequestID ||
		len(u.Query()) != 2 {
		t.Errorf("url = %s", e.fake.api()[0].url)
	}
}

func TestImagesInsertRefusesForeignSourcesRawDiskKeyMaterialAndBadInputBeforeIO(t *testing.T) {
	disk := `,"source_disk":"data-disk-a","source_disk_zone":"us-central1-a"`
	for _, args := range []string{
		// A source is a name of the project of the call, never a URL, a path, or a public image.
		imageInsertArgs + `,"source_image":"https://compute.googleapis.com/compute/v1/projects/other-project-123/global/images/img-a"}`,
		imageInsertArgs + `,"source_image":"projects/other-project-123/global/images/img-a"}`,
		imageInsertArgs + `,"source_snapshot":"projects/other-project-123/global/snapshots/snap-a"}`,
		imageInsertArgs + `,"source_disk":"zones/us-central1-a/disks/data-disk-a","source_disk_zone":"us-central1-a"}`,
		imageInsertArgs + `,"source_image":"img-a","source_image_project":"debian-cloud"}`,
		imageInsertArgs + `,"source_image":"img-a","source_project":"other-project-123"}`,
		// Exactly one source, and a disk with its zone.
		imageInsertArgs + `}`,
		imageInsertArgs + `,"source_image":"img-a","source_snapshot":"snap-a"}`,
		imageInsertArgs + `,"source_disk":"data-disk-a"}`,
		imageInsertArgs + `,"source_image":"img-a","source_disk_zone":"us-central1-a"}`,
		// An archive in Cloud Storage is no source.
		imageInsertArgs + `,"raw_disk":{"source":"https://storage.googleapis.com/bucket-a/disk.tar.gz"}}`,
		imageInsertArgs + `,"rawDisk":{"source":"https://storage.googleapis.com/bucket-a/disk.tar.gz"}}`,
		imageInsertArgs + `,"source_type":"RAW","source_image":"img-a"}`,
		// Key material and the key fields of a source are no argument.
		imageInsertArgs + disk + `,"raw_key":"canary-raw-value-not-a-key"}`,
		imageInsertArgs + disk + `,"image_encryption_key":{"rawKey":"canary-raw-value-not-a-key"}}`,
		imageInsertArgs + disk + `,"source_disk_encryption_key":{"rawKey":"canary-raw-value-not-a-key"}}`,
		imageInsertArgs + disk + `,"source_snapshot_encryption_key":{"rsaEncryptedKey":"canary-rsa-value-not-a-key"}}`,
		imageInsertArgs + disk + `,"source_image_encryption_key":{"kmsKeyName":"` + diskWriteKMSName + `"}}`,
		imageInsertArgs + disk + `,"kms_key_name":"canary-raw-value-not-a-key"}`,
		imageInsertArgs + disk + `,"kms_key_service_account":"encrypter-a@example-project-123.iam.gserviceaccount.com"}`,
		// Fields Google derives or that bind access are no argument.
		imageInsertArgs + disk + `,"license_codes":["1000201"]}`,
		imageInsertArgs + disk + `,"params":{"resourceManagerTags":{"tagKeys/1":"tagValues/2"}}}`,
		imageInsertArgs + disk + `,"disk_size_gb":10}`,
		imageInsertArgs + disk + `,"licenses":[{"license":"lic-a","project":"other-project-123"}]}`,
		imageInsertArgs + disk + `,"shielded_instance_initial_state":{"pk":{"content":"not base64!"}}}`,
		imageInsertArgs + disk + `,"shielded_instance_initial_state":{"pk":{"content":"` + keyFileContent + `","file_type":"UNDEFINED"}}}`,
		imageInsertArgs + disk + `,"labels":{"Env":"test"}}`,
		imageInsertArgs + disk + `,"storage_locations":["US"]}`,
		imageInsertArgs + disk + `,"guest_os_features":["GVNIC","GVNIC"]}`,
		imageInsertArgs + disk + `,"family":"Web_Base"}`,
		imageInsertArgs + disk + `,"description":"line\u0007bell"}`,
		imageInsertArgs + disk + `,"request_id":"not-a-uuid"}`,
		`{"project_id":"my-project-123","name":"Web_Base"` + disk + `}`,
		`{"project_id":"example-other-9","name":"web-base-v2"` + disk + `}`,
	} {
		e := imageEnvironment(t, nil)
		_, err := e.invokeConfirmed(imagesInsertID, args)
		if err == nil {
			t.Errorf("accepted %s", args)
		} else if strings.Contains(err.Error(), "canary") {
			t.Errorf("refusal repeats the value: %v", err)
		}
		noIO(t, e, args)
	}
}

func TestImagesInsertRefusesInvalidArgumentsInTheHandlerToo(t *testing.T) {
	// The schema already refuses these; the handler checks again before any secret is read.
	base := imageInsertArguments{ProjectID: "my-project-123", Name: "i", SourceSnapshot: "snap-a"}
	for name, mutate := range map[string]func(*imageInsertArguments){
		"no source":       func(a *imageInsertArguments) { a.SourceSnapshot = "" },
		"two sources":     func(a *imageInsertArguments) { a.SourceImage = "img-a" },
		"source url":      func(a *imageInsertArguments) { a.SourceSnapshot = "https://x/snapshots/snap-a" },
		"disk zone alone": func(a *imageInsertArguments) { a.SourceDiskZone = "us-central1-a" },
		"disk sans zone":  func(a *imageInsertArguments) { a.SourceSnapshot, a.SourceDisk = "", "d" },
		"foreign license": func(a *imageInsertArguments) {
			a.Licenses = []diskLicense{{License: "l", Project: "other-project-123"}}
		},
		"account sans key": func(a *imageInsertArguments) {
			a.KmsKeyServiceAccount = "encrypter-a@example-project-123.iam.gserviceaccount.com"
		},
		"empty key file": func(a *imageInsertArguments) {
			a.ShieldedInstanceInitialState = &imageInitialState{KEKs: []imageKeyFile{{Content: ""}}}
		},
		"too many key files": func(a *imageInsertArguments) {
			a.ShieldedInstanceInitialState = &imageInitialState{DBXs: make([]imageKeyFile, maxKeyFiles+1)}
		},
	} {
		a := base
		mutate(&a)
		if a.check() == nil {
			t.Errorf("%s: accepted %+v", name, a)
		}
	}
	if err := base.check(); err != nil {
		t.Errorf("valid arguments refused: %v", err)
	}
}

func TestSnapshotsAndImagesDeleteSendOneDelete(t *testing.T) {
	for id, c := range map[string]struct{ args, path string }{
		snapshotsDeleteID: {snapshotDeleteArgs, snapshotsPath + "/snap-a"},
		imagesDeleteID:    {imageDeleteArgs, imagesPath + "/img-a"},
	} {
		e := imageEnvironment(t, nil)
		answerAll(e, okReply(globalOperationBody("delete", "RUNNING", "")))
		out, err := e.invokeConfirmed(id, c.args+`,"request_id":"`+deleteRequestID+`"}`)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		api := e.fake.api()
		if len(api) != 1 || api[0].method != http.MethodDelete || api[0].body != "" ||
			api[0].url != "https://compute.googleapis.com"+c.path+"?requestId="+deleteRequestID {
			t.Fatalf("%s: requests = %+v", id, api)
		}
		if got := decode(t, out); got["scope"] != "global" || got["request_id"] != deleteRequestID {
			t.Errorf("%s: result = %s", id, out)
		}
	}
	// A numeric ID names the resource as well.
	e := imageEnvironment(t, nil)
	answerAll(e, okReply(globalOperationBody("delete", "RUNNING", "")))
	if _, err := e.invokeConfirmed(imagesDeleteID, `{"project_id":"my-project-123","image":"7400000000000000001"}`); err != nil {
		t.Fatal(err)
	}
	if u, _ := url.Parse(e.fake.api()[0].url); u.Path != imagesPath+"/7400000000000000001" {
		t.Errorf("url = %s", e.fake.api()[0].url)
	}
}

func TestSnapshotsAndImagesDeleteRefuseBeforeAnyIO(t *testing.T) {
	for id, list := range map[string][]string{
		snapshotsDeleteID: {
			`{"project_id":"my-project-123","snapshot":"global/snapshots/snap-a"}`,
			`{"project_id":"my-project-123","snapshot":"../snap-a"}`,
			`{"project_id":"example-other-9","snapshot":"snap-a"}`,
			snapshotDeleteArgs + `,"request_id":"` + nilRequestID + `"}`,
			snapshotDeleteArgs + `,"zone":"us-central1-a"}`,
		},
		imagesDeleteID: {
			`{"project_id":"my-project-123","image":"projects/debian-cloud/global/images/img-a"}`,
			`{"project_id":"my-project-123","image":"family/web-base"}`,
			`{"project_id":"example-other-9","image":"img-a"}`,
			imageDeleteArgs + `,"request_id":"nope"}`,
		},
	} {
		for _, args := range list {
			e := imageEnvironment(t, nil)
			if _, err := e.invokeConfirmed(id, args); err == nil {
				t.Errorf("%s accepted %s", id, args)
			}
			noIO(t, e, id+" "+args)
		}
	}
}

func TestImageToolsNeedConfirmationAndNameWhatIsLost(t *testing.T) {
	args := map[string]string{imagesInsertID: imageInsertArgs + `,"source_snapshot":"snap-a"}`,
		snapshotsDeleteID: snapshotDeleteArgs + `}`, imagesDeleteID: imageDeleteArgs + `}`}
	for _, id := range imageTools {
		e := imageEnvironment(t, nil)
		if _, err := e.invoke(id, args[id]); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		wantEffect := "delete"
		if id == imagesInsertID {
			wantEffect = "create"
		}
		if d.Risk.Confirmation != "required" || string(d.Risk.Effect) != wantEffect || !d.Risk.OpenWorld ||
			string(d.Risk.Idempotency) != "idempotent" || d.RequiresToolAllowList != (id != imagesInsertID) {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if got := tokenMetadata.Scopes[id]; !slices.Equal(got, []string{scopeCompute}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if slices.Contains(readTools, id) {
			t.Errorf("%s is in the profile read", id)
		}
		if reaches, _ := targetReaches(id, []string{"billingAccounts/012345-ABCDEF-678901"}); reaches {
			t.Errorf("%s is offered to a billing-only binding", id)
		}
	}
	e := newEnvironment(t, nil)
	insert := e.reg.ConfirmationSummary(imagesInsertID)(json.RawMessage(imageInsertArgs +
		`,"source_disk":"data-disk-a","source_disk_zone":"us-central1-a","family":"web-base",` +
		`"storage_locations":["eu"],"force_create":true}`))
	for _, part := range []string{"my-project-123", "image", "web-base-v2", "disk data-disk-a", "us-central1-a",
		"web-base", "eu", "running instance", "billed"} {
		if !strings.Contains(insert, part) {
			t.Errorf("insert summary %q lacks %q", insert, part)
		}
	}
	snapshot := e.reg.ConfirmationSummary(snapshotsDeleteID)(json.RawMessage(snapshotDeleteArgs + `}`))
	for _, part := range []string{"snapshot snap-a", "my-project-123", "backup", "lost", "cannot be restored"} {
		if !strings.Contains(snapshot, part) {
			t.Errorf("snapshot summary %q lacks %q", snapshot, part)
		}
	}
	image := e.reg.ConfirmationSummary(imagesDeleteID)(json.RawMessage(imageDeleteArgs + `}`))
	for _, part := range []string{"image img-a", "my-project-123", "lost", "cannot be restored"} {
		if !strings.Contains(image, part) {
			t.Errorf("image summary %q lacks %q", image, part)
		}
	}
}

func TestImageToolsRunWithTheProfileAloneAndProveAFolderTarget(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	var tools []string
	for _, profile := range metadata.Profiles {
		if profile.ID == "compute-images-create" {
			tools = profile.Tools
		}
	}
	if !slices.Equal(tools, []string{projectsGetID, imagesInsertID}) {
		t.Fatalf("profile tools = %v", tools)
	}
	// The deletions are listed-only, so the connection names them besides the profile.
	e = createEnvironment(t, []string{"folders/2001"}, append(slices.Clone(tools), snapshotsDeleteID, imagesDeleteID))
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"organizations/1"}`)
	e.fake.routes[imagesPath] = okReply(globalOperationBody("insert", "RUNNING", ""))
	e.fake.routes[snapshotsPath+"/snap-a"] = okReply(globalOperationBody("delete", "RUNNING", ""))
	e.fake.routes[imagesPath+"/img-a"] = okReply(globalOperationBody("delete", "RUNNING", ""))
	for id, args := range map[string]string{imagesInsertID: imageInsertArgs + `,"source_snapshot":"snap-a"}`,
		snapshotsDeleteID: snapshotDeleteArgs + `}`, imagesDeleteID: imageDeleteArgs + `}`} {
		if _, err := e.invokeConfirmed(id, args); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	if _, err := e.invokeConfirmed(disksDeleteID, diskDeleteArgs+diskDeleteExpected+`}`); err == nil {
		t.Error("disks.delete ran with the profile alone")
	}
}

func TestImageToolsRepeatTheSameRequestIDAndReportOutcomes(t *testing.T) {
	e := imageEnvironment(t, nil)
	answerAll(e, okReply(globalOperationBody("delete", "DONE", "")))
	for i := 0; i < 2; i++ {
		out, err := e.invokeConfirmed(snapshotsDeleteID, snapshotDeleteArgs+`,"request_id":"`+deleteRequestID+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		if got := decode(t, out); got["request_id"] != deleteRequestID {
			t.Errorf("result = %s", out)
		}
	}
	if api := e.fake.api(); len(api) != 2 || api[0].url != api[1].url {
		t.Errorf("requests = %+v", api)
	}
	// An image that disks still use, or a missing one, is reported as Google sends it, after the one request.
	for status, body := range map[int]string{400: errorObject(400, "FAILED_PRECONDITION", ""),
		404: errorObject(404, "NOT_FOUND", "")} {
		e = imageEnvironment(t, nil)
		answerAll(e, reply{status: status, body: body})
		_, err := e.invokeConfirmed(imagesDeleteID, imageDeleteArgs+`}`)
		if err == nil || strings.Contains(err.Error(), detailCanary) {
			t.Errorf("%d: err = %v", status, err)
		}
		if got := methodsOf(e); !slices.Equal(got, []string{http.MethodDelete}) {
			t.Errorf("%d: methods = %v", status, got)
		}
	}
	// A done operation with an error and an unclear outcome are no success.
	e = imageEnvironment(t, nil)
	answerAll(e, okReply(globalOperationBody("insert", "DONE", `,"httpErrorStatusCode":400,`+
		`"error":{"errors":[{"code":"RESOURCE_NOT_READY","message":"`+detailCanary+`"}]}`)))
	if _, err := e.invokeConfirmed(imagesInsertID, imageInsertArgs+`,"source_snapshot":"snap-a"}`); classOf(err) !=
		provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("done with error: class = %q, err = %v", classOf(err), err)
	}
	e = imageEnvironment(t, nil)
	answerAll(e, reply{status: 503, body: errorObject(503, "UNAVAILABLE", "")})
	if _, err := e.invokeConfirmed(imagesInsertID, imageInsertArgs+`,"source_snapshot":"snap-a"}`); err == nil ||
		!strings.Contains(err.Error(), "outcome") {
		t.Errorf("unavailable: err = %v", err)
	}
	if got := methodsOf(e); !slices.Equal(got, []string{http.MethodPost}) {
		t.Errorf("unavailable: methods = %v", got)
	}
}

func TestImageToolsDropKeyMaterialFromTheOperation(t *testing.T) {
	e := imageEnvironment(t, nil)
	answerAll(e, okReply(globalOperationBody("insert", "RUNNING", `,"warnings":[{"code":"X","data":[`+
		`{"key":"k","rawKey":"`+diskRawCanary+`","RSAEncryptedKey":"`+diskRSACanary+`"}]}]`)))
	out, err := e.invokeConfirmed(imagesInsertID, imageInsertArgs+`,"source_snapshot":"snap-a"}`)
	if err != nil {
		t.Fatal(err)
	}
	absentDiskKeys(t, "insert", out)
	if !strings.Contains(out, `"warnings"`) {
		t.Errorf("result = %s", out)
	}
}
