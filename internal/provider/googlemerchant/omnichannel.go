package googlemerchant

import (
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
	omnichannelSettingsListID   = Provider + ".omnichannel-settings.list"
	omnichannelSettingsGetID    = Provider + ".omnichannel-settings.get"
	omnichannelSettingsCreateID = Provider + ".omnichannel-settings.create"
	omnichannelSettingsUpdateID = Provider + ".omnichannel-settings.update"
	omnichannelVerifyID         = Provider + ".omnichannel-settings.request-inventory-verification"
)

// Bounds of the arguments. The API documents no length for any member; the bounds are ours. maxEntries and
// the page size of the list are the documented ones.
const (
	maxContactRunes      = maxNameRunes
	maxContactEmailRunes = 254
	omnichannelPageSize  = 1000
)

// A region code is a CLDR region code; settings are kept per country, so the two letters of a country.
var (
	regionCodeForm   = regexp.MustCompile(`^[A-Z]{2}$`)
	contactEmailForm = regexp.MustCompile(`^[^@\s]+@[^@\s]+$`)
	lsfTypes         = []string{"GHLSF", "MHLSF_BASIC", "MHLSF_FULL"}
)

// OmnichannelSetting members of the discovery document. Output-only members are state, lfpLink, and the
// verification states; none of them is an argument.
type (
	omnichannelState struct {
		URI   urlText `json:"uri,omitempty"`
		State idText  `json:"state,omitempty"`
	}
	omnichannelLfpLink struct {
		LfpProvider       idText `json:"lfpProvider,omitempty"`
		ExternalAccountID idText `json:"externalAccountId,omitempty"`
		State             idText `json:"state,omitempty"`
	}
	omnichannelVerification struct {
		State        idText   `json:"state,omitempty"`
		Contact      nameText `json:"contact,omitempty"`
		ContactEmail nameText `json:"contactEmail,omitempty"`
		ContactState idText   `json:"contactState,omitempty"`
	}
	omnichannelSetting struct {
		Name                  idText                   `json:"name,omitempty"`
		RegionCode            idText                   `json:"regionCode,omitempty"`
		LsfType               idText                   `json:"lsfType,omitempty"`
		InStock               *omnichannelState        `json:"inStock,omitempty"`
		Pickup                *omnichannelState        `json:"pickup,omitempty"`
		LfpLink               *omnichannelLfpLink      `json:"lfpLink,omitempty"`
		ODO                   *omnichannelState        `json:"odo,omitempty"`
		About                 *omnichannelState        `json:"about,omitempty"`
		InventoryVerification *omnichannelVerification `json:"inventoryVerification,omitempty"`
	}
)

type omnichannelAnswer struct {
	Settings      []omnichannelSetting `json:"omnichannelSettings"`
	NextPageToken string               `json:"nextPageToken"`
}

