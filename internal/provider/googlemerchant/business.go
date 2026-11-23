package googlemerchant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
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
	businessInfosGetID         = Provider + ".business-infos.get"
	businessInfosUpdateID      = Provider + ".business-infos.update"
	businessIdentitiesGetID    = Provider + ".business-identities.get"
	businessIdentitiesUpdateID = Provider + ".business-identities.update"
)

// The updateMask of both methods is a list of paths the discovery document names as supported, in
// lowerCamelCase. Nothing else is writable: phone and phoneVerificationState of BusinessInfo are output only
// and name is the identifier.
var (
	businessInfoPaths     = []string{"address", "customerService", "koreanBusinessRegistrationNumber"}
	businessIdentityPaths = []string{"blackOwned", "latinoOwned", "promotionsConsent", "smallBusiness", "veteranOwned",
		"womenOwned"}
	businessDeclarations = []string{"SELF_IDENTIFIES_AS", "DOES_NOT_SELF_IDENTIFY_AS"}
	businessConsents     = []string{"PROMOTIONS_CONSENT_GIVEN", "PROMOTIONS_CONSENT_DENIED"}
)

// Bounds of the structured arguments. The discovery document names none, so these are Qatlas's own and
// narrower than any text a merchant needs.
const (
	businessMaxLines   = 5
	businessMaxLine    = 200
	businessMaxPart    = 100
	businessMaxPostal  = 20
	businessMaxEmail   = 254
	businessMaxAddress = 2048
)

var (
	businessRegionForm    = regexp.MustCompile(`^[A-Z]{2}$`)
	businessE164Form      = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
	businessExtensionForm = regexp.MustCompile(`^[0-9]{1,40}$`)
	businessShortForm     = regexp.MustCompile(`^[0-9]{1,8}$`)
	businessKoreanForm    = regexp.MustCompile(`^[0-9]{3}-[0-9]{2}-[0-9]{5}$`)
)

func businessEnum(values []string) string {
	encoded, _ := json.Marshal(values)
	return `{"type":"string","enum":` + string(encoded) + `}`
}

