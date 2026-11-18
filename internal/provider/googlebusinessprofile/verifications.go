package googlebusinessprofile

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	verificationsListID          = Provider + ".verifications.list"
	verificationsFetchOptionsID  = Provider + ".verifications.fetch-options"
	verificationsMerchantStateID = Provider + ".verifications.get-merchant-state"

	verificationsPathSuffix = "/verifications"
	fetchOptionsPathSuffix  = ":fetchVerificationOptions"
	merchantStatePathSuffix = "/VoiceOfMerchantState"

	maxVerificationsPage = 100
)

var verificationsRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "verification state of a business location and, for the options, the masked targets a PIN " +
		"could be sent to: parts of a postal address, a phone number, or an email address",
}

// The verifications API as its discovery document documents it.
type (
	verification struct {
		Name         short `json:"name,omitempty"`
		State        short `json:"state,omitempty"`
		Method       short `json:"method,omitempty"`
		Announcement line  `json:"announcement,omitempty"`
		CreateTime   short `json:"createTime,omitempty"`
	}
	verificationsAnswer struct {
		Verifications list[verification] `json:"verifications"`
		NextPageToken string             `json:"nextPageToken"`
	}
	verificationsResult struct {
		Verifications []verification `json:"verifications"`
		NextCursor    string         `json:"next_cursor,omitempty"`
	}

	verificationOption struct {
		VerificationMethod short                `json:"verificationMethod,omitempty"`
		Announcement       line                 `json:"announcement,omitempty"`
		EmailData          *emailVerification   `json:"emailData,omitempty"`
		AddressData        *addressVerification `json:"addressData,omitempty"`
		PhoneNumber        line                 `json:"phoneNumber,omitempty"`
	}
	emailVerification struct {
		Domain             line `json:"domain,omitempty"`
		IsUserNameEditable flag `json:"isUserNameEditable,omitzero"`
		User               line `json:"user,omitempty"`
	}
	addressVerification struct {
		Business                   line           `json:"business,omitempty"`
		Address                    *postalAddress `json:"address,omitempty"`
		ExpectedDeliveryDaysRegion integer        `json:"expectedDeliveryDaysRegion,omitzero"`
	}
	optionsAnswer struct {
		Options list[verificationOption] `json:"options"`
	}
	optionsResult struct {
		Options []verificationOption `json:"options"`
	}

	merchantAnswer struct {
		HasVoiceOfMerchant       flag                  `json:"hasVoiceOfMerchant"`
		HasBusinessAuthority     flag                  `json:"hasBusinessAuthority"`
		WaitForVoiceOfMerchant   *struct{}             `json:"waitForVoiceOfMerchant"`
		ComplyWithGuidelines     *complyWithGuidelines `json:"complyWithGuidelines"`
		ResolveOwnershipConflict *struct{}             `json:"resolveOwnershipConflict"`
		Verify                   *struct {
			HasPendingVerification flag `json:"hasPendingVerification"`
		} `json:"verify"`
	}
	// merchantState shows the members Google documents as empty messages as true, because the broker leaves
	// an empty object out of a result, and shows a present verify with its flag, false if Google leaves it out.
	merchantState struct {
		HasVoiceOfMerchant       flag                  `json:"hasVoiceOfMerchant,omitzero"`
		HasBusinessAuthority     flag                  `json:"hasBusinessAuthority,omitzero"`
		WaitForVoiceOfMerchant   bool                  `json:"waitForVoiceOfMerchant,omitempty"`
		ComplyWithGuidelines     *complyWithGuidelines `json:"complyWithGuidelines,omitempty"`
		ResolveOwnershipConflict bool                  `json:"resolveOwnershipConflict,omitempty"`
		Verify                   *verifyState          `json:"verify,omitempty"`
	}
	complyWithGuidelines struct {
		RecommendationReason short `json:"recommendationReason,omitempty"`
	}
	verifyState struct {
		HasPendingVerification bool `json:"hasPendingVerification"`
	}
)