// omnichannelResult is the result of the list tool. A list is never null.
type omnichannelResult struct {
	Settings   []omnichannelSetting `json:"omnichannelSettings"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

var (
	stateObjectSchema = objectSchema(member("uri", stringSchema), member("state", stringSchema))
	omnichannelSchema = objectSchema(member("name", stringSchema), member("regionCode", stringSchema),
		member("lsfType", stringSchema), member("inStock", stateObjectSchema), member("pickup", stateObjectSchema),
		member("lfpLink", objectSchema(member("lfpProvider", stringSchema), member("externalAccountId", stringSchema),
			member("state", stringSchema))),
		member("odo", stateObjectSchema), member("about", stateObjectSchema),
		member("inventoryVerification", objectSchema(member("state", stringSchema), member("contact", stringSchema),
			member("contactEmail", stringSchema), member("contactState", stringSchema))))
)

const (
	regionCodeSchema = `{"type":"string","minLength":2,"maxLength":2,"pattern":"^[A-Z]{2}$",` +
		`"x-form":"two-letter uppercase region code of the country, such as CH"}`
	lsfTypeSchema = `{"type":"string","enum":["GHLSF","MHLSF_BASIC","MHLSF_FULL"]}`
	uriSchema     = `{"type":"string","minLength":1,"maxLength":2048,` +
		`"x-form":"absolute https address without user information"}`
	// in_stock may be an empty object: that is what GHLSF needs.
	inStockSchema = `{"type":"object","properties":{"uri":` + uriSchema + `},"additionalProperties":false}`
	pageURISchema = `{"type":"object","properties":{"uri":` + uriSchema + `},"required":["uri"],` +
		`"additionalProperties":false}`
	verificationSchema = `{"type":"object","properties":{"contact":{"type":"string","minLength":1,"maxLength":300},` +
		`"contact_email":{"type":"string","minLength":3,"maxLength":254}},` +
		`"required":["contact","contact_email"],"additionalProperties":false}`
	omnichannelPageSchema = `{"type":"integer","minimum":1,"maximum":1000}`
)

const (
	omnichannelFieldsDescription = "name (accounts/ACCOUNT/omnichannelSettings/REGION), regionCode, lsfType, inStock, " +
		"pickup, odo, and about (each uri and state), lfpLink (lfpProvider, externalAccountId, state), and " +
		"inventoryVerification (state, contact, contactEmail, contactState)"
	omnichannelUntrustedNote = "The addresses, the contact, and the LFP link are untrusted content of whoever runs the " +
		"account: texts are cut, and addresses are text that is never fetched."
	omnichannelDelayNote = "Google accepts the change and then reviews the addresses, which can take from a couple of " +
		"hours to a couple of days, so the answer only tells that Google accepted it and the state members start " +
		"as under review; inventory verification takes about 48 hours. Every call counts against the quota of " +
		"the Merchant API, a failed one too, and a call is never repeated: when the outcome is unknown, read the " +
		"setting before changing it again. The addresses steer customers to your shop. Needs confirmation."
	omnichannelMembers = "lsf_type (GHLSF, MHLSF_BASIC, or MHLSF_FULL), in_stock (uri), pickup (uri), odo (uri), " +
		"about (uri), and inventory_verification (contact, contact_email); every uri is an absolute https " +
		"address with a host and without user information, at most 2048 characters, and Qatlas refuses " +
		"anything else before any request"
)

func omnichannelRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true, DataSensitivity: "merchant-account-data"}
}

var omnichannelWriteArguments = []capability.Argument{
	{Name: "account", Target: kindAccount, Description: "Merchant Center account ID, digits only; the tool " +
		"refuses a call without it; it must be named by a target or be a direct sub-account of a bound advanced account"},
	{Name: "lsf_type", Description: "Local storefront type: GHLSF (hosted by Google; in_stock must be empty, " +
		"inventory verification is required, pickup is not eligible), MHLSF_BASIC or MHLSF_FULL (hosted by the merchant)"},
	{Name: "in_stock", Description: "Product page address Google reviews for in-stock serving; an empty object " +
		"for GHLSF, an address for merchant-hosted types"},
	{Name: "pickup", Description: "Address of the pickup product page, which Google reviews for pickup serving"},
	{Name: "odo", Description: "Address of the on display to order policy"},
	{Name: "about", Description: "Address of the about page (imprint); Switzerland needs one for every type, " +
		"Austria and Germany for GHLSF"},
	{Name: "inventory_verification", Description: "Contact of the inventory verification: contact (name) and " +
		"contact_email; Google sends the contact a mail to verify"},
}

var (
	omnichannelSettingsList = capability.Descriptor{
		ID: omnichannelSettingsListID, Version: 1, Title: "List the omnichannel settings of a Merchant Center account",
		Description: "List the omnichannel settings of one account (accounts.omnichannelSettings.list): one per country, " +
			"a batch per call with a cursor to read on. Google sends 50 by default and at most 1000. On a connection " +
			"bound to an advanced account, an account that is not named by a target is read only after it is found " +
			"among the direct sub-accounts of a bound advanced account, which costs further calls. " +
			omnichannelUntrustedNote,
		Tags: []string{"google", "merchant", "omnichannel-settings"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema(nil, member("account", accountArgSchema), member("page_size", omnichannelPageSchema),
			member("cursor", cursorSchema)),
		OutputSchema: json.RawMessage(listResultSchema("omnichannelSettings", omnichannelSchema)),
		Arguments: []capability.Argument{accountArgument,
			{Name: "page_size", Description: "Settings per batch, 1 to 1000; Google sends 50 when left out"},
			cursorArgument},
		Fields: []capability.Field{
			{Name: "omnichannelSettings", Description: "Settings with " + omnichannelFieldsDescription},
			{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"}},
		Examples: []capability.Example{{Description: "Settings of one account",
			Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}

	omnichannelSettingsGet = capability.Descriptor{
		ID: omnichannelSettingsGetID, Version: 1, Title: "Get the omnichannel setting of a country",
		Description: "Read the omnichannel setting of one country of an account (accounts.omnichannelSettings.get), " +
			"including the review states of its addresses and of the inventory verification. On a connection bound " +
			"to an advanced account, an account that is not named by a target is read only after it is found among " +
			"the direct sub-accounts of a bound advanced account, which costs further calls. After a change it can " +
			"take hours until the states settle. " + omnichannelUntrustedNote,
		Tags: []string{"google", "merchant", "omnichannel-settings"}, Risk: readRisk, Provider: Provider,
		InputSchema: inputSchema([]string{"region_code"}, member("account", accountArgSchema),
			member("region_code", regionCodeSchema)),
		OutputSchema: json.RawMessage(omnichannelSchema),
		Arguments: []capability.Argument{accountArgument,
			{Name: "region_code", Required: true, Description: "Region code of the country, two uppercase letters"}},
		Fields: []capability.Field{{Name: "omnichannelSetting", Description: omnichannelFieldsDescription}},
		Examples: []capability.Example{{Description: "Setting of one country",
			Arguments: json.RawMessage(`{"account":"1000001","region_code":"CH"}`)}},
	}

	omnichannelSettingsCreate = capability.Descriptor{
		ID: omnichannelSettingsCreateID, Version: 1, Title: "Create the omnichannel setting of a country",
		Description: "Create the omnichannel setting of one country of an account (accounts.omnichannelSettings.create). " +
			"region_code and lsf_type are required; the region code cannot be changed afterwards. The members are " +
			omnichannelMembers + ". A merchant-hosted type needs an address for in_stock or pickup, and a Google-hosted " +
			"one needs in_stock empty, and Google refuses what does not fit. Setting an address starts a review " +
			"of it. Offered only where a connection's tools list names it. " + omnichannelDelayNote + " " +
			omnichannelUntrustedNote,
		Tags: []string{"google", "merchant", "omnichannel-settings"}, Provider: Provider, RequiresToolAllowList: true,
		Risk: omnichannelRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
		InputSchema: inputSchema([]string{"region_code", "lsf_type"}, member("account", accountArgSchema),
			member("region_code", regionCodeSchema), member("lsf_type", lsfTypeSchema),
			member("in_stock", inStockSchema), member("pickup", pageURISchema), member("odo", pageURISchema),
			member("about", pageURISchema), member("inventory_verification", verificationSchema)),
		OutputSchema: json.RawMessage(omnichannelSchema),
		Arguments: append(append([]capability.Argument{omnichannelWriteArguments[0],
			{Name: "region_code", Required: true, Description: "Region code of the country, two uppercase letters; " +
				"fixed after creation"}},
			capability.Argument{Name: "lsf_type", Required: true, Description: omnichannelWriteArguments[1].Description}),
			omnichannelWriteArguments[2:]...),
		Fields: []capability.Field{{Name: "omnichannelSetting", Description: "The setting as Google answered: " +
			omnichannelFieldsDescription}},
		Examples: []capability.Example{{Description: "Create a merchant-hosted setting", Arguments: json.RawMessage(
			`{"account":"1000001","region_code":"CH","lsf_type":"MHLSF_BASIC",` +
				`"in_stock":{"uri":"https://shop.example.invalid/in-stock"}}`)}},
	}

	omnichannelSettingsUpdate = capability.Descriptor{
		ID: omnichannelSettingsUpdateID, Version: 1, Title: "Update the omnichannel setting of a country",
		Description: "Change the omnichannel setting of one country of an account (accounts.omnichannelSettings.patch): " +
			"only the members you pass are sent, each named in the update mask, so every other member stays as it is. " +
			"At least one is required. The members are " + omnichannelMembers + ". A member that is passed " +
			"replaces the whole member, and Google clears what the new value leaves out. Google does not allow a " +
			"change to a feature that is running or approved; switching from a merchant-hosted type to GHLSF " +
			"also needs in_stock in the mask, as an empty object. The region code " +
			"cannot be changed. Setting an address starts a review of it. Offered only where a connection's tools " +
			"list names it. " + omnichannelDelayNote + " " + omnichannelUntrustedNote,
		Tags: []string{"google", "merchant", "omnichannel-settings"}, Provider: Provider, RequiresToolAllowList: true,
		Risk: omnichannelRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
		InputSchema: inputSchema([]string{"region_code"}, member("account", accountArgSchema),
			member("region_code", regionCodeSchema), member("lsf_type", lsfTypeSchema),
			member("in_stock", inStockSchema), member("pickup", pageURISchema), member("odo", pageURISchema),
			member("about", pageURISchema), member("inventory_verification", verificationSchema)),
		OutputSchema: json.RawMessage(omnichannelSchema),
		Arguments: append([]capability.Argument{omnichannelWriteArguments[0],
			{Name: "region_code", Required: true, Description: "Region code of the country whose setting changes"}},
			omnichannelWriteArguments[1:]...),
		Fields: []capability.Field{{Name: "omnichannelSetting", Description: "The setting as Google answered: " +
			omnichannelFieldsDescription}},
		Examples: []capability.Example{{Description: "Set the pickup address", Arguments: json.RawMessage(
			`{"account":"1000001","region_code":"CH","pickup":{"uri":"https://shop.example.invalid/pickup"}}`)}},
	}
)

var omnichannelVerify = capability.Descriptor{
	ID: omnichannelVerifyID, Version: 1, Title: "Request the inventory verification of a country",
	Description: "Request the inventory verification of one country of an account " +
		"(accounts.omnichannelSettings.requestInventoryVerification). The request starts a verification process at " +
		"Google for that country: Google sends the contact stored in the setting (inventoryVerification.contact and " +
		"contactEmail) a mail, and the state of the verification changes as the process runs (documented states: " +
		"ACTION_REQUIRED, INACTIVE, RUNNING, SUCCEEDED, SUSPENDED). The request has no body and sets nothing; the " +
		"setting must already hold the contact. Offered only where a connection's tools list names it. Google only " +
		"accepts the request: the process can take from a couple of hours to about 48 hours, and the answer shows the setting as Google " +
		"holds it right after, which can still show the old state. Every call counts against the quota of the " +
		"Merchant API, a failed one too, and a call is never repeated: when the outcome is unknown, read the " +
		"setting before asking again. Needs confirmation. " + omnichannelUntrustedNote,
	Tags: []string{"google", "merchant", "omnichannel-settings"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:         omnichannelRisk(capability.EffectExecute, capability.IdempotencyNonIdempotent),
	InputSchema:  inputSchema([]string{"region_code"}, member("account", accountArgSchema), member("region_code", regionCodeSchema)),
	OutputSchema: json.RawMessage(omnichannelSchema),
	Arguments: []capability.Argument{omnichannelWriteArguments[0],
		{Name: "region_code", Required: true, Description: "Region code of the country whose inventory is verified, " +
			"two uppercase letters"}},
	Fields: []capability.Field{{Name: "omnichannelSetting", Description: "The setting as Google answered: " +
		omnichannelFieldsDescription}},
	Examples: []capability.Example{{Description: "Request the verification for one country",
		Arguments: json.RawMessage(`{"account":"1000001","region_code":"CH"}`)}},
}

func omnichannelDescriptors() []capability.Descriptor {
	return []capability.Descriptor{omnichannelSettingsList, omnichannelSettingsGet, omnichannelSettingsCreate,
		omnichannelSettingsUpdate, omnichannelVerify}
}

func omnichannelHandlers() map[string]capability.Handler {
	return map[string]capability.Handler{
		omnichannelSettingsListID: handleOmnichannelList, omnichannelSettingsGetID: handleOmnichannelGet,
		omnichannelSettingsCreateID: handleOmnichannelCreate, omnichannelSettingsUpdateID: handleOmnichannelUpdate,
		omnichannelVerifyID: handleOmnichannelVerify,
	}
}

type (
	omnichannelURI struct {
		URI string `json:"uri"`
	}
	omnichannelContact struct {
		Contact      string `json:"contact"`
		ContactEmail string `json:"contact_email"`
	}
	// omnichannelArguments holds the members as pointers: a member that was passed differs from one that was
	// left out, which decides the update mask.
	omnichannelArguments struct {
		Account               string              `json:"account"`
		RegionCode            string              `json:"region_code"`
		LsfType               *string             `json:"lsf_type"`
		InStock               *omnichannelURI     `json:"in_stock"`
		Pickup                *omnichannelURI     `json:"pickup"`
		ODO                   *omnichannelURI     `json:"odo"`
		About                 *omnichannelURI     `json:"about"`
		InventoryVerification *omnichannelContact `json:"inventory_verification"`
		PageSize              int                 `json:"page_size"`
		Cursor                string              `json:"cursor"`
	}
)

// omnichannelBody is the OmnichannelSetting as sent: only members the caller passed, never an output member.
type omnichannelBody struct {
	RegionCode            string                  `json:"regionCode,omitempty"`
	LsfType               string                  `json:"lsfType,omitempty"`
	InStock               *omnichannelURIBody     `json:"inStock,omitempty"`
	Pickup                *omnichannelURIBody     `json:"pickup,omitempty"`
	ODO                   *omnichannelURIBody     `json:"odo,omitempty"`
	About                 *omnichannelURIBody     `json:"about,omitempty"`
	InventoryVerification *omnichannelContactBody `json:"inventoryVerification,omitempty"`
}

type (
	omnichannelURIBody struct {
		URI string `json:"uri,omitempty"`
	}
	omnichannelContactBody struct {
		Contact      string `json:"contact"`
		ContactEmail string `json:"contactEmail"`
	}
)

func readOmnichannelArguments(raw json.RawMessage) (omnichannelArguments, error) {
	var a omnichannelArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, invalidArgument("the arguments could not be read")
	}
	return a, nil
}

func (a omnichannelArguments) checkRegion() error {
	if !regionCodeForm.MatchString(a.RegionCode) {
		return invalidArgument("region_code must be a two-letter uppercase region code such as CH")
	}
	return nil
}

// checkOmnichannelURI refuses an address that is too long or not plain https, with the one shared check.
func checkOmnichannelURI(name, raw string) error {
	if utf8.RuneCountInString(raw) > maxURLRunes {
		return invalidArgument(name + " must be at most " + strconv.Itoa(maxURLRunes) + " characters")
	}
	return checkHTTPSAddress(name, raw)
}

// check refuses a member that is not in the documented form, before any secret is read.
func (a omnichannelArguments) check() error {
	if a.LsfType != nil {
		valid := false
		for _, t := range lsfTypes {
			valid = valid || *a.LsfType == t
		}
		if !valid {
			return invalidArgument("lsf_type must be GHLSF, MHLSF_BASIC, or MHLSF_FULL")
		}
	}
	if a.InStock != nil && a.InStock.URI != "" {
		if err := checkOmnichannelURI("in_stock.uri", a.InStock.URI); err != nil {
			return err
		}
		if a.LsfType != nil && *a.LsfType == "GHLSF" {
			return invalidArgument("in_stock.uri must be empty for lsf_type GHLSF")
		}
	}
	for _, p := range []struct {
		name string
		uri  *omnichannelURI
	}{{"pickup.uri", a.Pickup}, {"odo.uri", a.ODO}, {"about.uri", a.About}} {
		if p.uri != nil {
			if err := checkOmnichannelURI(p.name, p.uri.URI); err != nil {
				return err
			}
		}
	}
	if v := a.InventoryVerification; v != nil {
		if strings.TrimSpace(v.Contact) == "" || utf8.RuneCountInString(v.Contact) > maxContactRunes ||
			strings.IndexFunc(v.Contact, unicode.IsControl) >= 0 {
			return invalidArgument("inventory_verification.contact must be 1 to " + strconv.Itoa(maxContactRunes) +
				" characters without control characters")
		}
		if utf8.RuneCountInString(v.ContactEmail) > maxContactEmailRunes || !contactEmailForm.MatchString(v.ContactEmail) ||
			strings.IndexFunc(v.ContactEmail, unicode.IsControl) >= 0 {
			return invalidArgument("inventory_verification.contact_email must be a mail address")
		}
	}
	return nil
}

// mask lists the members the call sets, in the snake_case the update mask of this method documents.
func (a omnichannelArguments) mask() string {
	var fields []string
	for _, m := range []struct {
		name string
		set  bool
	}{
		{"lsf_type", a.LsfType != nil}, {"in_stock", a.InStock != nil}, {"pickup", a.Pickup != nil},
		{"odo", a.ODO != nil}, {"about", a.About != nil}, {"inventory_verification", a.InventoryVerification != nil},
	} {
		if m.set {
			fields = append(fields, m.name)
		}
	}
	return strings.Join(fields, ",")
}

func uriBody(u *omnichannelURI) *omnichannelURIBody {
	if u == nil {
		return nil
	}
	return &omnichannelURIBody{URI: u.URI}
}

// body is the setting as sent. The region code goes into the body only on create; on update it is the path.
func (a omnichannelArguments) body(withRegion bool) omnichannelBody {
	b := omnichannelBody{InStock: uriBody(a.InStock), Pickup: uriBody(a.Pickup), ODO: uriBody(a.ODO),
		About: uriBody(a.About)}
	if withRegion {
		b.RegionCode = a.RegionCode
	}
	if a.LsfType != nil {
		b.LsfType = *a.LsfType
	}
	if v := a.InventoryVerification; v != nil {
		b.InventoryVerification = &omnichannelContactBody{Contact: v.Contact, ContactEmail: v.ContactEmail}
	}
	return b
}

func omnichannelSettingsPath(id string) string { return accountPath(id) + "/omnichannelSettings" }

func handleOmnichannelList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("list omnichannel settings", omnichannelSettingsListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(c.tool, id, c.args.PageSize, c.scope.key())
	query := url.Values{}
	if c.args.PageSize > 0 {
		query.Set("pageSize", strconv.Itoa(min(c.args.PageSize, omnichannelPageSize)))
	}
	if c.args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, c.args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	var answer omnichannelAnswer
	if err := cl.get(ctx, c.op, c.tool, omnichannelSettingsPath(id), query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Settings) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := omnichannelResult{Settings: append([]omnichannelSetting{}, answer.Settings...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleOmnichannelGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get omnichannel setting", omnichannelSettingsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	a, err := readOmnichannelArguments(raw)
	if err != nil {
		return nil, err
	}
	if err := a.checkRegion(); err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	var out omnichannelSetting
	if err := cl.get(ctx, c.op, c.tool, omnichannelSettingsPath(id)+"/"+a.RegionCode, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// changeOmnichannel runs the shared path of the two write tools: the account was settled against the targets
// before any secret is resolved, it is proven if needed, and the change is sent once.
func changeOmnichannel(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	c call, id string, needsProof bool, r request) (any, error) {
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	var out omnichannelSetting
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleOmnichannelCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("create omnichannel setting", omnichannelSettingsCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	a, err := readOmnichannelArguments(raw)
	if err != nil {
		return nil, err
	}
	if a.Account == "" {
		return nil, invalidArgument("account is required")
	}
	if a.RegionCode == "" || a.LsfType == nil {
		return nil, invalidArgument("region_code and lsf_type are required")
	}
	if err := a.checkRegion(); err != nil {
		return nil, err
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(a.body(true))
	if err != nil {
		return nil, provider.Fail(c.op, "the request could not be built")
	}
	return changeOmnichannel(ctx, resolved, secrets, red, c, id, needsProof, request{method: http.MethodPost,
		path: omnichannelSettingsPath(id), body: body, change: true})
}

func handleOmnichannelUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update omnichannel setting", omnichannelSettingsUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	a, err := readOmnichannelArguments(raw)
	if err != nil {
		return nil, err
	}
	if a.Account == "" {
		return nil, invalidArgument("account is required")
	}
	if a.RegionCode == "" {
		return nil, invalidArgument("region_code is required")
	}
	if err := a.checkRegion(); err != nil {
		return nil, err
	}
	if a.mask() == "" {
		return nil, invalidArgument("pass at least one of lsf_type, in_stock, pickup, odo, about, inventory_verification")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(a.body(false))
	if err != nil {
		return nil, provider.Fail(c.op, "the request could not be built")
	}
	return changeOmnichannel(ctx, resolved, secrets, red, c, id, needsProof, request{method: http.MethodPatch,
		path: omnichannelSettingsPath(id) + "/" + a.RegionCode, query: url.Values{"updateMask": {a.mask()}},
		body: body, change: true})
}

// verifyAnswer is the answer of the verification request: the setting is wrapped.
type verifyAnswer struct {
	Setting omnichannelSetting `json:"omnichannelSetting"`
}

func handleOmnichannelVerify(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("request inventory verification", omnichannelVerifyID, resolved, raw)
	if err != nil {
		return nil, err
	}
	a, err := readOmnichannelArguments(raw)
	if err != nil {
		return nil, err
	}
	if a.Account == "" {
		return nil, invalidArgument("account is required")
	}
	if a.RegionCode == "" {
		return nil, invalidArgument("region_code is required")
	}
	if err := a.checkRegion(); err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(a.Account)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	var out verifyAnswer
	// The documented request body is empty; an empty object is the JSON form of it.
	err = cl.send(ctx, c.op, c.tool, request{method: http.MethodPost,
		path: omnichannelSettingsPath(id) + "/" + a.RegionCode + ":requestInventoryVerification",
		body: []byte("{}"), change: true}, &out)
	if err != nil {
		return nil, err
	}
	return out.Setting, nil
}

// omnichannelSummaries phrase what a confirmed call would do, from its arguments alone.
func omnichannelSummaries() map[string]capability.SummaryFunc {
	return map[string]capability.SummaryFunc{
		omnichannelSettingsCreateID: func(raw json.RawMessage) string {
			a, err := readOmnichannelArguments(raw)
			if err != nil || a.LsfType == nil {
				return ""
			}
			return "Create the omnichannel setting " + strconv.Quote(*a.LsfType) + " of region " +
				strconv.Quote(a.RegionCode) + " for Merchant Center account " + a.Account + a.addresses()
		},
		omnichannelVerifyID: func(raw json.RawMessage) string {
			a, err := readOmnichannelArguments(raw)
			if err != nil {
				return ""
			}
			return "Start the inventory verification process at Google for region " + strconv.Quote(a.RegionCode) +
				" of Merchant Center account " + a.Account + "; Google mails the contact stored in the setting"
		},
		omnichannelSettingsUpdateID: func(raw json.RawMessage) string {
			a, err := readOmnichannelArguments(raw)
			if err != nil {
				return ""
			}
			return "Update the omnichannel setting of region " + strconv.Quote(a.RegionCode) +
				" of Merchant Center account " + a.Account + ": set " + strings.ReplaceAll(a.mask(), ",", ", ") +
				a.addresses()
		},
	}
}

// addresses names the addresses of the call, which steer customers.
func (a omnichannelArguments) addresses() string {
	var out []string
	for _, p := range []struct {
		name string
		uri  *omnichannelURI
	}{{"in_stock", a.InStock}, {"pickup", a.Pickup}, {"odo", a.ODO}, {"about", a.About}} {
		if p.uri != nil && p.uri.URI != "" {
			out = append(out, p.name+" "+strconv.Quote(p.uri.URI))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "; addresses: " + strings.Join(out, ", ")
}
