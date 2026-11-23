package googlemerchant

import (
	"encoding/json"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

const (
	regionsBatchNote = "One call sends exactly one request to Google for all entries (1 to 100, the documented " +
		"maximum), so a batch counts as one call against the quota, however many regions it holds; Qatlas never " +
		"splits or repeats it. Google applies a batch all or none: if one entry fails, no change is made. The " +
		"region IDs of one batch must be distinct, and one invalid entry refuses the whole call before any request. "
	regionsDeleteNote = "Deleting a region removes its definition from the account. Shipping settings and regional " +
		"inventory that use the region can be affected, and nothing in the Google documentation says that a " +
		"deleted region can be restored, so treat it as permanent. Google documents that deleting a region that " +
		"does not exist is no error. The request is sent once and never repeated. "
)

var (
	regionEntryMembers = []string{member("region_id", regionIDSchema), member("display_name", regionDisplayNameSchema),
		member("postal_code_area", postalAreaSchema), member("geotarget_area", geotargetAreaSchema),
		member("radius_area", radiusAreaSchema)}
	batchCreateEntrySchema = closedSchemaOf([]string{"region_id"}, regionEntryMembers...)
	batchUpdateEntrySchema = closedSchemaOf([]string{"region_id", "update_mask"},
		append([]string{member("update_mask", updateMaskSchema)}, regionEntryMembers...)...)
	batchCap = strconv.Itoa(maxRegionBatch)

	regionBatchResultSchema = objectSchema(member("accepted", boolSchema),
		member("requested", `{"type":"integer"}`),
		member("regions", `{"type":"array","items":`+regionResourceSchema+`}`))
	regionBatchDeleteSchema = objectSchema(member("accepted", boolSchema),
		member("requested", `{"type":"integer"}`), member("names", listSchema))
	regionDeleteSchema = objectSchema(member("accepted", boolSchema), member("name", stringSchema))

	regionBatchFields = []capability.Field{
		{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
		{Name: "requested", Description: "Number of entries sent in the one request"},
		{Name: "regions", Description: "The regions as Google returned them, at most 100, with " + regionFieldsDescription},
	}
	regionAccepted = capability.Field{Name: "accepted",
		Description: "true when Google accepted the request; nothing is said about processing"}
)

func regionBatchArgument(description string) capability.Argument {
	return capability.Argument{Name: "regions", Required: true, Description: description}
}

var regionsDelete = capability.Descriptor{
	ID: regionsDeleteID, Version: 1, Title: "Delete a Merchant Center region",
	Description: "Delete one region of a Merchant Center account. " + regionsDeleteNote + regionsAdminNote +
		regionsDelayNote + "The confirmation shows the account and the region ID. " + regionsUntrusted,
	Tags: []string{"google", "merchant", "regions", "delete"}, Risk: regionChangeRisk(capability.EffectDelete,
		capability.IdempotencyIdempotent), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"region_id"}, member("account", accountArgSchema),
		member("region_id", regionIDSchema)),
	OutputSchema: json.RawMessage(regionDeleteSchema),
	Arguments:    []capability.Argument{accountArgument, regionIDArgument},
	Fields: []capability.Field{regionAccepted,
		{Name: "name", Description: "Name of the region that was deleted: accounts/ACCOUNT/regions/REGION"}},
	Examples: []capability.Example{{Description: "Delete one region",
		Arguments: json.RawMessage(`{"account":"1000001","region_id":"north-1"}`)}},
}

