package googlecloud

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
)

const (
	insertInstancesPath = "/compute/v1/projects/my-project-123/zones/us-central1-a/instances"
	instanceBase        = `"project_id":"my-project-123","zone":"us-central1-a","name":"my-vm"`
	bootDisk            = `"disks":[{"boot":true,"initialize_params":{"disk_size_gb":20,"source_image":"projects/debian-cloud/global/images/family/debian-12"}}]`
	privateNetwork      = `"network_interfaces":[{"network":"default"}]`
	insertArgs          = `{` + instanceBase + `,"machine_type":"e2-medium",` + bootDisk + `,` + privateNetwork
)

var insertTargets = []string{"projects/my-project-123"}

// createEnvironment is an environment whose connection may read and create, or offers the tools listed.
func createEnvironment(t *testing.T, targets, tools []string) *environment {
	t.Helper()
	e := newEnvironment(t, targets)
	connection := config.Connection{Service: "cloud", Credential: credential, Targets: targets, Tools: tools}
	if tools == nil {
		connection.Effects = []config.Effect{config.EffectRead, config.EffectCreate, config.EffectDelete}
	}
	e.cfg.Connections["work"] = connection
	if err := e.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	e.core = application.New(e.reg, e.cfg, e.secrets(), e.red)
	return e
}

func zonalOperationBody(kind, status, extra string) string {
	return `{"kind":"compute#operation","id":"9100000000000000002","name":"` + operationName + `",` +
		`"operationType":"` + kind + `","status":"` + status + `","user":"person@example.com",` +
		`"zone":"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-central1-a"` + extra + `}`
}

func insertAnswers(e *environment, answer reply) {
	e.fake.routes[insertInstancesPath] = answer
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
}

func bodyOf(t *testing.T, r seen) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(r.body), &body); err != nil {
		t.Fatalf("body %q: %v", r.body, err)
	}
	return body
}

