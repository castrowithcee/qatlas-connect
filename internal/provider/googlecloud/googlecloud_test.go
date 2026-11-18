package googlecloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
)

func TestRegisterPublishesMetadata(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	metadata, found := reg.ProviderMetadata(Provider)
	if !found || metadata.DefaultBaseURL != "https://cloudresourcemanager.googleapis.com" ||
		metadata.DocPath() != "google.cloud/setup.md" || metadata.Token == nil {
		t.Fatalf("metadata = %+v", metadata)
	}
	if !reflect.DeepEqual(metadata.SecretRoles, config.TokenSecretRoles()) {
		t.Errorf("secret roles = %+v, want the token roles", metadata.SecretRoles)
	}
	var ids []string
	reads := 0
	for _, tool := range metadata.Tools {
		ids = append(ids, tool.ID)
		wantEffect := config.EffectRead
		switch {
		case tool.ID == serviceAccountsCreateID, tool.ID == disksInsertID, tool.ID == disksCreateSnapshotID,
			tool.ID == imagesInsertID:
			wantEffect = config.EffectCreate
		case tool.ID == serviceAccountsPatchID, tool.ID == serviceAccountsDisableID, tool.ID == serviceAccountsEnableID,
			tool.ID == serviceAccountKeysDisableID, tool.ID == serviceAccountKeysEnableID, tool.ID == apiKeysUndeleteID,
			tool.ID == servicesEnableID, tool.ID == servicesBatchEnableID, tool.ID == servicesDisableID,
			strings.Contains(tool.ID, ".iam-bindings."), tool.ID == instancesStartID, tool.ID == instancesStopID,
			tool.ID == instancesResetID, tool.ID == instancesSetMachineTypeID, tool.ID == disksResizeID:
			wantEffect = config.EffectUpdate
		case tool.ID == serviceAccountKeysDeleteID, tool.ID == serviceAccountsDeleteID, tool.ID == apiKeysDeleteID,
			tool.ID == firewallsDeleteID, tool.ID == addressesDeleteID, tool.ID == vpnTunnelsDeleteID,
			tool.ID == vpnGatewaysDeleteID, tool.ID == instancesDeleteID, tool.ID == disksDeleteID,
			tool.ID == snapshotsDeleteID, tool.ID == imagesDeleteID:
			wantEffect = config.EffectDelete
		case tool.ID == instancesInsertID:
			wantEffect = config.EffectCreate
		}
		// The quota tools belong to read-quotas and the bucket policy and ACL to read-storage-access, not to read-iam.
		if tool.Effect == config.EffectRead && !strings.Contains(tool.ID, ".quota-") &&
			tool.ID != bucketsGetIAMPolicyID && tool.ID != bucketAccessControlsListID &&
			tool.ID != objectAccessControlsListID {
			reads++
		}
		if tool.Effect != wantEffect {
			t.Errorf("%s effect = %v", tool.ID, tool.Effect)
		}
		wantScope := "https://www.googleapis.com/auth/cloud-platform.read-only"
		if strings.Contains(tool.ID, ".iam-") || strings.Contains(tool.ID, ".service-account") ||
			strings.Contains(tool.ID, ".quota-") ||
			tool.ID == servicesEnableID || tool.ID == servicesBatchEnableID || tool.ID == servicesDisableID {
			// The discovery documents list cloud-platform alone for the IAM methods.
			wantScope = "https://www.googleapis.com/auth/cloud-platform"
		}
		if tool.ID == apiKeysDeleteID || tool.ID == apiKeysUndeleteID {
			// apikeys v2 lists apikeys and cloud-platform for the two changes.
			wantScope = scopeAPIKeys
		}
		if strings.Contains(tool.ID, ".instances.") || strings.Contains(tool.ID, ".machine-types.") ||
			strings.Contains(tool.ID, ".zones.") || strings.Contains(tool.ID, ".regions.") ||
			strings.Contains(tool.ID, ".compute-operations.") ||
			strings.Contains(tool.ID, ".networks.") || strings.Contains(tool.ID, ".subnetworks.") ||
			strings.Contains(tool.ID, ".firewalls.") || strings.Contains(tool.ID, ".addresses.") ||
			strings.Contains(tool.ID, ".vpn-gateways.") || strings.Contains(tool.ID, ".vpn-tunnels.") ||
			strings.Contains(tool.ID, ".disks.") || strings.Contains(tool.ID, ".snapshots.") || strings.Contains(tool.ID, ".images.") {
			// The discovery document of compute v1 lists compute.readonly as the smallest scope.
			wantScope = "https://www.googleapis.com/auth/compute.readonly"
		}
		if slices.Contains([]string{firewallsDeleteID, addressesDeleteID, vpnTunnelsDeleteID, vpnGatewaysDeleteID,
			instancesStartID, instancesStopID, instancesResetID, instancesSetMachineTypeID,
			instancesInsertID, instancesDeleteID, disksInsertID, disksResizeID, disksCreateSnapshotID,
			disksDeleteID, imagesInsertID, imagesDeleteID, snapshotsDeleteID}, tool.ID) {
			// compute v1 lists compute and cloud-platform for the deletions, the instance tools, and the disk and
			// image changes.
			wantScope = scopeCompute
		}
		if strings.Contains(tool.ID, ".managed-zones.") || strings.Contains(tool.ID, ".record-sets.") ||
			strings.Contains(tool.ID, ".changes.") || strings.Contains(tool.ID, ".dns-keys.") {
			// dns v1 lists ndev.clouddns.readonly as the smallest scope for the reads of zones and records.
			wantScope = scopeDNSReadOnly
		}
		if strings.Contains(tool.ID, ".buckets.") || strings.Contains(tool.ID, ".bucket-access-controls.") ||
			strings.Contains(tool.ID, ".objects.") || strings.Contains(tool.ID, ".object-access-controls.") ||
			tool.ID == notificationsListID || tool.ID == managedFoldersListID || tool.ID == hmacKeysListID || tool.ID == hmacKeysGetID {
			// storage v1 lists devstorage.read_only for the bucket reads, and only devstorage.full_control and
			// cloud-platform for the policy and the ACL.
			wantScope = scopeStorageReadOnly
			if tool.ID == bucketsGetIAMPolicyID || tool.ID == bucketAccessControlsListID ||
				tool.ID == objectAccessControlsListID {
				wantScope = scopeStorageFullControl
			}
		}
		if strings.Contains(tool.ID, "billing") {
			// cloudbilling v1 lists cloud-billing.readonly for the four reads.
			wantScope = scopeBillingReadOnly
		}
		if scopes := metadata.Token.Scopes[tool.ID]; !slices.Equal(scopes, []string{wantScope}) {
			t.Errorf("%s scopes = %v", tool.ID, scopes)
		}
	}
	if want := []string{"google.cloud.addresses.delete", "google.cloud.addresses.get", "google.cloud.addresses.list", "google.cloud.api-keys.delete", "google.cloud.api-keys.get", "google.cloud.api-keys.list",
		"google.cloud.api-keys.undelete", "google.cloud.billing-accounts.get", "google.cloud.billing-accounts.list", "google.cloud.billing-accounts.list-projects", "google.cloud.bucket-access-controls.list", "google.cloud.buckets.get", "google.cloud.buckets.get-iam-policy", "google.cloud.buckets.list", "google.cloud.changes.get", "google.cloud.changes.list", "google.cloud.compute-operations.get", "google.cloud.compute-operations.list", "google.cloud.disks.aggregated-list", "google.cloud.disks.create-snapshot", "google.cloud.disks.delete", "google.cloud.disks.get", "google.cloud.disks.insert", "google.cloud.disks.list", "google.cloud.disks.resize", "google.cloud.dns-keys.get", "google.cloud.dns-keys.list", "google.cloud.firewalls.delete", "google.cloud.firewalls.get", "google.cloud.firewalls.list",
		"google.cloud.folders.get", "google.cloud.folders.list", "google.cloud.folders.search",
		"google.cloud.hmac-keys.get", "google.cloud.hmac-keys.list",
		"google.cloud.iam-bindings.add-member", "google.cloud.iam-bindings.change-role",
		"google.cloud.iam-bindings.remove-member", "google.cloud.iam-deny-policies.get", "google.cloud.iam-deny-policies.list",
		"google.cloud.iam-permissions.query-testable", "google.cloud.iam-permissions.test",
		"google.cloud.iam-policies.get", "google.cloud.iam-roles.get", "google.cloud.iam-roles.list",
		"google.cloud.iam-roles.query-grantable", "google.cloud.images.delete", "google.cloud.images.get", "google.cloud.images.insert", "google.cloud.images.list", "google.cloud.instances.aggregated-list",
		"google.cloud.instances.delete", "google.cloud.instances.get", "google.cloud.instances.insert",
		"google.cloud.instances.list", "google.cloud.instances.reset",
		"google.cloud.instances.set-machine-type", "google.cloud.instances.start", "google.cloud.instances.stop",
		"google.cloud.machine-types.list", "google.cloud.managed-folders.list", "google.cloud.managed-zones.get", "google.cloud.managed-zones.list",
		"google.cloud.networks.get", "google.cloud.networks.list", "google.cloud.notifications.list", "google.cloud.object-access-controls.list",
		"google.cloud.objects.download", "google.cloud.objects.get", "google.cloud.objects.list", "google.cloud.operations.get", "google.cloud.organizations.get",
		"google.cloud.organizations.search", "google.cloud.project-billing-info.get", "google.cloud.projects.get", "google.cloud.projects.list",
		"google.cloud.projects.search", "google.cloud.quota-infos.get", "google.cloud.quota-infos.list",
		"google.cloud.quota-preferences.get", "google.cloud.quota-preferences.list", "google.cloud.record-sets.get", "google.cloud.record-sets.list", "google.cloud.regions.list", "google.cloud.service-account-keys.delete",
		"google.cloud.service-account-keys.disable", "google.cloud.service-account-keys.enable",
		"google.cloud.service-account-keys.get",
		"google.cloud.service-account-keys.list", "google.cloud.service-accounts.create",
		"google.cloud.service-accounts.delete", "google.cloud.service-accounts.disable", "google.cloud.service-accounts.enable",
		"google.cloud.service-accounts.get", "google.cloud.service-accounts.list",
		"google.cloud.service-accounts.patch", "google.cloud.services.batch-enable", "google.cloud.services.batch-get",
		"google.cloud.services.disable", "google.cloud.services.enable", "google.cloud.services.get",
		"google.cloud.services.list", "google.cloud.snapshots.delete", "google.cloud.snapshots.get", "google.cloud.snapshots.list", "google.cloud.subnetworks.get", "google.cloud.subnetworks.list", "google.cloud.vpn-gateways.delete", "google.cloud.vpn-gateways.get", "google.cloud.vpn-gateways.list",
		"google.cloud.vpn-tunnels.delete", "google.cloud.vpn-tunnels.get", "google.cloud.vpn-tunnels.list", "google.cloud.zones.list"}; !slices.Equal(ids, want) {
		t.Errorf("tools = %v, want %v", ids, want)
	}
	if profile, found := metadata.RecommendedProfile(); !found || profile.ID != "read" || len(profile.Tools) != 62 {
		t.Errorf("profile = %+v", profile)
	}
	// The recommended profile asks only for read scopes, an explicit allow-list; a tool that needs the broad
	// cloud-platform scope belongs to read-iam, which holds every read tool, or to a changing profile.
	readScopes := [][]string{{"https://www.googleapis.com/auth/cloud-platform.read-only"},
		{"https://www.googleapis.com/auth/compute.readonly"}, {scopeStorageReadOnly}, {scopeBillingReadOnly}, {scopeDNSReadOnly}}
	for _, profile := range metadata.Profiles {
		switch profile.ID {
		case "read":
			for _, tool := range profile.Tools {
				if scopes := metadata.Token.Scopes[tool]; !slices.ContainsFunc(readScopes, func(want []string) bool {
					return slices.Equal(scopes, want)
				}) {
					t.Errorf("profile read holds %s with scopes %v", tool, scopes)
				}
			}
		case "read-iam":
			if profile.Recommended || len(profile.Tools) != reads {
				t.Errorf("profile read-iam = %+v", profile)
			}
		case "read-quotas":
			if profile.Recommended || len(profile.Tools) != len(readTools)+4 {
				t.Errorf("profile read-quotas = %+v", profile)
			}
		case "read-storage-access":
			if profile.Recommended || len(profile.Tools) != len(readTools)+3 {
				t.Errorf("profile read-storage-access = %+v", profile)
			}
		case "service-accounts":
			if profile.Recommended || len(profile.Tools) != 4 {
				t.Errorf("profile service-accounts = %+v", profile)
			}
		case "service-accounts-revoke":
			if profile.Recommended || len(profile.Tools) != 3 {
				t.Errorf("profile service-accounts-revoke = %+v", profile)
			}
		case "api-keys-revoke":
			if profile.Recommended || len(profile.Tools) != 2 {
				t.Errorf("profile api-keys-revoke = %+v", profile)
			}
		case "services":
			if profile.Recommended || !slices.Equal(profile.Tools, []string{servicesEnableID, servicesBatchEnableID}) {
				t.Errorf("profile services = %+v", profile)
			}
		case "compute-network-delete":
			if profile.Recommended || !slices.Equal(profile.Tools, []string{projectsGetID, firewallsDeleteID, addressesDeleteID, vpnTunnelsDeleteID, vpnGatewaysDeleteID}) {
				t.Errorf("profile compute-network-delete = %+v", profile)
			}
		case "compute-lifecycle":
			if profile.Recommended || !slices.Equal(profile.Tools, []string{projectsGetID, instancesStartID, instancesStopID, instancesResetID, instancesSetMachineTypeID}) {
				t.Errorf("profile compute-lifecycle = %+v", profile)
			}
		case "compute-instances-create":
			if profile.Recommended || !slices.Equal(profile.Tools, []string{projectsGetID, instancesInsertID}) {
				t.Errorf("profile compute-instances-create = %+v", profile)
			}
		case "compute-disks":
			if profile.Recommended || !slices.Equal(profile.Tools, []string{projectsGetID, disksInsertID, disksResizeID, disksCreateSnapshotID}) {
				t.Errorf("profile compute-disks = %+v", profile)
			}
		case "compute-images-delete":
			if profile.Recommended || !slices.Equal(profile.Tools, []string{projectsGetID, imagesInsertID, snapshotsDeleteID, imagesDeleteID}) {
				t.Errorf("profile compute-images-delete = %+v", profile)
			}
		case "iam-bindings":
			if profile.Recommended || !slices.Contains(profile.Tools, iamBindingsAddID) ||
				!slices.Contains(profile.Tools, iamBindingsRemoveID) || !slices.Contains(profile.Tools, iamBindingsChangeID) {
				t.Errorf("profile iam-bindings = %+v", profile)
			}
		default:
			t.Errorf("unexpected profile %s", profile.ID)
		}
	}
	if err := metadata.ValidateBaseURL("https://cloudresourcemanager.googleapis.com/"); err != nil {
		t.Errorf("official origin refused: %v", err)
	}
	for _, bad := range []string{"https://example.invalid", "http://cloudresourcemanager.googleapis.com",
		"https://cloudresourcemanager.googleapis.com.evil.example", "https://iam.googleapis.com"} {
		if metadata.ValidateBaseURL(bad) == nil {
			t.Errorf("base URL %q accepted", bad)
		}
	}
	oauth, found := reg.OAuth(Provider)
	if !found || oauth.TokenURL != googleapi.OAuth.TokenURL {
		t.Errorf("OAuth = %+v, %v", oauth, found)
	}
}

