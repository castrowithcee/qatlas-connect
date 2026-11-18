// Package googlecloud implements access to Google Cloud with one credential that issues access tokens: a
// person's consent, a service account key, or the impersonation of a service account, all through the token
// core of internal/provider. A connection takes no secret of its own and no scope: the scopes follow from the
// tools it offers (see scopes.go).
//
// The package starts with Cloud Resource Manager: the search of organizations, folders, and projects, one
// project, and the state of a long-running operation. It then reads IAM: roles, and the roles and permissions
// of a resource. It changes only through the profiles service-accounts (service_accounts_write.go),
// service-accounts-revoke and the listed-only service-accounts.delete (service_accounts_revoke.go),
// api-keys-revoke (the deletion and restoration of API keys, api_keys.go), projects-manage (creating, changing,
// moving, and restoring projects, projects_write.go), folders-manage (creating, renaming, moving, and
// restoring folders, folders_write.go), and
// iam-bindings (a single binding of the IAM policy of a project, folder, or organization, iam_bindings.go),
// compute-network-delete (firewall rules, reserved IP addresses, VPN tunnels and gateways, compute_delete.go),
// compute-instances-create (instances.insert, compute_instances_insert.go) and the listed-only instances.delete,
// compute-disks (creating, growing, and snapshotting disks, compute_disks_write.go) and the listed-only
// disks.delete, and compute-images-delete (creating images and deleting snapshots and images,
// compute_images_write.go), dns-write (changes and record sets of Cloud DNS zones, dns_record_sets_write.go),
// and storage-objects-write (uploading, copying, composing, and changing the metadata of Cloud Storage objects,
// storage_object_write.go), each change with the confirmation of a person. Every request goes to a fixed origin
// of the service table, with the access token in the Authorization header only, once, within a bounded size and time, and spaced per credential.
//
// A connection may be bound to organizations, folders, and projects, which nest as Google nests them, and to
// billing accounts, which belong to none of them. A project is bound to a target by reading it and the folders
// above it; a tool for a whole organization runs only on a connection that names an organization.
// Everything Google returns is untrusted third-party content: strings and lists are bounded.
package googlecloud

import (
	"context"
	"errors"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Provider is the provider name used in the configuration and as the operation namespace.
const Provider = "google.cloud"

// TestConnection obtains an access token and reads one project with projects.search, which costs nothing. A
// target of the connection is then proved to exist by reading it. A Cloud project of the credential in which
// the Cloud Resource Manager API is disabled ends as a failure of the class permission, which the message of
// the failed request explains.
func TestConnection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (provider.Class, error) {
	const op = "test connection"
	if err := checkConnection(resolved); err != nil {
		return testClass(err)
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return testClass(failure(op, "the targets of the connection are not usable"))
	}
	if secrets == nil {
		return testClass(failure(op, "no credential resolver was configured"))
	}
	probe := func(ctx context.Context, source *provider.TokenSource) (provider.Class, error) {
		c := withSource(resolved, source)
		if err := c.searchProjectsPage(ctx, op, projectsSearchID, 1); err != nil {
			return "", err
		}
		for _, t := range list.targets {
			var err error
			switch t.kind {
			case kindProject:
				_, err = c.project(ctx, op, projectsGetID, t.id)
			case kindFolder:
				_, err = c.folder(ctx, op, projectsGetID, t.id)
			case kindOrganization:
				err = c.organization(ctx, op, projectsGetID, t.id)
			}
			if err != nil {
				return "", err
			}
		}
		return provider.ClassOK, nil
	}
	return provider.TestTokenConnection(ctx, resolved, secrets, red, tokenOptions(), probe)
}

// testClass turns a provider failure into the class of a connection test; any other error is returned.
func testClass(err error) (provider.Class, error) {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class, nil
	}
	return "", err
}

// readTools are the tools of the profile read: every tool that needs no more than the read-only scope.
var readTools = []string{organizationsSearch.ID, foldersSearch.ID, projectsSearch.ID, projectsGet.ID, operationsGet.ID,
	organizationsGet.ID, foldersGet.ID, foldersList.ID, projectsList.ID, servicesList.ID, servicesGet.ID,
	servicesBatchGet.ID, apiKeysList.ID, apiKeysGet.ID, instancesList.ID, instancesAggregatedList.ID, instancesGet.ID,
	machineTypesList.ID, zonesList.ID, regionsList.ID, computeOperationsGet.ID, computeOperationsList.ID,
	networksList.ID, networksGet.ID, subnetworksList.ID, subnetworksGet.ID,
	firewallsList.ID, firewallsGet.ID, addressesList.ID, addressesGet.ID,
	vpnGatewaysList.ID, vpnGatewaysGet.ID, vpnTunnelsList.ID, vpnTunnelsGet.ID,
	disksList.ID, disksAggregatedList.ID, disksGet.ID,
	snapshotsList.ID, snapshotsGet.ID, imagesList.ID, imagesGet.ID,
	bucketsList.ID, bucketsGet.ID, notificationsList.ID, managedFoldersList.ID, hmacKeysList.ID, hmacKeysGet.ID,
	objectsList.ID, objectsGet.ID, objectsDownload.ID,
	billingAccountsList.ID, billingAccountsGet.ID, billingAccountsListProjects.ID, projectBillingInfoGet.ID,
	catalogServicesList.ID, catalogSkusList.ID,
	managedZonesList.ID, managedZonesGet.ID, recordSetsList.ID, recordSetsGet.ID,
	changesList.ID, changesGet.ID, dnsKeysList.ID, dnsKeysGet.ID,
	dnsPoliciesList.ID, dnsPoliciesGet.ID, responsePoliciesList.ID, responsePoliciesGet.ID}

