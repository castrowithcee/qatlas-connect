package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	// Fallback zone data for systems without a zoneinfo directory (Windows). Go reads the system database first.
	_ "time/tzdata"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	propertiesCreateID = Provider + ".properties.create"
	propertiesUpdateID = Provider + ".properties.update"
	propertiesDeleteID = Provider + ".properties.delete"
)

// writeRisk is the risk of a changing tool of this provider; delete is the destructive one.
func writeRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true, DataSensitivity: dataSensitivity}
}

// Bounds and enumerations of the Property resource, read from the Discovery document analyticsadmin v1beta
// (revision 20261111). Google documents the display name as at most 100 UTF-16 code units, the currency as an
// ISO 4217 code and the time zone as an IANA name. The industry categories are Google's enumeration without
// INDUSTRY_CATEGORY_UNSPECIFIED, which names no category.
const (
	maxDisplayNameUnits  = 100
	maxTimeZoneBytes     = 64
	propertyTypeOrdinary = "PROPERTY_TYPE_ORDINARY"
)

var industryCategories = []string{"AUTOMOTIVE", "BUSINESS_AND_INDUSTRIAL_MARKETS", "FINANCE", "HEALTHCARE",
	"TECHNOLOGY", "TRAVEL", "OTHER", "ARTS_AND_ENTERTAINMENT", "BEAUTY_AND_FITNESS", "BOOKS_AND_LITERATURE",
	"FOOD_AND_DRINK", "GAMES", "HOBBIES_AND_LEISURE", "HOME_AND_GARDEN", "INTERNET_AND_TELECOM",
	"LAW_AND_GOVERNMENT", "NEWS", "ONLINE_COMMUNITIES", "PEOPLE_AND_SOCIETY", "PETS_AND_ANIMALS", "REAL_ESTATE",
	"REFERENCE", "SCIENCE", "SPORTS", "JOBS_AND_EDUCATION", "SHOPPING"}

var (
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	zonePattern     = regexp.MustCompile(`^[A-Z][A-Za-z0-9_+-]*(/[A-Z][A-Za-z0-9_+-]*){0,2}$`)
)

// checkDisplayName refuses an empty or blank name, one that is not valid text or holds a control character,
// and one longer than Google accepts. The refusal never quotes the name.
func checkDisplayName(value string) error {
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return badRequest("display_name must be non-blank text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest("display_name must not hold control characters")
		}
	}
	if len(utf16.Encode([]rune(value))) > maxDisplayNameUnits {
		return badRequest("display_name must be at most 100 UTF-16 code units")
	}
	return nil
}

// checkCurrency accepts the form of an ISO 4217 code, three capital letters. Qatlas holds no list of
// currencies: which codes Analytics supports is Google's to decide and it answers a code it does not know.
func checkCurrency(value string) error {
	if !currencyPattern.MatchString(value) {
		return badRequest("currency_code must be an ISO 4217 code of three capital letters, such as CHF")
	}
	return nil
}

// checkTimeZone accepts an IANA time zone name whose every segment starts with a capital letter and that the
// zone database knows. The form check makes a wrong first letter fail on every platform. time.LoadLocation reads
// the system database first, and on a case-insensitive file system another case inside a segment can pass.
// "Local" and the empty name are no IANA zones. Which of the zones Analytics supports is Google's to decide.
func checkTimeZone(value string) error {
	if len(value) > maxTimeZoneBytes || value == "Local" || !zonePattern.MatchString(value) {
		return badRequest("time_zone must be an IANA time zone name, such as Europe/Zurich")
	}
	if _, err := time.LoadLocation(value); err != nil {
		return badRequest("time_zone must be an IANA time zone name, such as Europe/Zurich")
	}
	return nil
}

func checkIndustry(value string) error {
	if !slices.Contains(industryCategories, value) {
		return badRequest("industry_category must be one of the categories of the descriptor")
	}
	return nil
}

func quoted(values []string) string {
	out, _ := json.Marshal(values)
	return string(out)
}

