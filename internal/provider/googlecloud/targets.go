package googlecloud

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// The kinds of target. Organization, folder and project nest as Google nests them: an organization holds
// folders and projects, a folder holds folders and projects. A billing account belongs to none of them, so it
// is a flat kind of its own that only the tools for billing accounts read.
const (
	kindOrganization = "organization"
	kindFolder       = "folder"
	kindProject      = "project"
	kindBilling      = "billing-account"
)

const (
	prefixOrganization = "organizations/"
	prefixFolder       = "folders/"
	prefixProject      = "projects/"
	prefixBilling      = "billingAccounts/"
)

// maxFolderDepth is how many folders a parent chain may cross: Google nests folders at most ten deep, and one
// more step reaches the organization.
const maxFolderDepth = 11

var (
	numberPattern  = regexp.MustCompile(`^[0-9]{1,20}$`)
	projectIDShape = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	billingPattern = regexp.MustCompile(`^[A-Z0-9]{6}-[A-Z0-9]{6}-[A-Z0-9]{6}$`)
)

// validProject reports whether value is a project ID or a project number, the two names projects.get takes.
func validProject(value string) bool {
	return numberPattern.MatchString(value) || projectIDShape.MatchString(value)
}

// target is one parsed target of a connection.
type target struct {
	kind, id string
}

// parseTarget reads one target. The error never quotes the value.
func parseTarget(raw string) (target, error) {
	for _, form := range []struct {
		prefix, kind string
		valid        func(string) bool
	}{
		{prefixOrganization, kindOrganization, numberPattern.MatchString},
		{prefixFolder, kindFolder, numberPattern.MatchString},
		{prefixProject, kindProject, validProject},
		{prefixBilling, kindBilling, billingPattern.MatchString},
	} {
		if id, ok := strings.CutPrefix(raw, form.prefix); ok {
			if !form.valid(id) {
				return target{}, errors.New("a Google Cloud target holds an identifier in a form Google does not use")
			}
			return target{kind: form.kind, id: id}, nil
		}
	}
	return target{}, errors.New("a Google Cloud target must be organizations/ID, folders/ID, projects/ID, or billingAccounts/ID")
}

// allowlist is the targets of one connection. Without targets the connection is unbound and reaches what its
// credential reaches; the credential stays the upper bound either way.
type allowlist struct{ targets []target }

func allowlistOf(resolved *config.Resolved) (allowlist, error) {
	return parseAllowlist(provider.TargetsOf(resolved))
}

func parseAllowlist(values []string) (allowlist, error) {
	list := allowlist{targets: make([]target, 0, len(values))}
	for _, value := range values {
		parsed, err := parseTarget(value)
		if err != nil {
			return allowlist{}, err
		}
		if slices.Contains(list.targets, parsed) {
			return allowlist{}, errors.New("the Google Cloud target list names a target more than once")
		}
		list.targets = append(list.targets, parsed)
	}
	return list, nil
}

func (a allowlist) bound() bool { return len(a.targets) > 0 }

func (a allowlist) has(kind, id string) bool { return slices.Contains(a.targets, target{kind, id}) }

func (a allowlist) hasKind(kinds ...string) bool {
	return slices.ContainsFunc(a.targets, func(t target) bool { return slices.Contains(kinds, t.kind) })
}

// reachesResources reports whether the connection may read resources of the hierarchy: it is unbound or names
// an organization, a folder, or a project. A connection that names billing accounts alone reaches none.
func (a allowlist) reachesResources() bool {
	return !a.bound() || a.hasKind(kindOrganization, kindFolder, kindProject)
}

// reachesWithoutResource is the rule of a tool that is tied to no resource of the hierarchy, such as reading an
// operation: Qatlas cannot prove which target an operation belongs to, so the connection must be unbound or
// name an organization.
func (a allowlist) reachesWithoutResource() bool {
	return !a.bound() || a.hasKind(kindOrganization)
}

// reachesOrganizations is the rule of a tool that reads organizations themselves: the binding must name an
// organization expressly. A connection without targets does not, because its credential may reach
// organizations the person never meant to hand over.
func (a allowlist) reachesOrganizations() bool { return a.hasKind(kindOrganization) }

// reachesFolders reports whether a connection may search folders: it is unbound, or names an organization or a
// folder. A project holds no folders.
func (a allowlist) reachesFolders() bool {
	return !a.bound() || a.hasKind(kindOrganization, kindFolder)
}

// allowsBillingAccount reports whether the binding names the billing account itself. Billing accounts belong to
// no organization, folder, or project, so even an unbound connection does not reach one.
func (a allowlist) allowsBillingAccount(id string) bool { return a.has(kindBilling, id) }

// reachesBillingAccounts is the rule of the tools of billing accounts: the binding must name a billing account.
func (a allowlist) reachesBillingAccounts() bool { return a.hasKind(kindBilling) }

