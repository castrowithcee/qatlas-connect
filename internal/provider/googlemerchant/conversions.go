package googlemerchant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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
	conversionSourcesListID     = Provider + ".conversion-sources.list"
	conversionSourcesGetID      = Provider + ".conversion-sources.get"
	conversionSourcesCreateID   = Provider + ".conversion-sources.create"
	conversionSourcesUpdateID   = Provider + ".conversion-sources.update"
	conversionSourcesUndeleteID = Provider + ".conversion-sources.undelete"
	conversionSourcesDeleteID   = Provider + ".conversion-sources.delete"
)

// conversionsAPI is the path prefix of the conversions sub-API.
const conversionsAPI = "conversions/v1/"

// A source ID is the part of the resource name after the account: a four letter type, a colon, and the ID
// within the type. The reference names two types, galk for a Google Analytics link and mcdn for a Merchant
// Center destination. The characters after the colon are not specified; the form here is narrower than what
// Google may issue, so an ID that does not fit is refused instead of being put into a path.
const (
	sourceIDPattern   = `(galk|mcdn):[A-Za-z0-9_-]{1,64}`
	mcdnIDPattern     = `mcdn:[A-Za-z0-9_-]{1,64}`
	mcdnPrefix        = "mcdn:"
	maxDisplayName    = 64
	maxConversionSize = 200
)

var (
	sourceIDForm = regexp.MustCompile(`^` + sourceIDPattern + `$`)
	mcdnIDForm   = regexp.MustCompile(`^` + mcdnIDPattern + `$`)
	currencyForm = regexp.MustCompile(`^[A-Z]{3}$`)
)

var (
	attributionModels = []string{"CROSS_CHANNEL_LAST_CLICK", "ADS_PREFERRED_LAST_CLICK", "CROSS_CHANNEL_DATA_DRIVEN",
		"CROSS_CHANNEL_FIRST_CLICK", "CROSS_CHANNEL_LINEAR", "CROSS_CHANNEL_POSITION_BASED", "CROSS_CHANNEL_TIME_DECAY"}
	lookbackDays = []int{7, 30, 40}
)

// What Google returns for a source. The members are those of ConversionSource and its parts; the deprecated
// expireTime is not held. googleAnalyticsLink exists for output only: no type that is sent holds it.
type conversionType struct {
	Name   nameText `json:"name,omitempty"`
	Report *bool    `json:"report,omitempty"`
}

type conversionTypes []conversionType

// UnmarshalJSON keeps at most maxListItems entries.
func (l *conversionTypes) UnmarshalJSON(b []byte) error {
	var raw []conversionType
	if json.Unmarshal(b, &raw) != nil {
		*l = nil
		return nil
	}
	if len(raw) > maxListItems {
		raw = raw[:maxListItems]
	}
	*l = raw
	return nil
}

type attributionSettings struct {
	AttributionModel              idText          `json:"attributionModel,omitempty"`
	AttributionLookbackWindowDays *int            `json:"attributionLookbackWindowDays,omitempty"`
	ConversionType                conversionTypes `json:"conversionType,omitempty"`
}

type merchantCenterDestination struct {
	Destination         idText               `json:"destination,omitempty"`
	DisplayName         nameText             `json:"displayName,omitempty"`
	CurrencyCode        idText               `json:"currencyCode,omitempty"`
	AttributionSettings *attributionSettings `json:"attributionSettings,omitempty"`
}

type googleAnalyticsLink struct {
	Property            idText               `json:"property,omitempty"`
	PropertyID          idText               `json:"propertyId,omitempty"`
	AttributionSettings *attributionSettings `json:"attributionSettings,omitempty"`
}

type conversionSource struct {
	Name                      idText                     `json:"name,omitempty"`
	State                     idText                     `json:"state,omitempty"`
	Controller                idText                     `json:"controller,omitempty"`
	MerchantCenterDestination *merchantCenterDestination `json:"merchantCenterDestination,omitempty"`
	GoogleAnalyticsLink       *googleAnalyticsLink       `json:"googleAnalyticsLink,omitempty"`
}