func TestServiceOriginsAreFixedHTTPSGoogleOrigins(t *testing.T) {
	want := []service{serviceResourceManager, serviceIAM, serviceUsage, serviceCompute, serviceStorage, serviceDNS, serviceBilling, serviceAPIKeys, serviceQuotas}
	if len(serviceOrigins) != len(want) {
		t.Errorf("services = %d, want %d", len(serviceOrigins), len(want))
	}
	for _, svc := range want {
		origin := serviceOrigins[svc]
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Host, ".googleapis.com") || u.Path != "" ||
			u.RawQuery != "" || u.User != nil {
			t.Errorf("origin of %s = %q", svc, origin)
		}
	}
	if serviceOrigins[serviceResourceManager] != apiRoot {
		t.Errorf("default origin differs from the Resource Manager origin")
	}
}

func TestGoogleErrorMapsOnlyDocumentedValues(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   provider.Class
	}{
		{"anonymous request", 401, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING"), provider.ClassAuth},
		{"unauthenticated status alone", 401, errorObject(401, "UNAUTHENTICATED", ""), provider.ClassAuth},
		{"expired access token", 401, errorObject(401, "", "ACCESS_TOKEN_EXPIRED"), provider.ClassAuth},
		{"unknown 401", 401, `{"error":{"status":"SOMETHING_ELSE"}}`, provider.ClassInvalidResponse},
		{"permission denied", 403, errorObject(403, "PERMISSION_DENIED", ""), provider.ClassPermission},
		{"service disabled", 403, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"), provider.ClassPermission},
		{"scope", 403, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT"), provider.ClassScopeMissing},
		{"403 without error object", 403, `<html>`, provider.ClassInvalidResponse},
		{"not found", 404, errorObject(404, "NOT_FOUND", ""), provider.ClassNotFound},
		{"404 without error object", 404, ``, provider.ClassInvalidResponse},
		{"rate limited", 429, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"), provider.ClassRateLimited},
		{"rate limited without body", 429, ``, provider.ClassRateLimited},
		{"bad request", 400, errorObject(400, "INVALID_ARGUMENT", ""), provider.ClassInvalidResponse},
		{"unknown client error", 409, errorObject(409, "ALREADY_EXISTS", ""), provider.ClassInvalidResponse},
		{"etag conflict", 409, errorObject(409, "ABORTED", ""), provider.ClassProviderError},
		{"unavailable", 503, errorObject(503, "UNAVAILABLE", ""), provider.ClassUnreachable},
		{"gateway timeout", 504, ``, provider.ClassTimeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			failed := googleError("op", c.status, []byte(c.body))
			if failed.Class != c.want {
				t.Fatalf("class = %s, want %s", failed.Class, c.want)
			}
			if strings.Contains(failed.Error(), detailCanary) {
				t.Errorf("message %q carries provider text", failed.Error())
			}
			if c.body != "" && c.status != 503 && !strings.Contains(c.body, "<html>") &&
				(failed.Original == nil || failed.Original.Status != c.status) {
				t.Errorf("original = %+v", failed.Original)
			}
		})
	}
}

func TestProjectsGetReturnsEveryDocumentedField(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	out, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]any{"name": "projects/415104041262", "projectId": "my-project-123",
		"displayName": "My Project", "parent": "folders/2001", "state": "ACTIVE", "etag": `W/"abc"`,
		"createTime": "2026-01-02T03:04:05Z", "updateTime": "2026-02-03T04:05:06Z", "managementType": "SELF_MANAGED"} {
		if got[field] != want {
			t.Errorf("%s = %v, want %v", field, got[field], want)
		}
	}
	if labels, _ := got["labels"].(map[string]any); labels["env"] != "test" {
		t.Errorf("labels = %v", got["labels"])
	}
	requests := e.fake.api()
	if len(requests) != 1 || requests[0].method != "GET" ||
		requests[0].url != "https://cloudresourcemanager.googleapis.com/v3/projects/my-project-123" ||
		requests[0].header.Get("Accept") != "application/json" {
		t.Errorf("requests = %+v", requests)
	}
}

func TestProjectsGetDescriptorCoversTheDiscoveryFields(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(projectsGet.OutputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	// The members of the Project resource in the discovery document of cloudresourcemanager v3.
	for _, field := range []string{"name", "parent", "projectId", "state", "displayName", "createTime", "updateTime",
		"deleteTime", "etag", "labels", "tags", "managementType", "isManagementProject", "configuredCapabilities"} {
		if _, found := schema.Properties[field]; !found {
			t.Errorf("output schema lacks %s", field)
		}
		if !slices.ContainsFunc(projectsGet.Fields, func(f capability.Field) bool { return f.Name == field }) {
			t.Errorf("fields lack %s", field)
		}
	}
}

func TestProjectsGetBoundsProviderText(t *testing.T) {
	e := newEnvironment(t, nil)
	long := strings.Repeat("x", 5000)
	e.fake.routes["/v3/projects/my-project-123"] = okReply(`{"name":"projects/1","displayName":"` + long +
		"\\u0007bell" + `","labels":{"k":"` + long + `"}}`)
	out, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 2000 || !strings.Contains(out, "…") {
		t.Errorf("result of %d bytes is not bounded", len(out))
	}
}

func TestProjectsGetRefusesArgumentsBeforeAnyRequest(t *testing.T) {
	e := newEnvironment(t, nil)
	for _, arguments := range []string{`{"project_id":"projects/my-project"}`, `{"project_id":"../x"}`,
		`{"project_id":"My-Project"}`, `{"project_id":"a"}`, `{"project_id":"my-project-123","extra":1}`, `{}`} {
		if _, err := e.invoke(projectsGetID, arguments); err == nil {
			t.Errorf("arguments %s accepted", arguments)
		}
	}
	if len(e.fake.api()) != 0 || e.lookups != 0 {
		t.Errorf("requests = %d, secret lookups = %d, want none", len(e.fake.api()), e.lookups)
	}
}

func TestProjectsGetOutsideTargetIsRefusedBeforeSecretAndIO(t *testing.T) {
	e := newEnvironment(t, []string{"projects/allowed-project"})
	_, err := e.invoke(projectsGetID, `{"project_id":"other-project"}`)
	if err == nil || strings.Contains(err.Error(), "other-project") || strings.Contains(err.Error(), "allowed-project") {
		t.Fatalf("error = %v, want a refusal that names no project", err)
	}
	if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
		t.Errorf("api=%d tokens=%d lookups=%d, want no I/O and no secret read", len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
	}
}

func TestProjectsGetBilledOnlyConnectionReachesNoProject(t *testing.T) {
	e := newEnvironment(t, []string{"billingAccounts/012345-ABCDEF-678901"})
	if _, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err == nil {
		t.Fatal("a connection bound to a billing account alone read a project")
	}
	if len(e.fake.api()) != 0 || e.lookups != 0 {
		t.Errorf("api=%d lookups=%d", len(e.fake.api()), e.lookups)
	}
}

func TestProjectsGetProvesMembershipAlongTheParentChain(t *testing.T) {
	// project -> folders/2001 -> folders/2002 -> organizations/9001
	routes := func(e *environment) {
		e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
		e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"folders/2002"}`)
		e.fake.routes["/v3/folders/2002"] = okReply(`{"name":"folders/2002","parent":"organizations/9001"}`)
	}
	t.Run("folder target above", func(t *testing.T) {
		e := newEnvironment(t, []string{"folders/2002"})
		routes(e)
		if _, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err != nil {
			t.Fatal(err)
		}
		if got := len(e.fake.api()); got != 2 {
			t.Errorf("requests = %d, want the project and one folder", got)
		}
	})
	t.Run("organization target above", func(t *testing.T) {
		e := newEnvironment(t, []string{"organizations/9001"})
		routes(e)
		if _, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err != nil {
			t.Fatal(err)
		}
		if got := len(e.fake.api()); got != 3 {
			t.Errorf("requests = %d, want the project and two folders", got)
		}
	})
	t.Run("project number target and ID argument", func(t *testing.T) {
		e := newEnvironment(t, []string{"projects/415104041262"})
		routes(e)
		if _, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err != nil {
			t.Fatal(err)
		}
		if got := len(e.fake.api()); got != 1 {
			t.Errorf("requests = %d, want the project only", got)
		}
	})
	t.Run("other organization", func(t *testing.T) {
		e := newEnvironment(t, []string{"organizations/9999"})
		routes(e)
		out, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
		if err == nil || out != "" || strings.Contains(err.Error(), "9999") || strings.Contains(err.Error(), "9001") {
			t.Fatalf("out=%q err=%v, want a refusal without detail", out, err)
		}
	})
	t.Run("other folder", func(t *testing.T) {
		e := newEnvironment(t, []string{"folders/7777"})
		routes(e)
		if out, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err == nil || out != "" {
			t.Fatalf("out=%q err=%v, want a refusal", out, err)
		}
	})
	t.Run("project below the organization directly", func(t *testing.T) {
		e := newEnvironment(t, []string{"organizations/9001"})
		e.fake.routes["/v3/projects/my-project-123"] = okReply(`{"name":"projects/1","projectId":"my-project-123","parent":"organizations/9001"}`)
		if _, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err != nil {
			t.Fatal(err)
		}
		if got := len(e.fake.api()); got != 1 {
			t.Errorf("requests = %d, want the project only", got)
		}
	})
	t.Run("project without a parent", func(t *testing.T) {
		e := newEnvironment(t, []string{"organizations/9001"})
		e.fake.routes["/v3/projects/my-project-123"] = okReply(`{"name":"projects/1","projectId":"my-project-123"}`)
		if _, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err == nil {
			t.Fatal("a project without a parent was bound to an organization")
		}
	})
	t.Run("a chain that loops", func(t *testing.T) {
		e := newEnvironment(t, []string{"organizations/9001"})
		e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
		e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"folders/2001"}`)
		if _, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err == nil {
			t.Fatal("a looping chain was admitted")
		}
		if got := len(e.fake.api()); got != 2 {
			t.Errorf("requests = %d, want the project and one folder read", got)
		}
	})
	t.Run("a folder answer for another folder", func(t *testing.T) {
		e := newEnvironment(t, []string{"organizations/9001"})
		e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
		e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/9","parent":"organizations/9001"}`)
		_, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Fatalf("err = %v, want invalid-provider-response", err)
		}
	})
}