// reachesBilledProjects is the rule of the list of the projects of a billing account: the binding names a
// billing account and, to bound the rows, an organization, a folder, or a project.
func (a allowlist) reachesBilledProjects() bool {
	return a.hasKind(kindBilling) && a.hasKind(kindOrganization, kindFolder, kindProject)
}

// refuseOutside is the error of a resource that lies outside the targets. It never names the resource.
func refuseOutside(what string) error {
	return badRequest(what + " is outside the targets of this connection")
}

// decidesProject reports before any request whether the targets already settle a project given by name. Only
// project targets of the same spelling, ID or number, can refuse it without a read: a folder or an organization
// may hold the project, and a project may be named by its number in one place and by its ID in another.
func (a allowlist) decidesProject(name string) (allowed, decided bool) {
	if !a.bound() {
		return true, true
	}
	if !a.hasKind(kindOrganization, kindFolder, kindProject) {
		return false, true
	}
	if a.hasKind(kindOrganization, kindFolder) {
		return false, false
	}
	number := numberPattern.MatchString(name)
	for _, t := range a.targets {
		if t.kind != kindProject {
			continue
		}
		if t.id == name {
			return true, true
		}
		if numberPattern.MatchString(t.id) != number {
			return false, false
		}
	}
	return false, true
}

// maxProofReads bounds the reads one call spends on proving where resources sit. A read waits for the rate
// limit of the credential, so an unbounded chain per row would outlast the invoke limit.
const maxProofReads = 30

// admitter binds resources that Google returned to the targets. It remembers which folders it has already
// placed, so rows below the same folder cost one walk of the parent chain, not one each.
type admitter struct {
	list     allowlist
	c        *client
	op, tool string
	reads    int
	placed   map[string]bool
}

func newAdmitter(list allowlist, c *client, op, tool string) *admitter {
	return &admitter{list: list, c: c, op: op, tool: tool, placed: map[string]bool{}}
}

// project binds a project that was read to the targets: a project target that names it by number or ID, or a
// folder or organization target on its parent chain. The chain is walked with folders.get, the one reading
// that proves a folder's parent; it stops at the first match, at the organization, and at the end of a project
// without a parent.
func (a *admitter) project(ctx context.Context, p projectWire) (bool, error) {
	if !a.list.bound() {
		return true, nil
	}
	if a.list.has(kindProject, strings.TrimPrefix(p.Name, prefixProject)) || p.ProjectID != "" && a.list.has(kindProject, p.ProjectID) {
		return true, nil
	}
	return a.parent(ctx, p.Parent)
}

// folder binds a folder that was read to the targets: it is a folder target, or lies below a target.
func (a *admitter) folder(ctx context.Context, f folderWire) (bool, error) {
	if !a.list.bound() || a.list.has(kindFolder, strings.TrimPrefix(f.Name, prefixFolder)) {
		return true, nil
	}
	return a.parent(ctx, f.Parent)
}

// organization binds an organization row to the targets. It needs no read: only an organization target
// names an organization.
func (a *admitter) organization(o organizationWire) bool {
	return a.list.has(kindOrganization, strings.TrimPrefix(o.Name, prefixOrganization))
}

func (a *admitter) parent(ctx context.Context, parent string) (bool, error) {
	if !a.list.hasKind(kindOrganization, kindFolder) {
		return false, nil
	}
	var visited []string
	settle := func(admitted bool) (bool, error) {
		for _, id := range visited {
			a.placed[id] = admitted
		}
		return admitted, nil
	}
	for range maxFolderDepth {
		switch {
		case strings.HasPrefix(parent, prefixOrganization):
			return settle(a.list.has(kindOrganization, strings.TrimPrefix(parent, prefixOrganization)))
		case strings.HasPrefix(parent, prefixFolder):
			id := strings.TrimPrefix(parent, prefixFolder)
			if a.list.has(kindFolder, id) {
				return settle(true)
			}
			if placed, known := a.placed[id]; known {
				return settle(placed)
			}
			if slices.Contains(visited, id) || !numberPattern.MatchString(id) {
				return settle(false)
			}
			if a.reads >= maxProofReads {
				return false, badRequest("too many folders to read for the targets of this connection; narrow " +
					"the search with a parent filter or a smaller page_size")
			}
			a.reads++
			visited = append(visited, id)
			folder, err := a.c.folder(ctx, a.op, a.tool, id)
			if err != nil {
				return false, err
			}
			parent = folder.Parent
		default:
			return settle(false)
		}
	}
	return settle(false)
}

