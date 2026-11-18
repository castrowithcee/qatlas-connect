package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	attributesListID = Provider + ".attributes.list"
	attributesGetID  = Provider + ".attributes.get"
)

// maxMetadataPage is the largest page of attribute metadata a call asks for. The discovery document names
// 200 as the default page size and no maximum, so 200 is the bound this package relies on.
const maxMetadataPage = 200

const attributesNote = "Attribute names, values, and addresses are untrusted content of whoever edits the " +
	"profile: texts and lists are cut, and addresses (URI values) are text that is never fetched."

var attributesRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "public business profile attributes: amenities, payment types, and links such as booking or " +
		"menu addresses of a business location",
}

// The business information API as its discovery document, revision 20261116 (read 2026-11-18), documents
// attributes. A value that is neither a string nor a boolean is dropped, as is a value of the wrong type.
type (
	attribute struct {
		Name              short          `json:"name,omitempty"`
		ValueType         short          `json:"valueType,omitempty"`
		Values            attrValues     `json:"values,omitempty"`
		URIValues         list[uriValue] `json:"uriValues,omitempty"`
		RepeatedEnumValue *repeatedEnum  `json:"repeatedEnumValue,omitempty"`
	}
	uriValue struct {
		URI link `json:"uri,omitempty"`
	}
	repeatedEnum struct {
		SetValues   list[short] `json:"setValues,omitempty"`
		UnsetValues list[short] `json:"unsetValues,omitempty"`
	}
	attributesAnswer struct {
		Name       short           `json:"name,omitempty"`
		Attributes list[attribute] `json:"attributes,omitempty"`
	}
	attributeMetadata struct {
		DisplayName      line                `json:"displayName,omitempty"`
		Deprecated       flag                `json:"deprecated,omitzero"`
		ValueMetadata    list[valueMetadata] `json:"valueMetadata,omitempty"`
		Repeatable       flag                `json:"repeatable,omitzero"`
		GroupDisplayName line                `json:"groupDisplayName,omitempty"`
		Parent           short               `json:"parent,omitempty"`
		ValueType        short               `json:"valueType,omitempty"`
	}
	valueMetadata struct {
		DisplayName line      `json:"displayName,omitempty"`
		Value       attrValue `json:"value,omitzero"`
	}
	metadataAnswer struct {
		AttributeMetadata []attributeMetadata `json:"attributeMetadata"`
		NextPageToken     string              `json:"nextPageToken"`
	}
	metadataResult struct {
		AttributeMetadata []attributeMetadata `json:"attributeMetadata"`
		NextCursor        string              `json:"next_cursor,omitempty"`
	}
)

// attrValue is the value of an attribute: a string, cut like an identifier, or a boolean. Anything else is
// dropped and leaves the zero value.
type attrValue struct{ v any }

func (a *attrValue) UnmarshalJSON(b []byte) error {
	*a = attrValue{}
	switch strings.TrimSpace(string(b)) {
	case "true":
		a.v = true
	case "false":
		a.v = false
	default:
		var s string
		if json.Unmarshal(b, &s) == nil && strings.HasPrefix(strings.TrimSpace(string(b)), `"`) {
			a.v = clipJSON(b, maxShortRunes)
		}
	}
	return nil
}

func (a attrValue) MarshalJSON() ([]byte, error) { return json.Marshal(a.v) }

func (a attrValue) IsZero() bool { return a.v == nil }

// attrValues is a list of attribute values cut to maxItems; values that are dropped leave no entry.
type attrValues []any

func (l *attrValues) UnmarshalJSON(b []byte) error {
	*l = nil
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	for _, item := range raw {
		var v attrValue
		_ = v.UnmarshalJSON(item)
		if v.IsZero() {
			continue
		}
		if len(*l) >= maxItems {
			break
		}
		*l = append(*l, v.v)
	}
	return nil
}