func TestTokenTravelsInTheHeaderOnly(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	out, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	const issued = "synthetic-access-1"
	for _, request := range e.fake.api() {
		if request.auth != "Bearer "+issued {
			t.Errorf("Authorization = %q", request.auth)
		}
		if strings.Contains(request.url, issued) || strings.Contains(request.body, issued) || strings.Contains(request.url, refreshValue) {
			t.Errorf("request %s carries a secret outside the header", request.url)
		}
	}
	if strings.Contains(out, issued) || strings.Contains(out, refreshValue) {
		t.Errorf("result %q carries a secret", out)
	}
	if got := e.red.Apply("x " + issued + " " + refreshValue); strings.Contains(got, issued) || strings.Contains(got, refreshValue) {
		t.Errorf("redactor lets %q through", got)
	}
	// The scopes of the token request follow from the tools of the connection.
	if e.fake.tokenRequests() != 1 {
		t.Errorf("token requests = %d, want 1", e.fake.tokenRequests())
	}
}

func TestServiceAccountKeyIssuesTheToken(t *testing.T) {
	e := newEnvironment(t, nil)
	key := serviceAccountKey(t)
	e.roles = map[string]string{config.RoleServiceAccountKey: key}
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	if _, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err != nil {
		t.Fatal(err)
	}
	requests := e.fake.api()
	if len(requests) != 1 || requests[0].auth != "Bearer synthetic-access-1" {
		t.Fatalf("requests = %+v", requests)
	}
	if got := e.red.Apply(key); strings.Contains(got, "PRIVATE KEY") {
		t.Errorf("the key file is not registered with the redactor")
	}
}

