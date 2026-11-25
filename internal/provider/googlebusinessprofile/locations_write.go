package googlebusinessprofile

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	locationsCreateID           = Provider + ".locations.create"
	locationsUpdateID           = Provider + ".locations.update"
	locationsDeleteID           = Provider + ".locations.delete"
	locationsGetGoogleUpdatedID = Provider + ".locations.get-google-updated"
)

// The writable fields of a location are the top-level members of the Location schema of the business
// information API (discovery document revision 20261116, read on 2026-11-18) that are not output only:
// metadata is, name is Google's identifier. languageCode is immutable: Google sets it on creation only.
var (
	updateFields = []string{"storeCode", "title", "phoneNumbers", "categories", "storefrontAddress", "websiteUri",
		"regularHours", "specialHours", "serviceArea", "labels", "adWordsLocationExtensions", "latlng", "openInfo",
		"profile", "relationshipData", "moreHours", "serviceItems"}
	createFields = append([]string{"languageCode"}, updateFields...)
)

const (
	requestIDPattern = `^[A-Za-z0-9_-]{1,50}$`
)

var requestIDForm = regexp.MustCompile(requestIDPattern)

const reviewNote = "A change of the title, the address, or a category can make Google verify or review the " +
	"location again, or suspend its profile; that effect can be seen only live. "

const writeUntrustedNote = "Every value is public once Google accepts it, and nothing the tool sends is fetched. "

// Input schemas of the writable fields. Nested members that Google marks output only are not accepted.
func boundedText(maxRunes int) string {
	return fmt.Sprintf(`{"type":"string","maxLength":%d}`, maxRunes)
}

func requiredText(maxRunes int) string {
	return fmt.Sprintf(`{"type":"string","minLength":1,"maxLength":%d}`, maxRunes)
}

func whole(low, high int) string {
	return fmt.Sprintf(`{"type":"integer","minimum":%d,"maximum":%d}`, low, high)
}

func enum(values ...string) string { return `{"type":"string","enum":` + mustJSON(values) + `}` }

func arrayOf(item string, maxCount int) string {
	return fmt.Sprintf(`{"type":"array","maxItems":%d,"items":%s}`, maxCount, item)
}

// closedObject is objectSchema with required members; JSON Schema rejects a null required list.
func closedObject(required []string, pairs ...string) string {
	if len(required) == 0 {
		return objectSchema(pairs...)
	}
	names, _ := json.Marshal(required)
	return strings.TrimSuffix(objectSchema(pairs...), `,"additionalProperties":false}`) +
		`,"required":` + string(names) + `,"additionalProperties":false}`
}

