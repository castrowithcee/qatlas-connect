package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	locationsListID = Provider + ".locations.list"
	locationsGetID  = Provider + ".locations.get"
)

const (
	maxLocationsPage = 100
	// maxProofPages bounds the reading of the location list of a bound account that proves a location
	// belongs to it: 20 pages of 100 locations.
	maxProofPages  = 20
	maxFilterRunes = 1000
)

var locationsRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "business location data: names, postal addresses, phone numbers, coordinates, opening hours, " +
		"websites, descriptions, categories, services with prices, and labels of business locations",
}

// readMaskFields are the fields of a location a read mask may name: every field of the location the
// discovery document of the business information API documents, at the top level. The mask has no default
// and no wildcard, so the size and content of an answer are always chosen.
var readMaskFields = []string{"name", "languageCode", "storeCode", "title", "phoneNumbers", "categories",
	"storefrontAddress", "websiteUri", "regularHours", "specialHours", "serviceArea", "labels",
	"adWordsLocationExtensions", "latlng", "openInfo", "metadata", "profile", "relationshipData", "moreHours",
	"serviceItems"}

// The business information API as its discovery document documents it. The deprecated metadata flags
// canOperateLocalPost and canHaveBusinessCalls are left out.
type (
	location struct {
		Name                      short              `json:"name,omitempty"`
		LanguageCode              short              `json:"languageCode,omitempty"`
		StoreCode                 line               `json:"storeCode,omitempty"`
		Title                     line               `json:"title,omitempty"`
		PhoneNumbers              *phoneNumbers      `json:"phoneNumbers,omitempty"`
		Categories                *categories        `json:"categories,omitempty"`
		StorefrontAddress         *postalAddress     `json:"storefrontAddress,omitempty"`
		WebsiteURI                link               `json:"websiteUri,omitempty"`
		RegularHours              *businessHours     `json:"regularHours,omitempty"`
		SpecialHours              *specialHours      `json:"specialHours,omitempty"`
		ServiceArea               *serviceArea       `json:"serviceArea,omitempty"`
		Labels                    list[line]         `json:"labels,omitempty"`
		AdWordsLocationExtensions *adWordsExtensions `json:"adWordsLocationExtensions,omitempty"`
		Latlng                    *latLng            `json:"latlng,omitempty"`
		OpenInfo                  *openInfo          `json:"openInfo,omitempty"`
		Metadata                  *locationMetadata  `json:"metadata,omitempty"`
		Profile                   *profile           `json:"profile,omitempty"`
		RelationshipData          *relationshipData  `json:"relationshipData,omitempty"`
		MoreHours                 list[moreHours]    `json:"moreHours,omitempty"`
		ServiceItems              list[serviceItem]  `json:"serviceItems,omitempty"`
	}
	phoneNumbers struct {
		PrimaryPhone     line       `json:"primaryPhone,omitempty"`
		AdditionalPhones list[line] `json:"additionalPhones,omitempty"`
	}
	categories struct {
		PrimaryCategory      *category      `json:"primaryCategory,omitempty"`
		AdditionalCategories list[category] `json:"additionalCategories,omitempty"`
	}
	category struct {
		Name           short               `json:"name,omitempty"`
		DisplayName    line                `json:"displayName,omitempty"`
		ServiceTypes   list[serviceType]   `json:"serviceTypes,omitempty"`
		MoreHoursTypes list[moreHoursType] `json:"moreHoursTypes,omitempty"`
	}
	serviceType struct {
		ServiceTypeID short `json:"serviceTypeId,omitempty"`
		DisplayName   line  `json:"displayName,omitempty"`
	}
	moreHoursType struct {
		HoursTypeID          short `json:"hoursTypeId,omitempty"`
		DisplayName          line  `json:"displayName,omitempty"`
		LocalizedDisplayName line  `json:"localizedDisplayName,omitempty"`
	}
	businessHours struct {
		Periods list[timePeriod] `json:"periods,omitempty"`
	}
	timePeriod struct {
		OpenDay   short      `json:"openDay,omitempty"`
		OpenTime  *timeOfDay `json:"openTime,omitempty"`
		CloseDay  short      `json:"closeDay,omitempty"`
		CloseTime *timeOfDay `json:"closeTime,omitempty"`
	}
	timeOfDay struct {
		Hours   integer `json:"hours,omitzero"`
		Minutes integer `json:"minutes,omitzero"`
		Seconds integer `json:"seconds,omitzero"`
		Nanos   integer `json:"nanos,omitzero"`
	}
	specialHours struct {
		SpecialHourPeriods list[specialHourPeriod] `json:"specialHourPeriods,omitempty"`
	}
	specialHourPeriod struct {
		StartDate *date      `json:"startDate,omitempty"`
		OpenTime  *timeOfDay `json:"openTime,omitempty"`
		EndDate   *date      `json:"endDate,omitempty"`
		CloseTime *timeOfDay `json:"closeTime,omitempty"`
		Closed    flag       `json:"closed,omitzero"`
	}
	date struct {
		Year  integer `json:"year,omitzero"`
		Month integer `json:"month,omitzero"`
		Day   integer `json:"day,omitzero"`
	}
	serviceArea struct {
		BusinessType short   `json:"businessType,omitempty"`
		Places       *places `json:"places,omitempty"`
		RegionCode   short   `json:"regionCode,omitempty"`
	}
	places struct {
		PlaceInfos list[placeInfo] `json:"placeInfos,omitempty"`
	}
	placeInfo struct {
		PlaceName line  `json:"placeName,omitempty"`
		PlaceID   short `json:"placeId,omitempty"`
	}
	adWordsExtensions struct {
		AdPhone line `json:"adPhone,omitempty"`
	}
	latLng struct {
		Latitude  number `json:"latitude,omitzero"`
		Longitude number `json:"longitude,omitzero"`
	}
	openInfo struct {
		Status      short `json:"status,omitempty"`
		CanReopen   flag  `json:"canReopen,omitzero"`
		OpeningDate *date `json:"openingDate,omitempty"`
	}
	locationMetadata struct {
		HasGoogleUpdated            flag  `json:"hasGoogleUpdated,omitzero"`
		HasPendingEdits             flag  `json:"hasPendingEdits,omitzero"`
		CanDelete                   flag  `json:"canDelete,omitzero"`
		CanModifyServiceList        flag  `json:"canModifyServiceList,omitzero"`
		CanHaveFoodMenus            flag  `json:"canHaveFoodMenus,omitzero"`
		CanOperateHealthData        flag  `json:"canOperateHealthData,omitzero"`
		CanOperateLodgingData       flag  `json:"canOperateLodgingData,omitzero"`
		PlaceID                     short `json:"placeId,omitempty"`
		DuplicateLocation           short `json:"duplicateLocation,omitempty"`
		MapsURI                     link  `json:"mapsUri,omitempty"`
		NewReviewURI                link  `json:"newReviewUri,omitempty"`
		HasVoiceOfMerchant          flag  `json:"hasVoiceOfMerchant,omitzero"`
		IsParticularlyPersonalPlace flag  `json:"isParticularlyPersonalPlace,omitzero"`
	}
	profile struct {
		Description long `json:"description,omitempty"`
	}
	relationshipData struct {
		ParentLocation    *relevantLocation      `json:"parentLocation,omitempty"`
		ChildrenLocations list[relevantLocation] `json:"childrenLocations,omitempty"`
		ParentChain       short                  `json:"parentChain,omitempty"`
	}
	relevantLocation struct {
		PlaceID      short `json:"placeId,omitempty"`
		RelationType short `json:"relationType,omitempty"`
	}
	moreHours struct {
		HoursTypeID short            `json:"hoursTypeId,omitempty"`
		Periods     list[timePeriod] `json:"periods,omitempty"`
	}
	serviceItem struct {
		StructuredServiceItem *structuredServiceItem `json:"structuredServiceItem,omitempty"`
		FreeFormServiceItem   *freeFormServiceItem   `json:"freeFormServiceItem,omitempty"`
		Price                 *money                 `json:"price,omitempty"`
	}
	structuredServiceItem struct {
		ServiceTypeID short `json:"serviceTypeId,omitempty"`
		Description   long  `json:"description,omitempty"`
	}
	freeFormServiceItem struct {
		Category short  `json:"category,omitempty"`
		Label    *label `json:"label,omitempty"`
	}
	label struct {
		DisplayName  line  `json:"displayName,omitempty"`
		Description  long  `json:"description,omitempty"`
		LanguageCode short `json:"languageCode,omitempty"`
	}
	money struct {
		CurrencyCode short   `json:"currencyCode,omitempty"`
		Units        short   `json:"units,omitempty"`
		Nanos        integer `json:"nanos,omitzero"`
	}
	locationsAnswer struct {
		Locations     []location `json:"locations"`
		NextPageToken string     `json:"nextPageToken"`
		TotalSize     integer    `json:"totalSize"`
	}
	locationsResult struct {
		Locations  []location `json:"locations"`
		TotalSize  integer    `json:"totalSize,omitzero"`
		NextCursor string     `json:"next_cursor,omitempty"`
	}
)