func TestUnauthorizedRenewsTheTokenOnce(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.hook = func(n int, _ *http.Request) *reply {
		r := reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING")}
		return &r
	}
	_, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
	if classOf(err) != provider.ClassAuth {
		t.Fatalf("err = %v, want auth", err)
	}
	if got := len(e.fake.api()); got != 2 || e.fake.tokenRequests() != 2 {
		t.Errorf("requests = %d, token requests = %d, want one retry with one new token", got, e.fake.tokenRequests())
	}
	if strings.Contains(err.Error(), detailCanary) {
		t.Errorf("error %q carries provider text", err)
	}
}

func TestProjectsGetFailures(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"permission denied", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"api disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission},
		{"unreadable body", reply{status: 200, body: `{"name":`}, provider.ClassInvalidResponse},
		{"not an object", reply{status: 200, body: `[]`}, provider.ClassInvalidResponse},
		{"not json", reply{status: 200, body: `<html>`, header: http.Header{"Content-Type": {"text/html"}}}, provider.ClassInvalidResponse},
		{"no project name", reply{status: 200, body: `{"projectId":"x"}`}, provider.ClassInvalidResponse},
		{"too large", reply{status: 200, body: `{"name":"projects/1","displayName":"` + strings.Repeat("x", 3<<20) + `"}`}, provider.ClassInvalidResponse},
		{"redirect", reply{status: 302, header: http.Header{"Location": {"https://example.invalid/"}}}, provider.ClassProviderError},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout},
		{"unreachable", reply{err: &net.OpError{Op: "dial", Err: io.ErrUnexpectedEOF}}, provider.ClassUnreachable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnvironment(t, nil)
			e.fake.routes["/v3/projects/my-project-123"] = c.reply
			_, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
			if classOf(err) != c.want {
				t.Fatalf("err = %v (%s), want class %s", err, classOf(err), c.want)
			}
			if err != nil && (strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), "synthetic-access")) {
				t.Errorf("error %q carries provider text or a token", err)
			}
		})
	}
}