// Register adds the provider metadata, its connection test, its consent endpoints, and its tools.
func Register(reg *capability.Registry) error {
	token := tokenMetadata
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Cloud", DefaultBaseURL: apiRoot,
		Description:    "Google Cloud APIs from Resource Manager, IAM, and Service Usage to Compute Engine, Cloud Storage, Cloud DNS, and Cloud Billing: read, and changes and deletions through opt-in profiles or a tools list",
		DefaultEffects: []config.Effect{config.EffectRead},
		ValidateBaseURL: func(raw string) error {
			if !isAPIRoot(raw) {
				return errors.New("a Google Cloud service must use the official API origin " + apiRoot)
			}
			return nil
		},
		SecretRoles: config.TokenSecretRoles(),
		Token:       &token,
		Target:      targetMetadata,
		Profiles: []config.ToolProfile{{
			ID: "read", Title: "Read", Recommended: true,
			Description: "searches and reads organizations, folders, and projects and reads the state of long-running " +
				"operations from Google Cloud Resource Manager, the enabled and available APIs of a project from Service " +
				"Usage, and the metadata of API keys with the read-only scope and the Compute Engine instances, machine " +
				"types, zones, regions, operations (with the person who started each), VPC networks, subnetworks, firewall rules, " +
				"reserved IP addresses, HA VPN gateways, VPN tunnels, persistent disks, disk snapshots, and images of a project with the scope compute.readonly, " +
				"the Cloud Storage buckets of a project, their objects, and the content of an object (up to a fixed limit), " +
				"the notification configurations and managed folders of a bucket, and " +
				"the HMAC key metadata of a project with the scope devstorage.read_only, " +
				"the Cloud Billing accounts a connection names, their projects, and the billing account of a project, and the public services and SKUs of the Cloud Billing catalog with the scope cloud-billing.readonly, " +
				"and the Cloud DNS managed zones, record sets, changes, DNSSEC keys, server policies, and response policies of a project with the scope ndev.clouddns.readonly, and changes nothing",
			Tools: readTools,
		}, {
			ID: "read-iam", Title: "Read with IAM",
			Description: "everything of read plus IAM roles, policies, deny policies, the roles and permissions of a resource, " +
				"and service accounts with the metadata of their keys; these IAM methods accept only the broad cloud-platform " +
				"scope, so the token may change what its account may, although no tool of this profile changes anything",
			Tools: append(append([]string{}, readTools...), iamRolesList.ID, iamRolesGet.ID, iamRolesQuery.ID,
				iamPermissionsQuery.ID, iamPoliciesGet.ID, iamPermissionsTest.ID, denyPoliciesList.ID,
				denyPoliciesGet.ID, serviceAccountsList.ID, serviceAccountsGet.ID, serviceAccountKeysList.ID,
				serviceAccountKeysGet.ID),
		}, {
			ID: "read-quotas", Title: "Read with quotas",
			Description: "everything of read plus the quotas of a service and the quota preferences of a project from " +
				"Cloud Quotas; these methods accept only the broad cloud-platform scope, so the token may change what " +
				"its account may, although no tool of this profile changes anything; quota preferences may hold " +
				"personal data (contactEmail, justification)",
			Tools: append(append([]string{}, readTools...), quotaInfosList.ID, quotaInfosGet.ID,
				quotaPreferencesList.ID, quotaPreferencesGet.ID),
		}, {
			ID: "read-budgets", Title: "Read with budgets",
			Description: "everything of read plus the budgets of a billing account a connection names, with their filters, " +
				"amounts, alert thresholds, and notification targets from the Cloud Billing Budget API; this API accepts " +
				"only the scope cloud-billing (or cloud-platform), which also lets a token change budgets, so the token " +
				"may change what its account may, although no tool of this profile changes anything",
			Tools: append(append([]string{}, readTools...), budgetsList.ID, budgetsGet.ID),
		}, {
			ID: "read-storage-access", Title: "Read with Storage access",
			Description: "everything of read plus the IAM policy and the access control list of a Cloud Storage bucket and the access control list of an object; " +
				"these methods accept only the scope devstorage.full_control (or cloud-platform), so the token may " +
				"change what its account may in Cloud Storage, although no tool of this profile changes anything; " +
				"policies and lists hold members that are personal data",
			Tools: append(append([]string{}, readTools...), bucketsGetIAMPolicy.ID, bucketAccessControlsList.ID,
				objectAccessControlsList.ID),
		}, {
			ID: "service-accounts", Title: "Service accounts",
			Description: "creates service accounts, changes their display name and description, and disables and " +
				"enables them; the token holds the broad cloud-platform scope and may change what its account may, " +
				"every change needs its own confirmation, and no tool creates a key, a token, or a signature",
			Tools: []string{serviceAccountsCreate.ID, serviceAccountsPatch.ID, serviceAccountsDisable.ID,
				serviceAccountsEnable.ID},
		}, {
			ID: "iam-bindings", Title: "Change IAM bindings",
			Description: "everything of read plus reading IAM policies and adding a member to, removing a member from, " +
				"or moving a member between the bindings of a project, folder, or organization, each with the " +
				"confirmation of a person; only principals already in the policy, or service accounts of the same " +
				"project, are granted; these methods accept only the broad cloud-platform scope",
			Tools: append(append([]string{}, readTools...), iamPoliciesGet.ID, iamBindingsAdd.ID, iamBindingsRemove.ID,
				iamBindingsChange.ID),
		}, {
			ID: "service-accounts-revoke", Title: "Service account revocation",
			Description: "disables, enables, and deletes the keys of service accounts; deleting a service account itself " +
				"is not part of this profile and needs a tools list that names it; the token holds the broad " +
				"cloud-platform scope and may change what its account may, every change needs its own confirmation, " +
				"and no tool creates a key, a token, or a signature",
			Tools: []string{serviceAccountKeysDisable.ID, serviceAccountKeysEnable.ID, serviceAccountKeysDelete.ID},
		}, {
			ID: "api-keys-revoke", Title: "API key revocation",
			Description: "deletes API keys of a project and restores deleted ones within the 30 days Google keeps them; " +
				"the token holds the apikeys scope, which Google also uses to create keys and read their values, so " +
				"the token may do what its account may, every change needs its own confirmation, and no tool creates " +
				"a key or returns a key value",
			Tools: []string{apiKeysDelete.ID, apiKeysUndelete.ID},
		}, {
			ID: "storage-delete", Title: "Delete and restore Storage objects",
			Description: "deletes Cloud Storage objects, one per call, and restores soft-deleted ones, each with the " +
				"confirmation of a person; without a generation the live version is deleted, which cannot be undone " +
				"without versioning or soft delete; projects.get is part of the profile so that targets of an " +
				"organization or a folder can be proved; objects.delete asks for devstorage.read_write and " +
				"objects.restore only accepts devstorage.full_control (or cloud-platform), so the token may change " +
				"what its account may in Cloud Storage; deleting a bucket is not part of this profile and needs a " +
				"tools list that names it; no tool uploads or changes an object",
			Tools: []string{projectsGet.ID, objectsDelete.ID, objectsRestore.ID},
		}, {
			ID: "compute-network-delete", Title: "Delete network resources",
			Description: "deletes firewall rules, reserved IP addresses, VPN tunnels, and HA VPN gateways of a Compute Engine project, each with the " +
				"confirmation of a person; a firewall rule is deleted only if its direction, action, and priority " +
				"still match what was confirmed; projects.get is part of the profile so that targets of an " +
				"organization or a folder can be proved; the token holds the compute scope, which Google also uses to " +
				"change Compute resources, so the token may do what its account may; no tool creates or changes a " +
				"firewall rule, an address, a VPN tunnel, or a VPN gateway",
			Tools: []string{projectsGet.ID, firewallsDelete.ID, addressesDelete.ID, vpnTunnelsDelete.ID, vpnGatewaysDelete.ID},
		}, {
			ID: "compute-lifecycle", Title: "Instance lifecycle",
			Description: "starts, stops, hard-resets, and changes the machine type of existing Compute Engine instances, " +
				"each with the confirmation of a person; projects.get is part of the profile so that targets of an " +
				"organization or a folder can be proved; the token holds the compute scope, which Google also uses to " +
				"change Compute resources, so the token may do what its account may; a running instance is billed, " +
				"stopping with discard_local_ssd loses the data of local SSDs, and a reset loses the contents of memory; " +
				"no tool creates or deletes an instance or changes its network access",
			Tools: []string{projectsGet.ID, instancesStart.ID, instancesStop.ID, instancesReset.ID,
				instancesSetMachineType.ID},
		}, {
			ID: "compute-instances-create", Title: "Create Compute instances",
			Description: "creates Compute Engine instances in a project with the confirmation of a person; an instance " +
				"gets no external address, no ssh-keys or windows-keys metadata, and no service account of another " +
				"project; the instance and its disks are billed while they exist, and a start-up script in the " +
				"metadata is not checked; projects.get is part of the profile so that targets of an organization or a " +
				"folder can be proved and the default service account can be named by its project number; the token " +
				"holds the compute scope, which Google also uses to change Compute resources, so the token may do what " +
				"its account may; deleting an instance is not part of this profile and needs a tools list that names it",
			Tools: []string{projectsGet.ID, instancesInsert.ID},
		}, {
			ID: "compute-disks", Title: "Create and grow disks",
			Description: "creates persistent disks, grows them, and creates snapshots of them in a Compute Engine " +
				"project, each with the confirmation of a person; disks and snapshots are billed until they are " +
				"deleted, and a disk cannot be shrunk again; projects.get is part of the profile so that targets of " +
				"an organization or a folder can be proved; the token holds the compute scope, which Google also " +
				"uses to change Compute resources, so the token may do what its account may; no tool deletes, " +
				"attaches, or shares a disk or snapshot",
			Tools: []string{projectsGet.ID, disksInsert.ID, disksResize.ID, disksCreateSnapshot.ID},
		}, {
			ID: "compute-images-delete", Title: "Create images and delete snapshots and images",
			Description: "creates images from disks, snapshots, or images of a Compute Engine project and deletes " +
				"snapshots and images, each with the confirmation of a person; an image is billed until it is " +
				"deleted, and a deleted snapshot or image cannot be restored; projects.get is part of the profile so " +
				"that targets of an organization or a folder can be proved; the token holds the compute scope, which " +
				"Google also uses to change Compute resources, so the token may do what its account may; deleting a " +
				"disk is not part of this profile and needs a tools list that names it; no tool shares an image or " +
				"snapshot or changes its IAM policy",
			Tools: []string{projectsGet.ID, imagesInsert.ID, snapshotsDelete.ID, imagesDelete.ID},
		}, {
			ID: "storage-objects-write", Title: "Write Cloud Storage objects",
			Description: "uploads objects of up to 256 KiB, copies them, composes 2 to 32 objects into one, and changes the " +
				"metadata of objects in the Cloud Storage buckets of a project, each with the confirmation of a person; an " +
				"object is never overwritten unless the call says so, no object is made public (publicRead, " +
				"authenticatedRead, and free-form ACLs are not offered), and no tool deletes an object; projects.get is part " +
				"of the profile so that targets of an organization or a folder can be proved; uploading, copying, and " +
				"composing use the scope devstorage.read_write, but objects.patch accepts only devstorage.full_control (or " +
				"cloud-platform), so the token also holds the right to change the access control lists of Cloud Storage " +
				"resources, and written data is billed for its storage",
			Tools: []string{projectsGet.ID, objectsInsert.ID, objectsCopy.ID, objectsCompose.ID, objectsPatch.ID},
		}, {
			ID: "storage-revoke", Title: "Cloud Storage access revocation",
			Description: "deactivates and deletes HMAC keys of a project and deletes notification configurations of a " +
				"bucket, each with the confirmation of a person; a key is deleted only if Google finds it INACTIVE, and " +
				"a notification only if its topic is the one named; the profile holds the reads that name a key or a " +
				"notification and projects.get, which proves a project against targets of an organization or a folder; " +
				"the HMAC tools ask for the scope devstorage.full_control, which is broader than deleting a key needs " +
				"because the key is read first, and which Google also uses to create keys and change what the account " +
				"may in Cloud Storage, so the token may do that although no tool does; no tool creates or reactivates " +
				"a key or creates a notification",
			Tools: []string{projectsGet.ID, hmacKeysList.ID, hmacKeysGet.ID, notificationsList.ID, hmacKeysDeactivate.ID,
				hmacKeysDelete.ID, notificationsDelete.ID},
		}, {
			ID: "dns-write", Title: "Change DNS records",
			Description: "adds, changes, and deletes the record sets of the Cloud DNS managed zones of a project, one " +
				"record set at a time or several in one atomic change, each with the confirmation of a person; the " +
				"SOA record and the NS records of the zone apex are refused, because they can move the delegation of " +
				"the zone to foreign name servers; NS records below the apex are allowed; private and public zones " +
				"are treated alike, so a record in a public zone is visible on the internet; the profile holds the " +
				"reads of the zone, the record set, and the change, and projects.get, which proves a project " +
				"against targets of an organization or a folder; the tools ask for the scope " +
				"ndev.clouddns.readwrite, which Google also uses to change every record of the project's zones, so " +
				"the token may do that although no tool creates or deletes a zone; no tool opens network access, and " +
				"what a record makes reachable is the responsibility of the person who confirms",
			Tools: []string{projectsGet.ID, managedZonesGet.ID, recordSetsGet.ID, changesGet.ID, changesCreate.ID,
				recordSetsCreate.ID, recordSetsPatch.ID, recordSetsDelete.ID},
		}, {
			ID: "services", Title: "Enable APIs",
			Description: "enables Google APIs in a project, one or up to 20 at a time, each with the confirmation of a " +
				"person; these Service Usage methods accept only the broad cloud-platform scope, so the token may " +
				"change what its account may; the resources and calls of an enabled API may be billed; disabling an " +
				"API is not part of this profile and needs a tools list that names it",
			Tools: []string{servicesEnable.ID, servicesBatchEnable.ID},
		}, {
			ID: "projects-manage", Title: "Manage projects",
			Description: "creates projects under an organization or folder of the connection, changes their display " +
				"name and labels, moves them between organizations and folders of the connection, and restores " +
				"projects marked for deletion, each with the confirmation of a person; a move changes the IAM " +
				"roles and policies a project inherits; no billing account is linked and nothing is deleted; " +
				"these Resource Manager methods accept only the broad cloud-platform scope, so the token may " +
				"change what its account may; deleting a project is not part of this profile and needs a tools " +
				"list that names it",
			Tools: []string{projectsCreate.ID, projectsPatch.ID, projectsMove.ID, projectsUndelete.ID},
		}, {
			ID: "folders-manage", Title: "Manage folders",
			Description: "creates folders under an organization or folder of the connection, renames them, moves " +
				"them between organizations and folders of the connection, and restores folders marked for " +
				"deletion, each with the confirmation of a person; a move changes the IAM roles and policies " +
				"of the folder and of everything in it; nothing is deleted; these Resource Manager methods " +
				"accept only the broad cloud-platform scope, so the token may change what its account may; " +
				"deleting a folder is not part of this profile",
			Tools: []string{foldersCreate.ID, foldersPatch.ID, foldersMove.ID, foldersUndelete.ID},
		}},
	}, TestConnection); err != nil {
		return err
	}
	oauth := googleapi.OAuth
	oauth.Transport = transport
	if err := reg.RegisterOAuth(Provider, oauth); err != nil {
		return err
	}
	return reg.Register(Provider,
		capability.Operation{Descriptor: organizationsSearch, Handler: handleOrganizationsSearch},
		capability.Operation{Descriptor: foldersSearch, Handler: handleFoldersSearch},
		capability.Operation{Descriptor: projectsSearch, Handler: handleProjectsSearch},
		capability.Operation{Descriptor: projectsGet, Handler: handleProjectsGet},
		capability.Operation{Descriptor: operationsGet, Handler: handleOperationsGet},
		capability.Operation{Descriptor: organizationsGet, Handler: handleOrganizationsGet},
		capability.Operation{Descriptor: foldersGet, Handler: handleFoldersGet},
		capability.Operation{Descriptor: foldersList, Handler: handleFoldersList},
		capability.Operation{Descriptor: projectsList, Handler: handleProjectsList},
		capability.Operation{Descriptor: servicesList, Handler: handleServicesList},
		capability.Operation{Descriptor: servicesGet, Handler: handleServicesGet},
		capability.Operation{Descriptor: servicesBatchGet, Handler: handleServicesBatchGet},
		capability.Operation{Descriptor: instancesList, Handler: handleInstancesList},
		capability.Operation{Descriptor: instancesAggregatedList, Handler: handleInstancesAggregatedList},
		capability.Operation{Descriptor: instancesGet, Handler: handleInstancesGet},
		capability.Operation{Descriptor: machineTypesList, Handler: handleMachineTypesList},
		capability.Operation{Descriptor: zonesList, Handler: handleZonesList},
		capability.Operation{Descriptor: regionsList, Handler: handleRegionsList},
		capability.Operation{Descriptor: computeOperationsGet, Handler: handleComputeOperationsGet},
		capability.Operation{Descriptor: computeOperationsList, Handler: handleComputeOperationsList},
		capability.Operation{Descriptor: networksList, Handler: handleNetworksList},
		capability.Operation{Descriptor: networksGet, Handler: handleNetworksGet},
		capability.Operation{Descriptor: subnetworksList, Handler: handleSubnetworksList},
		capability.Operation{Descriptor: subnetworksGet, Handler: handleSubnetworksGet},
		capability.Operation{Descriptor: firewallsList, Handler: handleFirewallsList},
		capability.Operation{Descriptor: firewallsGet, Handler: handleFirewallsGet},
		capability.Operation{Descriptor: addressesList, Handler: handleAddressesList},
		capability.Operation{Descriptor: addressesGet, Handler: handleAddressesGet},
		capability.Operation{Descriptor: vpnGatewaysList, Handler: handleVpnGatewaysList},
		capability.Operation{Descriptor: vpnGatewaysGet, Handler: handleVpnGatewaysGet},
		capability.Operation{Descriptor: vpnTunnelsList, Handler: handleVpnTunnelsList},
		capability.Operation{Descriptor: vpnTunnelsGet, Handler: handleVpnTunnelsGet},
		capability.Operation{Descriptor: disksList, Handler: handleDisksList},
		capability.Operation{Descriptor: disksAggregatedList, Handler: handleDisksAggregatedList},
		capability.Operation{Descriptor: disksGet, Handler: handleDisksGet},
		capability.Operation{Descriptor: snapshotsList, Handler: handleSnapshotsList},
		capability.Operation{Descriptor: snapshotsGet, Handler: handleSnapshotsGet},
		capability.Operation{Descriptor: imagesList, Handler: handleImagesList},
		capability.Operation{Descriptor: imagesGet, Handler: handleImagesGet},
		capability.Operation{Descriptor: bucketsList, Handler: handleBucketsList},
		capability.Operation{Descriptor: bucketsGet, Handler: handleBucketsGet},
		capability.Operation{Descriptor: notificationsList, Handler: handleNotificationsList},
		capability.Operation{Descriptor: managedFoldersList, Handler: handleManagedFoldersList},
		capability.Operation{Descriptor: hmacKeysList, Handler: handleHMACKeysList},
		capability.Operation{Descriptor: hmacKeysGet, Handler: handleHMACKeysGet},
		capability.Operation{Descriptor: managedZonesList, Handler: handleManagedZonesList},
		capability.Operation{Descriptor: managedZonesGet, Handler: handleManagedZonesGet},
		capability.Operation{Descriptor: recordSetsList, Handler: handleRecordSetsList},
		capability.Operation{Descriptor: recordSetsGet, Handler: handleRecordSetsGet},
		capability.Operation{Descriptor: changesList, Handler: handleChangesList},
		capability.Operation{Descriptor: changesGet, Handler: handleChangesGet},
		capability.Operation{Descriptor: changesCreate, Handler: handleChangesCreate,
			ConfirmationSummary: changeSummary},
		capability.Operation{Descriptor: recordSetsCreate, Handler: handleRecordSetsCreate,
			ConfirmationSummary: recordSummary("create")},
		capability.Operation{Descriptor: recordSetsPatch, Handler: handleRecordSetsPatch,
			ConfirmationSummary: recordSummary("patch")},
		capability.Operation{Descriptor: recordSetsDelete, Handler: handleRecordSetsDelete,
			ConfirmationSummary: recordSummary("delete")},
		capability.Operation{Descriptor: dnsKeysList, Handler: handleDNSKeysList},
		capability.Operation{Descriptor: dnsKeysGet, Handler: handleDNSKeysGet},
		capability.Operation{Descriptor: dnsPoliciesList, Handler: handleDNSPoliciesList},
		capability.Operation{Descriptor: dnsPoliciesGet, Handler: handleDNSPoliciesGet},
		capability.Operation{Descriptor: responsePoliciesList, Handler: handleResponsePoliciesList},
		capability.Operation{Descriptor: responsePoliciesGet, Handler: handleResponsePoliciesGet},
		capability.Operation{Descriptor: bucketsGetIAMPolicy, Handler: handleBucketsGetIAMPolicy},
		capability.Operation{Descriptor: bucketAccessControlsList, Handler: handleBucketAccessControlsList},
		capability.Operation{Descriptor: objectsList, Handler: handleObjectsList},
		capability.Operation{Descriptor: objectsGet, Handler: handleObjectsGet},
		capability.Operation{Descriptor: objectsDownload, Handler: handleObjectsDownload},
		capability.Operation{Descriptor: objectAccessControlsList, Handler: handleObjectAccessControlsList},
		capability.Operation{Descriptor: objectsInsert, Handler: handleObjectsInsert,
			ConfirmationSummary: objectsInsertSummary},
		capability.Operation{Descriptor: objectsCopy, Handler: handleObjectsCopy,
			ConfirmationSummary: objectsCopySummary},
		capability.Operation{Descriptor: objectsCompose, Handler: handleObjectsCompose,
			ConfirmationSummary: objectsComposeSummary},
		capability.Operation{Descriptor: objectsPatch, Handler: handleObjectsPatch,
			ConfirmationSummary: objectsPatchSummary},
		capability.Operation{Descriptor: objectsDelete, Handler: handleObjectsDelete,
			ConfirmationSummary: objectChangeSummary(objectsDeleteID)},
		capability.Operation{Descriptor: objectsRestore, Handler: handleObjectsRestore,
			ConfirmationSummary: objectChangeSummary(objectsRestoreID)},
		capability.Operation{Descriptor: objectsRestoreOverwrite, Handler: handleObjectsRestoreOverwrite,
			ConfirmationSummary: objectsRestoreOverwriteSummary},
		capability.Operation{Descriptor: bucketsDelete, Handler: handleBucketsDelete,
			ConfirmationSummary: bucketDeleteSummary},
		capability.Operation{Descriptor: billingAccountsList, Handler: handleBillingAccountsList},
		capability.Operation{Descriptor: billingAccountsGet, Handler: handleBillingAccountsGet},
		capability.Operation{Descriptor: billingAccountsListProjects, Handler: handleBillingAccountsListProjects},
		capability.Operation{Descriptor: projectBillingInfoGet, Handler: handleProjectBillingInfoGet},
		capability.Operation{Descriptor: budgetsList, Handler: handleBudgetsList},
		capability.Operation{Descriptor: budgetsGet, Handler: handleBudgetsGet},
		capability.Operation{Descriptor: catalogServicesList, Handler: handleCatalogServicesList},
		capability.Operation{Descriptor: catalogSkusList, Handler: handleCatalogSkusList},
		capability.Operation{Descriptor: firewallsDelete, Handler: handleFirewallsDelete,
			ConfirmationSummary: firewallDeleteSummary},
		capability.Operation{Descriptor: addressesDelete, Handler: handleAddressesDelete,
			ConfirmationSummary: addressDeleteSummary},
		capability.Operation{Descriptor: vpnTunnelsDelete, Handler: vpnTunnelDelete.handler("delete VPN tunnel"),
			ConfirmationSummary: vpnTunnelDelete.summary},
		capability.Operation{Descriptor: vpnGatewaysDelete, Handler: vpnGatewayDelete.handler("delete VPN gateway"),
			ConfirmationSummary: vpnGatewayDelete.summary},
		capability.Operation{Descriptor: instancesStart, Handler: instanceLifecycleHandler("start instance",
			instancesStartID, "start"), ConfirmationSummary: instanceLifecycleSummary(instancesStartID)},
		capability.Operation{Descriptor: instancesStop, Handler: instanceLifecycleHandler("stop instance",
			instancesStopID, "stop"), ConfirmationSummary: instanceLifecycleSummary(instancesStopID)},
		capability.Operation{Descriptor: instancesReset, Handler: instanceLifecycleHandler("reset instance",
			instancesResetID, "reset"), ConfirmationSummary: instanceLifecycleSummary(instancesResetID)},
		capability.Operation{Descriptor: instancesSetMachineType, Handler: instanceLifecycleHandler(
			"change machine type", instancesSetMachineTypeID, "setMachineType"),
			ConfirmationSummary: instanceLifecycleSummary(instancesSetMachineTypeID)},
		capability.Operation{Descriptor: instancesInsert, Handler: handleInstancesInsert,
			ConfirmationSummary: instanceInsertSummary},
		capability.Operation{Descriptor: instancesDelete, Handler: handleInstancesDelete,
			ConfirmationSummary: instanceDeleteSummary},
		capability.Operation{Descriptor: disksInsert, Handler: handleDisksInsert, ConfirmationSummary: diskInsertSummary},
		capability.Operation{Descriptor: disksResize, Handler: handleDisksResize, ConfirmationSummary: diskResizeSummary},
		capability.Operation{Descriptor: disksCreateSnapshot, Handler: handleDisksCreateSnapshot,
			ConfirmationSummary: diskSnapshotSummary},
		capability.Operation{Descriptor: disksDelete, Handler: handleDisksDelete, ConfirmationSummary: diskDeleteSummary},
		capability.Operation{Descriptor: imagesInsert, Handler: handleImagesInsert,
			ConfirmationSummary: imageInsertSummary},
		capability.Operation{Descriptor: snapshotsDelete, Handler: snapshotDelete.handler("delete snapshot"),
			ConfirmationSummary: snapshotDelete.summary},
		capability.Operation{Descriptor: imagesDelete, Handler: imageDelete.handler("delete image"),
			ConfirmationSummary: imageDelete.summary},
		capability.Operation{Descriptor: iamRolesList, Handler: handleIAMRolesList},
		capability.Operation{Descriptor: iamRolesGet, Handler: handleIAMRolesGet},
		capability.Operation{Descriptor: iamRolesQuery, Handler: handleIAMRolesQuery},
		capability.Operation{Descriptor: iamPermissionsQuery, Handler: handleIAMPermissionsQuery},
		capability.Operation{Descriptor: iamPoliciesGet, Handler: handleIAMPoliciesGet},
		capability.Operation{Descriptor: iamPermissionsTest, Handler: handleIAMPermissionsTest},
		capability.Operation{Descriptor: denyPoliciesList, Handler: handleDenyPoliciesList},
		capability.Operation{Descriptor: denyPoliciesGet, Handler: handleDenyPoliciesGet},
		capability.Operation{Descriptor: serviceAccountsList, Handler: handleServiceAccountsList},
		capability.Operation{Descriptor: serviceAccountsGet, Handler: handleServiceAccountsGet},
		capability.Operation{Descriptor: serviceAccountKeysList, Handler: handleServiceAccountKeysList},
		capability.Operation{Descriptor: serviceAccountKeysGet, Handler: handleServiceAccountKeysGet},
		capability.Operation{Descriptor: serviceAccountsCreate, Handler: handleServiceAccountsCreate,
			ConfirmationSummary: accountSummary(serviceAccountsCreateID)},
		capability.Operation{Descriptor: serviceAccountsPatch, Handler: handleServiceAccountsPatch,
			ConfirmationSummary: accountSummary(serviceAccountsPatchID)},
		capability.Operation{Descriptor: serviceAccountsDisable, Handler: handleServiceAccountsDisable,
			ConfirmationSummary: accountSummary(serviceAccountsDisableID)},
		capability.Operation{Descriptor: serviceAccountsEnable, Handler: handleServiceAccountsEnable,
			ConfirmationSummary: accountSummary(serviceAccountsEnableID)},
		capability.Operation{Descriptor: iamBindingsAdd, Handler: handleIAMBindingsAdd,
			ConfirmationSummary: bindingSummary(changeAdd)},
		capability.Operation{Descriptor: iamBindingsRemove, Handler: handleIAMBindingsRemove,
			ConfirmationSummary: bindingSummary(changeRemove)},
		capability.Operation{Descriptor: iamBindingsChange, Handler: handleIAMBindingsChange,
			ConfirmationSummary: bindingSummary(changeMove)},
		capability.Operation{Descriptor: serviceAccountKeysDisable, Handler: handleServiceAccountKeysDisable,
			ConfirmationSummary: revokeSummary(serviceAccountKeysDisableID)},
		capability.Operation{Descriptor: serviceAccountKeysEnable, Handler: handleServiceAccountKeysEnable,
			ConfirmationSummary: revokeSummary(serviceAccountKeysEnableID)},
		capability.Operation{Descriptor: serviceAccountKeysDelete, Handler: handleServiceAccountKeysDelete,
			ConfirmationSummary: revokeSummary(serviceAccountKeysDeleteID)},
		capability.Operation{Descriptor: serviceAccountsDelete, Handler: handleServiceAccountsDelete,
			ConfirmationSummary: revokeSummary(serviceAccountsDeleteID)},
		capability.Operation{Descriptor: apiKeysList, Handler: handleAPIKeysList},
		capability.Operation{Descriptor: apiKeysGet, Handler: handleAPIKeysGet},
		capability.Operation{Descriptor: apiKeysDelete, Handler: handleAPIKeysDelete,
			ConfirmationSummary: apiKeySummary(apiKeysDeleteID)},
		capability.Operation{Descriptor: apiKeysUndelete, Handler: handleAPIKeysUndelete,
			ConfirmationSummary: apiKeySummary(apiKeysUndeleteID)},
		capability.Operation{Descriptor: quotaInfosList, Handler: handleQuotaInfosList},
		capability.Operation{Descriptor: quotaInfosGet, Handler: handleQuotaInfosGet},
		capability.Operation{Descriptor: quotaPreferencesList, Handler: handleQuotaPreferencesList},
		capability.Operation{Descriptor: quotaPreferencesGet, Handler: handleQuotaPreferencesGet},
		capability.Operation{Descriptor: hmacKeysDeactivate, Handler: handleHMACKeysDeactivate,
			ConfirmationSummary: storageRevokeSummary(hmacKeysDeactivateID)},
		capability.Operation{Descriptor: hmacKeysDelete, Handler: handleHMACKeysDelete,
			ConfirmationSummary: storageRevokeSummary(hmacKeysDeleteID)},
		capability.Operation{Descriptor: notificationsDelete, Handler: handleNotificationsDelete,
			ConfirmationSummary: storageRevokeSummary(notificationsDeleteID)},
		capability.Operation{Descriptor: servicesEnable, Handler: handleServicesEnable,
			ConfirmationSummary: serviceChangeSummary(servicesEnableID)},
		capability.Operation{Descriptor: servicesBatchEnable, Handler: handleServicesBatchEnable,
			ConfirmationSummary: serviceChangeSummary(servicesBatchEnableID)},
		capability.Operation{Descriptor: servicesDisable, Handler: handleServicesDisable,
			ConfirmationSummary: serviceChangeSummary(servicesDisableID)},
		capability.Operation{Descriptor: projectsCreate, Handler: handleProjectsCreate,
			ConfirmationSummary: projectSummary(projectsCreateID)},
		capability.Operation{Descriptor: projectsPatch, Handler: handleProjectsPatch,
			ConfirmationSummary: projectSummary(projectsPatchID)},
		capability.Operation{Descriptor: projectsMove, Handler: handleProjectsMove,
			ConfirmationSummary: projectSummary(projectsMoveID)},
		capability.Operation{Descriptor: projectsUndelete, Handler: handleProjectsUndelete,
			ConfirmationSummary: projectSummary(projectsUndeleteID)},
		capability.Operation{Descriptor: foldersCreate, Handler: handleFoldersCreate,
			ConfirmationSummary: folderSummary(foldersCreateID)},
		capability.Operation{Descriptor: foldersPatch, Handler: handleFoldersPatch,
			ConfirmationSummary: folderSummary(foldersPatchID)},
		capability.Operation{Descriptor: foldersMove, Handler: handleFoldersMove,
			ConfirmationSummary: folderSummary(foldersMoveID)},
		capability.Operation{Descriptor: foldersUndelete, Handler: handleFoldersUndelete,
			ConfirmationSummary: folderSummary(foldersUndeleteID)},
		capability.Operation{Descriptor: projectsDelete, Handler: handleProjectsDelete,
			ConfirmationSummary: deleteSummary(projectsDeleteID)},
		capability.Operation{Descriptor: foldersDelete, Handler: handleFoldersDelete,
			ConfirmationSummary: deleteSummary(foldersDeleteID)})
}
