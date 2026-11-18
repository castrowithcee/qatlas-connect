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
	diskWriteZoneURL = "https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a"
	diskRefBase      = "https://compute.googleapis.com/compute/v1/projects/"
	diskWritePath    = disksPath + "/data-disk-a"
	diskInsertArgs   = `{"project_id":"my-project-123","zone":"us-central1-a","name":"data-disk-a"`
	diskResizeArgs   = `{"project_id":"my-project-123","zone":"us-central1-a","disk":"data-disk-a"`
	diskSnapshotArgs = `{"project_id":"my-project-123","zone":"us-central1-a","disk":"data-disk-a","name":"snap-a"`
	diskWriteKMSName = "projects/kms-project-123/locations/us-central1/keyRings/ring-a/cryptoKeys/key-a"
)

var diskWriteTools = []string{disksInsertID, disksResizeID, disksCreateSnapshotID}

func zoneOperationBody(status, extra string) string {
	return `{"kind":"compute#operation","id":"9100000000000000002","name":"` + operationName + `",` +
		`"operationType":"insert","status":"` + status + `","user":"person@example.com",` +
		`"zone":"` + diskWriteZoneURL + `","targetLink":"` + diskWriteZoneURL + `/disks/data-disk-a"` + extra + `}`
}

// diskWriteRoutes answers the read of the disk with a disk of sizeGb and every POST with posted.
func diskWriteRoutes(e *environment, sizeGb string, posted reply) {
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.Method == http.MethodGet && req.URL.Path == diskWritePath {
			body := strings.Replace(diskBody("data-disk-a"), `"sizeGb":"100"`, `"sizeGb":"`+sizeGb+`"`, 1)
			return &reply{status: 200, body: body}
		}
		if req.Method == http.MethodPost {
			return &posted
		}
		return nil
	}
}

func sentBody(t *testing.T, r seen) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(r.body), &body); err != nil {
		t.Fatalf("body %q: %v", r.body, err)
	}
	return body
}