func TestInstancesInsertBuildsTheBodyAndReturnsTheZonalOperation(t *testing.T) {
	e := createEnvironment(t, insertTargets, nil)
	insertAnswers(e, okReply(zonalOperationBody("insert", "RUNNING", "")))
	out, err := e.invokeConfirmed(instancesInsertID, insertArgs+`,"request_id":"`+deleteRequestID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != http.MethodPost ||
		api[0].url != "https://compute.googleapis.com"+insertInstancesPath+"?requestId="+deleteRequestID {
		t.Fatalf("requests = %+v", api)
	}
	body := bodyOf(t, api[0])
	prefix := "projects/my-project-123/"
	if body["name"] != "my-vm" || body["machineType"] != prefix+"zones/us-central1-a/machineTypes/e2-medium" {
		t.Errorf("body = %s", api[0].body)
	}
	disk := body["disks"].([]any)[0].(map[string]any)
	params := disk["initializeParams"].(map[string]any)
	if disk["boot"] != true || params["diskSizeGb"] != "20" ||
		params["sourceImage"] != "projects/debian-cloud/global/images/family/debian-12" {
		t.Errorf("disk = %v", disk)
	}
	nic := body["networkInterfaces"].([]any)[0].(map[string]any)
	if nic["network"] != prefix+"global/networks/default" || nic["accessConfigs"] != nil || nic["ipv6AccessConfigs"] != nil {
		t.Errorf("interface = %v", nic)
	}
	for _, name := range []string{"accessConfigs", "ipv6AccessConfigs", "rawKey"} {
		if strings.Contains(api[0].body, name) {
			t.Errorf("body holds %s: %s", name, api[0].body)
		}
	}
	got := decode(t, out)
	operation, _ := got["operation"].(map[string]any)
	if operation["status"] != "RUNNING" || got["scope"] != "zone" || got["zone"] != "us-central1-a" ||
		got["request_id"] != deleteRequestID {
		t.Errorf("result = %s", out)
	}
}

func TestInstancesInsertBuildsPathsOfTheProjectAndZone(t *testing.T) {
	e := createEnvironment(t, insertTargets, nil)
	insertAnswers(e, okReply(zonalOperationBody("insert", "PENDING", "")))
	args := `{` + instanceBase + `,"machine_type":"zones/us-central1-a/machineTypes/n2-custom-4-8192",` +
		`"disks":[{"boot":true,"auto_delete":true,"initialize_params":{"disk_type":"pd-ssd","source_image":"debian-12-image"}},` +
		`{"source":"projects/my-project-123/zones/us-central1-a/disks/data","mode":"READ_ONLY"}],` +
		`"network_interfaces":[{"network":"global/networks/vpc","subnetwork":"sub","network_ip":"10.0.0.5"}],` +
		`"guest_accelerators":[{"type":"nvidia-tesla-t4","count":1}],` +
		`"metadata":[{"key":"block-project-ssh-keys","value":"TRUE"},{"key":"startup-script","value":"echo hi"}],` +
		`"labels":{"env":"test"},"tags":["web"],"scheduling":{"provisioning_model":"SPOT","instance_termination_action":"STOP"},` +
		`"deletion_protection":true,"shielded_instance_config":{"enable_secure_boot":true}}`
	if _, err := e.invokeConfirmed(instancesInsertID, args); err != nil {
		t.Fatal(err)
	}
	body := bodyOf(t, e.fake.api()[0])
	prefix := "projects/my-project-123/"
	disks := body["disks"].([]any)
	first := disks[0].(map[string]any)["initializeParams"].(map[string]any)
	if first["diskType"] != prefix+"zones/us-central1-a/diskTypes/pd-ssd" ||
		first["sourceImage"] != prefix+"global/images/debian-12-image" ||
		disks[1].(map[string]any)["source"] != prefix+"zones/us-central1-a/disks/data" {
		t.Errorf("disks = %v", disks)
	}
	nic := body["networkInterfaces"].([]any)[0].(map[string]any)
	if nic["subnetwork"] != prefix+"regions/us-central1/subnetworks/sub" || nic["network"] != prefix+"global/networks/vpc" ||
		nic["networkIP"] != "10.0.0.5" {
		t.Errorf("interface = %v", nic)
	}
	if got := body["guestAccelerators"].([]any)[0].(map[string]any)["acceleratorType"]; got != prefix+"zones/us-central1-a/acceleratorTypes/nvidia-tesla-t4" {
		t.Errorf("accelerator = %v", got)
	}
	if scheduling := body["scheduling"].(map[string]any); scheduling["provisioningModel"] != "SPOT" ||
		body["deletionProtection"] != true || body["labels"].(map[string]any)["env"] != "test" ||
		body["tags"].(map[string]any)["items"].([]any)[0] != "web" {
		t.Errorf("body = %v", body)
	}
	if items := body["metadata"].(map[string]any)["items"].([]any); len(items) != 2 {
		t.Errorf("metadata = %v", items)
	}
}

func TestInstancesInsertRefusesBeforeAnyIO(t *testing.T) {
	for name, args := range map[string]string{
		"access config":       `{` + instanceBase + `,"machine_type":"e2-medium",` + bootDisk + `,"network_interfaces":[{"network":"default","accessConfigs":[{"type":"ONE_TO_ONE_NAT"}]}]}`,
		"ipv6 access config":  `{` + instanceBase + `,"machine_type":"e2-medium",` + bootDisk + `,"network_interfaces":[{"network":"default","ipv6AccessConfigs":[{}]}]}`,
		"snake access config": `{` + instanceBase + `,"machine_type":"e2-medium",` + bootDisk + `,"network_interfaces":[{"network":"default","access_configs":[{}]}]}`,
		"ssh-keys":            insertArgs + `,"metadata":[{"key":"ssh-keys","value":"u:ssh-rsa AAAA"}]}`,
		"sshKeys":             insertArgs + `,"metadata":[{"key":"sshKeys","value":"u:ssh-rsa AAAA"}]}`,
		"other case":          insertArgs + `,"metadata":[{"key":"SSH-Keys","value":"x"}]}`,
		"windows-keys":        insertArgs + `,"metadata":[{"key":"windows-keys","value":"{}"}]}`,
		"duplicate key":       insertArgs + `,"metadata":[{"key":"a","value":"1"},{"key":"a","value":"2"}]}`,
		"rawKey":              `{` + instanceBase + `,"machine_type":"e2-medium","disks":[{"boot":true,"diskEncryptionKey":{"rawKey":"c2VjcmV0"}}],` + privateNetwork + `}`,
		"rsaEncryptedKey":     `{` + instanceBase + `,"machine_type":"e2-medium","disks":[{"boot":true,"diskEncryptionKey":{"rsaEncryptedKey":"c2VjcmV0"}}],` + privateNetwork + `}`,
		"raw key argument":    `{` + instanceBase + `,"machine_type":"e2-medium","disks":[{"boot":true,"raw_key":"c2VjcmV0"}],` + privateNetwork + `}`,
		"foreign image":       `{` + instanceBase + `,"machine_type":"e2-medium","disks":[{"boot":true,"initialize_params":{"source_image":"projects/other-project-9/global/images/evil"}}],` + privateNetwork + `}`,
		"foreign family url":  `{` + instanceBase + `,"machine_type":"e2-medium","disks":[{"boot":true,"initialize_params":{"source_image":"https://www.googleapis.com/compute/v1/projects/debian-cloud/global/images/family/debian-12"}}],` + privateNetwork + `}`,
		"foreign network":     `{` + instanceBase + `,"machine_type":"e2-medium",` + bootDisk + `,"network_interfaces":[{"network":"projects/other-project-9/global/networks/x"}]}`,
		"other zone type":     `{` + instanceBase + `,"machine_type":"zones/europe-west1-b/machineTypes/e2-medium",` + bootDisk + `,` + privateNetwork + `}`,
		"other region subnet": `{` + instanceBase + `,"machine_type":"e2-medium",` + bootDisk + `,"network_interfaces":[{"subnetwork":"regions/europe-west1/subnetworks/s"}]}`,
		"service account url": insertArgs + `,"service_accounts":[{"email":"a@example.com","scopes":[]}]}`,
		"scope alias":         insertArgs + `,"service_accounts":[{"scopes":["cloud-platform"]}]}`,
		"unknown argument":    insertArgs + `,"body":{}}`,
		"no machine type":     `{` + instanceBase + `,` + bootDisk + `,` + privateNetwork + `}`,
		"no disk":             `{` + instanceBase + `,"machine_type":"e2-medium",` + privateNetwork + `}`,
		"template alone":      `{` + instanceBase + `,"source_instance_template":"tpl"}`,
		"template no account": `{` + instanceBase + `,"source_instance_template":"tpl",` + privateNetwork + `}`,
		"template and image":  `{` + instanceBase + `,"source_instance_template":"tpl","source_machine_image":"img",` + privateNetwork + `,"service_accounts":[]}`,
		"bad request id":      insertArgs + `,"request_id":"nope"}`,
		"zero request id":     insertArgs + `,"request_id":"00000000-0000-0000-0000-000000000000"}`,
		"bad name":            `{"project_id":"my-project-123","zone":"us-central1-a","name":"My_VM","machine_type":"e2-medium",` + bootDisk + `}`,
		"project path":        `{"project_id":"projects/my-project-123","zone":"us-central1-a","name":"my-vm","machine_type":"e2-medium",` + bootDisk + `}`,
		"other project":       `{"project_id":"example-other-9","zone":"us-central1-a","name":"my-vm","machine_type":"e2-medium",` + bootDisk + `}`,
	} {
		e := createEnvironment(t, insertTargets, nil)
		if _, err := e.invokeConfirmed(instancesInsertID, args); err == nil {
			t.Errorf("%s: accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestInstancesInsertRefusalsDoNotNameTheForeignValue(t *testing.T) {
	for _, args := range []string{
		`{` + instanceBase + `,"machine_type":"e2-medium","disks":[{"boot":true,"initialize_params":{"source_image":"projects/other-project-9/global/images/evil"}}],` + privateNetwork + `}`,
		insertArgs + `,"metadata":[{"key":"ssh-keys","value":"SECRETKEYVALUE"}]}`,
	} {
		e := createEnvironment(t, insertTargets, nil)
		_, err := e.invokeConfirmed(instancesInsertID, args)
		if err == nil || strings.Contains(err.Error(), "other-project-9") || strings.Contains(err.Error(), "SECRETKEYVALUE") {
			t.Errorf("err = %v", err)
		}
	}
}

func TestDecodeInsertArgumentsRefusesForbiddenMembersAtAnyDepth(t *testing.T) {
	// The schema already refuses these; the handler does as well, should a nested field get past it.
	for _, raw := range []string{
		`{"network_interfaces":[{"accessConfigs":[]}]}`,
		`{"x":{"y":[{"ipv6AccessConfigs":{}}]}}`,
		`{"disks":[{"nested":{"rawKey":"a"}}]}`,
		`{"disks":[{"rsa-encrypted-key":"a"}]}`,
		`{"ACCESS_CONFIGS":1}`,
	} {
		if _, err := decodeInsertArguments(json.RawMessage(raw)); err == nil {
			t.Errorf("%s accepted", raw)
		}
	}
	if _, err := decodeInsertArguments(json.RawMessage(`{"name":"a"}`)); err != nil {
		t.Errorf("plain arguments refused: %v", err)
	}
}

// insertAccountDomain is the service account domain of the project of the test calls.
const insertAccountDomain = "my-project-123.iam.gserviceaccount.com"

func TestInstancesInsertServiceAccountsOfTheProjectOnly(t *testing.T) {
	own := `{"email":"deploy@` + insertAccountDomain + `","scopes":["https://www.googleapis.com/auth/devstorage.read_only"]}`
	standard := `{"email":"415104041262-compute@` + computeDomain + `","scopes":[]}`
	for name, c := range map[string]struct {
		account string
		ok      bool
	}{
		"own project":        {own, true},
		"default by number":  {standard, true},
		"no email":           {`{"scopes":[]}`, true},
		"other project":      {`{"email":"deploy@` + "other-project-9.iam.gserviceaccount.com" + `","scopes":[]}`, false},
		"other number":       {`{"email":"999999999999-compute@` + computeDomain + `","scopes":[]}`, false},
		"appspot":            {`{"email":"my-project-123@` + "appspot.gserviceaccount.com" + `","scopes":[]}`, false},
		"upper case account": {`{"email":"Deploy@` + insertAccountDomain + `","scopes":[]}`, false},
	} {
		e := createEnvironment(t, insertTargets, nil)
		insertAnswers(e, okReply(zonalOperationBody("insert", "RUNNING", "")))
		_, err := e.invokeConfirmed(instancesInsertID, insertArgs+`,"service_accounts":[`+c.account+`]}`)
		posts := 0
		for _, r := range e.fake.api() {
			if r.method == http.MethodPost {
				posts++
			}
		}
		if (err == nil) != c.ok || (posts == 1) != c.ok {
			t.Errorf("%s: err = %v, posts = %d", name, err, posts)
		}
	}
}

func TestInstancesInsertTakesTheProjectNumberFromTheProjectNotFromAnArgument(t *testing.T) {
	e := createEnvironment(t, insertTargets, nil)
	insertAnswers(e, okReply(zonalOperationBody("insert", "RUNNING", "")))
	_, err := e.invokeConfirmed(instancesInsertID,
		insertArgs+`,"service_accounts":[{"email":"415104041262-compute@`+computeDomain+`","scopes":[]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].method != http.MethodGet || !strings.HasSuffix(api[0].url, "/v3/projects/my-project-123") ||
		api[1].method != http.MethodPost {
		t.Fatalf("requests = %+v", api)
	}
}

func TestInstancesInsertAcceptsOnlyImageFamiliesOfOtherProjects(t *testing.T) {
	for image, want := range map[string]string{
		"projects/debian-cloud/global/images/family/debian-12": "projects/debian-cloud/global/images/family/debian-12",
		"family/web":          "projects/my-project-123/global/images/family/web",
		"global/images/web-1": "projects/my-project-123/global/images/web-1",
		"projects/my-project-123/global/images/web-1": "projects/my-project-123/global/images/web-1",
	} {
		got, err := (insertTarget{project: "my-project-123"}).image(image)
		if err != nil || got != want {
			t.Errorf("image(%q) = %q, %v", image, got, err)
		}
	}
	for _, image := range []string{"projects/debian-cloud/global/images/debian-12-v1", "projects/12345/global/images/family/x",
		"projects/debian-cloud/global/images/family", "https://x/y", "../x", "global/images", "a/b"} {
		if got, err := (insertTarget{project: "my-project-123"}).image(image); err == nil {
			t.Errorf("image(%q) = %q", image, got)
		}
	}
}

func TestInstancesInsertWithATemplateNeedsTheInterfacesAndAccountsItself(t *testing.T) {
	e := createEnvironment(t, insertTargets, nil)
	insertAnswers(e, okReply(zonalOperationBody("insert", "RUNNING", "")))
	_, err := e.invokeConfirmed(instancesInsertID, `{`+instanceBase+`,"source_instance_template":"regions/us-central1/instanceTemplates/tpl",`+
		privateNetwork+`,"service_accounts":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	body := bodyOf(t, e.fake.api()[0])
	if body["sourceInstanceTemplate"] != "projects/my-project-123/regions/us-central1/instanceTemplates/tpl" ||
		len(body["serviceAccounts"].([]any)) != 0 || body["machineType"] != nil {
		t.Errorf("body = %v", body)
	}
}

func TestInstancesInsertRepeatsWithTheSameRequestID(t *testing.T) {
	e := createEnvironment(t, insertTargets, nil)
	insertAnswers(e, okReply(zonalOperationBody("insert", "RUNNING", "")))
	for i := 0; i < 2; i++ {
		if _, err := e.invokeConfirmed(instancesInsertID, insertArgs+`,"request_id":"`+deleteRequestID+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	api := e.fake.api()
	if len(api) != 2 {
		t.Fatalf("requests = %+v", api)
	}
	for _, r := range api {
		u, _ := url.Parse(r.url)
		if u.Query().Get("requestId") != deleteRequestID || len(u.Query()) != 1 {
			t.Errorf("request = %+v", r)
		}
	}
	e = createEnvironment(t, insertTargets, nil)
	insertAnswers(e, okReply(zonalOperationBody("insert", "RUNNING", "")))
	out, err := e.invokeConfirmed(instancesInsertID, insertArgs+`}`)
	if err != nil {
		t.Fatal(err)
	}
	sent, _ := url.Parse(e.fake.api()[0].url)
	if id, _ := decode(t, out)["request_id"].(string); id == "" || sent.Query().Get("requestId") != id {
		t.Errorf("request_id = %q, sent %q", id, sent.Query().Get("requestId"))
	}
}

func TestInstancesInsertReportsAnOperationThatEndedWithAnError(t *testing.T) {
	e := createEnvironment(t, insertTargets, nil)
	insertAnswers(e, okReply(zonalOperationBody("insert", "DONE", `,"httpErrorStatusCode":403,`+
		`"error":{"errors":[{"code":"QUOTA_EXCEEDED","message":"Quota 'CPUS' exceeded"}]}`)))
	_, err := e.invokeConfirmed(instancesInsertID, insertArgs+`}`)
	if err == nil || !strings.Contains(err.Error(), "operation ended with an error") {
		t.Fatalf("err = %v", err)
	}
	if len(e.fake.api()) != 1 {
		t.Errorf("requests = %d, want one without retry", len(e.fake.api()))
	}
}

func TestInstancesInsertNeverRepeatsAFailedRequest(t *testing.T) {
	for _, status := range []int{500, 503} {
		e := createEnvironment(t, insertTargets, nil)
		insertAnswers(e, reply{status: status, body: errorObject(status, "UNAVAILABLE", "")})
		_, err := e.invokeConfirmed(instancesInsertID, insertArgs+`}`)
		if err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Errorf("status %d: err = %v", status, err)
		}
		if len(e.fake.api()) != 1 {
			t.Errorf("status %d: requests = %d", status, len(e.fake.api()))
		}
	}
}

func TestInstancesInsertNeedsConfirmationAndTheTargets(t *testing.T) {
	e := createEnvironment(t, insertTargets, nil)
	if _, err := e.invoke(instancesInsertID, insertArgs+`}`); err == nil {
		t.Error("ran without confirmation")
	}
	noIO(t, e, "no confirmation")
	d, _, _ := e.reg.Lookup(instancesInsertID)
	if d.Risk.Confirmation != "required" || string(d.Risk.Effect) != "create" || !d.Risk.OpenWorld ||
		d.RequiresToolAllowList || e.reg.ConfirmationSummary(instancesInsertID) == nil {
		t.Errorf("risk = %+v", d.Risk)
	}
	if !strings.Contains(d.Description, "start-up script") || !strings.Contains(d.Description, "does not check") {
		t.Error("the description does not name the unchecked start-up script")
	}
}

func TestInstancesInsertSummaryNamesTheCostDrivers(t *testing.T) {
	e := newEnvironment(t, nil)
	s := e.reg.ConfirmationSummary(instancesInsertID)(json.RawMessage(
		`{` + instanceBase + `,"machine_type":"a2-highgpu-1g","disks":[{"boot":true,"initialize_params":{"disk_size_gb":100}},` +
			`{"initialize_params":{"disk_size_gb":500}}],"guest_accelerators":[{"type":"nvidia-tesla-a100","count":2}],` +
			`"scheduling":{"provisioning_model":"SPOT"}}`))
	for _, part := range []string{"my-project-123", "us-central1-a", "my-vm", "a2-highgpu-1g", "100 GB", "500 GB",
		"2 x nvidia-tesla-a100", "SPOT"} {
		if !strings.Contains(s, part) {
			t.Errorf("summary %q lacks %q", s, part)
		}
	}
	if s := e.reg.ConfirmationSummary(instancesInsertID)(json.RawMessage(`{` + instanceBase + `}`)); !strings.Contains(s, "STANDARD") {
		t.Errorf("summary %q lacks the default model", s)
	}
}

func TestInstancesInsertRunsWithTheProfileAlone(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	var tools []string
	for _, profile := range metadata.Profiles {
		if profile.ID == "compute-instances-create" {
			tools = profile.Tools
		}
	}
	if !slices.Equal(tools, []string{projectsGetID, instancesInsertID}) {
		t.Fatalf("profile tools = %v", tools)
	}
	e = createEnvironment(t, []string{"folders/2001"}, tools)
	insertAnswers(e, okReply(zonalOperationBody("insert", "RUNNING", "")))
	e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"organizations/1"}`)
	if _, err := e.invokeConfirmed(instancesInsertID, insertArgs+`}`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.invokeConfirmed(instancesDeleteID, `{"project_id":"my-project-123","zone":"us-central1-a","instance":"my-vm"}`); err == nil {
		t.Error("instances.delete ran with the profile alone")
	}
}

func TestInstancesInsertIsNotOfferedToABillingOnlyConnection(t *testing.T) {
	e := createEnvironment(t, []string{"billingAccounts/012345-6789AB-CDEF01"}, nil)
	for _, id := range []string{instancesInsertID, instancesDeleteID} {
		if _, err := e.invokeConfirmed(id, `{`+instanceBase+`}`); err == nil {
			t.Errorf("%s ran on a billing-only connection", id)
		}
	}
	noIO(t, e, "billing only")
}

// --- instances.delete ---

const instanceDeleteArgs = `{"project_id":"my-project-123","zone":"us-central1-a","instance":"my-vm"`

func TestInstancesDeleteIsOnlyReachableByName(t *testing.T) {
	for name, tools := range map[string][]string{
		"no tools list":    nil,
		"other tool named": {instancesInsertID},
		"namespace prefix": {"google.cloud.instances.*"},
	} {
		e := createEnvironment(t, insertTargets, tools)
		if _, err := e.invokeConfirmed(instancesDeleteID, instanceDeleteArgs+`}`); err == nil {
			t.Errorf("%s: instances.delete was reachable", name)
		}
		noIO(t, e, name)
	}
	e := createEnvironment(t, insertTargets, []string{instancesDeleteID})
	e.fake.routes[insertInstancesPath+"/my-vm"] = okReply(zonalOperationBody("delete", "RUNNING", ""))
	out, err := e.invokeConfirmed(instancesDeleteID, instanceDeleteArgs+`,"request_id":"`+deleteRequestID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != http.MethodDelete || api[0].body != "" ||
		api[0].url != "https://compute.googleapis.com"+insertInstancesPath+"/my-vm?requestId="+deleteRequestID {
		t.Fatalf("requests = %+v", api)
	}
	if got := decode(t, out); got["scope"] != "zone" || got["zone"] != "us-central1-a" || got["request_id"] != deleteRequestID {
		t.Errorf("result = %s", out)
	}
}

func TestInstancesDeleteIsInNoProfile(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		if slices.Contains(profile.Tools, instancesDeleteID) {
			t.Errorf("profile %s holds %s", profile.ID, instancesDeleteID)
		}
	}
	d, _, _ := e.reg.Lookup(instancesDeleteID)
	if !d.RequiresToolAllowList || string(d.Risk.Effect) != "delete" || d.Risk.Confirmation != "required" ||
		e.reg.ConfirmationSummary(instancesDeleteID) == nil {
		t.Errorf("descriptor = %+v", d.Risk)
	}
}

func TestInstancesDeleteRefusesBeforeAnyIOAndWithoutConfirmation(t *testing.T) {
	for _, args := range []string{
		instanceDeleteArgs + `,"request_id":"nope"}`,
		`{"project_id":"my-project-123","zone":"us-central1-a","instance":"../x"}`,
		`{"project_id":"my-project-123","zone":"us-central1/a","instance":"my-vm"}`,
		`{"project_id":"example-other-9","zone":"us-central1-a","instance":"my-vm"}`,
		instanceDeleteArgs + `,"path":"/x"}`,
	} {
		e := createEnvironment(t, insertTargets, []string{instancesDeleteID})
		if _, err := e.invokeConfirmed(instancesDeleteID, args); err == nil {
			t.Errorf("accepted %s", args)
		}
		noIO(t, e, args)
	}
	e := createEnvironment(t, insertTargets, []string{instancesDeleteID})
	if _, err := e.invoke(instancesDeleteID, instanceDeleteArgs+`}`); err == nil {
		t.Error("ran without confirmation")
	}
	noIO(t, e, "no confirmation")
}

func TestInstancesDeleteReportsDeletionProtectionAsGoogleSendsIt(t *testing.T) {
	e := createEnvironment(t, insertTargets, []string{instancesDeleteID})
	e.fake.routes[insertInstancesPath+"/my-vm"] = reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")}
	_, err := e.invokeConfirmed(instancesDeleteID, instanceDeleteArgs+`}`)
	if err == nil || strings.Contains(err.Error(), detailCanary) {
		t.Fatalf("err = %v", err)
	}
	if len(e.fake.api()) != 1 {
		t.Errorf("requests = %d", len(e.fake.api()))
	}
}

func TestInstancesDeleteSummaryNamesInstanceZoneAndProject(t *testing.T) {
	e := newEnvironment(t, nil)
	s := e.reg.ConfirmationSummary(instancesDeleteID)(json.RawMessage(instanceDeleteArgs + `}`))
	for _, part := range []string{"my-vm", "us-central1-a", "my-project-123", "autoDelete"} {
		if !strings.Contains(s, part) {
			t.Errorf("summary %q lacks %q", s, part)
		}
	}
}