const (
	displayNameSchema = `{"type":"string","minLength":1,"maxLength":100}`
	zoneSchema        = `{"type":"string","minLength":1,"maxLength":64,"pattern":"^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$"}`
	currencySchema    = `{"type":"string","pattern":"^[A-Z]{3}$"}`
	propertyArgSchema = `{"type":"string","minLength":1,"maxLength":40}`
	accountArgSchema  = `{"type":"string","minLength":1,"maxLength":40}`
)

var industrySchema = `{"type":"string","enum":` + quoted(industryCategories) + `}`

const trashNote = "Google keeps a trashed property for a limited time: 35 days according to the Analytics help, and " +
	"the answer carries expireTime. The API has no method to restore it, a person restores it in the Trash Can " +
	"of the Analytics interface. After that Google deletes the property with all its data streams, links and " +
	"other child resources for good."

const writeNote = "Provider content (names) is untrusted data: it is passed on and returned, never run or fetched; " +
	"names in the result are cut to 300 characters. Reads no report data."

var (
	displayNameArgument = capability.Argument{Name: "display_name", Description: "Display name, at most 100 UTF-16 " +
		"code units, no control characters"}
	timeZoneArgument = capability.Argument{Name: "time_zone", Description: "IANA time zone name such as " +
		"Europe/Zurich, the day boundary of the reports. A change applies to data going forward only. Qatlas checks " +
		"the name against the IANA zone database; Google may accept only some of its zones"}
	currencyArgument = capability.Argument{Name: "currency_code", Description: "ISO 4217 currency code of three " +
		"capital letters such as CHF or USD; Qatlas checks only the form and Google the code"}
	industryArgument = capability.Argument{Name: "industry_category", Description: "Industry category, one of " +
		strings.Join(industryCategories, ", ")}
)