var (
	timeOfDaySchema = objectSchema("hours", integerSchema, "minutes", integerSchema, "seconds", integerSchema,
		"nanos", integerSchema)
	dateSchema       = objectSchema("year", integerSchema, "month", integerSchema, "day", integerSchema)
	timePeriodSchema = objectSchema("openDay", stringSchema, "openTime", timeOfDaySchema, "closeDay", stringSchema,
		"closeTime", timeOfDaySchema)
	categorySchema = objectSchema("name", stringSchema, "displayName", stringSchema,
		"serviceTypes", arraySchema(objectSchema("serviceTypeId", stringSchema, "displayName", stringSchema)),
		"moreHoursTypes", arraySchema(objectSchema("hoursTypeId", stringSchema, "displayName", stringSchema,
			"localizedDisplayName", stringSchema)))
	relevantLocationSchema = objectSchema("placeId", stringSchema, "relationType", stringSchema)
	locationSchema         = objectSchema(
		"name", stringSchema, "languageCode", stringSchema, "storeCode", stringSchema, "title", stringSchema,
		"phoneNumbers", objectSchema("primaryPhone", stringSchema, "additionalPhones", arraySchema(stringSchema)),
		"categories", objectSchema("primaryCategory", categorySchema, "additionalCategories", arraySchema(categorySchema)),
		"storefrontAddress", postalAddressSchema,
		"websiteUri", stringSchema,
		"regularHours", objectSchema("periods", arraySchema(timePeriodSchema)),
		"specialHours", objectSchema("specialHourPeriods", arraySchema(objectSchema("startDate", dateSchema,
			"openTime", timeOfDaySchema, "endDate", dateSchema, "closeTime", timeOfDaySchema, "closed", booleanSchema))),
		"serviceArea", objectSchema("businessType", stringSchema, "places", objectSchema("placeInfos",
			arraySchema(objectSchema("placeName", stringSchema, "placeId", stringSchema))), "regionCode", stringSchema),
		"labels", arraySchema(stringSchema),
		"adWordsLocationExtensions", objectSchema("adPhone", stringSchema),
		"latlng", objectSchema("latitude", numberSchema, "longitude", numberSchema),
		"openInfo", objectSchema("status", stringSchema, "canReopen", booleanSchema, "openingDate", dateSchema),
		"metadata", objectSchema("hasGoogleUpdated", booleanSchema, "hasPendingEdits", booleanSchema,
			"canDelete", booleanSchema, "canModifyServiceList", booleanSchema, "canHaveFoodMenus", booleanSchema,
			"canOperateHealthData", booleanSchema, "canOperateLodgingData", booleanSchema, "placeId", stringSchema,
			"duplicateLocation", stringSchema, "mapsUri", stringSchema, "newReviewUri", stringSchema,
			"hasVoiceOfMerchant", booleanSchema, "isParticularlyPersonalPlace", booleanSchema),
		"profile", objectSchema("description", stringSchema),
		"relationshipData", objectSchema("parentLocation", relevantLocationSchema,
			"childrenLocations", arraySchema(relevantLocationSchema), "parentChain", stringSchema),
		"moreHours", arraySchema(objectSchema("hoursTypeId", stringSchema, "periods", arraySchema(timePeriodSchema))),
		"serviceItems", arraySchema(objectSchema(
			"structuredServiceItem", objectSchema("serviceTypeId", stringSchema, "description", stringSchema),
			"freeFormServiceItem", objectSchema("category", stringSchema, "label", objectSchema(
				"displayName", stringSchema, "description", stringSchema, "languageCode", stringSchema)),
			"price", objectSchema("currencyCode", stringSchema, "units", stringSchema, "nanos", integerSchema))))
	locationsResultSchema = `{"type":"object","properties":{"locations":` + arraySchema(locationSchema) +
		`,"totalSize":{"type":"integer"},"next_cursor":{"type":"string"}},"required":["locations"],` +
		`"additionalProperties":false}`
)

