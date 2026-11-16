package googlecloud

import "github.com/castrowithcee/qatlas-connect/internal/config"

// scopeReadOnly is the smallest scope of the Resource Manager reads, which the discovery document of
// cloudresourcemanager v3 lists next to cloud-platform for every method of this package.
const scopeReadOnly = "https://www.googleapis.com/auth/cloud-platform.read-only"

// scopeCloudPlatform is the one scope the discovery document of iam v1 lists for every method of the role and
// permission reads (its Google documentation page names https://www.googleapis.com/auth/iam as a second one,
// which the discovery document does not list). No read-only variant exists for these methods.
const scopeCloudPlatform = "https://www.googleapis.com/auth/cloud-platform"

// tokenMetadata maps every tool to the smallest scopes it needs. The core derives the scopes of a connection
// from it, so a tool that is not offered adds none.
var tokenMetadata = config.TokenMetadata{Scopes: map[string][]string{
	organizationsSearchID: {scopeReadOnly},
	foldersSearchID:       {scopeReadOnly},
	projectsSearchID:      {scopeReadOnly},
	projectsGetID:         {scopeReadOnly},
	operationsGetID:       {scopeReadOnly},
	organizationsGetID:    {scopeReadOnly},
	foldersGetID:          {scopeReadOnly},
	foldersListID:         {scopeReadOnly},
	projectsListID:        {scopeReadOnly},
	iamRolesListID:        {scopeCloudPlatform},
	iamRolesGetID:         {scopeCloudPlatform},
	iamRolesQueryID:       {scopeCloudPlatform},
	iamPermissionsQueryID: {scopeCloudPlatform},
}}