type conversionSourcesAnswer struct {
	ConversionSources []conversionSource `json:"conversionSources"`
	NextPageToken     string             `json:"nextPageToken"`
}

type conversionSourcesResult struct {
	ConversionSources []conversionSource `json:"conversionSources"`
	NextCursor        string             `json:"next_cursor,omitempty"`
}

// What a write sends. Only a Merchant Center destination can be written: there is no member for a Google
// Analytics link here, so no argument can create or change one.
type attributionWrite struct {
	AttributionModel              string `json:"attributionModel,omitempty"`
	AttributionLookbackWindowDays *int   `json:"attributionLookbackWindowDays,omitempty"`
}

type destinationWrite struct {
	DisplayName         string            `json:"displayName,omitempty"`
	CurrencyCode        string            `json:"currencyCode,omitempty"`
	AttributionSettings *attributionWrite `json:"attributionSettings,omitempty"`
}

type conversionWrite struct {
	MerchantCenterDestination destinationWrite `json:"merchantCenterDestination"`
}

// conversionAccepted is the result of a create: Google accepted the request. The source is built with a delay,
// so the state is usually PENDING; the list and get tools show the outcome later.
type conversionAccepted struct {
	Accepted bool   `json:"accepted"`
	Name     idText `json:"name,omitempty"`
	State    idText `json:"state,omitempty"`
}

// conversionUpdated is the result of an update: Google accepted the request for the named members.
type conversionUpdated struct {
	Accepted bool     `json:"accepted"`
	Name     string   `json:"name"`
	Updated  []string `json:"updated"`
}

var (
	attributionOutSchema = objectSchema(member("attributionModel", stringSchema),
		member("attributionLookbackWindowDays", `{"type":"integer"}`),
		member("conversionType", `{"type":"array","items":`+objectSchema(member("name", stringSchema),
			member("report", boolSchema))+`}`))
	conversionSourceSchema = objectSchema(member("name", stringSchema), member("state", stringSchema),
		member("controller", stringSchema),
		member("merchantCenterDestination", objectSchema(member("destination", stringSchema),
			member("displayName", stringSchema), member("currencyCode", stringSchema),
			member("attributionSettings", attributionOutSchema))),
		member("googleAnalyticsLink", objectSchema(member("property", stringSchema),
			member("propertyId", stringSchema), member("attributionSettings", attributionOutSchema))))
	conversionAcceptedSchema = objectSchema(member("accepted", boolSchema), member("name", stringSchema),
		member("state", stringSchema))
	conversionDeletedSchema = objectSchema(member("account", stringSchema), member("conversion_source", stringSchema),
		member("deleted", boolSchema))
	conversionUpdatedSchema = objectSchema(member("accepted", boolSchema), member("name", stringSchema),
		member("updated", listSchema))
)

const (
	sourceArgSchema = `{"type":"string","maxLength":69,"pattern":"^` + sourceIDPattern + `$",` +
		`"x-form":"the conversion source ID: galk: or mcdn: and the ID within the type"}`
	mcdnArgSchema = `{"type":"string","maxLength":69,"pattern":"^` + mcdnIDPattern + `$",` +
		`"x-form":"the ID of a Merchant Center destination: mcdn: and the ID within the type"}`
	conversionPageSizeSchema    = `{"type":"integer","minimum":1,"maximum":200}`
	conversionDisplayNameSchema = `{"type":"string","minLength":1,"maxLength":64}`
	currencySchema              = `{"type":"string","pattern":"^[A-Z]{3}$"}`
	attributionModelSchema      = `{"type":"string","enum":["CROSS_CHANNEL_LAST_CLICK","ADS_PREFERRED_LAST_CLICK",` +
		`"CROSS_CHANNEL_DATA_DRIVEN","CROSS_CHANNEL_FIRST_CLICK","CROSS_CHANNEL_LINEAR",` +
		`"CROSS_CHANNEL_POSITION_BASED","CROSS_CHANNEL_TIME_DECAY"]}`
	lookbackSchema = `{"type":"integer","enum":[7,30,40]}`
)