var (
	writeDays = enum("MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY", "SUNDAY")

	writeTimeOfDay = closedObject([]string{"hours"}, "hours", whole(0, 24), "minutes", whole(0, 59),
		"seconds", whole(0, 60), "nanos", whole(0, 999999999))
	writeDate = closedObject([]string{"year", "month", "day"}, "year", whole(0, 9999), "month", whole(0, 12),
		"day", whole(0, 31))
	writeTimePeriod = closedObject([]string{"openDay", "openTime", "closeDay", "closeTime"},
		"openDay", writeDays, "openTime", writeTimeOfDay, "closeDay", writeDays, "closeTime", writeTimeOfDay)
	writeCategory = closedObject([]string{"name"}, "name", requiredText(maxShortRunes))

	writableSchemas = map[string]string{
		"languageCode": boundedText(maxShortRunes),
		"storeCode":    boundedText(maxLineRunes),
		"title":        requiredText(maxLineRunes),
		"phoneNumbers": closedObject([]string{"primaryPhone"}, "primaryPhone", requiredText(maxLineRunes),
			"additionalPhones", `{"type":"array","maxItems":2,"items":`+requiredText(maxLineRunes)+`}`),
		"categories": closedObject([]string{"primaryCategory"}, "primaryCategory", writeCategory,
			"additionalCategories", arrayOf(writeCategory, maxItems)),
		"storefrontAddress": closedObject([]string{"regionCode"}, "revision", whole(0, 1<<30),
			"regionCode", requiredText(maxShortRunes), "languageCode", boundedText(maxShortRunes),
			"postalCode", boundedText(maxShortRunes), "sortingCode", boundedText(maxShortRunes),
			"administrativeArea", boundedText(maxLineRunes), "locality", boundedText(maxLineRunes),
			"sublocality", boundedText(maxLineRunes), "addressLines", arrayOf(requiredText(maxLineRunes), 5),
			"recipients", arrayOf(requiredText(maxLineRunes), 5), "organization", boundedText(maxLineRunes)),
		"websiteUri":   boundedText(maxURLRunes),
		"regularHours": closedObject([]string{"periods"}, "periods", arrayOf(writeTimePeriod, maxItems)),
		"specialHours": closedObject([]string{"specialHourPeriods"}, "specialHourPeriods", arrayOf(closedObject(
			[]string{"startDate"}, "startDate", writeDate, "endDate", writeDate, "openTime", writeTimeOfDay,
			"closeTime", writeTimeOfDay, "closed", booleanSchema), maxItems)),
		"serviceArea": closedObject([]string{"businessType"},
			"businessType", enum("CUSTOMER_LOCATION_ONLY", "CUSTOMER_AND_BUSINESS_LOCATION"),
			"places", closedObject([]string{"placeInfos"}, "placeInfos", arrayOf(closedObject(
				[]string{"placeName", "placeId"}, "placeName", requiredText(maxLineRunes),
				"placeId", requiredText(maxShortRunes)), 20)),
			"regionCode", boundedText(maxShortRunes)),
		"labels":                    arrayOf(`{"type":"string","minLength":1,"maxLength":255}`, maxItems),
		"adWordsLocationExtensions": closedObject([]string{"adPhone"}, "adPhone", requiredText(maxLineRunes)),
		"latlng": closedObject([]string{"latitude", "longitude"},
			"latitude", `{"type":"number","minimum":-90,"maximum":90}`,
			"longitude", `{"type":"number","minimum":-180,"maximum":180}`),
		"openInfo": closedObject([]string{"status"}, "status",
			enum("OPEN", "CLOSED_PERMANENTLY", "CLOSED_TEMPORARILY"), "openingDate", writeDate),
		"profile": closedObject([]string{"description"}, "description", boundedText(maxLongRunes)),
		"relationshipData": closedObject(nil,
			"parentLocation", closedObject([]string{"relationType", "placeId"},
				"relationType", enum("DEPARTMENT_OF", "INDEPENDENT_ESTABLISHMENT_IN"), "placeId", requiredText(maxShortRunes)),
			"childrenLocations", arrayOf(closedObject([]string{"relationType", "placeId"},
				"relationType", enum("DEPARTMENT_OF", "INDEPENDENT_ESTABLISHMENT_IN"), "placeId", requiredText(maxShortRunes)),
				maxItems),
			"parentChain", boundedText(maxShortRunes)),
		"moreHours": arrayOf(closedObject([]string{"hoursTypeId", "periods"}, "hoursTypeId", requiredText(maxShortRunes),
			"periods", arrayOf(writeTimePeriod, maxItems)), maxItems),
		"serviceItems": arrayOf(closedObject(nil,
			"structuredServiceItem", closedObject([]string{"serviceTypeId"}, "serviceTypeId", requiredText(maxShortRunes),
				"description", boundedText(300)),
			"freeFormServiceItem", closedObject([]string{"category", "label"}, "category", requiredText(maxShortRunes),
				"label", closedObject([]string{"displayName"}, "displayName", requiredText(maxLineRunes),
					"description", boundedText(maxLongRunes), "languageCode", boundedText(maxShortRunes))),
			"price", closedObject(nil, "currencyCode", boundedText(maxShortRunes),
				"units", `{"type":"string","pattern":"^-?[0-9]{1,18}$"}`, "nanos", whole(-999999999, 999999999))),
			maxItems),
	}

	fieldDescriptions = map[string]string{
		"languageCode": "BCP-47 language of the location; set on creation and never changed afterwards",
		"storeCode":    "External identifier of the location, unique within its account",
		"title":        "Business name as used on the storefront, without taglines, store codes, hours, phone numbers, URLs, or addresses",
		"phoneNumbers": "primaryPhone (required) and at most two additionalPhones",
		"categories": "primaryCategory (required) and additionalCategories, each {name: categories/...}; names " +
			"come from Google's category list, displayName and the service types are answered but not sent",
		"storefrontAddress": "Postal address: regionCode (required), languageCode, postalCode, sortingCode, " +
			"administrativeArea, locality, sublocality, addressLines, recipients, organization, revision",
		"websiteUri":   "Website of the business; text that is never fetched",
		"regularHours": "Opening hours as periods with openDay, openTime, closeDay, closeTime",
		"specialHours": "Special hours: periods with startDate, endDate, openTime, closeTime, and closed",
		"serviceArea": "businessType (CUSTOMER_LOCATION_ONLY or CUSTOMER_AND_BUSINESS_LOCATION), places (at most " +
			"20 placeInfos with placeName and placeId), and regionCode",
		"labels":                    "Free-form labels of the business, 1 to 255 characters each, not shown to customers",
		"adWordsLocationExtensions": "adPhone shown in ads",
		"latlng":                    "latitude and longitude of the business",
		"openInfo":                  "status (OPEN, CLOSED_PERMANENTLY, CLOSED_TEMPORARILY) and openingDate; canReopen is answered, not sent",
		"profile":                   "description of the business in its own voice",
		"relationshipData":          "parentLocation and childrenLocations (each relationType and placeId), parentChain",
		"moreHours":                 "Hours of departments or for specific customers: hoursTypeId and periods",
		"serviceItems":              "Services: structuredServiceItem or freeFormServiceItem, each with an optional price",
	}
)