var (
	attrValueSchema = `{"anyOf":[` + booleanSchema + `,` + stringSchema + `]}`
	attributeSchema = objectSchema("name", stringSchema, "valueType", stringSchema,
		"values", arraySchema(attrValueSchema),
		"uriValues", arraySchema(objectSchema("uri", stringSchema)),
		"repeatedEnumValue", objectSchema("setValues", arraySchema(stringSchema), "unsetValues", arraySchema(stringSchema)))
	attributesResultSchema = objectSchema("name", stringSchema, "attributes", arraySchema(attributeSchema))
	metadataSchema         = objectSchema("displayName", stringSchema, "deprecated", booleanSchema,
		"valueMetadata", arraySchema(objectSchema("displayName", stringSchema, "value", attrValueSchema)),
		"repeatable", booleanSchema, "groupDisplayName", stringSchema, "parent", stringSchema, "valueType", stringSchema)
	metadataResultSchema = `{"type":"object","properties":{"attributeMetadata":` + arraySchema(metadataSchema) +
		`,"next_cursor":{"type":"string"}},"required":["attributeMetadata"],"additionalProperties":false}`
)

const (
	categoryNamePattern = `^categories/[A-Za-z0-9_:.-]{1,100}$`
	regionCodePattern   = `^[A-Z]{2}$`
	languageCodePattern = `^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`
)

var attributesList = capability.Descriptor{
	ID: attributesListID, Version: 1, Title: "List Business Profile attribute metadata",
	Description: "List which attributes a business location can have, with their names, value types, possible " +
		"values, group, and whether Google deprecated them, up to 200 per call with a cursor to read on. Name a " +
		"location for the attributes of that location, or leave it out and give a region_code, optionally with " +
		"category_name. show_all lists every attribute and ignores location and category_name; region_code is " +
		"then still required without a location. parent of an entry is the name to use in attributes.update. " +
		"Attributes marked deprecated are not changed by attributes.update. " + attributesNote,
	Tags: []string{"google", "business-profile", "attributes"}, Risk: attributesRisk, Provider: Provider,
	InputSchema: inputSchema(nil,
		"location", locationArgSchema,
		"category_name", `{"type":"string","maxLength":120,"pattern":"`+categoryNamePattern+`"}`,
		"region_code", `{"type":"string","pattern":"`+regionCodePattern+`"}`,
		"language_code", `{"type":"string","maxLength":35,"pattern":"`+languageCodePattern+`"}`,
		"show_all", booleanSchema,
		"page_size", `{"type":"integer","minimum":1,"maximum":200}`,
		"cursor", cursorSchema),
	OutputSchema: json.RawMessage(metadataResultSchema),
	Arguments: []capability.Argument{
		{Name: "location", Target: "location", Description: "Location as locations/LOCATION whose attributes to " +
			"list; on a connection bound to an account it is proved to belong to it first; on one bound to a " +
			"location it must be that location. Required unless region_code is given"},
		{Name: "category_name", Description: "Primary category as categories/CATEGORY to list the attributes of"},
		{Name: "region_code", Description: "Region as ISO 3166-1 alpha-2 code in capitals, such as CH; required " +
			"unless location is given"},
		{Name: "language_code", Description: "BCP 47 language of the display names, such as de; English if Google " +
			"has no translation"},
		{Name: "show_all", Description: "true: list all attributes and ignore location and category_name; " +
			"default false"},
		{Name: "page_size", Description: "Entries per call, 1 to 200; Google's default is 200"},
		cursorArgument,
	},
	Fields: []capability.Field{
		{Name: "attributeMetadata", Description: "Attributes with displayName, valueType (BOOL, ENUM, URL, or " +
			"REPEATED_ENUM), valueMetadata (displayName and value of each possible value), repeatable, " +
			"groupDisplayName, parent (the attribute name), and deprecated"},
		nextCursorField,
	},
	Examples: []capability.Example{{Description: "Attributes for a region", Arguments: json.RawMessage(
		`{"region_code":"CH","language_code":"en"}`)}},
}