var (
	verificationSchema = objectSchema("name", stringSchema, "state", stringSchema, "method", stringSchema,
		"announcement", stringSchema, "createTime", stringSchema)
	verificationsResultSchema = `{"type":"object","properties":{"verifications":` + arraySchema(verificationSchema) +
		`,"next_cursor":{"type":"string"}},"required":["verifications"],"additionalProperties":false}`

	optionSchema = objectSchema("verificationMethod", stringSchema, "announcement", stringSchema,
		"emailData", objectSchema("domain", stringSchema, "isUserNameEditable", booleanSchema, "user", stringSchema),
		"addressData", objectSchema("business", stringSchema, "address", postalAddressSchema,
			"expectedDeliveryDaysRegion", integerSchema),
		"phoneNumber", stringSchema)
	optionsResultSchema = `{"type":"object","properties":{"options":` + arraySchema(optionSchema) +
		`},"required":["options"],"additionalProperties":false}`

	merchantStateSchema = objectSchema("hasVoiceOfMerchant", booleanSchema, "hasBusinessAuthority", booleanSchema,
		"waitForVoiceOfMerchant", booleanSchema,
		"complyWithGuidelines", objectSchema("recommendationReason", stringSchema),
		"resolveOwnershipConflict", booleanSchema,
		"verify", objectSchema("hasPendingVerification", booleanSchema))
)

const (
	languageCodeSchema = `{"type":"string","minLength":2,"maxLength":35,` +
		`"pattern":"^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8}){0,3}$"}`
	shortInputSchema = `{"type":"string","maxLength":100}`
	lineInputSchema  = `{"type":"string","maxLength":500}`
	// postalAddressInputSchema bounds what a caller may send as the address of a service business. It is closed,
	// and only the region code is required, as Google documents.
	postalAddressInputSchema = `{"type":"object","properties":{"revision":{"type":"integer","enum":[0]},` +
		`"regionCode":{"type":"string","pattern":"^[A-Za-z0-9]{2,3}$"},"languageCode":` + shortInputSchema + `,` +
		`"postalCode":` + shortInputSchema + `,"sortingCode":` + shortInputSchema + `,` +
		`"administrativeArea":` + lineInputSchema + `,"locality":` + lineInputSchema + `,` +
		`"sublocality":` + lineInputSchema + `,"addressLines":{"type":"array","maxItems":20,"items":` +
		lineInputSchema + `},"recipients":{"type":"array","maxItems":20,"items":` + lineInputSchema + `},` +
		`"organization":` + lineInputSchema + `},"required":["regionCode"],"additionalProperties":false}`
	optionsContextSchema = `{"type":"object","properties":{"address":` + postalAddressInputSchema +
		`},"additionalProperties":false}`
)

var verificationFields = []capability.Field{
	{Name: "verifications", Description: "Verifications with name, state (PENDING, COMPLETED, or FAILED), method, " +
		"announcement, and createTime, ordered by create time"}, nextCursorField,
}

var verificationsList = capability.Descriptor{
	ID: verificationsListID, Version: 1, Title: "List Business Profile verifications",
	Description: "List the verification attempts of one Google Business Profile location, ordered by create time, " +
		"with their method, state, and time. Up to 100 per call with a cursor to read on. The tool only reads: it " +
		"starts no verification and confirms no PIN. On a connection bound to an account the location is first " +
		"proved to belong to it. " + untrustedNote,
	Tags: []string{"google", "business-profile", "verifications"}, Risk: verificationsRisk, Provider: Provider,
	InputSchema: inputSchema(nil, "location", locationArgSchema,
		"page_size", `{"type":"integer","minimum":1,"maximum":100}`, "cursor", cursorSchema),
	OutputSchema: json.RawMessage(verificationsResultSchema),
	Arguments: []capability.Argument{
		locationArgument,
		{Name: "page_size", Description: "Verifications per call, 1 to 100; Google's default and maximum is 100"},
		cursorArgument,
	},
	Fields: verificationFields,
	Examples: []capability.Example{{Description: "Verifications of one location",
		Arguments: json.RawMessage(`{"location":"locations/2000002"}`)}},
}