var (
	readMaskSchema = `{"type":"array","minItems":1,"maxItems":20,"uniqueItems":true,"items":{"type":"string","enum":` +
		mustJSON(readMaskFields) + `}}`
	readMaskArgument = capability.Argument{Name: "read_mask", Required: true, Description: "Fields of the location " +
		"to return, at least one of: " + strings.Join(readMaskFields, ", ") + ". There is no default and no " +
		"wildcard: name only what the task needs"}
	locationArgSchema   = `{"type":"string","maxLength":40,"pattern":"` + locationPattern + `","x-form":"` + formLocation + `"}`
	accountAnyArgSchema = `{"type":"string","maxLength":40,"pattern":"` + accountAnyPattern + `","x-form":"` +
		formAccount + `"}`
)

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

var locationFields = []capability.Field{
	{Name: "name", Description: "locations/LOCATION"},
	{Name: "title", Description: "Business name"},
	{Name: "storeCode", Description: "External identifier of the location within its account"},
	{Name: "languageCode", Description: "Language of the location"},
	{Name: "phoneNumbers", Description: "primaryPhone and additionalPhones"},
	{Name: "categories", Description: "primaryCategory and additionalCategories, each with name, displayName, " +
		"serviceTypes, and moreHoursTypes"},
	{Name: "storefrontAddress", Description: "Postal address"},
	{Name: "websiteUri", Description: "Website of the business, text that is never fetched"},
	{Name: "regularHours", Description: "Opening hours as periods"},
	{Name: "specialHours", Description: "Special hours such as holidays"},
	{Name: "moreHours", Description: "Hours of departments or for specific customers"},
	{Name: "serviceArea", Description: "Service area: businessType, places, regionCode"},
	{Name: "labels", Description: "Free-form labels of the business, not shown to customers"},
	{Name: "adWordsLocationExtensions", Description: "adPhone shown in ads"},
	{Name: "latlng", Description: "Latitude and longitude the business provided"},
	{Name: "openInfo", Description: "status, canReopen, openingDate"},
	{Name: "metadata", Description: "Google's own state: placeId, mapsUri, newReviewUri, duplicateLocation, " +
		"hasVoiceOfMerchant, hasPendingEdits, hasGoogleUpdated, and the can* flags"},
	{Name: "profile", Description: "description of the business in its own voice"},
	{Name: "relationshipData", Description: "parentLocation, childrenLocations, parentChain"},
	{Name: "serviceItems", Description: "Services with structured or free-form item and price"},
}