func TestDisksInsertFromAPublicImageFamilySendsOnePostWithBuiltURLs(t *testing.T) {
	e := writeEnvironment(t, []string{"projects/my-project-123"})
	diskWriteRoutes(e, "100", okReply(zoneOperationBody("RUNNING", "")))
	out, err := e.invokeConfirmed(disksInsertID, diskInsertArgs+`,"size_gb":20,"type":"pd-balanced",`+
		`"source_image_family":"debian-12","source_image_project":"debian-cloud","description":"boot disk",`+
		`"labels":{"env":"test","empty":""},"request_id":"`+deleteRequestID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != "POST" ||
		api[0].url != "https://compute.googleapis.com"+disksPath+"?requestId="+deleteRequestID {
		t.Fatalf("requests = %+v", api)
	}
	want := map[string]any{"name": "data-disk-a", "description": "boot disk", "sizeGb": "20",
		"type":        diskRefBase + "my-project-123/zones/us-central1-a/diskTypes/pd-balanced",
		"sourceImage": diskRefBase + "debian-cloud/global/images/family/debian-12",
		"labels":      map[string]any{"env": "test", "empty": ""}}
	if got := sentBody(t, api[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
	got := decode(t, out)
	operation, _ := got["operation"].(map[string]any)
	if operation["status"] != "RUNNING" || got["scope"] != "zone" || got["zone"] != "us-central1-a" ||
		got["request_id"] != deleteRequestID {
		t.Errorf("result = %s", out)
	}
}

func TestDisksInsertBuildsEveryReferenceInTheProjectAndZone(t *testing.T) {
	cases := map[string]struct {
		args string
		want map[string]any
	}{
		"own image": {`,"source_image":"img-a"`, map[string]any{
			"sourceImage": diskRefBase + "my-project-123/global/images/img-a"}},
		"own family": {`,"source_image_family":"fam-a"`, map[string]any{
			"sourceImage": diskRefBase + "my-project-123/global/images/family/fam-a"}},
		"public image": {`,"source_image":"ubuntu-2404-noble-amd64-v20261101","source_image_project":"ubuntu-os-cloud"`,
			map[string]any{"sourceImage": diskRefBase + "ubuntu-os-cloud/global/images/ubuntu-2404-noble-amd64-v20261101"}},
		"snapshot": {`,"source_snapshot":"snap-a"`, map[string]any{
			"sourceSnapshot": diskRefBase + "my-project-123/global/snapshots/snap-a"}},
		"disk clone": {`,"source_disk":"disk-b"`, map[string]any{
			"sourceDisk": diskRefBase + "my-project-123/zones/us-central1-a/disks/disk-b"}},
		"instant snapshot": {`,"source_instant_snapshot":"isnap-a"`, map[string]any{
			"sourceInstantSnapshot": diskRefBase + "my-project-123/zones/us-central1-a/instantSnapshots/isnap-a"}},
		"everything else": {`,"provisioned_iops":3000,"provisioned_throughput":140,"physical_block_size_bytes":4096,` +
			`"licenses":[{"license":"lic-a"},{"license":"debian-12-bookworm","project":"debian-cloud"}],` +
			`"kms_key_name":"` + diskWriteKMSName + `","kms_key_service_account":"encrypter-a@example-project-123.iam.gserviceaccount.com",` +
			`"resource_policies":["daily"],"storage_pool":"pool-a","architecture":"X86_64",` +
			`"guest_os_features":["UEFI_COMPATIBLE","GVNIC"],"access_mode":"READ_WRITE_SINGLE",` +
			`"enable_confidential_compute":false`,
			map[string]any{"provisionedIops": "3000", "provisionedThroughput": "140", "physicalBlockSizeBytes": "4096",
				"licenses": []any{diskRefBase + "my-project-123/global/licenses/lic-a",
					diskRefBase + "debian-cloud/global/licenses/debian-12-bookworm"},
				"diskEncryptionKey": map[string]any{"kmsKeyName": diskWriteKMSName,
					"kmsKeyServiceAccount": "encrypter-a@example-project-123.iam.gserviceaccount.com"},
				"resourcePolicies": []any{diskRefBase + "my-project-123/regions/us-central1/resourcePolicies/daily"},
				"storagePool":      diskRefBase + "my-project-123/zones/us-central1-a/storagePools/pool-a",
				"architecture":     "X86_64", "accessMode": "READ_WRITE_SINGLE", "enableConfidentialCompute": false,
				"guestOsFeatures": []any{map[string]any{"type": "UEFI_COMPATIBLE"}, map[string]any{"type": "GVNIC"}}}},
	}
	for name, c := range cases {
		e := writeEnvironment(t, nil)
		diskWriteRoutes(e, "100", okReply(zoneOperationBody("PENDING", "")))
		if _, err := e.invokeConfirmed(disksInsertID, diskInsertArgs+c.args+`}`); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		api := e.fake.api()
		if len(api) != 1 {
			t.Fatalf("%s: requests = %+v", name, api)
		}
		c.want["name"] = "data-disk-a"
		if got := sentBody(t, api[0]); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: body = %v, want %v", name, got, c.want)
		}
	}
}

func TestDiskWritesRefuseKeyMaterialForeignSourcesAndBadInputBeforeIO(t *testing.T) {
	args := map[string][]string{
		disksInsertID: {
			// Key material is no argument, under any name.
			diskInsertArgs + `,"raw_key":"canary-raw-value-not-a-key"}`,
			diskInsertArgs + `,"rawKey":"canary-raw-value-not-a-key"}`,
			diskInsertArgs + `,"disk_encryption_key":{"rawKey":"canary-raw-value-not-a-key"}}`,
			diskInsertArgs + `,"rsa_encrypted_key":"canary-rsa-value-not-a-key"}`,
			diskInsertArgs + `,"source_image_encryption_key":{"rawKey":"canary-raw-value-not-a-key"}}`,
			diskInsertArgs + `,"kms_key_name":"canary-raw-value-not-a-key"}`,
			diskInsertArgs + `,"kms_key_service_account":"encrypter-a@example-project-123.iam.gserviceaccount.com"}`,
			// A source is a name of the project itself or of a public image project, never a URL or path.
			diskInsertArgs + `,"source_image":"https://compute.googleapis.com/compute/v1/projects/other-project-123/global/images/img-a"}`,
			diskInsertArgs + `,"source_image":"projects/other-project-123/global/images/img-a"}`,
			diskInsertArgs + `,"source_image":"img-a","source_image_project":"other-project-123"}`,
			diskInsertArgs + `,"source_snapshot":"global/snapshots/snap-a"}`,
			diskInsertArgs + `,"source_disk":"../disk-b"}`,
			diskInsertArgs + `,"source_image_project":"debian-cloud"}`,
			diskInsertArgs + `,"source_image":"img-a","source_snapshot":"snap-a"}`,
			diskInsertArgs + `,"source_storage_object":"gs://bucket-a/disk.tar.gz"}`,
			diskInsertArgs + `,"type":"projects/other-project-123/zones/us-central1-a/diskTypes/pd-ssd"}`,
			diskInsertArgs + `,"licenses":[{"license":"lic-a","project":"other-project-123"}]}`,
			diskInsertArgs + `,"resource_policies":["daily","daily"]}`,
			diskInsertArgs + `,"labels":{"Env":"test"}}`,
			diskInsertArgs + `,"labels":{"env":"Test"}}`,
			diskInsertArgs + `,"description":"line\u0007bell"}`,
			diskInsertArgs + `,"size_gb":0}`,
			diskInsertArgs + `,"physical_block_size_bytes":8192}`,
			diskInsertArgs + `,"guest_os_features":["GVNIC","GVNIC"]}`,
			diskInsertArgs + `,"replica_zones":["us-central1-b"]}`,
			diskInsertArgs + `,"request_id":"not-a-uuid"}`,
			`{"project_id":"my-project-123","zone":"us-central1-a","name":"Data_Disk"}`,
			`{"project_id":"my-project-123","zone":"us-central1-a/x","name":"data-disk-a"}`,
			`{"project_id":"example-other-9","zone":"us-central1-a","name":"data-disk-a"}`,
		},
		disksResizeID: {
			diskResizeArgs + `}`,
			diskResizeArgs + `,"size_gb":0}`,
			diskResizeArgs + `,"size_gb":65537}`,
			`{"project_id":"my-project-123","zone":"us-central1-a","disk":"disks/data-disk-a","size_gb":200}`,
			`{"project_id":"example-other-9","zone":"us-central1-a","disk":"data-disk-a","size_gb":200}`,
		},
		disksCreateSnapshotID: {
			diskSnapshotArgs + `,"snapshot_encryption_key":{"rawKey":"canary-raw-value-not-a-key"}}`,
			diskSnapshotArgs + `,"source_disk_encryption_key":{"rsaEncryptedKey":"canary-rsa-value-not-a-key"}}`,
			diskSnapshotArgs + `,"storage_locations":["US"]}`,
			diskSnapshotArgs + `,"storage_locations":["us","us"]}`,
			diskSnapshotArgs + `,"snapshot_type":"FAST"}`,
			diskSnapshotArgs + `,"chain_name":"Chain"}`,
			`{"project_id":"my-project-123","zone":"us-central1-a","disk":"data-disk-a"}`,
			`{"project_id":"example-other-9","zone":"us-central1-a","disk":"data-disk-a","name":"snap-a"}`,
		},
	}
	for id, list := range args {
		for _, a := range list {
			e := writeEnvironment(t, []string{"projects/my-project-123"})
			_, err := e.invokeConfirmed(id, a)
			if err == nil {
				t.Errorf("%s accepted %s", id, a)
			} else if strings.Contains(err.Error(), "canary") {
				t.Errorf("%s refusal repeats the value: %v", id, err)
			}
			noIO(t, e, id+" "+a)
		}
	}
}

func TestDiskWritesRefuseInvalidArgumentsInTheHandlerToo(t *testing.T) {
	// The schema already refuses these; the handlers check again before any secret is read.
	insert := []diskInsertArguments{
		{ProjectID: "my-project-123", Zone: "us-central1-a", Name: "d", SourceImage: "img-a", SourceImageProject: "other-project-123"},
		{ProjectID: "my-project-123", Zone: "us-central1-a", Name: "d", SourceImage: "https://x/images/img-a"},
		{ProjectID: "my-project-123", Zone: "us-central1-a", Name: "d", KmsKeyServiceAccount: "encrypter-a@example-project-123.iam.gserviceaccount.com"},
		{ProjectID: "my-project-123", Zone: "us-central1-a", Name: "d", Licenses: []diskLicense{{License: "lic-a", Project: "other-project-123"}}},
		{ProjectID: "my-project-123", Zone: "us-central1-a", Name: "d", GuestOsFeatures: []string{"FEATURE_TYPE_UNSPECIFIED"}},
		{ProjectID: "my-project-123", Zone: "zone", Name: "d", ResourcePolicies: []string{"daily"}},
	}
	for _, a := range insert {
		if a.check() == nil {
			t.Errorf("accepted %+v", a)
		}
	}
	snapshot := diskSnapshotArguments{ProjectID: "my-project-123", Zone: "us-central1-a", Disk: "d", Name: "s",
		Labels: map[string]string{"-bad": ""}}
	if snapshot.check() == nil {
		t.Errorf("accepted %+v", snapshot)
	}
}

func TestDisksResizeReadsTheDiskAndGrowsIt(t *testing.T) {
	e := writeEnvironment(t, []string{"projects/my-project-123"})
	diskWriteRoutes(e, "100", okReply(zoneOperationBody("RUNNING", "")))
	out, err := e.invokeConfirmed(disksResizeID, diskResizeArgs+`,"size_gb":200,"request_id":"`+deleteRequestID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].method != "GET" || api[1].method != "POST" ||
		api[1].url != "https://compute.googleapis.com"+diskWritePath+"/resize?requestId="+deleteRequestID {
		t.Fatalf("requests = %+v", api)
	}
	if got := sentBody(t, api[1]); !reflect.DeepEqual(got, map[string]any{"sizeGb": "200"}) {
		t.Errorf("body = %v", got)
	}
	if got := decode(t, out); got["scope"] != "zone" || got["zone"] != "us-central1-a" {
		t.Errorf("result = %s", out)
	}
}

func TestDisksResizeNeverShrinksAndSendsNoChange(t *testing.T) {
	for _, size := range []string{"100", "150"} {
		e := writeEnvironment(t, nil)
		diskWriteRoutes(e, "150", okReply(zoneOperationBody("RUNNING", "")))
		_, err := e.invokeConfirmed(disksResizeID, diskResizeArgs+`,"size_gb":`+size+`}`)
		var invalid *provider.InvalidRequestError
		if err == nil || !asInvalid(err, &invalid) || !strings.Contains(err.Error(), "shrunk") {
			t.Errorf("size %s: err = %v", size, err)
		}
		if got := methodsOf(e); !slices.Equal(got, []string{"GET"}) {
			t.Errorf("size %s: methods = %v, want only the read", size, got)
		}
	}
	// A disk answer without a usable size or for another disk leaves nothing to compare.
	for _, body := range []string{strings.Replace(diskBody("data-disk-a"), `"sizeGb":"100",`, "", 1), diskBody("disk-other")} {
		e := writeEnvironment(t, nil)
		e.fake.hook = func(_ int, req *http.Request) *reply {
			if req.Method == http.MethodGet {
				return &reply{status: 200, body: body}
			}
			return &reply{status: 200, body: zoneOperationBody("RUNNING", "")}
		}
		if _, err := e.invokeConfirmed(disksResizeID, diskResizeArgs+`,"size_gb":200}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("class = %q, err = %v", classOf(err), err)
		}
		if got := methodsOf(e); !slices.Equal(got, []string{"GET"}) {
			t.Errorf("methods = %v", got)
		}
	}
}

func TestDisksCreateSnapshotSendsStorageLocationsAndGuestFlush(t *testing.T) {
	e := writeEnvironment(t, []string{"projects/my-project-123"})
	diskWriteRoutes(e, "100", okReply(zoneOperationBody("RUNNING", "")))
	out, err := e.invokeConfirmed(disksCreateSnapshotID, diskSnapshotArgs+`,"description":"nightly",`+
		`"labels":{"env":"test"},"storage_locations":["europe-west1"],"snapshot_type":"ARCHIVE","chain_name":"chain-a",`+
		`"kms_key_name":"`+diskWriteKMSName+`","guest_flush":true,"request_id":"`+deleteRequestID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != "POST" {
		t.Fatalf("requests = %+v", api)
	}
	u, _ := url.Parse(api[0].url)
	if u.Path != diskWritePath+"/createSnapshot" || u.Query().Get("requestId") != deleteRequestID ||
		u.Query().Get("guestFlush") != "true" || len(u.Query()) != 2 {
		t.Errorf("url = %s", api[0].url)
	}
	want := map[string]any{"name": "snap-a", "description": "nightly", "labels": map[string]any{"env": "test"},
		"storageLocations": []any{"europe-west1"}, "snapshotType": "ARCHIVE", "chainName": "chain-a",
		"snapshotEncryptionKey": map[string]any{"kmsKeyName": diskWriteKMSName}}
	if got := sentBody(t, api[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
	if got := decode(t, out); got["scope"] != "zone" || got["request_id"] != deleteRequestID {
		t.Errorf("result = %s", out)
	}
	// Without guest_flush the query holds requestId alone.
	e = writeEnvironment(t, nil)
	diskWriteRoutes(e, "100", okReply(zoneOperationBody("RUNNING", "")))
	if _, err := e.invokeConfirmed(disksCreateSnapshotID, diskSnapshotArgs+`}`); err != nil {
		t.Fatal(err)
	}
	if u, _ := url.Parse(e.fake.api()[0].url); len(u.Query()) != 1 || e.fake.api()[0].body != `{"name":"snap-a"}` {
		t.Errorf("request = %+v", e.fake.api()[0])
	}
}

func TestDiskWritesRepeatTheSameRequestIDUnchanged(t *testing.T) {
	e := writeEnvironment(t, nil)
	diskWriteRoutes(e, "100", okReply(zoneOperationBody("DONE", "")))
	for i := 0; i < 2; i++ {
		out, err := e.invokeConfirmed(disksInsertID, diskInsertArgs+`,"request_id":"`+deleteRequestID+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		if got := decode(t, out); got["request_id"] != deleteRequestID {
			t.Errorf("result = %s", out)
		}
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].url != api[1].url || api[0].body != api[1].body {
		t.Errorf("requests = %+v", api)
	}
}

func TestDiskWritesReportAnOperationDoneWithAnErrorAndAnUnclearOutcome(t *testing.T) {
	e := writeEnvironment(t, nil)
	diskWriteRoutes(e, "100", okReply(zoneOperationBody("DONE", `,"httpErrorStatusCode":400,`+
		`"error":{"errors":[{"code":"QUOTA_EXCEEDED","message":"`+detailCanary+`"}]}`)))
	_, err := e.invokeConfirmed(disksInsertID, diskInsertArgs+`}`)
	if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
	e = writeEnvironment(t, nil)
	diskWriteRoutes(e, "100", reply{status: 503, body: errorObject(503, "UNAVAILABLE", "")})
	if _, err := e.invokeConfirmed(disksCreateSnapshotID, diskSnapshotArgs+`}`); err == nil ||
		!strings.Contains(err.Error(), "outcome") {
		t.Errorf("err = %v", err)
	}
	if got := methodsOf(e); !slices.Equal(got, []string{"POST"}) {
		t.Errorf("methods = %v", got)
	}
	// An operation of another zone is no answer to this change.
	e = writeEnvironment(t, nil)
	diskWriteRoutes(e, "100", okReply(strings.Replace(zoneOperationBody("RUNNING", ""), "us-central1-a", "us-east1-b", -1)))
	if _, err := e.invokeConfirmed(disksInsertID, diskInsertArgs+`}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("other zone: class = %q, err = %v", classOf(err), err)
	}
}

func TestDiskWritesDropKeyMaterialFromTheOperation(t *testing.T) {
	e := writeEnvironment(t, nil)
	diskWriteRoutes(e, "100", okReply(zoneOperationBody("RUNNING", `,"warnings":[{"code":"X","data":[`+
		`{"key":"k","rawKey":"`+diskRawCanary+`","RSAEncryptedKey":"`+diskRSACanary+`"}]}]`)))
	out, err := e.invokeConfirmed(disksInsertID, diskInsertArgs+`}`)
	if err != nil {
		t.Fatal(err)
	}
	absentDiskKeys(t, "insert", out)
	if !strings.Contains(out, `"warnings"`) {
		t.Errorf("result = %s", out)
	}
}

func TestDiskWritesNeedConfirmationAndNameTheCostInIt(t *testing.T) {
	args := map[string]string{disksInsertID: diskInsertArgs + `}`, disksResizeID: diskResizeArgs + `,"size_gb":200}`,
		disksCreateSnapshotID: diskSnapshotArgs + `}`}
	for _, id := range diskWriteTools {
		e := writeEnvironment(t, nil)
		if _, err := e.invoke(id, args[id]); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		wantEffect := "create"
		if id == disksResizeID {
			wantEffect = "update"
		}
		if d.Risk.Confirmation != "required" || string(d.Risk.Effect) != wantEffect || !d.Risk.OpenWorld ||
			string(d.Risk.Idempotency) != "idempotent" || d.RequiresToolAllowList {
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
	insert := e.reg.ConfirmationSummary(disksInsertID)(json.RawMessage(diskInsertArgs + `,"size_gb":500,` +
		`"type":"pd-ssd","source_image_family":"debian-12","source_image_project":"debian-cloud"}`))
	for _, part := range []string{"my-project-123", "us-central1-a", "data-disk-a", "500 GB", "pd-ssd", "debian-12",
		"debian-cloud", "billed"} {
		if !strings.Contains(insert, part) {
			t.Errorf("insert summary %q lacks %q", insert, part)
		}
	}
	empty := e.reg.ConfirmationSummary(disksInsertID)(json.RawMessage(diskInsertArgs + `}`))
	if !strings.Contains(empty, "default") || !strings.Contains(empty, "empty") {
		t.Errorf("empty summary = %q", empty)
	}
	resize := e.reg.ConfirmationSummary(disksResizeID)(json.RawMessage(diskResizeArgs + `,"size_gb":200}`))
	for _, part := range []string{"my-project-123", "us-central1-a", "data-disk-a", "200 GB", "cannot be shrunk", "billed"} {
		if !strings.Contains(resize, part) {
			t.Errorf("resize summary %q lacks %q", resize, part)
		}
	}
	snapshot := e.reg.ConfirmationSummary(disksCreateSnapshotID)(json.RawMessage(diskSnapshotArgs +
		`,"storage_locations":["eu"],"snapshot_type":"ARCHIVE"}`))
	for _, part := range []string{"snap-a", "data-disk-a", "us-central1-a", "my-project-123", "ARCHIVE", "eu", "billed"} {
		if !strings.Contains(snapshot, part) {
			t.Errorf("snapshot summary %q lacks %q", snapshot, part)
		}
	}
}

func TestDiskWritesRunWithTheProfileAlone(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	var tools []string
	for _, profile := range metadata.Profiles {
		if profile.ID == "compute-disks" {
			tools = profile.Tools
		}
	}
	if len(tools) == 0 {
		t.Fatal("no profile compute-disks")
	}
	e = revokeEnvironment(t, []string{"projects/my-project-123"}, tools)
	diskWriteRoutes(e, "100", okReply(zoneOperationBody("RUNNING", "")))
	for id, args := range map[string]string{disksInsertID: diskInsertArgs + `}`,
		disksResizeID: diskResizeArgs + `,"size_gb":200}`, disksCreateSnapshotID: diskSnapshotArgs + `}`} {
		if _, err := e.invokeConfirmed(id, args); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

// TestNoToolSharesDisksOrSnapshots holds decision 4 for the disk writes: no disk or snapshot tool changes an IAM
// policy, attaches a disk, or shares a disk or snapshot.
func TestNoToolSharesDisksOrSnapshots(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	for _, tool := range metadata.Tools {
		if !strings.Contains(tool.ID, ".disks.") && !strings.Contains(tool.ID, ".snapshots.") {
			continue
		}
		for _, verb := range []string{"iam", "attach", "share", "public"} {
			if strings.Contains(tool.ID, verb) {
				t.Errorf("tool %s may share a disk or snapshot", tool.ID)
			}
		}
	}
}