var verificationsFetchOptions = capability.Descriptor{
	ID: verificationsFetchOptionsID, Version: 1, Title: "List Business Profile verification options",
	Description: "List the verification options Google offers for one location in a language: the method " +
		"(ADDRESS, EMAIL, PHONE_CALL, SMS, AUTO, or TRUSTED_PARTNER) and where a PIN would go, as the masked " +
		"target Google returns: the domain and the user part of an email address, a phone number, or a postal " +
		"address. The targets are personal data and are shown exactly as Google masks them. Google documents " +
		"this method as a report only: it is sent as a POST but starts no verification, sends no PIN, and " +
		"changes nothing. context.address is only for a location that serves customers at their location and " +
		"may enable more options. On a connection bound to an account the location is first proved to belong " +
		"to it. " + untrustedNote,
	Tags: []string{"google", "business-profile", "verifications"}, Risk: verificationsRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"language_code"}, "location", locationArgSchema,
		"language_code", languageCodeSchema, "context", optionsContextSchema),
	OutputSchema: json.RawMessage(optionsResultSchema),
	Arguments: []capability.Argument{
		locationArgument,
		{Name: "language_code", Required: true, Description: "BCP 47 language code for the verification, such as de or en-GB"},
		{Name: "context", Description: "Optional extra context for a location that serves customers at their " +
			"location: an object with address, a postal address with regionCode (required) and the optional " +
			"revision, languageCode, postalCode, sortingCode, administrativeArea, locality, sublocality, " +
			"addressLines, recipients, and organization. Google refuses it for other business types"},
	},
	Fields: []capability.Field{{Name: "options", Description: "Options with verificationMethod, announcement, " +
		"emailData (domain, isUserNameEditable, user), addressData (business, address, " +
		"expectedDeliveryDaysRegion), and phoneNumber; the targets are masked by Google"}},
	Examples: []capability.Example{{Description: "Options in English",
		Arguments: json.RawMessage(`{"location":"locations/2000002","language_code":"en"}`)}},
}

var verificationsMerchantState = capability.Descriptor{
	ID: verificationsMerchantStateID, Version: 1, Title: "Get the Voice of Merchant state of a Business Profile location",
	Description: "Read whether one Google Business Profile location is in good standing and has control over the " +
		"business on Google (Voice of Merchant), and why not: waiting for review, failing the guidelines, " +
		"duplicating a location in good standing, or needing verification. Members Google leaves out are not " +
		"shown. On a connection bound to an account the location is first proved to belong to it.",
	Tags: []string{"google", "business-profile", "verifications"}, Risk: verificationsRisk, Provider: Provider,
	InputSchema:  inputSchema(nil, "location", locationArgSchema),
	OutputSchema: json.RawMessage(merchantStateSchema),
	Arguments:    []capability.Argument{locationArgument},
	Fields: []capability.Field{
		{Name: "hasVoiceOfMerchant", Description: "True if the location is in good standing and has control over the business on Google"},
		{Name: "hasBusinessAuthority", Description: "True if the location has the authority (ownership) over the business on Google"},
		{Name: "waitForVoiceOfMerchant", Description: "true while the location is under review (Google sends an empty message)"},
		{Name: "complyWithGuidelines", Description: "Present if the location fails the guidelines: recommendationReason"},
		{Name: "resolveOwnershipConflict", Description: "true if the location duplicates another in good standing (Google sends an empty message)"},
		{Name: "verify", Description: "Present if the location needs verification: hasPendingVerification, false when Google leaves it out"},
	},
	Examples: []capability.Example{{Description: "State of one location",
		Arguments: json.RawMessage(`{"location":"locations/2000002"}`)}},
}

type verificationsArguments struct {
	Location     string          `json:"location"`
	PageSize     int             `json:"page_size"`
	Cursor       string          `json:"cursor"`
	LanguageCode string          `json:"language_code"`
	Context      *optionsContext `json:"context,omitempty"`
}

// optionsContext is the extra context of a service business, as Google documents it.
type optionsContext struct {
	Address *postalAddressInput `json:"address,omitempty"`
}