// targetReaches says whether a connection bound to targets reaches a tool, so a tool the targets cannot serve
// is not offered. A value that does not parse is left to the refusal at the call.
func targetReaches(tool string, targets []string) (bool, []string) {
	list, err := parseAllowlist(targets)
	if err != nil {
		return true, nil
	}
	switch tool {
	case billingAccountsListID, billingAccountsGetID:
		if !list.reachesBillingAccounts() {
			return false, []string{"billingAccounts/BILLING_ACCOUNT"}
		}
	case billingAccountsListProjectsID:
		if !list.reachesBilledProjects() {
			return false, []string{"billingAccounts/BILLING_ACCOUNT with organizations/ORGANIZATION, " +
				"folders/FOLDER, or projects/PROJECT"}
		}
	case projectsGetID, projectBillingInfoGetID, projectsSearchID, servicesListID, servicesGetID, servicesBatchGetID, iamPoliciesGetID,
		iamPermissionsTestID, denyPoliciesListID, denyPoliciesGetID, serviceAccountsListID, serviceAccountsGetID,
		serviceAccountKeysListID, serviceAccountKeysGetID, iamBindingsAddID, iamBindingsRemoveID, iamBindingsChangeID:
		if !list.reachesResources() {
			return false, []string{"organizations/ORGANIZATION", "folders/FOLDER", "projects/PROJECT"}
		}
	case instancesListID, instancesAggregatedListID, instancesGetID, machineTypesListID, zonesListID, regionsListID,
		computeOperationsGetID, computeOperationsListID, networksListID, networksGetID, subnetworksListID, subnetworksGetID,
		firewallsListID, firewallsGetID, addressesListID, addressesGetID,
		vpnGatewaysListID, vpnGatewaysGetID, vpnTunnelsListID, vpnTunnelsGetID,
		disksListID, disksAggregatedListID, disksGetID,
		snapshotsListID, snapshotsGetID, imagesListID, imagesGetID,
		firewallsDeleteID, addressesDeleteID, vpnTunnelsDeleteID, vpnGatewaysDeleteID,
		instancesStartID, instancesStopID, instancesResetID, instancesSetMachineTypeID,
		instancesInsertID, instancesDeleteID,
		disksInsertID, disksResizeID, disksCreateSnapshotID,
		disksDeleteID, imagesInsertID, imagesDeleteID, snapshotsDeleteID,
		bucketsListID, bucketsGetID, bucketsGetIAMPolicyID, bucketAccessControlsListID,
		notificationsListID, managedFoldersListID, hmacKeysListID, hmacKeysGetID,
		objectsListID, objectsGetID, objectsDownloadID, objectAccessControlsListID,
		managedZonesListID, managedZonesGetID, recordSetsListID, recordSetsGetID:
		if !list.reachesResources() {
			return false, []string{"organizations/ORGANIZATION", "folders/FOLDER", "projects/PROJECT"}
		}
	case projectsPatchID, projectsUndeleteID:
		if !list.reachesResources() {
			return false, []string{"organizations/ORGANIZATION", "folders/FOLDER", "projects/PROJECT"}
		}
	case projectsCreateID, projectsMoveID:
		// A project is created or moved only under an organization or a folder.
		if !list.reachesFolders() {
			return false, []string{"organizations/ORGANIZATION", "folders/FOLDER"}
		}
	case foldersSearchID, foldersGetID, foldersListID:
		if !list.reachesFolders() {
			return false, []string{"organizations/ORGANIZATION", "folders/FOLDER"}
		}
	case projectsListID:
		if !list.reachesFolders() {
			return false, []string{"organizations/ORGANIZATION", "folders/FOLDER"}
		}
	case organizationsSearchID, organizationsGetID:
		if !list.reachesOrganizations() {
			return false, []string{"organizations/ORGANIZATION"}
		}
	case operationsGetID:
		if !list.reachesWithoutResource() {
			return false, []string{"organizations/ORGANIZATION"}
		}
	case iamRolesListID, iamRolesGetID, iamRolesQueryID, iamPermissionsQueryID:
		if !list.reachesResources() {
			return false, []string{"organizations/ORGANIZATION", "folders/FOLDER", "projects/PROJECT"}
		}
	}
	return true, nil
}

var targetMetadata = config.TargetMetadata{
	Label:    "organization, folder, project, or billing account",
	Multiple: true,
	Description: "optional allow-list of organizations/ID, folders/ID, projects/ID, and billingAccounts/ID; an " +
		"organization takes in its folders and projects, a folder its subfolders and projects; a billing account " +
		"belongs to none of them and is reached only by a connection that names it; without targets a connection " +
		"reaches whatever its credential reaches, except billing accounts",
	Kinds: []config.TargetKind{{
		Name: kindOrganization, Description: "one organization with every folder and project in it",
		Forms: []string{"organizations/ORGANIZATION"},
	}, {
		Name: kindFolder, Description: "one folder with its subfolders and projects",
		Forms: []string{"folders/FOLDER"}, Parents: []string{kindOrganization}, Nested: true, Top: true,
	}, {
		Name: kindProject, Description: "one project, by its ID or its number",
		Forms: []string{"projects/PROJECT"}, Parents: []string{kindFolder, kindOrganization}, Top: true,
	}, {
		Name: kindBilling, Description: "one billing account, which belongs to no organization, folder, or project",
		Forms: []string{"billingAccounts/BILLING_ACCOUNT"},
	}},
	Validate: func(raw string) error {
		_, err := parseTarget(raw)
		return err
	},
	ValidateSet: func(values []string) error {
		_, err := parseAllowlist(values)
		return err
	},
	Reaches: targetReaches,
}