func businessMaskSchema(paths []string) string {
	encoded, _ := json.Marshal(paths)
	return `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(len(paths)) + `,"uniqueItems":true,"items":{"type":"string","enum":` +
		string(encoded) + `}}`
}

const (
	businessPhoneSchemaIn = `{"type":"object","additionalProperties":false,"properties":{` +
		`"e164Number":{"type":"string","pattern":"^\\+[1-9][0-9]{6,14}$"},` +
		`"extension":{"type":"string","pattern":"^[0-9]{1,40}$"},` +
		`"shortCode":{"type":"object","additionalProperties":false,"required":["regionCode","number"],"properties":{` +
		`"regionCode":{"type":"string","pattern":"^[A-Z]{2}$"},"number":{"type":"string","pattern":"^[0-9]{1,8}$"}}}}}`
	businessAddressSchemaIn = `{"type":"object","additionalProperties":false,"required":["regionCode"],"properties":{` +
		`"regionCode":{"type":"string","pattern":"^[A-Z]{2}$"},` +
		`"addressLines":{"type":"array","maxItems":5,"items":{"type":"string","minLength":1,"maxLength":200}},` +
		`"postalCode":{"type":"string","minLength":1,"maxLength":20},` +
		`"administrativeArea":{"type":"string","minLength":1,"maxLength":100},` +
		`"locality":{"type":"string","minLength":1,"maxLength":100}}}`
	businessServiceSchemaIn = `{"type":"object","additionalProperties":false,"minProperties":1,"properties":{` +
		`"uri":{"type":"string","minLength":1,"maxLength":2048},` +
		`"email":{"type":"string","minLength":1,"maxLength":254},` +
		`"phone":` + businessPhoneSchemaIn + `}}`
	businessKoreanSchemaIn = `{"type":"string","pattern":"^[0-9]{3}-[0-9]{2}-[0-9]{5}$"}`
)

const (
	businessUntrusted = "Texts, addresses, e-mail addresses, and phone numbers come from Google and from whoever " +
		"runs the account: they are shown as untrusted text, texts are cut, lists are capped at 100 entries, and " +
		"addresses are never fetched."
	businessInfoFields = "name (accounts/ACCOUNT/businessInfo), phone (e164Number, extension, shortCode with regionCode " +
		"and number; output only, set through Merchant Center verification), phoneVerificationState " +
		"(PHONE_VERIFICATION_STATE_VERIFIED, _UNVERIFIED; output only), customerService (uri, email, phone), address " +
		"(regionCode, recipients, languageCode, locality, addressLines, sublocality, organization, postalCode, " +
		"sortingCode, administrativeArea, revision), and koreanBusinessRegistrationNumber"
	businessIdentityFields = "name (accounts/ACCOUNT/businessIdentity), promotionsConsent " +
		"(PROMOTIONS_CONSENT_GIVEN, PROMOTIONS_CONSENT_DENIED), and for each of blackOwned, latinoOwned, " +
		"smallBusiness, veteranOwned, and womenOwned the identityDeclaration (SELF_IDENTIFIES_AS, " +
		"DOES_NOT_SELF_IDENTIFY_AS)"
	businessAdminNote = "Changing it needs the Admin role in Merchant Center: Google answers a missing role with a " +
		"403 \"User is not an administrator of account ...\", which ends as a permission error without a retry."
	businessChangeNote = "The change is processed by Google afterwards and can take a while to show: the result says " +
		"that Google accepted the request, so read the object again to see the state. A failed call may or may not " +
		"have changed it and is never repeated automatically. Only the fields listed in update_mask are replaced, " +
		"a listed object such as address or customer_service replaces the whole object."
	businessAdminPermission = " Changing the business profile needs the Admin role in Merchant Center, and Google " +
		"refuses a user or service account without it as \"User is not an administrator of the account\"; ask an " +
		"administrator of the account to make the change or to give this user the Admin role"
)

func businessChangeRisk() capability.Risk {
	return capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "merchant-account-data"}
}

var (
	businessInfoSchema = objectSchema(member("name", stringSchema), member("phone", businessPhoneSchema),
		member("phoneVerificationState", stringSchema),
		member("customerService", objectSchema(member("uri", stringSchema), member("email", stringSchema),
			member("phone", businessPhoneSchema))),
		member("address", businessAddressSchema), member("koreanBusinessRegistrationNumber", stringSchema))
	businessPhoneSchema = objectSchema(member("e164Number", stringSchema), member("extension", stringSchema),
		member("shortCode", objectSchema(member("regionCode", stringSchema), member("number", stringSchema))))
	businessAddressSchema = objectSchema(member("regionCode", stringSchema), member("recipients", listSchema),
		member("languageCode", stringSchema), member("locality", stringSchema), member("addressLines", listSchema),
		member("sublocality", stringSchema), member("organization", stringSchema), member("postalCode", stringSchema),
		member("sortingCode", stringSchema), member("administrativeArea", stringSchema),
		member("revision", `{"type":"integer"}`))
	businessAttributeSchema = objectSchema(member("identityDeclaration", stringSchema))
	businessIdentitySchema  = objectSchema(member("name", stringSchema), member("promotionsConsent", stringSchema),
		member("blackOwned", businessAttributeSchema), member("latinoOwned", businessAttributeSchema),
		member("smallBusiness", businessAttributeSchema), member("veteranOwned", businessAttributeSchema),
		member("womenOwned", businessAttributeSchema))
	businessInfoChangeSchema     = objectSchema(member("accepted", boolSchema), member("businessInfo", businessInfoSchema))
	businessIdentityChangeSchema = objectSchema(member("accepted", boolSchema), member("businessIdentity", businessIdentitySchema))
)

var (
	businessMaskArgument = capability.Argument{Name: "update_mask", Required: true, Description: "Fields to change, a " +
		"list without duplicates; each listed field needs its value argument and a value without its field listed is refused"}

	businessInfosGet = capability.Descriptor{
		ID: businessInfosGetID, Version: 1, Title: "Get the business info of a Merchant Center account",
		Description: "Read the business info of a Merchant Center account: the business address, the customer service " +
			"contacts, the verified phone number, and the Korean business registration number. The contact data is " +
			"personal data of the merchant and is shown unmasked. An account outside the targets of the connection is " +
			"refused without a request. After a change it can take a while until it shows. " + businessUntrusted,
		Tags: []string{"google", "merchant", "business-profile"}, Risk: readRisk, Provider: Provider,
		InputSchema:  inputSchema(nil, member("account", accountArgSchema)),
		OutputSchema: json.RawMessage(businessInfoSchema),
		Arguments:    []capability.Argument{accountArgument},
		Fields:       []capability.Field{{Name: "businessInfo", Description: businessInfoFields}},
		Examples:     []capability.Example{{Description: "Business info of one account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}
	businessInfosUpdate = capability.Descriptor{
		ID: businessInfosUpdateID, Version: 1, Title: "Update the business info of a Merchant Center account",
		Description: "Change the business address, the customer service contacts, or the Korean business registration " +
			"number of a Merchant Center account. The body is built from the structured arguments, only the fields " +
			"in update_mask are sent, and the phone number and its verification state are output only and cannot be " +
			"written. Of the address Google reads only regionCode, addressLines, postalCode, administrativeArea, and " +
			"locality. customer_service.uri is an address that sends customers to a page, so it must be an https " +
			"address with a host and without user information; e-mail and phone are checked for their form, " +
			"phone takes an E.164 number with an optional extension or a short code. " + businessAdminNote + " " +
			businessChangeNote + " The tool is offered only when the connection lists it in tools, and every call " +
			"needs a confirmation that shows the account and the fields. " + businessUntrusted,
		Tags: []string{"google", "merchant", "business-profile"}, Risk: businessChangeRisk(), Provider: Provider,
		RequiresToolAllowList: true,
		InputSchema: inputSchema([]string{"update_mask"}, member("account", accountArgSchema),
			member("update_mask", businessMaskSchema(businessInfoPaths)), member("address", businessAddressSchemaIn),
			member("customer_service", businessServiceSchemaIn),
			member("korean_business_registration_number", businessKoreanSchemaIn)),
		OutputSchema: json.RawMessage(businessInfoChangeSchema),
		Arguments: []capability.Argument{accountArgument, businessMaskArgument,
			{Name: "address", Description: "Business address with regionCode (two capital letters, required), " +
				"addressLines (up to 5 lines of 200 characters), postalCode, administrativeArea, and locality; for " +
				"update_mask address"},
			{Name: "customer_service", Description: "Customer service with uri (https, no user information), email, and " +
				"phone (e164Number with optional extension, or shortCode with regionCode and number); for update_mask " +
				"customerService"},
			{Name: "korean_business_registration_number", Description: "Ten digits as XXX-XX-XXXXX; for update_mask " +
				"koreanBusinessRegistrationNumber"}},
		Fields: []capability.Field{
			{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
			{Name: "businessInfo", Description: "The business info as Google returned it, with " + businessInfoFields},
		},
		Examples: []capability.Example{{Description: "Change the customer service e-mail", Arguments: json.RawMessage(
			`{"account":"1000001","update_mask":["customerService"],"customer_service":{"email":"help@example.invalid"}}`)}},
	}
	businessIdentitiesGet = capability.Descriptor{
		ID: businessIdentitiesGetID, Version: 1, Title: "Get the business identity of a Merchant Center account",
		Description: "Read the business identity of a Merchant Center account: the declarations that the business is " +
			"black-owned, latino-owned, a small business, veteran-owned, or women-owned, and the consent to use them " +
			"for promotions. Google offers the owner declarations only to businesses in the US. An account outside " +
			"the targets of the connection is refused without a request. " + businessUntrusted,
		Tags: []string{"google", "merchant", "business-profile"}, Risk: readRisk, Provider: Provider,
		InputSchema:  inputSchema(nil, member("account", accountArgSchema)),
		OutputSchema: json.RawMessage(businessIdentitySchema),
		Arguments:    []capability.Argument{accountArgument},
		Fields:       []capability.Field{{Name: "businessIdentity", Description: businessIdentityFields}},
		Examples:     []capability.Example{{Description: "Business identity of one account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
	}
	businessIdentitiesUpdate = capability.Descriptor{
		ID: businessIdentitiesUpdateID, Version: 1, Title: "Update the business identity of a Merchant Center account",
		Description: "Change the business identity declarations or the promotions consent of a Merchant Center account. " +
			"The body is built from the structured arguments and only the fields in update_mask are sent. " +
			"Google offers the owner declarations only to businesses in the US and not to marketplaces. The consent " +
			"decides whether Google may use the declarations in promotions, so a change can become public. " +
			businessAdminNote + " " + businessChangeNote + " The tool is offered only when the connection lists it in " +
			"tools, and every call needs a confirmation that shows the account and the fields. " + businessUntrusted,
		Tags: []string{"google", "merchant", "business-profile"}, Risk: businessChangeRisk(), Provider: Provider,
		RequiresToolAllowList: true,
		InputSchema: inputSchema([]string{"update_mask"}, member("account", accountArgSchema),
			member("update_mask", businessMaskSchema(businessIdentityPaths)),
			member("promotions_consent", businessEnum(businessConsents)),
			member("black_owned", businessEnum(businessDeclarations)), member("latino_owned", businessEnum(businessDeclarations)),
			member("small_business", businessEnum(businessDeclarations)),
			member("veteran_owned", businessEnum(businessDeclarations)), member("women_owned", businessEnum(businessDeclarations))),
		OutputSchema: json.RawMessage(businessIdentityChangeSchema),
		Arguments: []capability.Argument{accountArgument, businessMaskArgument,
			{Name: "promotions_consent", Description: "PROMOTIONS_CONSENT_GIVEN or PROMOTIONS_CONSENT_DENIED; for update_mask promotionsConsent"},
			{Name: "black_owned", Description: "SELF_IDENTIFIES_AS or DOES_NOT_SELF_IDENTIFY_AS; for update_mask blackOwned"},
			{Name: "latino_owned", Description: "SELF_IDENTIFIES_AS or DOES_NOT_SELF_IDENTIFY_AS; for update_mask latinoOwned"},
			{Name: "small_business", Description: "SELF_IDENTIFIES_AS or DOES_NOT_SELF_IDENTIFY_AS; for update_mask smallBusiness"},
			{Name: "veteran_owned", Description: "SELF_IDENTIFIES_AS or DOES_NOT_SELF_IDENTIFY_AS; for update_mask veteranOwned"},
			{Name: "women_owned", Description: "SELF_IDENTIFIES_AS or DOES_NOT_SELF_IDENTIFY_AS; for update_mask womenOwned"}},
		Fields: []capability.Field{
			{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
			{Name: "businessIdentity", Description: "The business identity as Google returned it, with " + businessIdentityFields},
		},
		Examples: []capability.Example{{Description: "Deny the promotions consent", Arguments: json.RawMessage(
			`{"account":"1000001","update_mask":["promotionsConsent"],"promotions_consent":"PROMOTIONS_CONSENT_DENIED"}`)}},
	}
)

// businessTexts keeps at most maxListItems non-empty texts of a list, each cut to the length of a name.
type businessTexts []string

func (l *businessTexts) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		*l = nil
		return nil
	}
	var out []string
	for _, item := range raw {
		if len(out) == maxListItems {
			break
		}
		if text := clipJSON(item, maxNameRunes); text != "" {
			out = append(out, text)
		}
	}
	*l = out
	return nil
}

// businessInt is an integer member; any other value is dropped.
type businessInt int

func (n *businessInt) UnmarshalJSON(b []byte) error {
	var value int32
	if json.Unmarshal(b, &value) != nil {
		value = 0
	}
	*n = businessInt(value)
	return nil
}

type businessShortCode struct {
	RegionCode idText `json:"regionCode,omitempty"`
	Number     idText `json:"number,omitempty"`
}

type businessPhone struct {
	ShortCode  *businessShortCode `json:"shortCode,omitempty"`
	Extension  idText             `json:"extension,omitempty"`
	E164Number idText             `json:"e164Number,omitempty"`
}

type businessAddress struct {
	RegionCode         idText        `json:"regionCode,omitempty"`
	Recipients         businessTexts `json:"recipients,omitempty"`
	LanguageCode       idText        `json:"languageCode,omitempty"`
	Locality           nameText      `json:"locality,omitempty"`
	AddressLines       businessTexts `json:"addressLines,omitempty"`
	Sublocality        nameText      `json:"sublocality,omitempty"`
	Organization       nameText      `json:"organization,omitempty"`
	PostalCode         idText        `json:"postalCode,omitempty"`
	SortingCode        idText        `json:"sortingCode,omitempty"`
	AdministrativeArea nameText      `json:"administrativeArea,omitempty"`
	Revision           businessInt   `json:"revision,omitempty"`
}

type businessCustomerService struct {
	URI   urlText        `json:"uri,omitempty"`
	Email nameText       `json:"email,omitempty"`
	Phone *businessPhone `json:"phone,omitempty"`
}

// businessInfo is the BusinessInfo resource. Members the discovery document does not list are never held.
type businessInfo struct {
	Name                             idText                   `json:"name,omitempty"`
	Phone                            *businessPhone           `json:"phone,omitempty"`
	PhoneVerificationState           idText                   `json:"phoneVerificationState,omitempty"`
	CustomerService                  *businessCustomerService `json:"customerService,omitempty"`
	Address                          *businessAddress         `json:"address,omitempty"`
	KoreanBusinessRegistrationNumber idText                   `json:"koreanBusinessRegistrationNumber,omitempty"`
}

type businessAttribute struct {
	IdentityDeclaration idText `json:"identityDeclaration,omitempty"`
}

// businessIdentity is the BusinessIdentity resource.
type businessIdentity struct {
	Name              idText             `json:"name,omitempty"`
	PromotionsConsent idText             `json:"promotionsConsent,omitempty"`
	BlackOwned        *businessAttribute `json:"blackOwned,omitempty"`
	LatinoOwned       *businessAttribute `json:"latinoOwned,omitempty"`
	SmallBusiness     *businessAttribute `json:"smallBusiness,omitempty"`
	VeteranOwned      *businessAttribute `json:"veteranOwned,omitempty"`
	WomenOwned        *businessAttribute `json:"womenOwned,omitempty"`
}

type businessInfoChangeResult struct {
	Accepted     bool         `json:"accepted"`
	BusinessInfo businessInfo `json:"businessInfo"`
}

type businessIdentityChangeResult struct {
	Accepted         bool             `json:"accepted"`
	BusinessIdentity businessIdentity `json:"businessIdentity"`
}

// Structured arguments of the writes. The same types build the request body, so nothing but their members can
// reach Google.
type businessPhoneInput struct {
	E164Number string `json:"e164Number,omitempty"`
	Extension  string `json:"extension,omitempty"`
	ShortCode  *struct {
		RegionCode string `json:"regionCode"`
		Number     string `json:"number"`
	} `json:"shortCode,omitempty"`
}

type businessAddressInput struct {
	RegionCode         string   `json:"regionCode"`
	AddressLines       []string `json:"addressLines,omitempty"`
	PostalCode         string   `json:"postalCode,omitempty"`
	AdministrativeArea string   `json:"administrativeArea,omitempty"`
	Locality           string   `json:"locality,omitempty"`
}

type businessServiceInput struct {
	URI   string              `json:"uri,omitempty"`
	Email string              `json:"email,omitempty"`
	Phone *businessPhoneInput `json:"phone,omitempty"`
}

type businessInfoArguments struct {
	Account         string                `json:"account"`
	UpdateMask      []string              `json:"update_mask"`
	Address         *businessAddressInput `json:"address"`
	CustomerService *businessServiceInput `json:"customer_service"`
	Korean          *string               `json:"korean_business_registration_number"`
}

type businessIdentityArguments struct {
	Account           string   `json:"account"`
	UpdateMask        []string `json:"update_mask"`
	PromotionsConsent *string  `json:"promotions_consent"`
	BlackOwned        *string  `json:"black_owned"`
	LatinoOwned       *string  `json:"latino_owned"`
	SmallBusiness     *string  `json:"small_business"`
	VeteranOwned      *string  `json:"veteran_owned"`
	WomenOwned        *string  `json:"women_owned"`
}

func businessDecode(raw json.RawMessage, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(into) != nil {
		return invalidArgument("the arguments hold a member or a value this tool does not take")
	}
	return nil
}

// businessText refuses an empty text, one over limit runes, and one with a control character.
func businessText(name, value string, limit int) error {
	if value == "" || utf8.RuneCountInString(value) > limit || !utf8.ValidString(value) ||
		strings.ContainsFunc(value, unicode.IsControl) {
		return invalidArgument(name + " must be a text of 1 to " + strconv.Itoa(limit) + " characters without control characters")
	}
	return nil
}

func businessForm(name, value string, form *regexp.Regexp, shape string) error {
	if !form.MatchString(value) {
		return invalidArgument(name + " must be " + shape)
	}
	return nil
}

func businessCheckPhone(name string, p *businessPhoneInput) error {
	hasNumber, hasShort := p.E164Number != "", p.ShortCode != nil
	switch {
	case hasNumber == hasShort:
		return invalidArgument(name + " needs exactly one of e164Number and shortCode")
	case hasNumber:
		if err := businessForm(name+".e164Number", p.E164Number, businessE164Form, "an E.164 number: + and up to 15 digits"); err != nil {
			return err
		}
		if p.Extension != "" {
			return businessForm(name+".extension", p.Extension, businessExtensionForm, "1 to 40 digits")
		}
		return nil
	case p.Extension != "":
		return invalidArgument(name + ".extension goes only with e164Number")
	}
	if err := businessForm(name+".shortCode.regionCode", p.ShortCode.RegionCode, businessRegionForm, "two capital letters"); err != nil {
		return err
	}
	return businessForm(name+".shortCode.number", p.ShortCode.Number, businessShortForm, "1 to 8 digits")
}

func businessCheckAddress(a *businessAddressInput) error {
	if err := businessForm("address.regionCode", a.RegionCode, businessRegionForm, "a CLDR region code of two capital letters"); err != nil {
		return err
	}
	if len(a.AddressLines) > businessMaxLines {
		return invalidArgument("address.addressLines takes at most 5 lines")
	}
	for _, line := range a.AddressLines {
		if err := businessText("address.addressLines", line, businessMaxLine); err != nil {
			return err
		}
	}
	for _, part := range []struct {
		name, value string
		limit       int
	}{{"address.postalCode", a.PostalCode, businessMaxPostal}, {"address.administrativeArea", a.AdministrativeArea, businessMaxPart},
		{"address.locality", a.Locality, businessMaxPart}} {
		if part.value != "" {
			if err := businessText(part.name, part.value, part.limit); err != nil {
				return err
			}
		}
	}
	return nil
}

func businessCheckService(s *businessServiceInput) error {
	if s.URI == "" && s.Email == "" && s.Phone == nil {
		return invalidArgument("customer_service needs at least one of uri, email, and phone")
	}
	if s.URI != "" {
		if utf8.RuneCountInString(s.URI) > businessMaxAddress {
			return invalidArgument("customer_service.uri must be at most 2048 characters")
		}
		if err := checkHTTPSAddress("customer_service.uri", s.URI); err != nil {
			return err
		}
	}
	if s.Email != "" {
		parsed, err := mail.ParseAddress(s.Email)
		if err != nil || parsed.Name != "" || parsed.Address != s.Email || len(s.Email) > businessMaxEmail ||
			strings.ContainsFunc(s.Email, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == '<' || r == '>' }) {
			return invalidArgument("customer_service.email must be one plain e-mail address")
		}
	}
	if s.Phone != nil {
		return businessCheckPhone("customer_service.phone", s.Phone)
	}
	return nil
}

// businessMask checks update_mask against the allowlist and against the values given: every listed path needs
// its value and every value needs its path. It returns the mask in allowlist order.
func businessMask(mask, allowed []string, given map[string]bool) (string, error) {
	if len(mask) == 0 {
		return "", invalidArgument("update_mask must list at least one field")
	}
	for i, path := range mask {
		switch {
		case !slices.Contains(allowed, path):
			return "", invalidArgument("update_mask holds a field this tool cannot change; allowed: " + strings.Join(allowed, ", "))
		case slices.Contains(mask[:i], path):
			return "", invalidArgument("update_mask lists a field twice")
		case !given[path]:
			return "", invalidArgument("update_mask lists " + path + " without a value for it")
		}
	}
	for path, has := range given {
		if has && !slices.Contains(mask, path) {
			return "", invalidArgument("a value for " + path + " needs the field in update_mask")
		}
	}
	var ordered []string
	for _, path := range allowed {
		if slices.Contains(mask, path) {
			ordered = append(ordered, path)
		}
	}
	return strings.Join(ordered, ","), nil
}

// businessInfoRequest builds the checked query and body of an update of the business info.
func businessInfoRequest(a businessInfoArguments) (mask string, body []byte, err error) {
	mask, err = businessMask(a.UpdateMask, businessInfoPaths, map[string]bool{"address": a.Address != nil,
		"customerService": a.CustomerService != nil, "koreanBusinessRegistrationNumber": a.Korean != nil})
	if err != nil {
		return "", nil, err
	}
	out := map[string]any{}
	if a.Address != nil {
		if err := businessCheckAddress(a.Address); err != nil {
			return "", nil, err
		}
		out["address"] = a.Address
	}
	if a.CustomerService != nil {
		if err := businessCheckService(a.CustomerService); err != nil {
			return "", nil, err
		}
		out["customerService"] = a.CustomerService
	}
	if a.Korean != nil {
		if err := businessForm("korean_business_registration_number", *a.Korean, businessKoreanForm, "ten digits as XXX-XX-XXXXX"); err != nil {
			return "", nil, err
		}
		out["koreanBusinessRegistrationNumber"] = *a.Korean
	}
	body, _ = json.Marshal(out)
	return mask, body, nil
}

// businessIdentityRequest builds the checked query and body of an update of the business identity.
func businessIdentityRequest(a businessIdentityArguments) (mask string, body []byte, err error) {
	values := map[string]*string{"blackOwned": a.BlackOwned, "latinoOwned": a.LatinoOwned, "promotionsConsent": a.PromotionsConsent,
		"smallBusiness": a.SmallBusiness, "veteranOwned": a.VeteranOwned, "womenOwned": a.WomenOwned}
	given := map[string]bool{}
	for path, value := range values {
		given[path] = value != nil
	}
	if mask, err = businessMask(a.UpdateMask, businessIdentityPaths, given); err != nil {
		return "", nil, err
	}
	out := map[string]any{}
	for _, path := range businessIdentityPaths {
		value := values[path]
		if value == nil {
			continue
		}
		if path == "promotionsConsent" {
			if !slices.Contains(businessConsents, *value) {
				return "", nil, invalidArgument("promotions_consent must be PROMOTIONS_CONSENT_GIVEN or PROMOTIONS_CONSENT_DENIED")
			}
			out[path] = *value
			continue
		}
		if !slices.Contains(businessDeclarations, *value) {
			return "", nil, invalidArgument(path + " must be SELF_IDENTIFIES_AS or DOES_NOT_SELF_IDENTIFY_AS")
		}
		out[path] = map[string]string{"identityDeclaration": *value}
	}
	body, _ = json.Marshal(out)
	return mask, body, nil
}

// businessBind settles the connection and the account before any secret is read.
func businessBind(account string, resolved *config.Resolved) (id string, needsProof bool, err error) {
	bound, err := checkConnection(resolved)
	if err != nil {
		return "", false, err
	}
	return bound.accountOf(account)
}

// businessOpen opens the client and, for an account that only a sub-account proof binds to the targets, reads
// that proof before the detail or the change follows.
func businessOpen(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool string, needsProof bool, account string) (*client, error) {
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, op, tool, account); err != nil {
			return nil, err
		}
	}
	return cl, nil
}

func businessRead(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool, leaf string, out any) error {
	var args struct {
		Account string `json:"account"`
	}
	if err := businessDecode(raw, &args); err != nil {
		return err
	}
	account, needsProof, err := businessBind(args.Account, resolved)
	if err != nil {
		return err
	}
	cl, err := businessOpen(ctx, resolved, secrets, red, op, tool, needsProof, account)
	if err != nil {
		return err
	}
	return cl.get(ctx, op, tool, accountPath(account)+"/"+leaf, nil, out)
}

func handleBusinessInfosGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	var out businessInfo
	if err := businessRead(ctx, resolved, secrets, red, raw, "get business info", businessInfosGetID, "businessInfo", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleBusinessIdentitiesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	var out businessIdentity
	if err := businessRead(ctx, resolved, secrets, red, raw, "get business identity", businessIdentitiesGetID,
		"businessIdentity", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// businessPermission adds the Admin hint to a permission error of a change. The class stays, and Google's own
// message is not read to choose it.
func businessPermission(err error) error {
	var failed *provider.Error
	if errors.As(err, &failed) && failed.Class == provider.ClassPermission {
		failed.Message += "." + businessAdminPermission
	}
	return err
}

// businessPatch sends the one PATCH of a change. The path comes from a checked account ID and a fixed leaf.
func businessPatch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, leaf, account, mask string, body []byte, needsProof bool, out any) error {
	cl, err := businessOpen(ctx, resolved, secrets, red, op, tool, needsProof, account)
	if err != nil {
		return businessPermission(err)
	}
	return businessPermission(cl.send(ctx, op, tool, request{method: http.MethodPatch, path: accountPath(account) + "/" + leaf,
		query: url.Values{"updateMask": {mask}}, body: body, change: true}, out))
}

func handleBusinessInfosUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update business info"
	var args businessInfoArguments
	if err := businessDecode(raw, &args); err != nil {
		return nil, err
	}
	account, needsProof, err := businessBind(args.Account, resolved)
	if err != nil {
		return nil, err
	}
	mask, body, err := businessInfoRequest(args)
	if err != nil {
		return nil, err
	}
	var out businessInfo
	if err := businessPatch(ctx, resolved, secrets, red, op, businessInfosUpdateID, "businessInfo", account, mask, body,
		needsProof, &out); err != nil {
		return nil, err
	}
	return businessInfoChangeResult{Accepted: true, BusinessInfo: out}, nil
}

func handleBusinessIdentitiesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update business identity"
	var args businessIdentityArguments
	if err := businessDecode(raw, &args); err != nil {
		return nil, err
	}
	account, needsProof, err := businessBind(args.Account, resolved)
	if err != nil {
		return nil, err
	}
	mask, body, err := businessIdentityRequest(args)
	if err != nil {
		return nil, err
	}
	var out businessIdentity
	if err := businessPatch(ctx, resolved, secrets, red, op, businessIdentitiesUpdateID, "businessIdentity", account, mask,
		body, needsProof, &out); err != nil {
		return nil, err
	}
	return businessIdentityChangeResult{Accepted: true, BusinessIdentity: out}, nil
}

// businessSummary phrases the confirmation from the account and the allowlisted mask paths alone.
func businessSummary(what string, allowed []string, effect string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a struct {
			Account    string   `json:"account"`
			UpdateMask []string `json:"update_mask"`
		}
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		var fields []string
		for _, path := range allowed {
			if slices.Contains(a.UpdateMask, path) {
				fields = append(fields, path)
			}
		}
		out := "Update " + what + " (" + strings.Join(fields, ", ") + ")"
		if a.Account != "" {
			out += " of account " + a.Account
		} else {
			out += " of the account of this connection"
		}
		return out + "; " + effect + ", and Google processes it afterwards"
	}
}