// postalAddressInput is the address of the argument context, sent as given; the schema has bounded it.
type postalAddressInput struct {
	Revision           *int     `json:"revision,omitempty"`
	RegionCode         string   `json:"regionCode"`
	LanguageCode       string   `json:"languageCode,omitempty"`
	PostalCode         string   `json:"postalCode,omitempty"`
	SortingCode        string   `json:"sortingCode,omitempty"`
	AdministrativeArea string   `json:"administrativeArea,omitempty"`
	Locality           string   `json:"locality,omitempty"`
	Sublocality        string   `json:"sublocality,omitempty"`
	AddressLines       []string `json:"addressLines,omitempty"`
	Recipients         []string `json:"recipients,omitempty"`
	Organization       string   `json:"organization,omitempty"`
}

func readVerificationsArguments(op string, raw json.RawMessage) (verificationsArguments, error) {
	var args verificationsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, provider.Fail(op, "the validated arguments could not be read")
	}
	return args, nil
}

// locationCall settles the location of a call against the target and opens the client, proving the location
// for an account target.
func locationCall(ctx context.Context, op string, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, given string) (*client, string, error) {
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, "", err
	}
	name, err := bound.locationOf(given)
	if err != nil {
		return nil, "", err
	}
	c, err := openLocation(ctx, op, resolved, secrets, red, bound, name)
	return c, name, err
}

func handleVerificationsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list verifications"
	args, err := readVerificationsArguments(op, raw)
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
	binding := provider.CursorBinding(verificationsListID, name, bound.binding())
	query, err := pageQuery(binding, args.Cursor, pageSizeQuery(nil, args.PageSize))
	if err != nil {
		return nil, err
	}
	c, err := openLocation(ctx, op, resolved, secrets, red, bound, name)
	if err != nil {
		return nil, err
	}
	var answer verificationsAnswer
	if err := c.get(ctx, op, verificationsListID, request{root: verificationsRoot,
		path: locationPath(name) + verificationsPathSuffix, query: query, notFound: notFoundLocation}, &answer); err != nil {
		return nil, err
	}
	if len(answer.Verifications) > maxVerificationsPage {
		return nil, tooMany(op, maxVerificationsPage)
	}
	out := verificationsResult{Verifications: append([]verification{}, answer.Verifications...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleVerificationsFetchOptions(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "fetch verification options"
	args, err := readVerificationsArguments(op, raw)
	if err != nil {
		return nil, err
	}
	if args.LanguageCode == "" {
		return nil, invalidArgument("language_code is required")
	}
	body := struct {
		LanguageCode string          `json:"languageCode"`
		Context      *optionsContext `json:"context,omitempty"`
	}{LanguageCode: args.LanguageCode, Context: args.Context}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, provider.Fail(op, "the request could not be built")
	}
	c, name, err := locationCall(ctx, op, resolved, secrets, red, args.Location)
	if err != nil {
		return nil, err
	}
	var answer optionsAnswer
	if err := c.post(ctx, op, verificationsFetchOptionsID, request{root: verificationsRoot,
		path: locationPath(name) + fetchOptionsPathSuffix, body: payload, notFound: notFoundLocation}, &answer); err != nil {
		return nil, err
	}
	return optionsResult{Options: append([]verificationOption{}, answer.Options...)}, nil
}

func handleVerificationsMerchantState(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get merchant state"
	args, err := readVerificationsArguments(op, raw)
	if err != nil {
		return nil, err
	}
	c, name, err := locationCall(ctx, op, resolved, secrets, red, args.Location)
	if err != nil {
		return nil, err
	}
	var answer merchantAnswer
	if err := c.get(ctx, op, verificationsMerchantStateID, request{root: verificationsRoot,
		path: locationPath(name) + merchantStatePathSuffix, notFound: notFoundLocation}, &answer); err != nil {
		return nil, err
	}
	state := merchantState{HasVoiceOfMerchant: answer.HasVoiceOfMerchant, HasBusinessAuthority: answer.HasBusinessAuthority,
		WaitForVoiceOfMerchant: answer.WaitForVoiceOfMerchant != nil, ComplyWithGuidelines: answer.ComplyWithGuidelines,
		ResolveOwnershipConflict: answer.ResolveOwnershipConflict != nil}
	if answer.Verify != nil {
		state.Verify = &verifyState{HasPendingVerification: answer.Verify.HasPendingVerification.value}
	}
	return state, nil
}