// fieldPairs are the schema members of the named fields; fieldArguments are their arguments.
func fieldPairs(names []string) []string {
	pairs := make([]string, 0, 2*len(names))
	for _, name := range names {
		pairs = append(pairs, name, writableSchemas[name])
	}
	return pairs
}

func fieldArguments(names []string, required ...string) []capability.Argument {
	out := make([]capability.Argument, 0, len(names))
	for _, name := range names {
		out = append(out, capability.Argument{Name: name, Required: slices.Contains(required, name),
			Description: fieldDescriptions[name]})
	}
	return out
}

func propertiesOf(schema string) map[string]json.RawMessage {
	var parsed struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
		panic(err)
	}
	return parsed.Properties
}

// previousValuesSchema takes each field the way a read answers it.
func previousValuesSchema() string {
	answered := propertiesOf(locationSchema)
	pairs := make([]string, 0, 2*len(updateFields))
	for _, name := range updateFields {
		pairs = append(pairs, name, string(answered[name]))
	}
	return objectSchema(pairs...)
}

var updateMaskSchemaLocation = `{"type":"array","minItems":1,"maxItems":17,"uniqueItems":true,"items":{"type":"string","enum":` +
	mustJSON(updateFields) + `}}`

var locationWriteRisk = func(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true, DataSensitivity: locationsRisk.DataSensitivity + "; every value written becomes public " +
			"profile data"}
}

var requestIDSchema = `{"type":"string","pattern":"` + requestIDPattern + `"}`

