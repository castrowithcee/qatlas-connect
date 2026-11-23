package googlemerchant

import (
	"encoding/json"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

const (
	regionFieldsDescription = "name (accounts/ACCOUNT/regions/REGION), displayName, postalCodeArea (regionCode, " +
		"postalCodes with begin and end), geotargetArea (geotargetCriteriaIds), radiusArea (radius, latLng, " +
		"regionCode, radiusUnits), shippingEligible, and regionalInventoryEligible"
	regionsUntrusted = "Display names are untrusted content of whoever runs the account: texts are cut, lists " +
		"are capped, and members that are not listed are never shown."
	regionsAdminNote = "Changing regions needs the administrator role in the Merchant Center account; Google " +
		"refuses the call otherwise. "
	regionsDelayNote = "Google applies a change afterwards: the region, and the shipping settings and regional " +
		"inventory that use it, can take some minutes to follow, and the result says only that Google accepted " +
		"the request. "
	regionsAreaNote = "A region has exactly one area: postal_code_area (a two-letter regionCode and 1 to 250 " +
		"postalCodes ranges, each a begin and an optional end that is a pattern with a pattern and a code with a " +
		"code, such as 94108 or 9410*), geotarget_area (1 to 100 geotargetCriteriaIds that Google requires to be " +
		"of one location type, available in some countries), or radius_area (radius from 0.01 to 100000, latLng, " +
		"regionCode, and optional radiusUnits MILES or KILOMETERS). The caps are Qatlas' own; Google documents " +
		"none. "
)

var (
	regionIDSchema = `{"type":"string","pattern":"^` + regionIDPattern + `$","maxLength":` +
		strconv.Itoa(maxRegionIDLen) + `}`
	regionPageSizeSchema    = `{"type":"integer","minimum":1,"maximum":` + strconv.Itoa(maxRegionRows) + `}`
	regionDisplayNameSchema = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxRegionNameRunes) + `}`
	territorySchema         = `{"type":"string","pattern":"^[A-Z]{2}$"}`
	postalCodeSchema        = `{"type":"string","pattern":"` + postalCodePattern + `","maxLength":21}`
	postalAreaSchema        = closedSchemaOf([]string{"regionCode", "postalCodes"}, member("regionCode", territorySchema),
		member("postalCodes", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxPostalRanges)+`,"items":`+
			closedSchemaOf([]string{"begin"}, member("begin", postalCodeSchema), member("end", postalCodeSchema))+`}`))
	geotargetAreaSchema = closedSchemaOf([]string{"geotargetCriteriaIds"},
		member("geotargetCriteriaIds", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxGeotargets)+
			`,"uniqueItems":true,"items":{"type":"string","pattern":"^`+idPattern+`$"}}`))
	radiusAreaSchema = closedSchemaOf([]string{"radius", "latLng", "regionCode"},
		member("radius", `{"type":"number","minimum":0.01,"maximum":100000}`),
		member("latLng", closedSchemaOf([]string{"latitude", "longitude"},
			member("latitude", `{"type":"number","minimum":-90,"maximum":90}`),
			member("longitude", `{"type":"number","minimum":-180,"maximum":180}`))),
		member("regionCode", territorySchema),
		member("radiusUnits", `{"type":"string","enum":["MILES","KILOMETERS"]}`))
	updateMaskSchema = `{"type":"array","minItems":1,"maxItems":4,"uniqueItems":true,` +
		`"items":{"type":"string","enum":["displayName","postalCodeArea","geotargetArea","radiusArea"]}}`

	numberSchema         = `{"type":"number"}`
	regionResourceSchema = objectSchema(member("name", stringSchema), member("displayName", stringSchema),
		member("postalCodeArea", objectSchema(member("regionCode", stringSchema), member("postalCodes",
			`{"type":"array","items":`+objectSchema(member("begin", stringSchema), member("end", stringSchema))+`}`))),
		member("geotargetArea", objectSchema(member("geotargetCriteriaIds", listSchema))),
		member("radiusArea", objectSchema(member("radius", numberSchema), member("latLng", objectSchema(
			member("latitude", numberSchema), member("longitude", numberSchema))), member("regionCode", stringSchema),
			member("radiusUnits", stringSchema))),
		member("shippingEligible", boolSchema), member("regionalInventoryEligible", boolSchema))
	regionChangeSchema = objectSchema(member("accepted", boolSchema), member("region", regionResourceSchema))
)

func closedSchemaOf(required []string, members ...string) string {
	return string(inputSchema(required, members...))
}

var regionIDArgument = capability.Argument{Name: "region_id", Required: true, Description: "ID of the region: 1 to " +
	"64 ASCII letters, digits, dashes, and underscores, starting with a letter or digit; Google documents no form, " +
	"so a region with another ID cannot be reached here"}

