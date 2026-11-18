package googlecloud

import "github.com/castrowithcee/qatlas-connect/internal/config"

// scopeReadOnly is the smallest scope of the Resource Manager reads, which the discovery document of
// cloudresourcemanager v3 lists next to cloud-platform for every method of this package, and of the Service
// Usage reads, for which serviceusage v1 lists it for services.list, get, and batchGet.
const scopeReadOnly = "https://www.googleapis.com/auth/cloud-platform.read-only"

// scopeCloudPlatform is the one scope the discovery documents of iam v1 and v2 list for the role, policy,
// deny policy, and permission reads, and of cloudresourcemanager v3 for folders' testIamPermissions; none of
// them lists a read-only variant.
const scopeCloudPlatform = "https://www.googleapis.com/auth/cloud-platform"

// scopeAPIKeys is the narrowest scope that apikeys v2 lists for keys.delete and keys.undelete, next to
// cloud-platform; both reads list cloud-platform.read-only as well, which the profile read uses.
const scopeAPIKeys = "https://www.googleapis.com/auth/apikeys"

// scopeComputeReadOnly is the smallest scope the discovery document of compute v1 lists for the read methods
// of instances, machineTypes, zones, regions, and the Compute operations (next to compute and cloud-platform).
const scopeComputeReadOnly = "https://www.googleapis.com/auth/compute.readonly"

// scopeCompute is the smallest scope the discovery document of compute v1 lists for firewalls.delete and
// addresses.delete, next to cloud-platform.
const scopeCompute = "https://www.googleapis.com/auth/compute"

// scopeStorageReadOnly is the smallest scope the discovery document of storage v1 lists for buckets.list and
// buckets.get (next to devstorage.read_write, devstorage.full_control, and the cloud-platform scopes).
const scopeStorageReadOnly = "https://www.googleapis.com/auth/devstorage.read_only"