func conversionAttributionInput(required bool) string {
	schema := `{"type":"object","properties":{"attribution_model":` + attributionModelSchema +
		`,"attribution_lookback_window_days":` + lookbackSchema + `},"additionalProperties":false`
	if required {
		return schema + `,"required":["attribution_model","attribution_lookback_window_days"]}`
	}
	return schema + `,"minProperties":1}`
}

const (
	conversionContentNote = "Display names and the names of conversion types are untrusted content of whoever runs " +
		"the account: texts are cut. Property IDs are text that is never used."
	conversionAcceptedNote = " Only the acceptance of the request is reported: Google builds or changes the source " +
		"with a delay, so read the source again after a while to see the result."
	conversionOnlyDestination = "Only a Merchant Center destination can be written; a link to a Google Analytics " +
		"property cannot be created or changed with this tool."
	conversionEffectNote = "The conversion data of the source feeds the conversion reporting of the account and, " +
		"through it, what the account sees of its ads."
)

var (
	conversionFieldsText = "name (accounts/ACCOUNT/conversionSources/ID), state, controller, " +
		"merchantCenterDestination (destination, displayName, currencyCode, attributionSettings with " +
		"attributionModel, attributionLookbackWindowDays, and conversionType name and report), and " +
		"googleAnalyticsLink (property, propertyId, attributionSettings), which this provider only shows"
	conversionAcceptedFields = []capability.Field{
		{Name: "accepted", Description: "true when Google accepted the request"},
		{Name: "name", Description: "the source of the request"},
		{Name: "state", Description: "state Google reported for the new source, usually PENDING; create only"},
		{Name: "updated", Description: "the update mask that was sent; update only"}}
	createRisk = capability.Risk{Effect: capability.EffectCreate, Idempotency: capability.IdempotencyNonIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "merchant-account-data"}
	updateRisk = capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "merchant-account-data"}
)

var (
	undeleteRisk = capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyUnknown,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "merchant-account-data"}
	deleteRisk = capability.Risk{Effect: capability.EffectDelete, Idempotency: capability.IdempotencyUnknown,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "merchant-account-data"}
)

const (
	conversionDeleteEffect = "A Merchant Center destination is archived and can be restored for 30 days; a link to a " +
		"Google Analytics property is removed immediately and cannot be restored, only created again."
	conversionDeleteNote = " Offered only where a connection's tools list names it."
)

var conversionSourcesList = capability.Descriptor{
	ID: conversionSourcesListID, Version: 1, Title: "List the conversion sources of a Merchant Center account",
	Description: "List the conversion sources of a Merchant Center account, Merchant Center destinations and links " +
		"to Google Analytics properties alike. Google returns at most 200 sources in total, whatever the paging. " +
		"With show_deleted the archived sources are listed too; they can be restored for 30 days. " + conversionContentNote,
	Tags: []string{"google", "merchant", "conversions"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema), member("show_deleted", boolSchema),
		member("page_size", conversionPageSizeSchema), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("conversionSources", conversionSourceSchema)),
	Arguments: []capability.Argument{accountArgument,
		{Name: "show_deleted", Description: "true also lists archived sources; by default they are left out"},
		{Name: "page_size", Description: "Sources per batch, 1 to 200; Google sends 100 when left out"},
		cursorArgument},
	Fields: []capability.Field{{Name: "conversionSources", Description: "Sources with " + conversionFieldsText},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"}},
	Examples: []capability.Example{{Description: "Sources of one account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var conversionSourcesGet = capability.Descriptor{
	ID: conversionSourcesGetID, Version: 1, Title: "Get a conversion source",
	Description: "Read one conversion source of a Merchant Center account, a Merchant Center destination or a link " +
		"to a Google Analytics property. " + conversionContentNote,
	Tags: []string{"google", "merchant", "conversions"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"conversion_source"}, member("account", accountArgSchema),
		member("conversion_source", sourceArgSchema)),
	OutputSchema: json.RawMessage(conversionSourceSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "conversion_source", Required: true, Description: "ID of the source as in its name after " +
			"conversionSources/, for example mcdn:ID or galk:ID"}},
	Fields: []capability.Field{{Name: "conversionSource", Description: conversionFieldsText}},
	Examples: []capability.Example{{Description: "One source",
		Arguments: json.RawMessage(`{"account":"1000001","conversion_source":"mcdn:12345"}`)}},
}

