package googlecloud

import "github.com/castrowithcee/qatlas-connect/internal/config"

// scopeReadOnly is the smallest scope of the Resource Manager reads, which the discovery document of
// cloudresourcemanager v3 lists next to cloud-platform for every method of this package.
const scopeReadOnly = "https://www.googleapis.com/auth/cloud-platform.read-only"

// scopeCloudPlatform is the one scope that IAM v2 deny policies, IAM v1 service account policies, and folders'
// testIamPermissions list in their discovery documents; none of them lists a smaller one.
const scopeCloudPlatform = "https://www.googleapis.com/auth/cloud-platform"

// tokenMetadata maps every tool to the smallest scopes it needs. The core derives the scopes of a connection
// from it, so a tool that is not offered adds none.
var tokenMetadata = config.TokenMetadata{Scopes: map[string][]string{
	organizationsSearchID: {scopeReadOnly},
	foldersSearchID:       {scopeReadOnly},
	projectsSearchID:      {scopeReadOnly},
	projectsGetID:         {scopeReadOnly},
	operationsGetID:       {scopeReadOnly},
	iamPoliciesGetID:      {scopeCloudPlatform},
	iamPermissionsTestID:  {scopeCloudPlatform},
	denyPoliciesListID:    {scopeCloudPlatform},
	denyPoliciesGetID:     {scopeCloudPlatform},
}}