var attributesGet = capability.Descriptor{
	ID: attributesGetID, Version: 1, Title: "Get Business Profile attributes",
	Description: "Read the attributes set on one business location with their values: a boolean or enumeration " +
		"in values, addresses in uriValues, and set and unset values of a repeated enumeration. At most 250 " +
		"attributes are shown. A connection bound to an account first proves from the location list of the " +
		"account that the location belongs to it, reading at most 2000 locations; a connection bound to a " +
		"location reads only that location. " + attributesNote,
	Tags: []string{"google", "business-profile", "attributes"}, Risk: attributesRisk, Provider: Provider,
	InputSchema:  inputSchema(nil, "location", locationArgSchema),
	OutputSchema: json.RawMessage(attributesResultSchema),
	Arguments: []capability.Argument{
		{Name: "location", Target: "location", Description: "Location as locations/LOCATION; optional when the " +
			"connection is bound to a location, and then it must be that location"},
	},
	Fields: []capability.Field{
		{Name: "name", Description: "locations/LOCATION/attributes"},
		{Name: "attributes", Description: "Attributes with name (attributes/ATTRIBUTE), valueType, values, " +
			"uriValues, and repeatedEnumValue"},
	},
	Examples: []capability.Example{{Description: "Attributes of one location", Arguments: json.RawMessage(
		`{"location":"locations/2000002"}`)}},
}

type attributesListArguments struct {
	Location     string `json:"location"`
	CategoryName string `json:"category_name"`
	RegionCode   string `json:"region_code"`
	LanguageCode string `json:"language_code"`
	ShowAll      bool   `json:"show_all"`
	PageSize     int    `json:"page_size"`
	Cursor       string `json:"cursor"`
}

func attributesPath(location string) string { return locationPath(location) + "/attributes" }

func handleAttributesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "list attribute metadata"
	var args attributesListArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	var name string
	if args.Location != "" {
		if name, err = bound.locationOf(args.Location); err != nil {
			return nil, err
		}
	} else if args.RegionCode == "" {
		return nil, invalidArgument("region_code is required unless location is given")
	}
	binding := provider.CursorBinding(attributesListID, name, args.CategoryName, args.RegionCode, args.LanguageCode,
		args.ShowAll, bound.binding())
	query, err := pageQuery(binding, args.Cursor, metadataQuery(name, args))
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if name != "" && bound.account != "" {
		if err := c.proveLocation(ctx, op, bound.account, name); err != nil {
			return nil, err
		}
	}
	var answer metadataAnswer
	if err := c.get(ctx, op, attributesListID, request{root: informationRoot, path: "attributes", query: query,
		notFound: notFoundLocation}, &answer); err != nil {
		return nil, err
	}
	if len(answer.AttributeMetadata) > maxMetadataPage {
		return nil, tooMany(op, maxMetadataPage)
	}
	out := metadataResult{AttributeMetadata: append([]attributeMetadata{}, answer.AttributeMetadata...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func metadataQuery(location string, args attributesListArguments) url.Values {
	query := url.Values{}
	if location != "" {
		query.Set("parent", location)
	}
	if args.CategoryName != "" {
		query.Set("categoryName", args.CategoryName)
	}
	if args.RegionCode != "" {
		query.Set("regionCode", args.RegionCode)
	}
	if args.LanguageCode != "" {
		query.Set("languageCode", args.LanguageCode)
	}
	if args.ShowAll {
		query.Set("showAll", "true")
	}
	return pageSizeQuery(query, args.PageSize)
}

func handleAttributesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get attributes"
	var args struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
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
	var out attributesAnswer
	if err := c.get(ctx, op, attributesGetID, request{root: informationRoot, path: attributesPath(name),
		notFound: notFoundLocation}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