var locationsCreate = capability.Descriptor{
	ID: locationsCreateID, Version: 1, Title: "Create a Business Profile location",
	Description: "Create a business location in one Google Business Profile account, owned by the consenting " +
		"account; it becomes publicly visible profile data. title and categories (with a primaryCategory) are " +
		"required, every other field is optional. " + reviewNote + "The call is not idempotent: request_id lets " +
		"Google detect a repeated call, and the tool generates a unique one when it is left out and names it in " +
		"the result; after an unclear outcome check locations.list before creating again, or repeat with the same " +
		"request_id. A create counts against the quota of the account (300 per day). validate_only asks Google to " +
		"check the call without creating anything; the tool still needs confirmation. The confirmation shows the " +
		"title and the field names within " + limitText(locationsCreateID) + ": a call whose title does not fit is refused before " +
		"anything is sent. A connection bound to a location does not offer this tool. " + writeUntrustedNote + untrustedNote,
	Tags: []string{"google", "business-profile", "locations", "create"}, Provider: Provider,
	Risk: locationWriteRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: inputSchema([]string{"title", "categories"}, append([]string{
		"account", accountArgSchema, "request_id", requestIDSchema, "validate_only", booleanSchema},
		fieldPairs(createFields)...)...),
	OutputSchema: json.RawMessage(createResultSchema),
	Arguments: append([]capability.Argument{
		{Name: "account", Target: "account", Description: "Account as accounts/ACCOUNT; optional when the " +
			"connection is bound to an account, and then it must be that account"},
		{Name: "request_id", Description: "Unique ID of this create call, 1 to 50 letters, digits, _ and -, a UUID " +
			"is recommended; generated when left out"},
		{Name: "validate_only", Description: "true: Google validates the call and creates nothing; default false"},
	}, fieldArguments(createFields, "title", "categories")...),
	Fields: append([]capability.Field{{Name: "request_id", Description: "The request_id sent to Google"}},
		locationFields...),
	Examples: []capability.Example{{Description: "Create a store with address, phone, and a primary category",
		Arguments: json.RawMessage(`{"account":"accounts/1000001","title":"Sample Bakery",` +
			`"categories":{"primaryCategory":{"name":"categories/gcid:bakery"}},` +
			`"storefrontAddress":{"regionCode":"DE","postalCode":"10115","locality":"Berlin","addressLines":["Sample Street 1"]},` +
			`"phoneNumbers":{"primaryPhone":"030 0000000"}}`)}},
}

var locationsUpdate = capability.Descriptor{
	ID: locationsUpdateID, Version: 1, Title: "Update a Business Profile location",
	Description: "Change fields of one business location; the new values are public profile data. update_mask " +
		"names the fields to change, each field of the mask needs its new value as an argument of the same name " +
		"(an empty text or list clears the field), and no other field is sent. previous_values is required and " +
		"only compared: it holds the value of each changed field as last read with locations.get, the tool reads " +
		"the location first and changes nothing unless it still has exactly those values, so the confirmation " +
		"shows the old and the new values in full, within " + limitText(locationsUpdateID) + ": a change whose confirmation would be " +
		"longer is refused before anything is sent, so change fewer fields or shorter values per call. " + reviewNote + "languageCode is immutable. validate_only asks Google " +
		"to check the change without making it. Google counts at most 10 changes per minute and profile. On a " +
		"connection bound to an account the location is first proved to belong to it. " + writeUntrustedNote +
		untrustedNote,
	Tags: []string{"google", "business-profile", "locations", "update"}, Provider: Provider,
	Risk: locationWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"update_mask", "previous_values"}, append([]string{
		"location", locationArgSchema, "update_mask", updateMaskSchemaLocation,
		"previous_values", previousValuesSchema(), "validate_only", booleanSchema},
		fieldPairs(updateFields)...)...),
	OutputSchema: json.RawMessage(locationSchema),
	Arguments: append([]capability.Argument{
		{Name: "location", Target: "location", Description: "Location as locations/LOCATION; optional when the " +
			"connection is bound to a location, and then it must be that location"},
		{Name: "update_mask", Required: true, Description: "The fields to change, at least one of: " +
			strings.Join(updateFields, ", ") + "; each needs its new value as an argument"},
		{Name: "previous_values", Required: true, Description: "An object with exactly the fields of update_mask, " +
			"each as last read with locations.get (as that tool answers it); only compared with what Google holds, " +
			"never sent, and the change is refused when anything differs"},
		{Name: "validate_only", Description: "true: Google validates the change and makes none; default false"},
	}, fieldArguments(updateFields)...),
	Fields: locationFields,
	Examples: []capability.Example{{Description: "Change the phone number and the website", Arguments: json.RawMessage(
		`{"location":"locations/2000002","update_mask":["phoneNumbers","websiteUri"],` +
			`"previous_values":{"phoneNumbers":{"primaryPhone":"030 0000000"},"websiteUri":"https://old.example.invalid"},` +
			`"phoneNumbers":{"primaryPhone":"030 1111111"},"websiteUri":"https://new.example.invalid"}`)}},
}