var conversionSourcesCreate = capability.Descriptor{
	ID: conversionSourcesCreateID, Version: 1, Title: "Create a Merchant Center destination as conversion source",
	Description: "Create a conversion source of the type Merchant Center destination, which takes the conversion " +
		"events of an online store from a Google tag into the account. " + conversionOnlyDestination + " " +
		conversionEffectNote + " An account can have up to 200 sources, and the call is not idempotent: sending it " +
		"again creates a second source. A standard purchase conversion type is created with the source." +
		conversionAcceptedNote + " " + conversionContentNote,
	Tags: []string{"google", "merchant", "conversions", "create"}, Risk: createRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"display_name", "currency_code", "attribution_settings"},
		member("account", accountArgSchema), member("display_name", conversionDisplayNameSchema),
		member("currency_code", currencySchema), member("attribution_settings", conversionAttributionInput(true))),
	OutputSchema: json.RawMessage(conversionAcceptedSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "display_name", Required: true, Description: "name of the source in the Merchant Center interface, " +
			"1 to 64 characters"},
		{Name: "currency_code", Required: true, Description: "three-letter ISO 4217 code in capitals; the " +
			"currency conversions are reported in"},
		{Name: "attribution_settings", Required: true, Description: "object with attribution_model " +
			"(" + strings.Join(attributionModels, ", ") + ") and attribution_lookback_window_days (7, 30, or 40), both required"}},
	Fields: conversionAcceptedFields,
	Examples: []capability.Example{{Description: "A destination with last click attribution",
		Arguments: json.RawMessage(`{"account":"1000001","display_name":"Web store","currency_code":"EUR",` +
			`"attribution_settings":{"attribution_model":"CROSS_CHANNEL_LAST_CLICK","attribution_lookback_window_days":30}}`)}},
}

var conversionSourcesUpdate = capability.Descriptor{
	ID: conversionSourcesUpdateID, Version: 1, Title: "Change a Merchant Center destination conversion source",
	Description: "Change the display name, the currency, or the attribution settings of a conversion source of the " +
		"type Merchant Center destination. Only the members that are given are changed. " + conversionOnlyDestination +
		" The ID must start with mcdn:; a source with another type is refused here before anything is sent. " +
		conversionEffectNote + conversionAcceptedNote + " " + conversionContentNote,
	Tags: []string{"google", "merchant", "conversions", "update"}, Risk: updateRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"conversion_source"}, member("account", accountArgSchema),
		member("conversion_source", mcdnArgSchema), member("display_name", conversionDisplayNameSchema),
		member("currency_code", currencySchema), member("attribution_settings", conversionAttributionInput(false))),
	OutputSchema: json.RawMessage(conversionUpdatedSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "conversion_source", Required: true, Description: "ID of the Merchant Center destination, mcdn: and " +
			"the ID within the type"},
		{Name: "display_name", Description: "new name of the source, 1 to 64 characters"},
		{Name: "currency_code", Description: "new three-letter ISO 4217 code in capitals"},
		{Name: "attribution_settings", Description: "object with attribution_model and " +
			"attribution_lookback_window_days (7, 30, or 40), at least one; members that are left out are kept"}},
	Fields: conversionAcceptedFields,
	Examples: []capability.Example{{Description: "Change the lookback window",
		Arguments: json.RawMessage(`{"account":"1000001","conversion_source":"mcdn:12345",` +
			`"attribution_settings":{"attribution_lookback_window_days":40}}`)}},
}