var locationsList = capability.Descriptor{
	ID: locationsListID, Version: 1, Title: "List Business Profile locations",
	Description: "List the business locations of one Google Business Profile account with the fields read_mask " +
		"names, up to 100 per call (Google's default is 10) with a cursor to read on; an empty list means none. " +
		"accounts/- names the locations of the consenting account, also those it reaches through groups. A " +
		"connection bound to an account lists that account; one bound to a location answers with that location " +
		"only, read directly. " + untrustedNote,
	Tags: []string{"google", "business-profile", "locations"}, Risk: locationsRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"read_mask"},
		"account", accountAnyArgSchema,
		"read_mask", readMaskSchema,
		"filter", `{"type":"string","minLength":1,"maxLength":1000}`,
		"order_by", `{"type":"array","minItems":1,"maxItems":2,"uniqueItems":true,"items":{"type":"string",`+
			`"enum":["title","title desc","store_code","store_code desc"]}}`,
		"page_size", `{"type":"integer","minimum":1,"maximum":100}`,
		"cursor", cursorSchema),
	OutputSchema: json.RawMessage(locationsResultSchema),
	Arguments: []capability.Argument{
		{Name: "account", Target: "account", Description: "Account as accounts/ACCOUNT, or accounts/- for the " +
			"consenting account on an unbound connection; optional when the connection is bound to an account, and " +
			"then it must be that account; not available on a connection bound to a location"},
		readMaskArgument,
		{Name: "filter", Description: "Google's location filter, for example categories.primaryCategory.name or " +
			"storefrontAddress.regionCode conditions; Google applies it, not available on a connection bound to a location"},
		{Name: "order_by", Description: "Sort order: title or store_code, each optionally with desc, at most both"},
		{Name: "page_size", Description: "Locations per call, 1 to 100; Google's default is 10"},
		cursorArgument,
	},
	Fields: []capability.Field{
		{Name: "locations", Description: "Locations with the fields of read_mask"},
		{Name: "totalSize", Description: "Approximate number of locations, only when filter is set"},
		nextCursorField,
	},
	Examples: []capability.Example{{Description: "Names and addresses of the locations of one account",
		Arguments: json.RawMessage(`{"account":"accounts/1000001","read_mask":["name","title","storefrontAddress"]}`)}},
}

