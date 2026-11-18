package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	placeActionLinksListID   = Provider + ".place-action-links.list"
	placeActionLinksGetID    = Provider + ".place-action-links.get"
	placeActionLinksCreateID = Provider + ".place-action-links.create"
	placeActionLinksUpdateID = Provider + ".place-action-links.update"
	placeActionLinksDeleteID = Provider + ".place-action-links.delete"
	placeActionTypesListID   = Provider + ".place-action-types.list"
)

// Forms of a link path. The ID of a link is a short token of letters, digits, underscore, and hyphen, so no
// segment can be a path step and one link has one spelling.
const (
	formPlaceActionLink = "locations/LOCATION/placeActionLinks/LINK"

	linkIDPattern          = `[A-Za-z0-9_-]{1,128}`
	placeActionLinkPattern = `^locations/` + idPattern + `/placeActionLinks/` + linkIDPattern + `$`
	linkSegment            = "/placeActionLinks/"
)

var placeActionLinkForm = regexp.MustCompile(placeActionLinkPattern)

// placeActionTypes are the types Google documents, without the unspecified one that no request may name.
var placeActionTypes = []string{"APPOINTMENT", "ONLINE_APPOINTMENT", "DINING_RESERVATION", "FOOD_ORDERING",
	"FOOD_DELIVERY", "FOOD_TAKEOUT", "SHOP_ONLINE", "SOLOPRENEUR_APPOINTMENT"}

// updatableFields are the fields Google lets an update change, in the spelling of its update mask.
var updatableFields = []string{"uri", "place_action_type", "is_preferred", "is_disabled"}

// Bounds of what a call sends and of what a result shows.
const (
	maxLinkURIBytes = 2000
	maxLinksPage    = 100
	maxTypesPage    = 100
	// maxConfirmationRunes mirrors the bound the core puts on a confirmation summary. A summary that would be
	// cut is refused instead, so a person never confirms text that hides part of the address.
	maxConfirmationRunes = 500
)