var conversionSourcesUndelete = capability.Descriptor{
	ID: conversionSourcesUndeleteID, Version: 1, Title: "Restore an archived Merchant Center destination",
	Description: "Re-enable an archived conversion source. This works only for a source of the type Merchant Center " +
		"destination, whose ID starts with mcdn:; a link to a Google Analytics property is refused here before " +
		"anything is sent. The account argument is required. " + conversionEffectNote + conversionDeleteNote +
		conversionAcceptedNote + " " + conversionContentNote,
	Tags: []string{"google", "merchant", "conversions", "update"}, Risk: undeleteRisk, Provider: Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"conversion_source"}, member("account", accountArgSchema),
		member("conversion_source", mcdnArgSchema)),
	OutputSchema: json.RawMessage(conversionAcceptedSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "conversion_source", Required: true, Description: "ID of the archived Merchant Center destination, " +
			"mcdn: and the ID within the type"}},
	Fields: conversionAcceptedFields,
	Examples: []capability.Example{{Description: "Restore a source",
		Arguments: json.RawMessage(`{"account":"1000001","conversion_source":"mcdn:12345"}`)}},
}

var conversionSourcesDelete = capability.Descriptor{
	ID: conversionSourcesDeleteID, Version: 1, Title: "Delete a conversion source",
	Description: "Delete a conversion source. " + conversionDeleteEffect + " " + conversionEffectNote +
		" The account argument is required." + conversionDeleteNote + " Only the acceptance of the request is " +
		"reported: Google processes it with a delay. " + conversionContentNote,
	Tags: []string{"google", "merchant", "conversions", "delete"}, Risk: deleteRisk, Provider: Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"conversion_source"}, member("account", accountArgSchema),
		member("conversion_source", sourceArgSchema)),
	OutputSchema: json.RawMessage(conversionDeletedSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "conversion_source", Required: true, Description: "ID of the source, galk: or mcdn: and the ID " +
			"within the type"}},
	Fields: []capability.Field{{Name: "account", Description: "ID of the account whose source Google was asked to delete"},
		{Name: "conversion_source", Description: "ID of the source"},
		{Name: "deleted", Description: "true once Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Delete a source",
		Arguments: json.RawMessage(`{"account":"1000001","conversion_source":"mcdn:12345"}`)}},
}

type conversionDeleteResult struct {
	Account          string `json:"account"`
	ConversionSource string `json:"conversion_source"`
	Deleted          bool   `json:"deleted"`
}

type attributionArguments struct {
	Model *string `json:"attribution_model"`
	Days  *int    `json:"attribution_lookback_window_days"`
}

// conversionArguments is read strictly: a member that is not listed, such as one that names an analytics
// property, ends the call before any secret is read, whatever the schema check before the handler did.
type conversionArguments struct {
	Account           string                `json:"account"`
	ConversionSource  string                `json:"conversion_source"`
	ShowDeleted       bool                  `json:"show_deleted"`
	PageSize          int                   `json:"page_size"`
	Cursor            string                `json:"cursor"`
	DisplayName       *string               `json:"display_name"`
	CurrencyCode      *string               `json:"currency_code"`
	AttributionSetups *attributionArguments `json:"attribution_settings"`
}

func readConversionArguments(raw json.RawMessage) (conversionArguments, error) {
	var args conversionArguments
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return args, invalidArgument("the arguments hold a member this tool does not take or a value of the wrong type")
	}
	return args, nil
}

func conversionsPath(id string) string {
	return conversionsAPI + "accounts/" + id + "/conversionSources"
}

// openConversionAccount opens the client of a call whose account is settled and reads the proof for an account no target
// names. Every refusal of the arguments comes before it.
func openConversionAccount(ctx context.Context, c call, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	id string, needsProof bool) (*client, error) {
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	return cl, nil
}