var locationsGet = capability.Descriptor{
	ID: locationsGetID, Version: 1, Title: "Get a Business Profile location",
	Description: "Read one business location with the fields read_mask names, as last set by the business; it " +
		"may differ from what Google Maps shows. A connection bound to an account first proves from the location " +
		"list of the account that the location belongs to it, reading at most 2000 locations; a connection bound " +
		"to a location reads only that location. " + untrustedNote,
	Tags: []string{"google", "business-profile", "locations"}, Risk: locationsRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"read_mask"}, "location", locationArgSchema, "read_mask", readMaskSchema),
	OutputSchema: json.RawMessage(locationSchema),
	Arguments: []capability.Argument{
		{Name: "location", Target: "location", Description: "Location as locations/LOCATION; optional when the " +
			"connection is bound to a location, and then it must be that location"},
		readMaskArgument,
	},
	Fields: locationFields,
	Examples: []capability.Example{{Description: "Name, hours, and phone numbers of one location",
		Arguments: json.RawMessage(`{"location":"locations/2000002","read_mask":["title","regularHours","phoneNumbers"]}`)}},
}

type locationsArguments struct {
	Account  string   `json:"account"`
	Location string   `json:"location"`
	ReadMask []string `json:"read_mask"`
	Filter   string   `json:"filter"`
	OrderBy  []string `json:"order_by"`
	PageSize int      `json:"page_size"`
	Cursor   string   `json:"cursor"`
}

// readMaskOf checks a read mask against the known fields and returns it sorted, so that one set of fields
// has one spelling.
func readMaskOf(given []string) ([]string, error) {
	if len(given) == 0 {
		return nil, invalidArgument("read_mask is required: name at least one field of the location")
	}
	mask := make([]string, 0, len(given))
	for _, field := range given {
		if !slices.Contains(readMaskFields, field) {
			return nil, invalidArgument("read_mask names a field the location does not have")
		}
		if slices.Contains(mask, field) {
			return nil, invalidArgument("read_mask names a field twice")
		}
		mask = append(mask, field)
	}
	slices.Sort(mask)
	return mask, nil
}

func readMaskQuery(mask []string) url.Values {
	return url.Values{"readMask": {strings.Join(mask, ",")}}
}

// checkFilter refuses a filter with control characters; Google evaluates it and nothing else reads it.
func checkFilter(filter string) error {
	if len([]rune(filter)) > maxFilterRunes {
		return invalidArgument("filter must be at most 1000 characters")
	}
	for _, r := range filter {
		if r < ' ' || r == 0x7f {
			return invalidArgument("filter must not contain control characters")
		}
	}
	return nil
}