// scopeStorageFullControl is the smallest scope the discovery document of storage v1 lists for
// buckets.getIamPolicy and bucketAccessControls.list, next to cloud-platform; neither lists a read-only variant.
const scopeStorageFullControl = "https://www.googleapis.com/auth/devstorage.full_control"

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
	servicesListID:        {scopeReadOnly},
	servicesGetID:         {scopeReadOnly},
	servicesBatchGetID:    {scopeReadOnly},
	iamRolesListID:        {scopeCloudPlatform},
	iamRolesGetID:         {scopeCloudPlatform},
	iamRolesQueryID:       {scopeCloudPlatform},
	iamPermissionsQueryID: {scopeCloudPlatform},
	iamPoliciesGetID:      {scopeCloudPlatform},
	iamPermissionsTestID:  {scopeCloudPlatform},
	denyPoliciesListID:    {scopeCloudPlatform},
	denyPoliciesGetID:     {scopeCloudPlatform},
	// iam v1 lists only cloud-platform for projects.serviceAccounts list and get and for keys list and get.
	serviceAccountsListID:    {scopeCloudPlatform},
	serviceAccountsGetID:     {scopeCloudPlatform},
	serviceAccountKeysListID: {scopeCloudPlatform},
	serviceAccountKeysGetID:  {scopeCloudPlatform},
	// iam v1 lists only cloud-platform for projects.serviceAccounts create, patch, enable, and disable.
	serviceAccountsCreateID:  {scopeCloudPlatform},
	serviceAccountsPatchID:   {scopeCloudPlatform},
	serviceAccountsDisableID: {scopeCloudPlatform},
	serviceAccountsEnableID:  {scopeCloudPlatform},
	// cloudresourcemanager v3 lists only cloud-platform for setIamPolicy of projects, folders, and organizations.
	iamBindingsAddID:    {scopeCloudPlatform},
	iamBindingsRemoveID: {scopeCloudPlatform},
	iamBindingsChangeID: {scopeCloudPlatform},
	// iam v1 lists only cloud-platform for projects.serviceAccounts delete and keys disable, enable, and delete.
	serviceAccountKeysDisableID: {scopeCloudPlatform},
	serviceAccountKeysEnableID:  {scopeCloudPlatform},
	serviceAccountKeysDeleteID:  {scopeCloudPlatform},
	serviceAccountsDeleteID:     {scopeCloudPlatform},
	// apikeys v2 lists cloud-platform.read-only for keys.list and get, and apikeys for keys.delete and undelete.
	apiKeysListID:     {scopeReadOnly},
	apiKeysGetID:      {scopeReadOnly},
	apiKeysDeleteID:   {scopeAPIKeys},
	apiKeysUndeleteID: {scopeAPIKeys},
	// cloudquotas v1 lists only cloud-platform for quotaInfos and quotaPreferences list and get.
	quotaInfosListID:       {scopeCloudPlatform},
	quotaInfosGetID:        {scopeCloudPlatform},
	quotaPreferencesListID: {scopeCloudPlatform},
	quotaPreferencesGetID:  {scopeCloudPlatform},
	// serviceusage v1 lists only cloud-platform for services enable, batchEnable, and disable.
	servicesEnableID:      {scopeCloudPlatform},
	servicesBatchEnableID: {scopeCloudPlatform},
	servicesDisableID:     {scopeCloudPlatform},
	// compute v1 lists compute.readonly as the smallest scope for the reads of instances and machine types.
	instancesListID:           {scopeComputeReadOnly},
	instancesAggregatedListID: {scopeComputeReadOnly},
	instancesGetID:            {scopeComputeReadOnly},
	machineTypesListID:        {scopeComputeReadOnly},
	zonesListID:               {scopeComputeReadOnly},
	regionsListID:             {scopeComputeReadOnly},
	computeOperationsGetID:    {scopeComputeReadOnly},
	computeOperationsListID:   {scopeComputeReadOnly},
	networksListID:            {scopeComputeReadOnly},
	networksGetID:             {scopeComputeReadOnly},
	subnetworksListID:         {scopeComputeReadOnly},
	subnetworksGetID:          {scopeComputeReadOnly},
	firewallsListID:           {scopeComputeReadOnly},
	firewallsGetID:            {scopeComputeReadOnly},
	addressesListID:           {scopeComputeReadOnly},
	addressesGetID:            {scopeComputeReadOnly},
	vpnGatewaysListID:         {scopeComputeReadOnly},
	vpnGatewaysGetID:          {scopeComputeReadOnly},
	vpnTunnelsListID:          {scopeComputeReadOnly},
	vpnTunnelsGetID:           {scopeComputeReadOnly},
	disksListID:               {scopeComputeReadOnly},
	disksAggregatedListID:     {scopeComputeReadOnly},
	disksGetID:                {scopeComputeReadOnly},
	snapshotsListID:           {scopeComputeReadOnly},
	snapshotsGetID:            {scopeComputeReadOnly},
	imagesListID:              {scopeComputeReadOnly},
	imagesGetID:               {scopeComputeReadOnly},
	// storage v1 lists devstorage.read_only for buckets.list and get, and only devstorage.full_control and
	// cloud-platform for buckets.getIamPolicy and bucketAccessControls.list.
	bucketsListID:              {scopeStorageReadOnly},
	bucketsGetID:               {scopeStorageReadOnly},
	bucketsGetIAMPolicyID:      {scopeStorageFullControl},
	bucketAccessControlsListID: {scopeStorageFullControl},
	// compute v1 lists compute as the smallest scope for the deletions of firewalls, addresses, VPN tunnels, and VPN gateways.
	firewallsDeleteID:   {scopeCompute},
	addressesDeleteID:   {scopeCompute},
	vpnTunnelsDeleteID:  {scopeCompute},
	vpnGatewaysDeleteID: {scopeCompute},
	// compute v1 lists compute as the smallest scope for start, stop, reset, and setMachineType of instances.
	instancesStartID:          {scopeCompute},
	instancesStopID:           {scopeCompute},
	instancesResetID:          {scopeCompute},
	instancesSetMachineTypeID: {scopeCompute},
	// compute v1 lists compute as the smallest scope for instances.insert and instances.delete.
	instancesInsertID: {scopeCompute},
	instancesDeleteID: {scopeCompute},
}}