func checkConversionDisplayName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return invalidArgument("display_name must not be empty")
	case utf8.RuneCountInString(name) > maxDisplayName:
		return invalidArgument("display_name is longer than " + strconv.Itoa(maxDisplayName) + " characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return invalidArgument("display_name must not contain control characters")
		}
	}
	return nil
}

func checkConversionCurrency(code string) error {
	if !currencyForm.MatchString(code) {
		return invalidArgument("currency_code must be three capital letters (ISO 4217)")
	}
	return nil
}

func checkConversionAttribution(a attributionArguments, whole bool) error {
	if whole && (a.Model == nil || a.Days == nil) {
		return invalidArgument("attribution_settings needs attribution_model and attribution_lookback_window_days")
	}
	if a.Model == nil && a.Days == nil {
		return invalidArgument("attribution_settings needs at least one member")
	}
	if a.Model != nil {
		known := false
		for _, m := range attributionModels {
			known = known || m == *a.Model
		}
		if !known {
			return invalidArgument("attribution_model is not one of " + strings.Join(attributionModels, ", "))
		}
	}
	if a.Days != nil {
		known := false
		for _, d := range lookbackDays {
			known = known || d == *a.Days
		}
		if !known {
			return invalidArgument("attribution_lookback_window_days must be 7, 30, or 40")
		}
	}
	return nil
}

func handleConversionSourcesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("list conversion sources", conversionSourcesListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	args, err := readConversionArguments(raw)
	if err != nil {
		return nil, err
	}
	if args.PageSize < 0 || args.PageSize > maxConversionSize {
		return nil, invalidArgument("page_size must be between 1 and " + strconv.Itoa(maxConversionSize))
	}
	id, needsProof, err := c.scope.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(c.tool, id, args.ShowDeleted, args.PageSize, c.scope.key())
	query := url.Values{}
	if args.ShowDeleted {
		query.Set("showDeleted", "true")
	}
	if args.PageSize > 0 {
		query.Set("pageSize", strconv.Itoa(args.PageSize))
	}
	if args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	cl, err := openConversionAccount(ctx, c, resolved, secrets, red, id, needsProof)
	if err != nil {
		return nil, err
	}
	var answer conversionSourcesAnswer
	if err := cl.get(ctx, c.op, c.tool, conversionsPath(id), query, &answer); err != nil {
		return nil, err
	}
	if len(answer.ConversionSources) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := conversionSourcesResult{ConversionSources: answer.ConversionSources}
	if out.ConversionSources == nil {
		out.ConversionSources = []conversionSource{}
	}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleConversionSourcesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get conversion source", conversionSourcesGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	args, err := readConversionArguments(raw)
	if err != nil {
		return nil, err
	}
	if !sourceIDForm.MatchString(args.ConversionSource) {
		return nil, invalidArgument("conversion_source must be galk: or mcdn: followed by the ID of the source")
	}
	id, needsProof, err := c.scope.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	cl, err := openConversionAccount(ctx, c, resolved, secrets, red, id, needsProof)
	if err != nil {
		return nil, err
	}
	var out conversionSource
	if err := cl.get(ctx, c.op, c.tool, conversionsPath(id)+"/"+args.ConversionSource, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleConversionSourcesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("create conversion source", conversionSourcesCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	args, err := readConversionArguments(raw)
	if err != nil {
		return nil, err
	}
	if args.DisplayName == nil || args.CurrencyCode == nil || args.AttributionSetups == nil {
		return nil, invalidArgument("display_name, currency_code, and attribution_settings are required")
	}
	if err := checkConversionDisplayName(*args.DisplayName); err != nil {
		return nil, err
	}
	if err := checkConversionCurrency(*args.CurrencyCode); err != nil {
		return nil, err
	}
	if err := checkConversionAttribution(*args.AttributionSetups, true); err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	cl, err := openConversionAccount(ctx, c, resolved, secrets, red, id, needsProof)
	if err != nil {
		return nil, err
	}
	body := conversionWrite{MerchantCenterDestination: destinationWrite{
		DisplayName: *args.DisplayName, CurrencyCode: *args.CurrencyCode,
		AttributionSettings: &attributionWrite{AttributionModel: *args.AttributionSetups.Model,
			AttributionLookbackWindowDays: args.AttributionSetups.Days}}}
	var answer struct {
		Name  idText `json:"name"`
		State idText `json:"state"`
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, provider.Fail(c.op, "the request body could not be encoded")
	}
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodPost, path: conversionsPath(id), body: payload,
		change: true}, &answer); err != nil {
		return nil, err
	}
	return conversionAccepted{Accepted: true, Name: answer.Name, State: answer.State}, nil
}