var locationsDelete = capability.Descriptor{
	ID: locationsDeleteID, Version: 1, Title: "Delete a Business Profile location",
	Description: "Delete one business location from Google Business Profile. Treat the deletion as irreversible: " +
		"nothing documents that a deleted location can be restored, and the location disappears from Google " +
		"Maps and Search for its customers. Google refuses a location that it marks as not deletable through the " +
		"API (metadata.canDelete). The tool is listed-only: a connection offers it only through its tools list, " +
		"and every call needs confirmation. On a connection bound to an account the location is first proved to " +
		"belong to it. " + untrustedNote,
	Tags: []string{"google", "business-profile", "locations", "delete"}, Provider: Provider,
	RequiresToolAllowList: true,
	Risk: capability.Risk{Effect: capability.EffectDelete, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true,
		DataSensitivity: locationsRisk.DataSensitivity + "; the location and its public profile are deleted"},
	InputSchema:  inputSchema(nil, "location", locationArgSchema),
	OutputSchema: json.RawMessage(objectSchema("location", stringSchema, "deleted", booleanSchema)),
	Arguments: []capability.Argument{{Name: "location", Target: "location",
		Description: "Location as locations/LOCATION; required unless the connection is bound to a location, and " +
			"then it must be that location"}},
	Fields: []capability.Field{{Name: "location", Description: "locations/LOCATION that was deleted"},
		{Name: "deleted", Description: "true after Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Delete one location",
		Arguments: json.RawMessage(`{"location":"locations/2000002"}`)}},
}

var locationsGetGoogleUpdated = capability.Descriptor{
	ID: locationsGetGoogleUpdatedID, Version: 1, Title: "Get the Google-updated version of a Business Profile location",
	Description: "Read one business location as it appears live on Google Maps and Search, with the fields " +
		"read_mask names, together with pendingMask and diffMask, which name the fields Google changed or has " +
		"pending compared with what the business set. On a connection bound to an account the location is first " +
		"proved to belong to it. " + untrustedNote,
	Tags: []string{"google", "business-profile", "locations"}, Risk: locationsRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"read_mask"}, "location", locationArgSchema, "read_mask", readMaskSchema),
	OutputSchema: json.RawMessage(googleUpdatedSchema),
	Arguments: []capability.Argument{
		{Name: "location", Target: "location", Description: "Location as locations/LOCATION; optional when the " +
			"connection is bound to a location, and then it must be that location"},
		readMaskArgument,
	},
	Fields: []capability.Field{
		{Name: "location", Description: "The location as Google shows it, with the fields of read_mask"},
		{Name: "pendingMask", Description: "Fields with edits that Google has not applied yet"},
		{Name: "diffMask", Description: "Fields that differ between what the business set and what Google shows"}},
	Examples: []capability.Example{{Description: "What Google changed in the title and the address",
		Arguments: json.RawMessage(`{"location":"locations/2000002","read_mask":["title","storefrontAddress"]}`)}},
}

type (
	googleUpdated struct {
		Location    *location `json:"location,omitempty"`
		PendingMask long      `json:"pendingMask,omitempty"`
		DiffMask    long      `json:"diffMask,omitempty"`
	}
	createResult struct {
		location
		RequestID string `json:"request_id"`
	}
	deleteResult struct {
		Location string `json:"location"`
		Deleted  bool   `json:"deleted"`
	}
)

var (
	googleUpdatedSchema = objectSchema("location", locationSchema, "pendingMask", stringSchema,
		"diffMask", stringSchema)
	createResultSchema = strings.TrimSuffix(locationSchema, `},"additionalProperties":false}`) +
		`,"request_id":{"type":"string"}},"additionalProperties":false}`
)

// splitArguments reads the validated arguments as members, so that the fields a call names are known.
func splitArguments(op string, raw json.RawMessage) (map[string]json.RawMessage, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	return members, nil
}