var (
	typeEnumSchema        = `{"type":"string","enum":` + mustJSON(placeActionTypes) + `}`
	linkArgSchema         = `{"type":"string","maxLength":200,"pattern":"` + placeActionLinkPattern + `","x-form":"` + formPlaceActionLink + `"}`
	linkURISchema         = `{"type":"string","minLength":1,"maxLength":2000}`
	linksFilterRe         = regexp.MustCompile(`^place_action_type=(` + strings.Join(placeActionTypes, "|") + `)$`)
	typesFilterRe         = regexp.MustCompile(`^(location=` + locationPattern[1:len(locationPattern)-1] + `|region_code=([A-Z]{2}|[0-9]{3}))$`)
	languageCodeRe        = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`)
	placeActionMaskSchema = `{"type":"array","minItems":1,"maxItems":4,"uniqueItems":true,"items":{"type":"string","enum":` +
		mustJSON(updatableFields) + `}}`
)

var placeActionNote = "A place action link is a public redirect from the Google profile of the business to an address " +
	"outside Google, shown to every visitor of the profile; the address is text that is never fetched. "

func placeActionWriteRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true,
		DataSensitivity: "public profile content: the address, the type, and the preference of a link that every visitor " +
			"of the business profile sees; a wrong address would send customers to another site"}
}

var placeActionReadRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "public profile content: the addresses, types, and state of the place action links of a business " +
		"location, and the names of the available link types",
}

// The place actions API as its discovery document documents it. The output-only members are only read.
type (
	placeActionLink struct {
		Name            short `json:"name,omitempty"`
		URI             link  `json:"uri,omitempty"`
		PlaceActionType short `json:"placeActionType,omitempty"`
		ProviderType    short `json:"providerType,omitempty"`
		IsEditable      flag  `json:"isEditable,omitzero"`
		IsDisabled      flag  `json:"isDisabled,omitzero"`
		IsPreferred     flag  `json:"isPreferred,omitzero"`
		CreateTime      short `json:"createTime,omitempty"`
		UpdateTime      short `json:"updateTime,omitempty"`
	}
	placeActionLinksAnswer struct {
		PlaceActionLinks []placeActionLink `json:"placeActionLinks"`
		NextPageToken    string            `json:"nextPageToken"`
	}
	placeActionLinksResult struct {
		PlaceActionLinks []placeActionLink `json:"placeActionLinks"`
		NextCursor       string            `json:"next_cursor,omitempty"`
	}
	placeActionType struct {
		DisplayName     line  `json:"displayName,omitempty"`
		PlaceActionType short `json:"placeActionType,omitempty"`
	}
	placeActionTypesAnswer struct {
		PlaceActionTypeMetadata []placeActionType `json:"placeActionTypeMetadata"`
		NextPageToken           string            `json:"nextPageToken"`
	}
	placeActionTypesResult struct {
		PlaceActionTypeMetadata []placeActionType `json:"placeActionTypeMetadata"`
		NextCursor              string            `json:"next_cursor,omitempty"`
	}
	placeActionDeleted struct {
		Deleted bool   `json:"deleted"`
		Link    string `json:"link"`
	}
)

var (
	placeActionLinkSchema = objectSchema("name", stringSchema, "uri", stringSchema, "placeActionType", stringSchema,
		"providerType", stringSchema, "isEditable", booleanSchema, "isDisabled", booleanSchema,
		"isPreferred", booleanSchema, "createTime", stringSchema, "updateTime", stringSchema)
	placeActionLinksResultSchema = `{"type":"object","properties":{"placeActionLinks":` +
		arraySchema(placeActionLinkSchema) + `,"next_cursor":{"type":"string"}},"required":["placeActionLinks"],` +
		`"additionalProperties":false}`
	placeActionTypeSchema        = objectSchema("displayName", stringSchema, "placeActionType", stringSchema)
	placeActionTypesResultSchema = `{"type":"object","properties":{"placeActionTypeMetadata":` +
		arraySchema(placeActionTypeSchema) + `,"next_cursor":{"type":"string"}},"required":["placeActionTypeMetadata"],` +
		`"additionalProperties":false}`
	placeActionDeletedSchema = objectSchema("deleted", booleanSchema, "link", stringSchema)
)

var placeActionLinkFields = []capability.Field{
	{Name: "name", Description: "locations/LOCATION/placeActionLinks/LINK"},
	{Name: "uri", Description: "Address the link leads to, text that is never fetched"},
	{Name: "placeActionType", Description: "APPOINTMENT, ONLINE_APPOINTMENT, DINING_RESERVATION, FOOD_ORDERING, " +
		"FOOD_DELIVERY, FOOD_TAKEOUT, SHOP_ONLINE, or SOLOPRENEUR_APPOINTMENT"},
	{Name: "providerType", Description: "MERCHANT (the business or an agency for it) or AGGREGATOR_3P (a partner such " +
		"as Reserve with Google)"},
	{Name: "isEditable", Description: "Whether the client may change the link"},
	{Name: "isDisabled", Description: "Whether the business disabled the link"},
	{Name: "isPreferred", Description: "Whether the business prefers this link for its type"},
	{Name: "createTime", Description: "When the link was created"},
	{Name: "updateTime", Description: "When the link was last changed"},
}

var (
	linkLocationArgument = capability.Argument{Name: "location", Target: "location", Description: "Location as " +
		"locations/LOCATION; optional when the connection is bound to a location, and then it must be that location"}
	linkArgument = capability.Argument{Name: "link", Required: true, Description: "Path of a link as listed: " +
		formPlaceActionLink}
	uriArgument = capability.Argument{Name: "uri", Description: "Address of the link: https only, ASCII only " +
		"(percent-encode other characters), no user information, no control characters, at most 2000 bytes; the " +
		"whole address is shown in the confirmation, which must fit in 500 characters, and it is never fetched"}
	typeArgument = capability.Argument{Name: "place_action_type", Description: "Type of the action: " +
		strings.Join(placeActionTypes, ", ")}
	preferredArgument = capability.Argument{Name: "is_preferred", Description: "true marks the link as the " +
		"preferred one of its type at the location; a preferred link of the same type loses that mark"}
	disabledArgument = capability.Argument{Name: "is_disabled", Description: "true disables the link; Google " +
		"accepts it only for a link of a partner (AGGREGATOR_3P)"}
)

var placeActionLinksList = capability.Descriptor{
	ID: placeActionLinksListID, Version: 1, Title: "List place action links of a Business Profile location",
	Description: "List the place action links (such as ordering, booking, or reserving) of one business location, " +
		"10 per call unless page_size says otherwise, with a cursor to read on; an empty list means none. filter " +
		"is place_action_type=TYPE. On a connection bound to an account the location is first proved to belong to " +
		"it. " + placeActionNote + untrustedNote,
	Tags: []string{"google", "business-profile", "place-action-links"}, Risk: placeActionReadRisk, Provider: Provider,
	InputSchema: inputSchema(nil,
		"location", locationArgSchema,
		"filter", `{"type":"string","minLength":1,"maxLength":60}`,
		"page_size", `{"type":"integer","minimum":1,"maximum":100}`,
		"cursor", cursorSchema),
	OutputSchema: json.RawMessage(placeActionLinksResultSchema),
	Arguments: []capability.Argument{linkLocationArgument,
		{Name: "filter", Description: "place_action_type=TYPE, the only filter Google supports; TYPE is one of " +
			strings.Join(placeActionTypes, ", ")},
		{Name: "page_size", Description: "Links per call, 1 to 100; Google's default is 10"}, cursorArgument},
	Fields: []capability.Field{{Name: "placeActionLinks", Description: "Links with name, uri, placeActionType, " +
		"providerType, isEditable, isDisabled, isPreferred, createTime, and updateTime"}, nextCursorField},
	Examples: []capability.Example{{Description: "Links of one location for ordering food",
		Arguments: json.RawMessage(`{"location":"locations/2000002","filter":"place_action_type=FOOD_ORDERING"}`)}},
}

var placeActionLinksGet = capability.Descriptor{
	ID: placeActionLinksGetID, Version: 1, Title: "Get a place action link of a Business Profile location",
	Description: "Read one place action link by its path. On a connection bound to an account the location of the " +
		"link is first proved to belong to it. " + placeActionNote + untrustedNote,
	Tags: []string{"google", "business-profile", "place-action-links"}, Risk: placeActionReadRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"link"}, "link", linkArgSchema),
	OutputSchema: json.RawMessage(placeActionLinkSchema),
	Arguments:    []capability.Argument{linkArgument},
	Fields:       placeActionLinkFields,
	Examples: []capability.Example{{Description: "One link",
		Arguments: json.RawMessage(`{"link":"locations/2000002/placeActionLinks/4000004"}`)}},
}

var placeActionLinksCreate = capability.Descriptor{
	ID: placeActionLinksCreateID, Version: 1, Title: "Create a place action link of a Business Profile location",
	Description: "Create a place action link at one business location. The link is visible on the public profile at " +
		"once, so the confirmation shows the whole address, the type, and the location, and the tool refuses " +
		"before sending when that text does not fit in 500 characters. The address must be https. Google allows " +
		"one link per address, type, and location. On a connection bound to an account the location is first " +
		"proved to belong to it. Google counts at most 10 changes per minute and profile. " + placeActionNote +
		untrustedNote,
	Tags: []string{"google", "business-profile", "place-action-links", "create"}, Provider: Provider,
	Risk: placeActionWriteRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: inputSchema([]string{"uri", "place_action_type"},
		"location", locationArgSchema, "uri", linkURISchema, "place_action_type", typeEnumSchema,
		"is_preferred", booleanSchema, "is_disabled", booleanSchema),
	OutputSchema: json.RawMessage(placeActionLinkSchema),
	Arguments: []capability.Argument{{Name: "location", Target: "location", Description: "Location as " +
		"locations/LOCATION, which the confirmation names, so the tool refuses a call without it, also on a " +
		"connection bound to a location; there it must be that location"}, withRequired(uriArgument), withRequired(typeArgument), preferredArgument, disabledArgument},
	Fields: placeActionLinkFields,
	Examples: []capability.Example{{Description: "A link for online booking", Arguments: json.RawMessage(
		`{"location":"locations/2000002","uri":"https://booking.example.com/shop","place_action_type":"APPOINTMENT"}`)}},
}

var placeActionLinksUpdate = capability.Descriptor{
	ID: placeActionLinksUpdateID, Version: 1, Title: "Change a place action link of a Business Profile location",
	Description: "Change fields of a place action link. update_mask is required and names exactly the fields given: " +
		strings.Join(updatableFields, ", ") + "; a field without a value in the mask, or a value without its field " +
		"in the mask, is refused. The confirmation shows the link, which holds the location, and each changed " +
		"field with its new value, the whole address included; the tool refuses before sending when that text " +
		"does not fit in 500 characters. The address must be https. On a connection bound to an account the " +
		"location is first proved to belong to it. Google counts at most 10 changes per minute and profile. " +
		placeActionNote + untrustedNote,
	Tags: []string{"google", "business-profile", "place-action-links", "update"}, Provider: Provider,
	Risk: placeActionWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"link", "update_mask"},
		"link", linkArgSchema, "update_mask", placeActionMaskSchema, "uri", linkURISchema,
		"place_action_type", typeEnumSchema, "is_preferred", booleanSchema, "is_disabled", booleanSchema),
	OutputSchema: json.RawMessage(placeActionLinkSchema),
	Arguments: []capability.Argument{linkArgument,
		{Name: "update_mask", Required: true, Description: "Fields to change, each with a value: " +
			strings.Join(updatableFields, ", ")},
		uriArgument, typeArgument, preferredArgument, disabledArgument},
	Fields: placeActionLinkFields,
	Examples: []capability.Example{{Description: "Move a link to another address", Arguments: json.RawMessage(
		`{"link":"locations/2000002/placeActionLinks/4000004","update_mask":["uri"],` +
			`"uri":"https://booking.example.com/new"}`)}},
}

var placeActionLinksDelete = capability.Descriptor{
	ID: placeActionLinksDeleteID, Version: 1, Title: "Delete a place action link of a Business Profile location",
	Description: "Delete one place action link; it disappears from the public profile at once. The confirmation shows " +
		"the link, which holds the location. On a connection bound to an account the location is first proved to " +
		"belong to it. Google counts at most 10 changes per minute and profile. " + placeActionNote + untrustedNote,
	Tags: []string{"google", "business-profile", "place-action-links", "delete"}, Provider: Provider,
	Risk:         placeActionWriteRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:  inputSchema([]string{"link"}, "link", linkArgSchema),
	OutputSchema: json.RawMessage(placeActionDeletedSchema),
	Arguments:    []capability.Argument{linkArgument},
	Fields: []capability.Field{{Name: "deleted", Description: "true when Google accepted the deletion"},
		{Name: "link", Description: "Path of the deleted link"}},
	Examples: []capability.Example{{Description: "Delete one link",
		Arguments: json.RawMessage(`{"link":"locations/2000002/placeActionLinks/4000004"}`)}},
}

var placeActionTypesList = capability.Descriptor{
	ID: placeActionTypesListID, Version: 1, Title: "List place action link types",
	Description: "List the types of place action links Google offers, with their display name in language_code or " +
		"in English, 10 per call unless page_size says otherwise, with a cursor to read on. filter narrows the " +
		"list to the types available at a location (location=locations/LOCATION) or in a region " +
		"(region_code=CODE); with a location, a connection bound to an account first proves that the location " +
		"belongs to it. " + untrustedNote,
	Tags: []string{"google", "business-profile", "place-action-types"}, Risk: placeActionReadRisk, Provider: Provider,
	InputSchema: inputSchema(nil,
		"language_code", `{"type":"string","minLength":2,"maxLength":35}`,
		"filter", `{"type":"string","minLength":1,"maxLength":60}`,
		"page_size", `{"type":"integer","minimum":1,"maximum":100}`,
		"cursor", cursorSchema),
	OutputSchema: json.RawMessage(placeActionTypesResultSchema),
	Arguments: []capability.Argument{
		{Name: "language_code", Description: "IETF BCP-47 code of the language of the display names; English when " +
			"Google has none for it"},
		{Name: "filter", Description: "location=locations/LOCATION or region_code=CODE (a CLDR region code such as " +
			"DE or 419), the only filters Google supports; leave it out for every type"},
		{Name: "page_size", Description: "Types per call, 1 to 100; Google's default is 10"}, cursorArgument},
	Fields: []capability.Field{{Name: "placeActionTypeMetadata", Description: "Types with displayName and " +
		"placeActionType"}, nextCursorField},
	Examples: []capability.Example{{Description: "Types available in Germany, named in German",
		Arguments: json.RawMessage(`{"language_code":"de","filter":"region_code=DE"}`)}},
}

func withRequired(a capability.Argument) capability.Argument {
	a.Required = true
	return a
}

type placeActionArguments struct {
	Location        string   `json:"location"`
	Link            string   `json:"link"`
	URI             *string  `json:"uri"`
	PlaceActionType *string  `json:"place_action_type"`
	IsPreferred     *bool    `json:"is_preferred"`
	IsDisabled      *bool    `json:"is_disabled"`
	UpdateMask      []string `json:"update_mask"`
	Filter          string   `json:"filter"`
	LanguageCode    string   `json:"language_code"`
	PageSize        int      `json:"page_size"`
	Cursor          string   `json:"cursor"`
}

func readPlaceActionArguments(op string, raw json.RawMessage) (placeActionArguments, error) {
	var args placeActionArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, provider.Fail(op, "the validated arguments could not be read")
	}
	return args, nil
}

// linkOf checks the path of a link against its form and the target and returns the location it lies under.
// A location target refuses a link of another location; no message names the other object.
func (s scope) linkOf(given string) (string, error) {
	if !placeActionLinkForm.MatchString(given) {
		return "", invalidArgument("link must be " + formPlaceActionLink + " with a decimal location ID")
	}
	parent := given[:strings.Index(given, linkSegment)]
	if s.location != "" && parent != s.location {
		return "", invalidArgument("link " + outsideTarget)
	}
	return parent, nil
}

// checkLinkURI refuses an address that is not a plain https address. The address is never fetched, so the
// checks only keep a link from carrying anything a person could misread in the confirmation: user
// information that disguises the host, a scheme other than https, control, format, and space characters,
// and anything beyond ASCII.
func checkLinkURI(uri string) error {
	switch {
	case uri == "" || len(uri) > maxLinkURIBytes:
		return invalidArgument("uri must be 1 to 2000 bytes")
	case !strings.HasPrefix(uri, "https://"):
		return invalidArgument("uri must start with https://")
	}
	for _, r := range uri {
		if r > unicode.MaxASCII || r == '\\' || unicode.IsControl(r) || unicode.IsSpace(r) ||
			unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return invalidArgument("uri must be ASCII without spaces, control characters, or backslashes; " +
				"percent-encode other characters")
		}
	}
	parsed, err := url.Parse(uri)
	switch {
	case err != nil || parsed.Scheme != "https" || parsed.Opaque != "":
		return invalidArgument("uri is not a valid https address")
	case parsed.Hostname() == "":
		return invalidArgument("uri must name a host")
	case parsed.User != nil || strings.Contains(parsed.Host, "@"):
		return invalidArgument("uri must not contain user information")
	}
	return nil
}

// checkLinkFields checks the values of a create or update call and, for an update, that the mask names
// exactly the fields given.
func checkLinkFields(args placeActionArguments, update bool) error {
	if args.URI != nil {
		if err := checkLinkURI(*args.URI); err != nil {
			return err
		}
	}
	if args.PlaceActionType != nil && !slices.Contains(placeActionTypes, *args.PlaceActionType) {
		return invalidArgument("place_action_type must be one of " + strings.Join(placeActionTypes, ", "))
	}
	if !update {
		switch {
		case args.Location == "":
			return invalidArgument("location is required: the confirmation names it")
		case args.URI == nil:
			return invalidArgument("uri is required")
		case args.PlaceActionType == nil:
			return invalidArgument("place_action_type is required")
		case len(args.UpdateMask) > 0:
			return invalidArgument("update_mask belongs to the update tool")
		}
		return nil
	}
	given := givenFields(args)
	if len(args.UpdateMask) == 0 {
		return invalidArgument("update_mask is required: name the fields to change")
	}
	mask := slices.Clone(args.UpdateMask)
	slices.Sort(mask)
	for i, field := range mask {
		switch {
		case !slices.Contains(updatableFields, field):
			return invalidArgument("update_mask names a field that cannot be changed")
		case i > 0 && mask[i-1] == field:
			return invalidArgument("update_mask names a field twice")
		}
	}
	if !slices.Equal(mask, given) {
		return invalidArgument("update_mask must name exactly the fields that are given with a value: a field " +
			"in the mask without a value would be cleared, a value outside the mask would be ignored")
	}
	return nil
}

// givenFields lists the updatable fields that carry a value, sorted.
func givenFields(args placeActionArguments) []string {
	var given []string
	for field, set := range map[string]bool{"uri": args.URI != nil, "place_action_type": args.PlaceActionType != nil,
		"is_preferred": args.IsPreferred != nil, "is_disabled": args.IsDisabled != nil} {
		if set {
			given = append(given, field)
		}
	}
	slices.Sort(given)
	return given
}

// linkBody builds the request body from the given fields only.
func linkBody(op string, args placeActionArguments) ([]byte, error) {
	body := struct {
		URI             *string `json:"uri,omitempty"`
		PlaceActionType *string `json:"placeActionType,omitempty"`
		IsPreferred     *bool   `json:"isPreferred,omitempty"`
		IsDisabled      *bool   `json:"isDisabled,omitempty"`
	}{args.URI, args.PlaceActionType, args.IsPreferred, args.IsDisabled}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, provider.Fail(op, "the request body could not be built")
	}
	return raw, nil
}

// confirmationText phrases what a confirmed create, update, or delete would do, with every address and
// value whole. ok is false when the text does not fit in the confirmation of the core.
func confirmationText(tool string, args placeActionArguments) (text string, ok bool) {
	var b strings.Builder
	switch tool {
	case placeActionLinksCreateID:
		b.WriteString("Create a place action link, visible on the public profile of " + args.Location + ": type " + deref(args.PlaceActionType) + ", address " + deref(args.URI) +
			" (never fetched by Qatlas)")
		writeFlags(&b, args)
	case placeActionLinksUpdateID:
		b.WriteString("Change the public place action link " + args.Link + ": ")
		var parts []string
		if args.URI != nil {
			parts = append(parts, "address to "+*args.URI)
		}
		if args.PlaceActionType != nil {
			parts = append(parts, "type to "+*args.PlaceActionType)
		}
		if args.IsPreferred != nil {
			parts = append(parts, "preferred to "+boolText(*args.IsPreferred))
		}
		if args.IsDisabled != nil {
			parts = append(parts, "disabled to "+boolText(*args.IsDisabled))
		}
		b.WriteString(strings.Join(parts, ", "))
	case placeActionLinksDeleteID:
		b.WriteString("Delete the place action link " + args.Link + ", which disappears from the public profile")
	}
	text = b.String()
	return text, utf8.RuneCountInString(text) <= maxConfirmationRunes
}

func writeFlags(b *strings.Builder, args placeActionArguments) {
	if args.IsPreferred != nil {
		b.WriteString(", preferred " + boolText(*args.IsPreferred))
	}
	if args.IsDisabled != nil {
		b.WriteString(", disabled " + boolText(*args.IsDisabled))
	}
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// tooLong is the refusal of a change whose confirmation would not show everything.
const tooLong = "the confirmation of this change would exceed 500 characters, so it could not show the whole " +
	"address; use a shorter address"

// placeActionSummary phrases a confirmation from the arguments alone. A call that the handler refuses says so
// instead of asking for a confirmation of what will not happen.
func placeActionSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var args placeActionArguments
		if json.Unmarshal(raw, &args) != nil {
			return ""
		}
		if tool != placeActionLinksDeleteID {
			if err := checkLinkFields(args, tool == placeActionLinksUpdateID); err != nil {
				return "Refused before sending: " + err.Error()
			}
		}
		text, ok := confirmationText(tool, args)
		if !ok {
			return "Refused before sending: " + tooLong
		}
		return text
	}
}

// proveFor proves, for a connection bound to an account, that the location belongs to it.
func (c *client) proveFor(ctx context.Context, op, location string) error {
	if c.scope.account == "" {
		return nil
	}
	return c.proveLocation(ctx, op, c.scope.account, location)
}

func handlePlaceActionLinksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list place action links"
	args, err := readPlaceActionArguments(op, raw)
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
	if args.Filter != "" && !linksFilterRe.MatchString(args.Filter) {
		return nil, invalidArgument("filter must be place_action_type=TYPE with one of " +
			strings.Join(placeActionTypes, ", "))
	}
	binding := provider.CursorBinding(placeActionLinksListID, name, args.Filter, bound.binding())
	query := url.Values{}
	if args.Filter != "" {
		query.Set("filter", args.Filter)
	}
	query = pageSizeQuery(query, args.PageSize)
	if query, err = pageQuery(binding, args.Cursor, query); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveFor(ctx, op, name); err != nil {
		return nil, err
	}
	var answer placeActionLinksAnswer
	if err := c.get(ctx, op, placeActionLinksListID, request{root: placeActionsRoot,
		path: name + "/placeActionLinks", query: query, notFound: notFoundLocation}, &answer); err != nil {
		return nil, err
	}
	if len(answer.PlaceActionLinks) > maxLinksPage {
		return nil, tooMany(op, maxLinksPage)
	}
	out := placeActionLinksResult{PlaceActionLinks: append([]placeActionLink{}, answer.PlaceActionLinks...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handlePlaceActionLinksGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get place action link"
	args, err := readPlaceActionArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	parent, err := bound.linkOf(args.Link)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveFor(ctx, op, parent); err != nil {
		return nil, err
	}
	var one placeActionLink
	if err := c.get(ctx, op, placeActionLinksGetID, request{root: placeActionsRoot, path: args.Link,
		notFound: notFoundLink}, &one); err != nil {
		return nil, err
	}
	return one, nil
}

const notFoundLink = "Google did not find the place action link or its location for this credential; check the " +
	"link and that the consenting account manages the location"

func handlePlaceActionLinksCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create place action link"
	args, err := readPlaceActionArguments(op, raw)
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
	if err := checkLinkFields(args, false); err != nil {
		return nil, err
	}
	args.Location = name
	if _, ok := confirmationText(placeActionLinksCreateID, args); !ok {
		return nil, invalidArgument(tooLong)
	}
	body, err := linkBody(op, args)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveFor(ctx, op, name); err != nil {
		return nil, err
	}
	var out placeActionLink
	if err := c.patch(ctx, op, placeActionLinksCreateID, change{method: http.MethodPost, root: placeActionsRoot,
		path: name + "/placeActionLinks", body: body, key: name, notFound: notFoundLocation}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handlePlaceActionLinksUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "change place action link"
	args, err := readPlaceActionArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	parent, err := bound.linkOf(args.Link)
	if err != nil {
		return nil, err
	}
	if err := checkLinkFields(args, true); err != nil {
		return nil, err
	}
	if _, ok := confirmationText(placeActionLinksUpdateID, args); !ok {
		return nil, invalidArgument(tooLong)
	}
	body, err := linkBody(op, args)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveFor(ctx, op, parent); err != nil {
		return nil, err
	}
	query := url.Values{"updateMask": {strings.Join(sortedMask(args.UpdateMask), ",")}}
	var out placeActionLink
	if err := c.patch(ctx, op, placeActionLinksUpdateID, change{root: placeActionsRoot, path: args.Link, query: query,
		body: body, key: parent, notFound: notFoundLink}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func sortedMask(mask []string) []string {
	out := slices.Clone(mask)
	slices.Sort(out)
	return out
}

func handlePlaceActionLinksDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete place action link"
	args, err := readPlaceActionArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	parent, err := bound.linkOf(args.Link)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveFor(ctx, op, parent); err != nil {
		return nil, err
	}
	var empty struct{}
	if err := c.patch(ctx, op, placeActionLinksDeleteID, change{method: http.MethodDelete, root: placeActionsRoot,
		path: args.Link, key: parent, notFound: notFoundLink}, &empty); err != nil {
		return nil, err
	}
	return placeActionDeleted{Deleted: true, Link: args.Link}, nil
}

func handlePlaceActionTypesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list place action types"
	args, err := readPlaceActionArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	if args.LanguageCode != "" && !languageCodeRe.MatchString(args.LanguageCode) {
		return nil, invalidArgument("language_code must be a BCP-47 code such as en or pt-BR")
	}
	filterLocation := ""
	if args.Filter != "" {
		if !typesFilterRe.MatchString(args.Filter) {
			return nil, invalidArgument("filter must be location=locations/LOCATION or region_code=CODE")
		}
		if value, found := strings.CutPrefix(args.Filter, "location="); found {
			if filterLocation, err = bound.locationOf(value); err != nil {
				return nil, err
			}
		}
	}
	binding := provider.CursorBinding(placeActionTypesListID, args.Filter, args.LanguageCode, bound.binding())
	query := url.Values{}
	if args.Filter != "" {
		query.Set("filter", args.Filter)
	}
	if args.LanguageCode != "" {
		query.Set("languageCode", args.LanguageCode)
	}
	query = pageSizeQuery(query, args.PageSize)
	if query, err = pageQuery(binding, args.Cursor, query); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if filterLocation != "" {
		if err := c.proveFor(ctx, op, filterLocation); err != nil {
			return nil, err
		}
	}
	var answer placeActionTypesAnswer
	if err := c.get(ctx, op, placeActionTypesListID, request{root: placeActionsRoot, path: "placeActionTypeMetadata",
		query: query, notFound: notFoundLocation}, &answer); err != nil {
		return nil, err
	}
	if len(answer.PlaceActionTypeMetadata) > maxTypesPage {
		return nil, tooMany(op, maxTypesPage)
	}
	out := placeActionTypesResult{PlaceActionTypeMetadata: append([]placeActionType{}, answer.PlaceActionTypeMetadata...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}