// conversionUpdateBody maps the arguments to the body and the update mask. The mask names each member that is given by
// its own path under merchantCenterDestination, so a member that is left out is kept, and the immutable list
// of conversion types is never named.
func conversionUpdateBody(args conversionArguments) (conversionWrite, []string, error) {
	var body conversionWrite
	var mask []string
	dest := &body.MerchantCenterDestination
	if args.DisplayName != nil {
		if err := checkConversionDisplayName(*args.DisplayName); err != nil {
			return body, nil, err
		}
		dest.DisplayName = *args.DisplayName
		mask = append(mask, "merchantCenterDestination.displayName")
	}
	if args.CurrencyCode != nil {
		if err := checkConversionCurrency(*args.CurrencyCode); err != nil {
			return body, nil, err
		}
		dest.CurrencyCode = *args.CurrencyCode
		mask = append(mask, "merchantCenterDestination.currencyCode")
	}
	if a := args.AttributionSetups; a != nil {
		if err := checkConversionAttribution(*a, false); err != nil {
			return body, nil, err
		}
		dest.AttributionSettings = &attributionWrite{}
		if a.Model != nil {
			dest.AttributionSettings.AttributionModel = *a.Model
			mask = append(mask, "merchantCenterDestination.attributionSettings.attributionModel")
		}
		if a.Days != nil {
			dest.AttributionSettings.AttributionLookbackWindowDays = a.Days
			mask = append(mask, "merchantCenterDestination.attributionSettings.attributionLookbackWindowDays")
		}
	}
	if len(mask) == 0 {
		return body, nil, invalidArgument("give at least one of display_name, currency_code, attribution_settings")
	}
	return body, mask, nil
}

func handleConversionSourcesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update conversion source", conversionSourcesUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	args, err := readConversionArguments(raw)
	if err != nil {
		return nil, err
	}
	// The type prefix of the ID is what keeps a link to an analytics property out of reach: the body can hold
	// destination members only, and a source of another type is not sent anything.
	if !mcdnIDForm.MatchString(args.ConversionSource) {
		return nil, invalidArgument("conversion_source must be the ID of a Merchant Center destination: mcdn: " +
			"followed by the ID; other source types cannot be changed here")
	}
	body, mask, err := conversionUpdateBody(args)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	cl, err := openConversionAccount(ctx, c, resolved, secrets, red, id, needsProof)
	if err != nil {
		return nil, err
	}
	path := conversionsPath(id) + "/" + args.ConversionSource
	var answer struct {
		Name idText `json:"name"`
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, provider.Fail(c.op, "the request body could not be encoded")
	}
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodPatch, path: path,
		query: url.Values{"updateMask": {strings.Join(mask, ",")}}, body: payload, change: true}, &answer); err != nil {
		return nil, err
	}
	return conversionUpdated{Accepted: true, Name: "accounts/" + id + "/conversionSources/" + args.ConversionSource,
		Updated: mask}, nil
}

func handleConversionSourcesUndelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("restore conversion source", conversionSourcesUndeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	args, err := readConversionArguments(raw)
	if err != nil {
		return nil, err
	}
	if args.Account == "" {
		return nil, invalidArgument("account is required")
	}
	if !mcdnIDForm.MatchString(args.ConversionSource) {
		return nil, invalidArgument("conversion_source must be the ID of a Merchant Center destination: mcdn: " +
			"followed by the ID; other source types cannot be restored")
	}
	id, needsProof, err := c.scope.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	cl, err := openConversionAccount(ctx, c, resolved, secrets, red, id, needsProof)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Name  idText `json:"name"`
		State idText `json:"state"`
	}
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodPost,
		path: conversionsPath(id) + "/" + args.ConversionSource + ":undelete", change: true}, &answer); err != nil {
		return nil, err
	}
	return conversionAccepted{Accepted: true, Name: answer.Name, State: answer.State}, nil
}

func handleConversionSourcesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("delete conversion source", conversionSourcesDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	args, err := readConversionArguments(raw)
	if err != nil {
		return nil, err
	}
	if args.Account == "" {
		return nil, invalidArgument("account is required")
	}
	if !sourceIDForm.MatchString(args.ConversionSource) {
		return nil, invalidArgument("conversion_source must be galk: or mcdn: followed by the ID of the source")
	}
	id, needsProof, err := c.scope.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	cl, err := openConversionAccount(ctx, c, resolved, secrets, red, id, needsProof)
	if err != nil {
		return nil, err
	}
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodDelete,
		path: conversionsPath(id) + "/" + args.ConversionSource, change: true}, nil); err != nil {
		return nil, err
	}
	return conversionDeleteResult{Account: id, ConversionSource: args.ConversionSource, Deleted: true}, nil
}

func conversionUndeleteSummary(raw json.RawMessage) string {
	var args conversionArguments
	_ = json.Unmarshal(raw, &args)
	return "Restore the archived Merchant Center destination " + args.ConversionSource + " of Merchant Center account " +
		conversionAccountText(args.Account) + "; it works only for Merchant Center destinations." + conversionEffect
}

func conversionDeleteSummary(raw json.RawMessage) string {
	var args conversionArguments
	_ = json.Unmarshal(raw, &args)
	return "Delete conversion source " + args.ConversionSource + " of Merchant Center account " +
		conversionAccountText(args.Account) + ". A Merchant Center destination is archived and can be restored for " +
		"30 days; a Google Analytics link is removed immediately." + conversionEffect
}

const conversionEffect = " This changes the conversion data of the account; Google processes it with a delay."

func conversionCreateSummary(raw json.RawMessage) string {
	var args conversionArguments
	_ = json.Unmarshal(raw, &args)
	text := "Create a Merchant Center destination as conversion source of Merchant Center account " + conversionAccountText(args.Account)
	if args.DisplayName != nil {
		text += " named " + strconv.Quote(*args.DisplayName)
	}
	if args.CurrencyCode != nil {
		text += " in " + *args.CurrencyCode
	}
	return text + ". Sending it again creates a second source." + conversionEffect
}

func conversionUpdateSummary(raw json.RawMessage) string {
	var args conversionArguments
	_ = json.Unmarshal(raw, &args)
	var parts []string
	if args.DisplayName != nil {
		parts = append(parts, "display name "+strconv.Quote(*args.DisplayName))
	}
	if args.CurrencyCode != nil {
		parts = append(parts, "currency "+*args.CurrencyCode)
	}
	if a := args.AttributionSetups; a != nil {
		if a.Model != nil {
			parts = append(parts, "attribution model "+*a.Model)
		}
		if a.Days != nil {
			parts = append(parts, "lookback window "+strconv.Itoa(*a.Days)+" days")
		}
	}
	return "Change " + strings.Join(parts, ", ") + " of conversion source " + args.ConversionSource +
		" of Merchant Center account " + conversionAccountText(args.Account) + "." + conversionEffect
}

func conversionAccountText(id string) string {
	if id == "" {
		return "of the connection"
	}
	return id
}