var regionsList = capability.Descriptor{
	ID: regionsListID, Version: 1, Title: "List Merchant Center regions",
	Description: "List the regions defined in a Merchant Center account, a batch per call with a cursor to read " +
		"on. page_size is 1 to 1000; Google sends up to 50 when it is left out. " + regionsUntrusted,
	Tags: []string{"google", "merchant", "regions"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema), member("page_size", regionPageSizeSchema),
		member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("regions", regionResourceSchema)),
	Arguments: []capability.Argument{accountArgument,
		{Name: "page_size", Description: "Regions per batch, 1 to 1000"}, cursorArgument},
	Fields: []capability.Field{
		{Name: "regions", Description: "Regions with " + regionFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "First batch of regions", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var regionsGet = capability.Descriptor{
	ID: regionsGetID, Version: 1, Title: "Get a Merchant Center region",
	Description: "Read one region of a Merchant Center account. After a region is created or changed it can " +
		"take some minutes until the change shows. " + regionsUntrusted,
	Tags: []string{"google", "merchant", "regions"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"region_id"}, member("account", accountArgSchema),
		member("region_id", regionIDSchema)),
	OutputSchema: json.RawMessage(regionResourceSchema),
	Arguments:    []capability.Argument{accountArgument, regionIDArgument},
	Fields:       []capability.Field{{Name: "region", Description: regionFieldsDescription}},
	Examples: []capability.Example{{Description: "One region",
		Arguments: json.RawMessage(`{"account":"1000001","region_id":"north-1"}`)}},
}

var regionsCreate = capability.Descriptor{
	ID: regionsCreateID, Version: 1, Title: "Create a Merchant Center region",
	Description: "Create a region in a Merchant Center account. Shipping settings and regional inventory use " +
		"regions to decide where offers are shown and shipped, so a region changes what Google Shopping shows. " +
		regionsAdminNote + regionsAreaNote + "region_id must not exist yet; Google refuses a repeated ID. " +
		regionsDelayNote + "The confirmation shows the account, the region ID, and the kind of area. " +
		regionsUntrusted,
	Tags: []string{"google", "merchant", "regions"}, Risk: regionChangeRisk(capability.EffectCreate,
		capability.IdempotencyNonIdempotent), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"region_id"}, member("account", accountArgSchema),
		member("region_id", regionIDSchema), member("display_name", regionDisplayNameSchema),
		member("postal_code_area", postalAreaSchema), member("geotarget_area", geotargetAreaSchema),
		member("radius_area", radiusAreaSchema)),
	OutputSchema: json.RawMessage(regionChangeSchema),
	Arguments: []capability.Argument{accountArgument, regionIDArgument,
		{Name: "display_name", Description: "Name of the region, 1 to 200 characters"},
		{Name: "postal_code_area", Description: "Area of postal codes: regionCode and postalCodes"},
		{Name: "geotarget_area", Description: "Area of predefined geotargets: geotargetCriteriaIds"},
		{Name: "radius_area", Description: "Area of a radius: radius, latLng, regionCode, radiusUnits"}},
	Fields: []capability.Field{
		{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
		{Name: "region", Description: "The region as Google returned it, with " + regionFieldsDescription},
	},
	Examples: []capability.Example{{Description: "A region of two postal code ranges",
		Arguments: json.RawMessage(`{"account":"1000001","region_id":"north-1","display_name":"North",` +
			`"postal_code_area":{"regionCode":"US","postalCodes":[{"begin":"94108"},{"begin":"95*","end":"96*"}]}}`)}},
}

var regionsUpdate = capability.Descriptor{
	ID: regionsUpdateID, Version: 1, Title: "Update a Merchant Center region",
	Description: "Change a region of a Merchant Center account. update_mask lists what is replaced, from " +
		"displayName, postalCodeArea, geotargetArea, and radiusArea, and each entry replaces the whole member. " +
		"Qatlas takes no other path: displayName needs display_name, and any area path needs exactly one area " +
		"block, because a region has exactly one area. To change the kind of the area, name the old area and " +
		"the new one in update_mask and give the new block. Shipping settings and regional inventory that use " +
		"the region follow the change. " + regionsAdminNote + regionsAreaNote + regionsDelayNote +
		"The confirmation shows the account, the region ID, and the replaced members. " + regionsUntrusted,
	Tags: []string{"google", "merchant", "regions"}, Risk: regionChangeRisk(capability.EffectUpdate,
		capability.IdempotencyIdempotent), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"region_id", "update_mask"}, member("account", accountArgSchema),
		member("region_id", regionIDSchema), member("update_mask", updateMaskSchema),
		member("display_name", regionDisplayNameSchema), member("postal_code_area", postalAreaSchema),
		member("geotarget_area", geotargetAreaSchema), member("radius_area", radiusAreaSchema)),
	OutputSchema: json.RawMessage(regionChangeSchema),
	Arguments: []capability.Argument{accountArgument, regionIDArgument,
		{Name: "update_mask", Required: true, Description: "Members to replace: displayName, postalCodeArea, " +
			"geotargetArea, radiusArea; each at most once"},
		{Name: "display_name", Description: "New name, needed when update_mask holds displayName"},
		{Name: "postal_code_area", Description: "New area of postal codes"},
		{Name: "geotarget_area", Description: "New area of geotargets"},
		{Name: "radius_area", Description: "New area of a radius"}},
	Fields: []capability.Field{
		{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
		{Name: "region", Description: "The region as Google returned it, with " + regionFieldsDescription},
	},
	Examples: []capability.Example{{Description: "Rename a region",
		Arguments: json.RawMessage(`{"account":"1000001","region_id":"north-1","update_mask":["displayName"],` +
			`"display_name":"North and east"}`)}},
}