var propertiesCreate = capability.Descriptor{
	ID: propertiesCreateID, Version: 1, Title: "Create an Analytics property",
	Description: "Create a Google Analytics property in an account (properties.create of the Analytics Admin API). " +
		"Needs the confirmation of a person for each call and is not idempotent: a repeated call creates a second " +
		"property. On a connection bound to accounts only a bound account is accepted, a connection bound to " +
		"properties only is refused. The property is an ordinary one; rollup and subproperties, the consent to " +
		"user data collection and the links of rollup sources are not tools. " + writeNote,
	Tags: []string{"google", "analytics", "properties", "create"}, Provider: Provider,
	Risk: writeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"parent":` + accountArgSchema + `,"display_name":` +
		displayNameSchema + `,"time_zone":` + zoneSchema + `,"currency_code":` + currencySchema +
		`,"industry_category":` + industrySchema + `,"property_type":{"type":"string","enum":["` +
		propertyTypeOrdinary + `"]}},"required":["parent","display_name","time_zone"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(propertySchema),
	Arguments: []capability.Argument{
		{Name: "parent", Required: true, Description: "Account that holds the property, accounts/ID, from " +
			"accounts.list or accounts.list-summaries"},
		{Name: "display_name", Required: true, Description: displayNameArgument.Description},
		{Name: "time_zone", Required: true, Description: timeZoneArgument.Description},
		currencyArgument, industryArgument,
		{Name: "property_type", Description: "Only " + propertyTypeOrdinary + ", which is also the default"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The new property: " + propertyFieldsText}},
	Examples: []capability.Example{{Description: "A property for a shop",
		Arguments: json.RawMessage(`{"parent":"accounts/100","display_name":"Shop web","time_zone":"Europe/Zurich",` +
			`"currency_code":"CHF","industry_category":"SHOPPING"}`)}},
}

var propertiesUpdate = capability.Descriptor{
	ID: propertiesUpdateID, Version: 1, Title: "Update an Analytics property",
	Description: "Change the display name, time zone, currency or industry category of a Google Analytics property " +
		"(properties.patch of the Analytics Admin API). Only the arguments that are set are changed, and at least " +
		"one is required; Qatlas sends exactly them as the update mask. Needs the confirmation of a person for each " +
		"call. Changing the time zone applies to data going forward only. On a connection with targets the " +
		"property must be a bound property or belong to a bound account, which is proven by one read of its " +
		"parent before the change. " + writeNote,
	Tags: []string{"google", "analytics", "properties", "update"}, Provider: Provider,
	Risk: writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema + `,"display_name":` +
		displayNameSchema + `,"time_zone":` + zoneSchema + `,"currency_code":` + currencySchema +
		`,"industry_category":` + industrySchema + `},"required":["property"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(propertySchema),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID, from " +
			"accounts.list-summaries or properties.list"},
		displayNameArgument, timeZoneArgument, currencyArgument, industryArgument,
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The property after the change: " + propertyFieldsText}},
	Examples: []capability.Example{{Description: "Rename a property",
		Arguments: json.RawMessage(`{"property":"properties/200","display_name":"Shop web EU"}`)}},
}

var propertiesDelete = capability.Descriptor{
	ID: propertiesDeleteID, Version: 1, Title: "Move an Analytics property to the trash",
	Description: "Move a Google Analytics property to the trash (properties.delete of the Analytics Admin API, a " +
		"soft delete) and return it with deleteTime and expireTime. " + trashNote + " Needs the confirmation of a " +
		"person for each call. Offered only where a connection's tools list names it. On a connection with targets " +
		"the property must be a bound property or belong to a bound account, which is proven by one read of its " +
		"parent before the change. " + writeNote,
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "analytics", "properties", "delete"}, Provider: Provider,
	Risk: writeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema +
		`},"required":["property"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(propertySchema),
	Arguments: []capability.Argument{{Name: "property", Required: true, Description: "Property resource name " +
		"properties/ID, from accounts.list-summaries or properties.list"}},
	Fields: []capability.Field{{Name: "(result)", Description: "The trashed property: " + propertyFieldsText}},
	Examples: []capability.Example{{Description: "Trash one property",
		Arguments: json.RawMessage(`{"property":"properties/200"}`)}},
}

type propertyWriteArguments struct {
	Parent           string  `json:"parent"`
	Property         string  `json:"property"`
	DisplayName      *string `json:"display_name"`
	TimeZone         *string `json:"time_zone"`
	CurrencyCode     *string `json:"currency_code"`
	IndustryCategory *string `json:"industry_category"`
	PropertyType     *string `json:"property_type"`
}

// checkFields checks the property members that are set; an empty value counts as set and is refused.
func (a propertyWriteArguments) checkFields() error {
	for _, field := range []struct {
		value *string
		check func(string) error
	}{{a.DisplayName, checkDisplayName}, {a.TimeZone, checkTimeZone}, {a.CurrencyCode, checkCurrency},
		{a.IndustryCategory, checkIndustry}} {
		if field.value == nil {
			continue
		}
		if err := field.check(*field.value); err != nil {
			return err
		}
	}
	return nil
}

// checkOrdinary accepts the property type of a created property: only the ordinary one is created here.
func (a propertyWriteArguments) checkOrdinary() error {
	if a.PropertyType != nil && *a.PropertyType != propertyTypeOrdinary {
		return badRequest("property_type must be " + propertyTypeOrdinary + "; other types are not created by a tool")
	}
	return nil
}

func parseWriteArguments(op string, raw json.RawMessage) (propertyWriteArguments, error) {
	var arguments propertyWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return arguments, failure(op, "the validated arguments could not be read")
	}
	return arguments, nil
}

// createBody is the Property of properties.create: only what the caller set, never an output-only member.
type createBody struct {
	Parent           string `json:"parent"`
	DisplayName      string `json:"displayName"`
	TimeZone         string `json:"timeZone"`
	CurrencyCode     string `json:"currencyCode,omitempty"`
	IndustryCategory string `json:"industryCategory,omitempty"`
	PropertyType     string `json:"propertyType"`
}

func handlePropertiesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create property"
	arguments, err := parseWriteArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	if _, err := checkAccountName("parent", arguments.Parent); err != nil {
		return nil, err
	}
	if err := bind.account(arguments.Parent); err != nil {
		return nil, err
	}
	if arguments.DisplayName == nil || arguments.TimeZone == nil {
		return nil, badRequest("parent, display_name and time_zone are required")
	}
	if err := arguments.checkFields(); err != nil {
		return nil, err
	}
	if err := arguments.checkOrdinary(); err != nil {
		return nil, err
	}
	body := createBody{Parent: arguments.Parent, DisplayName: *arguments.DisplayName, TimeZone: *arguments.TimeZone,
		PropertyType: propertyTypeOrdinary}
	if arguments.CurrencyCode != nil {
		body.CurrencyCode = *arguments.CurrencyCode
	}
	if arguments.IndustryCategory != nil {
		body.IndustryCategory = *arguments.IndustryCategory
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the property could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out property
	if err := c.change(ctx, changeCall{op: op, tool: propertiesCreateID, method: http.MethodPost, path: propertiesPath,
		body: encoded, limit: maxPropertyBytes, notFound: notFoundGeneric}, &out); err != nil {
		return nil, err
	}
	if !isName(propertyPrefix, string(out.Name)) {
		return nil, unsure(failure(op, subject+" answered without the name of a property"))
	}
	return out, nil
}

// proveProperty reads a property that is not bound itself once, as proof that it belongs to a bound account,
// before anything is changed. A property of another account is refused like one outside the targets.
func (c *client) proveProperty(ctx context.Context, op, tool, name string, proof bool) error {
	if !proof {
		return nil
	}
	out, err := c.getProperty(ctx, op, tool, name)
	if err != nil {
		return err
	}
	if !c.bind.ownsProperty(out) {
		return badRequest(outsideTargets)
	}
	return nil
}

// updateBody holds the members that are set, in Google's snake case and a fixed order.
func (a propertyWriteArguments) updateBody() (body map[string]string, mask string) {
	body = map[string]string{}
	var paths []string
	for _, field := range []struct {
		path, key string
		value     *string
	}{{"display_name", "displayName", a.DisplayName}, {"industry_category", "industryCategory", a.IndustryCategory},
		{"time_zone", "timeZone", a.TimeZone}, {"currency_code", "currencyCode", a.CurrencyCode}} {
		if field.value != nil {
			body[field.key] = *field.value
			paths = append(paths, field.path)
		}
	}
	return body, strings.Join(paths, ",")
}

func handlePropertiesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update property"
	arguments, err := parseWriteArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	name, err := checkPropertyName("property", arguments.Property)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(name)
	if err != nil {
		return nil, err
	}
	body, mask := arguments.updateBody()
	if mask == "" {
		return nil, badRequest("set at least one of display_name, time_zone, currency_code and industry_category")
	}
	if err := arguments.checkFields(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the property could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, propertiesUpdateID, name, proof); err != nil {
		return nil, err
	}
	var out property
	if err := c.change(ctx, changeCall{op: op, tool: propertiesUpdateID, method: http.MethodPatch,
		path: "/v1beta/" + name, query: url.Values{"updateMask": {mask}}, body: encoded, limit: maxPropertyBytes,
		notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != name {
		return nil, unsure(failure(op, subject+" answered with another property than the one asked for"))
	}
	return out, nil
}

func handlePropertiesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete property"
	arguments, err := parseWriteArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	name, err := checkPropertyName("property", arguments.Property)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(name)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, propertiesDeleteID, name, proof); err != nil {
		return nil, err
	}
	var out property
	if err := c.change(ctx, changeCall{op: op, tool: propertiesDeleteID, method: http.MethodDelete,
		path: "/v1beta/" + name, limit: maxPropertyBytes, notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != name {
		return nil, unsure(failure(op, subject+" answered with another property than the one asked for"))
	}
	return out, nil
}

// propertyWriteSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func propertyWriteSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a propertyWriteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		switch tool {
		case propertiesCreateID:
			out := "Create Analytics property"
			if a.DisplayName != nil {
				out += " " + strconv.Quote(*a.DisplayName)
			}
			out += " in " + a.Parent
			if a.TimeZone != nil {
				out += ", time zone " + *a.TimeZone
			}
			if a.CurrencyCode != nil {
				out += ", currency " + *a.CurrencyCode
			}
			return out
		case propertiesUpdateID:
			_, mask := a.updateBody()
			return "Change " + strings.ReplaceAll(mask, ",", ", ") + " of Analytics property " + a.Property
		}
		return "Move Analytics property " + a.Property + " to the trash (35 days, then deleted for good with all its " +
			"child resources; no tool restores it)"
	}
}