func decodeMember[T any](op string, members map[string]json.RawMessage, name string) (T, error) {
	var value T
	if raw, ok := members[name]; ok {
		if err := json.Unmarshal(raw, &value); err != nil {
			return value, provider.Fail(op, "the validated arguments could not be read")
		}
	}
	return value, nil
}

// fieldMembers are the members of a call that are location fields, among the allowed names.
func fieldMembers(members map[string]json.RawMessage, allowed []string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, name := range allowed {
		if raw, ok := members[name]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			out[name] = raw
		}
	}
	return out
}

// newRequestID is a random UUID, version 4.
func newRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func handleLocationsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create location"
	members, err := splitArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	givenAccount, err := decodeMember[string](op, members, "account")
	if err != nil {
		return nil, err
	}
	account, err := bound.accountOf("account", givenAccount, false)
	if err != nil {
		return nil, err
	}
	fields := fieldMembers(members, createFields)
	var title string
	var cats struct {
		PrimaryCategory *struct {
			Name string `json:"name"`
		} `json:"primaryCategory"`
	}
	_ = json.Unmarshal(fields["title"], &title)
	_ = json.Unmarshal(fields["categories"], &cats)
	switch {
	case strings.TrimSpace(title) == "":
		return nil, invalidArgument("title is required")
	case cats.PrimaryCategory == nil || strings.TrimSpace(cats.PrimaryCategory.Name) == "":
		return nil, invalidArgument("categories.primaryCategory.name is required: Google creates no location " +
			"without a primary category")
	}
	if err := checkConfirmationFits(locationsCreateID, locationCreateConfirmation(members)); err != nil {
		return nil, err
	}
	requestID, err := decodeMember[string](op, members, "request_id")
	if err != nil {
		return nil, err
	}
	switch {
	case requestID == "":
		if requestID, err = newRequestID(); err != nil {
			return nil, provider.Fail(op, "a request ID could not be generated")
		}
	case !requestIDForm.MatchString(requestID):
		return nil, invalidArgument("request_id must be 1 to 50 letters, digits, _ or -")
	}
	validateOnly, err := decodeMember[bool](op, members, "validate_only")
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, provider.Fail(op, "the request body could not be built")
	}
	query := url.Values{"requestId": {requestID}}
	if validateOnly {
		query.Set("validateOnly", "true")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out location
	if err := c.post(ctx, op, locationsCreateID, change{root: informationRoot,
		path: locationsOfPath(account), query: query, body: body, key: account, notFound: notFoundAccount}, &out); err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) && strings.Contains(failed.Message, "outcome of the change is unknown") {
			failed.Message += "; request_id was " + requestID + ", and locations.list shows whether the location exists"
		}
		return nil, err
	}
	return createResult{location: out, RequestID: requestID}, nil
}

func handleLocationsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update location"
	members, err := splitArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	givenLocation, err := decodeMember[string](op, members, "location")
	if err != nil {
		return nil, err
	}
	name, err := bound.locationOf(givenLocation)
	if err != nil {
		return nil, err
	}
	given, err := decodeMember[[]string](op, members, "update_mask")
	if err != nil {
		return nil, err
	}
	mask, err := updateMaskOf(given)
	if err != nil {
		return nil, err
	}
	previous, err := decodeMember[map[string]json.RawMessage](op, members, "previous_values")
	if err != nil {
		return nil, err
	}
	fields := fieldMembers(members, updateFields)
	if err := checkFieldsMatchMask(mask, fields, previous); err != nil {
		return nil, err
	}
	if err := checkConfirmationFits(locationsUpdateID, locationUpdateConfirmation(members)); err != nil {
		return nil, err
	}
	changed := false
	for _, field := range mask {
		if !reflect.DeepEqual(normalized(field, previous[field]), normalized(field, fields[field])) {
			changed = true
		}
	}
	if !changed {
		return nil, invalidArgument("every new value equals its previous value; there is nothing to change")
	}
	validateOnly, err := decodeMember[bool](op, members, "validate_only")
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, provider.Fail(op, "the request body could not be built")
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
	var current location
	if err := c.get(ctx, op, locationsUpdateID, request{root: informationRoot, path: locationPath(name),
		query: readMaskQuery(mask), notFound: notFoundLocation}, &current); err != nil {
		return nil, err
	}
	if stale := staleFields(mask, current, previous); len(stale) > 0 {
		return nil, invalidArgument("previous_values differ from what Google holds now for " +
			strings.Join(stale, ", ") + "; read the location with locations.get and send those values")
	}
	query := url.Values{"updateMask": {strings.Join(mask, ",")}}
	if validateOnly {
		query.Set("validateOnly", "true")
	}
	var out location
	if err := c.patch(ctx, op, locationsUpdateID, change{root: informationRoot, path: locationPath(name),
		query: query, body: body, key: name, notFound: notFoundLocation}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// updateMaskOf checks an update mask against the writable fields and returns it sorted.
func updateMaskOf(given []string) ([]string, error) {
	if len(given) == 0 {
		return nil, invalidArgument("update_mask is required: name at least one field to change")
	}
	mask := make([]string, 0, len(given))
	for _, field := range given {
		switch {
		case !slices.Contains(updateFields, field):
			return nil, invalidArgument("update_mask names a field that cannot be changed")
		case slices.Contains(mask, field):
			return nil, invalidArgument("update_mask names a field twice")
		}
		mask = append(mask, field)
	}
	slices.Sort(mask)
	return mask, nil
}

// checkFieldsMatchMask requires exactly one new and one previous value for each field of the mask and none
// for any other field: a field outside the mask would be silently ignored by Google, or worse, expected.
func checkFieldsMatchMask(mask []string, fields, previous map[string]json.RawMessage) error {
	for _, field := range mask {
		if _, ok := fields[field]; !ok {
			return invalidArgument("update_mask names " + field + " but no new value is given for it")
		}
		if _, ok := previous[field]; !ok {
			return invalidArgument("previous_values has no value for " + field + ": send the value as last read")
		}
	}
	for field := range fields {
		if !slices.Contains(mask, field) {
			return invalidArgument(field + " is given but not named in update_mask")
		}
	}
	for field := range previous {
		if !slices.Contains(mask, field) {
			return invalidArgument("previous_values holds " + field + ", which update_mask does not name")
		}
	}
	return nil
}

// staleFields names the fields of the mask whose previous value is not what the location holds.
func staleFields(mask []string, current location, previous map[string]json.RawMessage) []string {
	encoded, err := json.Marshal(current)
	var held map[string]json.RawMessage
	if err != nil || json.Unmarshal(encoded, &held) != nil {
		return mask
	}
	var stale []string
	for _, field := range mask {
		if !reflect.DeepEqual(normalized(field, held[field]), normalized(field, previous[field])) {
			stale = append(stale, field)
		}
	}
	return stale
}

// normalized decodes a field value for comparison: the members Google answers but never accepts are left
// out and so is everything empty, since a read omits an empty value.
func normalized(field string, raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	dropOutputOnly(field, value)
	return prune(value)
}

func dropOutputOnly(field string, value any) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	switch field {
	case "categories":
		drop := func(category any) {
			if m, ok := category.(map[string]any); ok {
				delete(m, "displayName")
				delete(m, "serviceTypes")
				delete(m, "moreHoursTypes")
			}
		}
		drop(object["primaryCategory"])
		if more, ok := object["additionalCategories"].([]any); ok {
			for _, category := range more {
				drop(category)
			}
		}
	case "openInfo":
		delete(object, "canReopen")
	}
}

func prune(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, member := range v {
			if kept := prune(member); kept == nil {
				delete(v, key)
			} else {
				v[key] = kept
			}
		}
		if len(v) == 0 {
			return nil
		}
	case []any:
		for i := range v {
			v[i] = prune(v[i])
		}
		if len(v) == 0 {
			return nil
		}
	case string:
		if v == "" {
			return nil
		}
	}
	return value
}

func handleLocationsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete location"
	members, err := splitArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	givenLocation, err := decodeMember[string](op, members, "location")
	if err != nil {
		return nil, err
	}
	name, err := bound.locationOf(givenLocation)
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
	var empty struct{}
	if err := c.remove(ctx, op, locationsDeleteID, change{root: informationRoot,
		path: locationPath(name), key: name, notFound: notFoundLocation}, &empty); err != nil {
		return nil, err
	}
	return deleteResult{Location: name, Deleted: true}, nil
}

func handleLocationsGetGoogleUpdated(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get Google-updated location"
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
	var out googleUpdated
	if err := c.get(ctx, op, locationsGetGoogleUpdatedID, request{root: informationRoot,
		path: locationPath(name) + ":getGoogleUpdated", query: readMaskQuery(mask), notFound: notFoundLocation}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func tooLongForConfirmation(id string) error {
	return invalidArgument("the confirmation would need more than " + limitText(id) + " to show every old and new " +
		"value or the title; change fewer fields in one call, or use shorter values")
}

func unconfirmable(id string) string {
	return "This call cannot be confirmed: showing every value needs more than " + limitText(id) +
		", so it is refused"
}

// compactValue is a JSON value as shown in a confirmation.
func compactValue(raw json.RawMessage) string {
	var compact bytes.Buffer
	if json.Compact(&compact, raw) == nil {
		return compact.String()
	}
	return string(raw)
}

// locationCreateConfirmation phrases the confirmation of a create from the arguments alone.
func locationCreateConfirmation(members map[string]json.RawMessage) string {
	fields := fieldMembers(members, createFields)
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	out := "Create public business location"
	if title, ok := fields["title"]; ok {
		out += " " + compactValue(title)
	}
	if account, _ := decodeMember[string]("", members, "account"); account != "" {
		out += " in " + account
	}
	out += " with " + strings.Join(names, ", ") + "; Google may review it"
	if validateOnly, _ := decodeMember[bool]("", members, "validate_only"); validateOnly {
		out += "; validate only"
	}
	return out
}

// locationUpdateConfirmation shows the old and the new value of every field of the mask.
func locationUpdateConfirmation(members map[string]json.RawMessage) string {
	mask, _ := decodeMember[[]string]("", members, "update_mask")
	previous, _ := decodeMember[map[string]json.RawMessage]("", members, "previous_values")
	fields := fieldMembers(members, updateFields)
	slices.Sort(mask)
	out := "Change public data of location"
	if location, _ := decodeMember[string]("", members, "location"); location != "" {
		out += " " + location
	}
	out += ":"
	for _, field := range mask {
		out += " " + field + " " + compactValue(previous[field]) + " -> " + compactValue(fields[field]) + ";"
	}
	out += " Google may review it"
	if validateOnly, _ := decodeMember[bool]("", members, "validate_only"); validateOnly {
		out += "; validate only"
	}
	return out
}

// checkConfirmationFits refuses a call whose confirmation the core would have to cut.
func checkConfirmationFits(id, text string) error {
	if !fitsConfirmation(id, text) {
		return tooLongForConfirmation(id)
	}
	return nil
}

func locationCreateSummary(raw json.RawMessage) string {
	members, err := splitArguments("create location", raw)
	if err != nil {
		return ""
	}
	if text := locationCreateConfirmation(members); checkConfirmationFits(locationsCreateID, text) == nil {
		return text
	}
	return unconfirmable(locationsCreateID)
}

func locationUpdateSummary(raw json.RawMessage) string {
	members, err := splitArguments("update location", raw)
	if err != nil {
		return ""
	}
	if text := locationUpdateConfirmation(members); checkConfirmationFits(locationsUpdateID, text) == nil {
		return text
	}
	return unconfirmable(locationsUpdateID)
}

// locationDeleteSummary names the location and says that the deletion is not reversible.
func locationDeleteSummary(raw json.RawMessage) string {
	members, err := splitArguments("delete location", raw)
	if err != nil {
		return ""
	}
	location, _ := decodeMember[string]("", members, "location")
	if location == "" {
		return "Delete the business location of this connection permanently; this cannot be undone"
	}
	return "Delete the business location " + location + " permanently; this cannot be undone"
}