func TestRateLimitWithoutRetryAfterNeitherRepeatsNorSpins(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects/my-project-123"] = reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")}
	_, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
	if classOf(err) != provider.ClassRateLimited {
		t.Fatalf("err = %v, want rate-limited", err)
	}
	if got := len(e.fake.api()); got != 1 {
		t.Fatalf("requests = %d, want exactly one", got)
	}
	// The next call of the credential waits at least a second.
	_, _ = e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
	if len(e.sleeps) == 0 || e.sleeps[len(e.sleeps)-1] < time.Second {
		t.Errorf("sleeps = %v, want a wait of at least a second before the next request", e.sleeps)
	}
}

func TestRetryAfterHoldsTheNextRequest(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects/my-project-123"] = reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", ""),
		header: http.Header{"Retry-After": {"30"}}}
	_, _ = e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
	_, _ = e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
	if len(e.sleeps) == 0 || e.sleeps[len(e.sleeps)-1] < 29*time.Second {
		t.Errorf("sleeps = %v, want the wait Google asked for", e.sleeps)
	}
}

func TestRequestsAreSpacedPerCredential(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	for range 3 {
		if _, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.sleeps) != 2 {
		t.Errorf("sleeps = %v, want two waits for three requests", e.sleeps)
	}
}

func TestMissingConsentEndsBeforeAnyRequest(t *testing.T) {
	e := newEnvironment(t, nil)
	delete(e.roles, config.RoleRefreshToken)
	_, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`)
	if classOf(err) != provider.ClassConsentRequired {
		t.Fatalf("err = %v, want consent-required", err)
	}
	if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
		t.Error("a request was sent without a consent")
	}
}

func TestOperationsGet(t *testing.T) {
	const path = "/v3/operations/abc123"
	call := func(t *testing.T, targets []string, answer reply) (*environment, map[string]any, error) {
		e := newEnvironment(t, targets)
		e.fake.routes[path] = answer
		out, err := e.invoke(operationsGetID, `{"service":"resource-manager","name":"operations/abc123"}`)
		var got map[string]any
		if err == nil {
			if jsonErr := json.Unmarshal([]byte(out), &got); jsonErr != nil {
				t.Fatal(jsonErr)
			}
		}
		return e, got, err
	}
	t.Run("running", func(t *testing.T) {
		e, got, err := call(t, nil, okReply(`{"name":"operations/abc123","metadata":{"@type":"type.googleapis.com/x.Meta","detail":"working"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if got["done"] == true || got["service"] != "resource-manager" || got["response"] != nil {
			t.Errorf("result = %v", got)
		}
		if meta, _ := got["metadata"].(map[string]any); meta["detail"] != "working" {
			t.Errorf("metadata = %v", got["metadata"])
		}
		if r := e.fake.api(); len(r) != 1 || r[0].url != "https://cloudresourcemanager.googleapis.com/v3/operations/abc123" {
			t.Errorf("requests = %+v", r)
		}
	})
	t.Run("done with a response", func(t *testing.T) {
		_, got, err := call(t, nil, okReply(`{"name":"operations/abc123","done":true,"response":{"@type":"type.googleapis.com/x.Project","name":"projects/1"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if response, _ := got["response"].(map[string]any); got["done"] != true || response["name"] != "projects/1" {
			t.Errorf("result = %v", got)
		}
	})
	t.Run("done with an error", func(t *testing.T) {
		_, _, err := call(t, nil, okReply(`{"name":"operations/abc123","done":true,"error":{"code":7,"message":"`+detailCanary+`","details":[{"x":1}]}}`))
		if classOf(err) != provider.ClassPermission {
			t.Fatalf("err = %v, want permission", err)
		}
		if strings.Contains(err.Error(), detailCanary) {
			t.Errorf("message %q carries the provider text", err)
		}
		var failed *provider.Error
		if !errors.As(err, &failed) || failed.Original == nil || failed.Original.Entries[0].Message != detailCanary {
			t.Errorf("original = %+v", failed)
		}
	})
	t.Run("error of an unknown code", func(t *testing.T) {
		_, _, err := call(t, nil, okReply(`{"name":"operations/abc123","done":true,"error":{"code":13,"message":"m"}}`))
		if classOf(err) != provider.ClassProviderError {
			t.Fatalf("err = %v, want provider-error", err)
		}
	})
	t.Run("an error while running is not an outcome", func(t *testing.T) {
		_, got, err := call(t, nil, okReply(`{"name":"operations/abc123","error":{"code":7}}`))
		if err != nil || got["done"] == true {
			t.Errorf("got=%v err=%v", got, err)
		}
	})
	t.Run("another operation answered", func(t *testing.T) {
		_, _, err := call(t, nil, okReply(`{"name":"operations/other","done":true}`))
		if classOf(err) != provider.ClassInvalidResponse {
			t.Fatalf("err = %v, want invalid-provider-response", err)
		}
	})
	t.Run("bounded provider data", func(t *testing.T) {
		deep := strings.Repeat(`{"a":`, 20) + `"x"` + strings.Repeat(`}`, 20)
		_, got, err := call(t, nil, okReply(`{"name":"operations/abc123","done":true,"response":`+deep+`}`))
		if err != nil {
			t.Fatal(err)
		}
		if encoded, _ := json.Marshal(got["response"]); strings.Count(string(encoded), `"a"`) > maxDepth {
			t.Errorf("response %s is deeper than %d", encoded, maxDepth)
		}
	})
}

func TestOperationsGetNeedsAnOrganizationWhenBound(t *testing.T) {
	for _, targets := range [][]string{{"projects/my-project-123"}, {"folders/2001"}, {"billingAccounts/012345-ABCDEF-678901"}} {
		e := newEnvironment(t, targets)
		e.fake.routes["/v3/operations/abc123"] = okReply(`{"name":"operations/abc123"}`)
		if _, err := e.invoke(operationsGetID, `{"service":"resource-manager","name":"operations/abc123"}`); err == nil {
			t.Errorf("targets %v read an operation", targets)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("targets %v: api=%d tokens=%d lookups=%d, want a refusal before I/O and secret", targets,
				len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	e := newEnvironment(t, []string{"organizations/9001", "projects/my-project-123"})
	e.fake.routes["/v3/operations/abc123"] = okReply(`{"name":"operations/abc123"}`)
	if _, err := e.invoke(operationsGetID, `{"service":"resource-manager","name":"operations/abc123"}`); err != nil {
		t.Errorf("a connection that names the organization was refused: %v", err)
	}
}

func TestOperationsGetRefusesBadArguments(t *testing.T) {
	e := newEnvironment(t, nil)
	for _, arguments := range []string{
		`{"service":"iam","name":"operations/abc123"}`, `{"service":"resource-manager","name":"operations/../x"}`,
		`{"service":"resource-manager","name":"operations/a/b"}`, `{"service":"resource-manager","name":"abc123"}`,
		`{"service":"resource-manager","name":"operations/..%2f"}`, `{"name":"operations/abc123"}`,
		`{"service":"resource-manager","name":"operations/abc123","url":"https://example.invalid"}`,
	} {
		if _, err := e.invoke(operationsGetID, arguments); err == nil {
			t.Errorf("arguments %s accepted", arguments)
		}
	}
	if len(e.fake.api()) != 0 || e.lookups != 0 {
		t.Errorf("api=%d lookups=%d", len(e.fake.api()), e.lookups)
	}
}

func TestOperationOfPassesAMutationsOperationThrough(t *testing.T) {
	result, err := operationOf("op", operationWire{Name: "operations/x", Metadata: json.RawMessage(`{"@type":"t"}`)})
	if err != nil || result.Done || result.Name != "operations/x" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := operationOf("op", operationWire{}); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("an operation without a name: %v", err)
	}
}

func TestConnectionTest(t *testing.T) {
	searched := okReply(`{"projects":[{"name":"projects/1","projectId":"my-project-123"}]}`)
	t.Run("success", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.fake.routes["/v3/projects:search"] = searched
		class, err := e.testConnection()
		if err != nil || class != provider.ClassOK {
			t.Fatalf("class=%s err=%v", class, err)
		}
		requests := e.fake.api()
		if len(requests) != 1 || requests[0].url != "https://cloudresourcemanager.googleapis.com/v3/projects:search?pageSize=1" ||
			requests[0].auth != "Bearer synthetic-access-1" {
			t.Errorf("requests = %+v", requests)
		}
	})
	t.Run("targets are proved by reading them", func(t *testing.T) {
		e := newEnvironment(t, []string{"projects/my-project-123", "folders/2001", "organizations/9001", "billingAccounts/012345-ABCDEF-678901"})
		e.fake.routes["/v3/projects:search"] = searched
		e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
		e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001"}`)
		e.fake.routes["/v3/organizations/9001"] = okReply(`{"name":"organizations/9001"}`)
		class, err := e.testConnection()
		if err != nil || class != provider.ClassOK {
			t.Fatalf("class=%s err=%v", class, err)
		}
		var paths []string
		for _, r := range e.fake.api() {
			u, _ := url.Parse(r.url)
			paths = append(paths, u.Path)
		}
		want := []string{"/v3/projects:search", "/v3/projects/my-project-123", "/v3/folders/2001", "/v3/organizations/9001"}
		if !slices.Equal(paths, want) {
			t.Errorf("paths = %v, want %v", paths, want)
		}
	})
	t.Run("a target that does not exist", func(t *testing.T) {
		e := newEnvironment(t, []string{"folders/2001"})
		e.fake.routes["/v3/projects:search"] = searched
		class, err := e.testConnection()
		if err != nil || class != provider.ClassNotFound {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("invalid credential", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.fake.tokenReply = &reply{status: 401, body: `{"error":"invalid_client","error_description":"` + detailCanary + `"}`}
		class, err := e.testConnection()
		if err != nil || class != provider.ClassAuth {
			t.Fatalf("class=%s err=%v", class, err)
		}
		if len(e.fake.api()) != 0 {
			t.Error("a request was sent without a token")
		}
	})
	t.Run("expired consent", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.fake.tokenReply = &reply{status: 400, body: `{"error":"invalid_grant"}`}
		if class, err := e.testConnection(); err != nil || class != provider.ClassConsentExpired {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("no consent yet", func(t *testing.T) {
		e := newEnvironment(t, nil)
		delete(e.roles, config.RoleRefreshToken)
		if class, err := e.testConnection(); err != nil || class != provider.ClassConsentRequired {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("missing permission", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.fake.routes["/v3/projects:search"] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}
		if class, err := e.testConnection(); err != nil || class != provider.ClassPermission {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("disabled api is a setup failure", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.fake.routes["/v3/projects:search"] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}
		class, err := e.testConnection()
		if err != nil || class != provider.ClassPermission {
			t.Fatalf("class=%s err=%v", class, err)
		}
		failed := googleError("test connection", 403, []byte(errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")))
		if !strings.Contains(failed.Message, "not enabled") {
			t.Errorf("message = %q, want it to say the API is not enabled", failed.Message)
		}
	})
	t.Run("service account key", func(t *testing.T) {
		e := newEnvironment(t, nil)
		e.roles = map[string]string{config.RoleServiceAccountKey: serviceAccountKey(t)}
		e.fake.routes["/v3/projects:search"] = searched
		if class, err := e.testConnection(); err != nil || class != provider.ClassOK {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("bad targets end before a secret is read", func(t *testing.T) {
		e := newEnvironment(t, nil)
		resolved := e.resolved()
		resolved.Targets = []string{"projects/BAD"}
		class, err := TestConnection(context.Background(), resolved, e.secrets(), e.red)
		if err != nil || class != provider.ClassProviderError || e.lookups != 0 {
			t.Fatalf("class=%s err=%v lookups=%d", class, err, e.lookups)
		}
	})
	t.Run("foreign base URL", func(t *testing.T) {
		e := newEnvironment(t, nil)
		resolved := e.resolved()
		resolved.BaseURL = "https://example.invalid"
		class, err := TestConnection(context.Background(), resolved, e.secrets(), e.red)
		if err != nil || class != provider.ClassProviderError || e.lookups != 0 || len(e.fake.api()) != 0 {
			t.Fatalf("class=%s err=%v lookups=%d", class, err, e.lookups)
		}
	})
}

func TestTokenEndpointsAreTheFixedGoogleOnes(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	if _, err := e.invoke(projectsGetID, `{"project_id":"my-project-123"}`); err != nil {
		t.Fatal(err)
	}
	// The fake answers only the fixed token URL; an invocation that reached another one would have failed.
	if e.fake.tokenRequests() != 1 {
		t.Errorf("token requests = %d", e.fake.tokenRequests())
	}
}