func readLocationArguments(op string, raw json.RawMessage) (locationsArguments, []string, error) {
	var args locationsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, nil, provider.Fail(op, "the validated arguments could not be read")
	}
	mask, err := readMaskOf(args.ReadMask)
	return args, mask, err
}

func handleLocationsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "list locations"
	args, mask, err := readLocationArguments(op, raw)
	if err != nil {
		return nil, err
	}
	if err := checkFilter(args.Filter); err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	if bound.location != "" {
		// The list of a location target is that location: the location names no account to list, and a
		// filter Google would apply cannot be applied to it here.
		switch {
		case args.Account != "":
			return nil, invalidArgument("account is not available on a connection bound to a location")
		case args.Filter != "":
			return nil, invalidArgument("filter is not available on a connection bound to a location")
		case args.Cursor != "":
			return nil, refusedCursor()
		}
		c, err := open(ctx, resolved, secrets, red)
		if err != nil {
			return nil, err
		}
		var one location
		if err := c.get(ctx, op, locationsListID, request{root: informationRoot, path: locationPath(bound.location),
			query: readMaskQuery(mask), notFound: notFoundLocation}, &one); err != nil {
			return nil, err
		}
		return locationsResult{Locations: []location{one}}, nil
	}
	account, err := bound.accountOf("account", args.Account, !bound.bound())
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(locationsListID, account, mask, args.Filter, args.OrderBy, bound.binding())
	query := readMaskQuery(mask)
	if args.Filter != "" {
		query.Set("filter", args.Filter)
	}
	if len(args.OrderBy) > 0 {
		query.Set("orderBy", strings.Join(args.OrderBy, ","))
	}
	query = pageSizeQuery(query, args.PageSize)
	if query, err = pageQuery(binding, args.Cursor, query); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer locationsAnswer
	if err := c.get(ctx, op, locationsListID, request{root: informationRoot, path: locationsOfPath(account),
		query: query, notFound: notFoundAccount}, &answer); err != nil {
		return nil, err
	}
	if len(answer.Locations) > maxLocationsPage {
		return nil, tooMany(op, maxLocationsPage)
	}
	out := locationsResult{Locations: append([]location{}, answer.Locations...), TotalSize: answer.TotalSize}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleLocationsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get location"
	args, mask, err := readLocationArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	name, err := bound.locationOf(args.Location)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if bound.account != "" {
		if err := c.proveLocation(ctx, op, bound.account, name); err != nil {
			return nil, err
		}
	}
	var one location
	if err := c.get(ctx, op, locationsGetID, request{root: informationRoot, path: locationPath(name),
		query: readMaskQuery(mask), notFound: notFoundLocation}, &one); err != nil {
		return nil, err
	}
	return one, nil
}

// proveLocation reads the location list of the bound account, names only, until it finds the location. A
// location the list does not hold is refused like one outside the target, without naming it; a list longer
// than the proof reads is refused as well, since the location could not be proved.
func (c *client) proveLocation(ctx context.Context, op, account, name string) error {
	query := readMaskQuery([]string{"name"})
	query = pageSizeQuery(query, maxLocationsPage)
	for range maxProofPages {
		var answer locationsAnswer
		if err := c.get(ctx, op, locationsGetID, request{root: informationRoot, path: locationsOfPath(account),
			query: query, notFound: notFoundAccount}, &answer); err != nil {
			return err
		}
		for _, entry := range answer.Locations {
			if string(entry.Name) == name {
				return nil
			}
		}
		if answer.NextPageToken == "" {
			return invalidArgument("location " + outsideTarget)
		}
		if len(answer.NextPageToken) > maxPageToken {
			return provider.InvalidResponse(op, subject+" returned a continuation token that is too long")
		}
		query.Set("pageToken", answer.NextPageToken)
	}
	return invalidArgument("location could not be proved to belong to the account of this connection within " +
		"the first 2000 locations of the account")
}