var regionsBatchCreate = capability.Descriptor{
	ID: regionsBatchCreateID, Version: 1, Title: "Create Merchant Center regions in one batch",
	Description: "Create 1 to " + batchCap + " regions in a Merchant Center account with one request. Each entry " +
		"has the arguments of regions.create: region_id, an optional display_name, and exactly one area. " +
		regionsBatchNote + regionsAdminNote + regionsAreaNote + "No region_id may exist yet. " + regionsDelayNote +
		"The confirmation shows the account, the number of regions, and their IDs. " + regionsUntrusted,
	Tags: []string{"google", "merchant", "regions", "batch"}, Risk: regionChangeRisk(capability.EffectCreate,
		capability.IdempotencyNonIdempotent), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"regions"}, member("account", accountArgSchema),
		member("regions", `{"type":"array","minItems":1,"maxItems":`+batchCap+`,"items":`+batchCreateEntrySchema+`}`)),
	OutputSchema: json.RawMessage(regionBatchResultSchema),
	Arguments: []capability.Argument{accountArgument, regionBatchArgument("Regions to create, 1 to " + batchCap +
		"; each with region_id, optional display_name, and one of postal_code_area, geotarget_area, radius_area")},
	Fields: regionBatchFields,
	Examples: []capability.Example{{Description: "Two regions in one request",
		Arguments: json.RawMessage(`{"account":"1000001","regions":[{"region_id":"north-1","display_name":"North",` +
			`"postal_code_area":{"regionCode":"US","postalCodes":[{"begin":"94108"}]}},` +
			`{"region_id":"east-1","geotarget_area":{"geotargetCriteriaIds":["21137"]}}]}`)}},
}

var regionsBatchUpdate = capability.Descriptor{
	ID: regionsBatchUpdateID, Version: 1, Title: "Update Merchant Center regions in one batch",
	Description: "Change 1 to " + batchCap + " regions of a Merchant Center account with one request. Each entry has " +
		"the arguments of regions.update: region_id, update_mask (from displayName, postalCodeArea, geotargetArea, " +
		"and radiusArea, each replacing the whole member), and the members it names. Shipping settings and " +
		"regional inventory that use the regions follow the change. " + regionsBatchNote + regionsAdminNote +
		regionsAreaNote + regionsDelayNote + "The confirmation shows the account, the number of regions, and their " +
		"IDs. " + regionsUntrusted,
	Tags: []string{"google", "merchant", "regions", "batch"}, Risk: regionChangeRisk(capability.EffectUpdate,
		capability.IdempotencyIdempotent), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"regions"}, member("account", accountArgSchema),
		member("regions", `{"type":"array","minItems":1,"maxItems":`+batchCap+`,"items":`+batchUpdateEntrySchema+`}`)),
	OutputSchema: json.RawMessage(regionBatchResultSchema),
	Arguments: []capability.Argument{accountArgument, regionBatchArgument("Regions to change, 1 to " + batchCap +
		"; each with region_id, update_mask, and the members that the mask names")},
	Fields: regionBatchFields,
	Examples: []capability.Example{{Description: "Rename two regions in one request",
		Arguments: json.RawMessage(`{"account":"1000001","regions":[{"region_id":"north-1","update_mask":["displayName"],` +
			`"display_name":"North and east"},{"region_id":"east-1","update_mask":["displayName"],"display_name":"East"}]}`)}},
}

var regionsBatchDelete = capability.Descriptor{
	ID: regionsBatchDeleteID, Version: 1, Title: "Delete Merchant Center regions in one batch",
	Description: "Delete 1 to " + batchCap + " regions of a Merchant Center account with one request. " +
		regionsBatchNote + regionsDeleteNote + regionsAdminNote + regionsDelayNote +
		"The confirmation shows the account, the number of regions, and their IDs. " + regionsUntrusted,
	Tags: []string{"google", "merchant", "regions", "batch", "delete"}, Risk: regionChangeRisk(capability.EffectDelete,
		capability.IdempotencyIdempotent), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"region_ids"}, member("account", accountArgSchema),
		member("region_ids", `{"type":"array","minItems":1,"maxItems":`+batchCap+`,"uniqueItems":true,"items":`+
			regionIDSchema+`}`)),
	OutputSchema: json.RawMessage(regionBatchDeleteSchema),
	Arguments: []capability.Argument{accountArgument, {Name: "region_ids", Required: true,
		Description: "IDs of the regions to delete, 1 to " + batchCap + ", distinct; each in the form of region_id"}},
	Fields: []capability.Field{regionAccepted,
		{Name: "requested", Description: "Number of regions named in the one request"},
		{Name: "names", Description: "Names of the regions of the request: accounts/ACCOUNT/regions/REGION"}},
	Examples: []capability.Example{{Description: "Delete two regions in one request",
		Arguments: json.RawMessage(`{"account":"1000001","region_ids":["north-1","east-1"]}`)}},
}
