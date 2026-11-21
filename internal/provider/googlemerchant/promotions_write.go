package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Bounds of the arguments. Google documents none of them except the five Google product categories; the
// others are Qatlas' own.
const (
	maxPromotionIDRunes          = 128
	maxPromotionTextRune         = 300
	maxPromotionCodeRune         = 100
	maxPromotionCustomAttributes = 100
	maxCustomMembers             = 100
	maxCategories                = 5
	// maxJSONInteger is the largest integer every JSON reader keeps exactly.
	maxJSONInteger = 9007199254740991
)

var (
	redemptionChannels = []string{"IN_STORE", "ONLINE"}
	productApplicables = []string{"ALL_PRODUCTS", "SPECIFIC_PRODUCTS"}
	eventApplicables   = []string{"SITEWIDE", "SPECIFIC_CATEGORIES"}
	offerTypes         = []string{"NO_CODE", "GENERIC_CODE"}
	couponValueTypes   = []string{"MONEY_OFF", "PERCENT_OFF", "BUY_M_GET_N_MONEY_OFF", "BUY_M_GET_N_PERCENT_OFF",
		"BUY_M_GET_MONEY_OFF", "BUY_M_GET_PERCENT_OFF", "FREE_GIFT", "FREE_GIFT_WITH_VALUE", "FREE_GIFT_WITH_ITEM_ID",
		"FREE_SHIPPING_STANDARD", "FREE_SHIPPING_OVERNIGHT", "FREE_SHIPPING_TWO_DAY", "MONEY_OFF_RANGE",
		"PERCENT_OFF_RANGE"}
	storeApplicables = []string{"ALL_STORES", "SPECIFIC_STORES"}
	restrictions     = []string{"SUBSCRIBE", "FIRST_ORDER", "SIGNUP_FOR_EMAIL", "SIGNUP_FOR_TEXT", "CUSTOM",
		"FORMS_OF_PAYMENT"}
	audiences = []string{"NEW_CUSTOMERS", "LOCATION"}
)

// wholeNumber is an int64 that Google takes and returns as a string; the argument is a plain integer.
type wholeNumber int64

func (n wholeNumber) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(strconv.FormatInt(int64(n), 10))), nil
}

type promotionPriceInput struct {
	AmountMicros *wholeNumber `json:"amountMicros"`
	CurrencyCode string       `json:"currencyCode"`
}

type promotionIntervalInput struct {
	StartTime string `json:"startTime,omitempty"`
	EndTime   string `json:"endTime,omitempty"`
}

// promotionAttributes holds the 41 attributes of the promotions v1 discovery document under their own names.
type promotionAttributes struct {
	ProductApplicability         string                  `json:"productApplicability,omitempty"`
	EventApplicability           string                  `json:"eventApplicability,omitempty"`
	OfferType                    string                  `json:"offerType,omitempty"`
	GenericRedemptionCode        string                  `json:"genericRedemptionCode,omitempty"`
	LongTitle                    string                  `json:"longTitle,omitempty"`
	CouponValueType              string                  `json:"couponValueType,omitempty"`
	PromotionDestinations        []string                `json:"promotionDestinations,omitempty"`
	ItemIDInclusion              []string                `json:"itemIdInclusion,omitempty"`
	BrandInclusion               []string                `json:"brandInclusion,omitempty"`
	ItemGroupIDInclusion         []string                `json:"itemGroupIdInclusion,omitempty"`
	ProductTypeInclusion         []string                `json:"productTypeInclusion,omitempty"`
	ItemIDExclusion              []string                `json:"itemIdExclusion,omitempty"`
	BrandExclusion               []string                `json:"brandExclusion,omitempty"`
	ItemGroupIDExclusion         []string                `json:"itemGroupIdExclusion,omitempty"`
	ProductTypeExclusion         []string                `json:"productTypeExclusion,omitempty"`
	MinimumPurchaseAmount        *promotionPriceInput    `json:"minimumPurchaseAmount,omitempty"`
	MinimumPurchaseQuantity      *wholeNumber            `json:"minimumPurchaseQuantity,omitempty"`
	LimitQuantity                *wholeNumber            `json:"limitQuantity,omitempty"`
	LimitValue                   *promotionPriceInput    `json:"limitValue,omitempty"`
	PercentOff                   *wholeNumber            `json:"percentOff,omitempty"`
	MoneyOffAmount               *promotionPriceInput    `json:"moneyOffAmount,omitempty"`
	GetThisQuantityDiscounted    *wholeNumber            `json:"getThisQuantityDiscounted,omitempty"`
	FreeGiftValue                *promotionPriceInput    `json:"freeGiftValue,omitempty"`
	FreeGiftDescription          string                  `json:"freeGiftDescription,omitempty"`
	FreeGiftItemID               string                  `json:"freeGiftItemId,omitempty"`
	PromotionEffectiveTimePeriod *promotionIntervalInput `json:"promotionEffectiveTimePeriod,omitempty"`
	PromotionDisplayTimePeriod   *promotionIntervalInput `json:"promotionDisplayTimePeriod,omitempty"`
	StoreApplicability           string                  `json:"storeApplicability,omitempty"`
	StoreCodesInclusion          []string                `json:"storeCodesInclusion,omitempty"`
	StoreCodesExclusion          []string                `json:"storeCodesExclusion,omitempty"`
	PromotionURL                 string                  `json:"promotionUrl,omitempty"`
	RedemptionRestriction        string                  `json:"redemptionRestriction,omitempty"`
	CustomRedemptionRestriction  string                  `json:"customRedemptionRestriction,omitempty"`
	MaxDiscountAmount            *promotionPriceInput    `json:"maxDiscountAmount,omitempty"`
	GoogleProductCategories      []string                `json:"googleProductCategories,omitempty"`
	Audience                     string                  `json:"audience,omitempty"`
	RegionIDInclusion            []string                `json:"regionIdInclusion,omitempty"`
	MinMoneyOffAmount            *promotionPriceInput    `json:"minMoneyOffAmount,omitempty"`
	MaxMoneyOffAmount            *promotionPriceInput    `json:"maxMoneyOffAmount,omitempty"`
	MinPercentOff                *wholeNumber            `json:"minPercentOff,omitempty"`
	MaxPercentOff                *wholeNumber            `json:"maxPercentOff,omitempty"`
}

type customMemberInput struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type customAttributeInput struct {
	Name        string              `json:"name"`
	Value       string              `json:"value,omitempty"`
	GroupValues []customMemberInput `json:"groupValues,omitempty"`
}

type promotionArguments struct {
	Account           string                 `json:"account"`
	DataSource        string                 `json:"data_source"`
	PromotionID       string                 `json:"promotion_id"`
	ContentLanguage   string                 `json:"content_language"`
	TargetCountry     string                 `json:"target_country"`
	RedemptionChannel []string               `json:"redemption_channel"`
	VersionNumber     *wholeNumber           `json:"version_number"`
	Attributes        *promotionAttributes   `json:"attributes"`
	CustomAttributes  []customAttributeInput `json:"custom_attributes"`
}

// promotionBody is the Promotion Google receives. The data source is the one of the call, inside the account
// of the call, so it can never name another account.
type promotionBody struct {
	PromotionID       string                 `json:"promotionId"`
	ContentLanguage   string                 `json:"contentLanguage"`
	TargetCountry     string                 `json:"targetCountry"`
	RedemptionChannel []string               `json:"redemptionChannel"`
	VersionNumber     *wholeNumber           `json:"versionNumber,omitempty"`
	Attributes        *promotionAttributes   `json:"attributes"`
	CustomAttributes  []customAttributeInput `json:"customAttributes,omitempty"`
}

type promotionInsertBody struct {
	Promotion  promotionBody `json:"promotion"`
	DataSource string        `json:"dataSource"`
}

var (
	listOfTexts = `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxPromotionEntries) +
		`,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxPromotionCodeRune) + `}}`
	wholeSchema = `{"type":"integer","minimum":0,"maximum":` + strconv.Itoa(maxJSONInteger) + `}`
	moneySchema = closedSchemaOf([]string{"amountMicros", "currencyCode"}, member("amountMicros", wholeSchema),
		member("currencyCode", `{"type":"string","pattern":"^[A-Z]{3}$"}`))
	periodSchema = closedSchemaOf(nil, member("startTime", `{"type":"string","minLength":20,"maxLength":40}`),
		member("endTime", `{"type":"string","minLength":20,"maxLength":40}`))
	titleSchema = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxPromotionTextRune) + `}`
	codeSchema  = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxPromotionCodeRune) + `}`

	promotionAttributesInput = closedSchemaOf(
		[]string{"offerType", "longTitle", "couponValueType", "promotionDestinations", "promotionEffectiveTimePeriod"},
		member("productApplicability", enumSchema(productApplicables)),
		member("eventApplicability", enumSchema(eventApplicables)),
		member("offerType", enumSchema(offerTypes)), member("genericRedemptionCode", codeSchema),
		member("longTitle", titleSchema), member("couponValueType", enumSchema(couponValueTypes)),
		member("promotionDestinations", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(len(destinationNames))+
			`,"uniqueItems":true,"items":`+enumSchema(destinationNames)+`}`),
		member("itemIdInclusion", listOfTexts), member("brandInclusion", listOfTexts),
		member("itemGroupIdInclusion", listOfTexts), member("productTypeInclusion", listOfTexts),
		member("itemIdExclusion", listOfTexts), member("brandExclusion", listOfTexts),
		member("itemGroupIdExclusion", listOfTexts), member("productTypeExclusion", listOfTexts),
		member("minimumPurchaseAmount", moneySchema), member("minimumPurchaseQuantity", wholeSchema),
		member("limitQuantity", wholeSchema), member("limitValue", moneySchema),
		member("percentOff", wholeSchema), member("moneyOffAmount", moneySchema),
		member("getThisQuantityDiscounted", wholeSchema), member("freeGiftValue", moneySchema),
		member("freeGiftDescription", titleSchema), member("freeGiftItemId", codeSchema),
		member("promotionEffectiveTimePeriod", periodSchema), member("promotionDisplayTimePeriod", periodSchema),
		member("storeApplicability", enumSchema(storeApplicables)),
		member("storeCodesInclusion", listOfTexts), member("storeCodesExclusion", listOfTexts),
		member("promotionUrl", `{"type":"string","minLength":1,"maxLength":`+strconv.Itoa(maxURLRunes)+
			`,"x-form":"absolute https address without user information"}`),
		member("redemptionRestriction", enumSchema(restrictions)),
		member("customRedemptionRestriction", titleSchema), member("maxDiscountAmount", moneySchema),
		member("googleProductCategories", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxCategories)+
			`,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":`+strconv.Itoa(maxPromotionCodeRune)+`}}`),
		member("audience", enumSchema(audiences)), member("regionIdInclusion", listOfTexts),
		member("minMoneyOffAmount", moneySchema), member("maxMoneyOffAmount", moneySchema),
		member("minPercentOff", wholeSchema), member("maxPercentOff", wholeSchema))

	promotionCustomAttributesInput = `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxPromotionCustomAttributes) + `,"items":` +
		closedSchemaOf([]string{"name"}, member("name", codeSchema), member("value", titleSchema),
			member("groupValues", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxCustomMembers)+`,"items":`+
				closedSchemaOf([]string{"name", "value"}, member("name", codeSchema), member("value", titleSchema))+`}`)) + `}`

	promotionIDSchema = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxPromotionIDRunes) + `}`
)

const promotionInsertNote = "Insert creates the promotion, and when a promotion with the same promotion_id, " +
	"content_language, and target_country exists, it REPLACES it: Qatlas sends the whole promotion " +
	"and Google documents no merge. A promotion is shown publicly on Google once it is approved and its display period has begun. " +
	"Google documents no way to delete a promotion, so Qatlas cannot remove one: it ends with its time period, or " +
	"a later insert replaces it. There is no dry run. "

var promotionsInsert = capability.Descriptor{
	ID: promotionsInsertID, Version: 1, Title: "Insert a Merchant Center promotion",
	Description: promotionInsertNote + "data_source is the ID of a data source of the same account. redemption_channel lists ONLINE and IN_STORE, at least " +
		"one. attributes takes the documented attributes under their Google names; offerType, longTitle, " +
		"couponValueType, promotionDestinations, and promotionEffectiveTimePeriod are required, and exactly one of " +
		"productApplicability and eventApplicability must be set. money amounts are amountMicros (one millionth of " +
		"the currency unit) with an ISO 4217 currencyCode, times are RFC 3339 date-times, promotionUrl must be an " +
		"https address without user information. Qatlas checks the rules Google documents: customRedemptionRestriction " +
		"with CUSTOM, regionIdInclusion with the LOCATION audience, at most 5 googleProductCategories, store codes " +
		"not with ALL_STORES, and a lower or upper bound for the range coupon types. custom_attributes holds " +
		"merchant-provided name and value pairs, or a name with groupValues; names or values that carry an address " +
		"are refused, because only promotionUrl may direct customers, and it is checked. List lengths and text " +
		"lengths are capped by Qatlas, since Google documents none. Google refuses the call when the role " +
		"of the user in Merchant Center does not allow it. " + promotionsDelayNote +
		"The confirmation shows the account, the promotion ID, country, language, and data source. " + promotionsUntrusted,
	Tags: []string{"google", "merchant", "promotions"}, Risk: regionChangeRisk(capability.EffectCreate,
		capability.IdempotencyIdempotent), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"data_source", "promotion_id", "content_language", "target_country",
		"redemption_channel", "attributes"}, member("account", accountArgSchema),
		member("data_source", dataSourceIDSchema), member("promotion_id", promotionIDSchema),
		member("content_language", `{"type":"string","pattern":"`+sourceLanguagePattern+`"}`),
		member("target_country", `{"type":"string","pattern":"`+countryPattern+`"}`),
		member("redemption_channel", `{"type":"array","minItems":1,"maxItems":2,"uniqueItems":true,"items":`+
			enumSchema(redemptionChannels)+`}`),
		member("version_number", wholeSchema), member("attributes", promotionAttributesInput),
		member("custom_attributes", promotionCustomAttributesInput)),
	OutputSchema: json.RawMessage(promotionAcceptedSchema),
	Arguments: []capability.Argument{accountArgument, dataSourceIDArgument,
		{Name: "promotion_id", Required: true, Description: "Your ID of the promotion, 1 to 128 characters"},
		{Name: "content_language", Required: true, Description: "Two-letter lower case ISO 639-1 language code"},
		{Name: "target_country", Required: true, Description: "Two-letter upper case CLDR territory code"},
		{Name: "redemption_channel", Required: true, Description: "ONLINE, IN_STORE, or both"},
		{Name: "version_number", Description: "Version of the promotion; Google refuses the insert when it is lower " +
			"than the current version"},
		{Name: "attributes", Required: true, Description: "The attributes of the promotion under their Google names"},
		{Name: "custom_attributes", Description: "Merchant-provided attributes: name with value, or name with groupValues"}},
	Fields: []capability.Field{
		{Name: "accepted", Description: "true when Google accepted the request; nothing is said about processing"},
		{Name: "promotion", Description: "The promotion as Google returned it, with " + promotionFieldsDescription},
	},
	Examples: []capability.Example{{Description: "An online promotion of ten percent off the whole assortment",
		Arguments: json.RawMessage(`{"account":"1000001","data_source":"2000002","promotion_id":"spring-sale",` +
			`"content_language":"en","target_country":"US","redemption_channel":["ONLINE"],"attributes":{` +
			`"productApplicability":"ALL_PRODUCTS","offerType":"NO_CODE","longTitle":"10% off everything",` +
			`"couponValueType":"PERCENT_OFF","percentOff":10,"promotionDestinations":["FREE_LISTINGS"],` +
			`"promotionEffectiveTimePeriod":{"startTime":"2030-03-01T00:00:00Z","endTime":"2030-03-31T00:00:00Z"}}}`)}},
}

func in(value string, allowed []string) bool { return slices.Contains(allowed, value) }

func badMember(name string) error { return invalidArgument(name + " is not valid for this tool") }

func checkText(name, value string, max int) error {
	if !validText(value, max) {
		return invalidArgument(name + " must be 1 to " + strconv.Itoa(max) + " characters without control characters")
	}
	return nil
}

func checkTexts(name string, values []string, max int) error {
	if len(values) > max {
		return invalidArgument(name + " takes at most " + strconv.Itoa(max) + " entries")
	}
	for _, v := range values {
		if err := checkText(name, v, maxPromotionCodeRune); err != nil {
			return err
		}
	}
	return nil
}

func (p *promotionPriceInput) check(name string) error {
	if p == nil {
		return nil
	}
	if p.AmountMicros == nil || *p.AmountMicros < 0 || *p.AmountMicros > maxJSONInteger ||
		!currencyForm.MatchString(p.CurrencyCode) {
		return invalidArgument(name + " needs amountMicros of at least 0 and a three-letter upper case currencyCode")
	}
	return nil
}

func (i *promotionIntervalInput) check(name string) error {
	if i == nil {
		return nil
	}
	var start, end time.Time
	var err error
	if i.StartTime == "" && i.EndTime == "" {
		return invalidArgument(name + " needs startTime or endTime")
	}
	if i.StartTime != "" {
		if start, err = time.Parse(time.RFC3339, i.StartTime); err != nil {
			return invalidArgument(name + " startTime must be an RFC 3339 date-time")
		}
	}
	if i.EndTime != "" {
		if end, err = time.Parse(time.RFC3339, i.EndTime); err != nil {
			return invalidArgument(name + " endTime must be an RFC 3339 date-time")
		}
	}
	if !start.IsZero() && !end.IsZero() && !end.After(start) {
		return invalidArgument(name + " endTime must be after startTime")
	}
	return nil
}

func isSet[T any](p *T) bool { return p != nil }

// check enforces the required members and the rules the discovery document states. Everything else is left
// to Google.
func (a *promotionAttributes) check() error {
	enums := []struct {
		name, value string
		allowed     []string
	}{
		{"productApplicability", a.ProductApplicability, productApplicables},
		{"eventApplicability", a.EventApplicability, eventApplicables},
		{"offerType", a.OfferType, offerTypes}, {"couponValueType", a.CouponValueType, couponValueTypes},
		{"storeApplicability", a.StoreApplicability, storeApplicables},
		{"redemptionRestriction", a.RedemptionRestriction, restrictions}, {"audience", a.Audience, audiences},
	}
	for _, e := range enums {
		if e.value != "" && !in(e.value, e.allowed) {
			return badMember(e.name)
		}
	}
	if (a.ProductApplicability == "") == (a.EventApplicability == "") {
		return invalidArgument("set exactly one of productApplicability and eventApplicability")
	}
	if a.OfferType == "" || a.CouponValueType == "" || len(a.PromotionDestinations) == 0 ||
		a.PromotionEffectiveTimePeriod == nil {
		return invalidArgument("attributes need offerType, couponValueType, promotionDestinations, and " +
			"promotionEffectiveTimePeriod")
	}
	if err := checkText("longTitle", a.LongTitle, maxPromotionTextRune); err != nil {
		return err
	}
	for _, d := range a.PromotionDestinations {
		if !in(d, destinationNames) {
			return badMember("promotionDestinations")
		}
	}
	if len(a.PromotionDestinations) > len(destinationNames) {
		return badMember("promotionDestinations")
	}
	for _, t := range []struct {
		name, value string
		max         int
	}{{"genericRedemptionCode", a.GenericRedemptionCode, maxPromotionCodeRune},
		{"freeGiftItemId", a.FreeGiftItemID, maxPromotionCodeRune},
		{"freeGiftDescription", a.FreeGiftDescription, maxPromotionTextRune},
		{"customRedemptionRestriction", a.CustomRedemptionRestriction, maxPromotionTextRune}} {
		if t.value != "" {
			if err := checkText(t.name, t.value, t.max); err != nil {
				return err
			}
		}
	}
	for _, l := range []struct {
		name   string
		values []string
		max    int
	}{{"itemIdInclusion", a.ItemIDInclusion, maxPromotionEntries}, {"brandInclusion", a.BrandInclusion, maxPromotionEntries},
		{"itemGroupIdInclusion", a.ItemGroupIDInclusion, maxPromotionEntries},
		{"productTypeInclusion", a.ProductTypeInclusion, maxPromotionEntries},
		{"itemIdExclusion", a.ItemIDExclusion, maxPromotionEntries}, {"brandExclusion", a.BrandExclusion, maxPromotionEntries},
		{"itemGroupIdExclusion", a.ItemGroupIDExclusion, maxPromotionEntries},
		{"productTypeExclusion", a.ProductTypeExclusion, maxPromotionEntries},
		{"storeCodesInclusion", a.StoreCodesInclusion, maxPromotionEntries},
		{"storeCodesExclusion", a.StoreCodesExclusion, maxPromotionEntries},
		{"regionIdInclusion", a.RegionIDInclusion, maxPromotionEntries},
		{"googleProductCategories", a.GoogleProductCategories, maxCategories}} {
		if err := checkTexts(l.name, l.values, l.max); err != nil {
			return err
		}
	}
	for _, p := range []struct {
		name  string
		price *promotionPriceInput
	}{{"minimumPurchaseAmount", a.MinimumPurchaseAmount}, {"limitValue", a.LimitValue},
		{"moneyOffAmount", a.MoneyOffAmount}, {"freeGiftValue", a.FreeGiftValue},
		{"maxDiscountAmount", a.MaxDiscountAmount}, {"minMoneyOffAmount", a.MinMoneyOffAmount},
		{"maxMoneyOffAmount", a.MaxMoneyOffAmount}} {
		if err := p.price.check(p.name); err != nil {
			return err
		}
	}
	for _, n := range []struct {
		name  string
		value *wholeNumber
	}{{"minimumPurchaseQuantity", a.MinimumPurchaseQuantity}, {"limitQuantity", a.LimitQuantity},
		{"percentOff", a.PercentOff}, {"getThisQuantityDiscounted", a.GetThisQuantityDiscounted},
		{"minPercentOff", a.MinPercentOff}, {"maxPercentOff", a.MaxPercentOff}} {
		if n.value != nil && (*n.value < 0 || *n.value > maxJSONInteger) {
			return badMember(n.name)
		}
	}
	if err := a.PromotionEffectiveTimePeriod.check("promotionEffectiveTimePeriod"); err != nil {
		return err
	}
	if err := a.PromotionDisplayTimePeriod.check("promotionDisplayTimePeriod"); err != nil {
		return err
	}
	if a.PromotionURL != "" {
		if utf8.RuneCountInString(a.PromotionURL) > maxURLRunes {
			return invalidArgument("promotionUrl is longer than " + strconv.Itoa(maxURLRunes) + " characters")
		}
		if err := checkHTTPSAddress("promotionUrl", a.PromotionURL); err != nil {
			return err
		}
	}
	return a.checkRules()
}

// checkRules holds the cross-member rules the discovery document states.
func (a *promotionAttributes) checkRules() error {
	switch {
	case a.RedemptionRestriction == "CUSTOM" && a.CustomRedemptionRestriction == "":
		return invalidArgument("customRedemptionRestriction is required when redemptionRestriction is CUSTOM")
	case a.Audience == "LOCATION" && len(a.RegionIDInclusion) == 0:
		return invalidArgument("regionIdInclusion is required when audience is LOCATION")
	case a.StoreApplicability == "ALL_STORES" && (len(a.StoreCodesInclusion) > 0 || len(a.StoreCodesExclusion) > 0):
		return invalidArgument("store codes cannot be combined with storeApplicability ALL_STORES")
	case a.CouponValueType == "MONEY_OFF_RANGE" && !isSet(a.MinMoneyOffAmount) && !isSet(a.MaxMoneyOffAmount):
		return invalidArgument("minMoneyOffAmount or maxMoneyOffAmount is required for MONEY_OFF_RANGE")
	case a.CouponValueType == "PERCENT_OFF_RANGE" && !isSet(a.MinPercentOff) && !isSet(a.MaxPercentOff):
		return invalidArgument("minPercentOff or maxPercentOff is required for PERCENT_OFF_RANGE")
	}
	return nil
}

// directsCustomers reports whether a custom attribute could carry an address that directs customers, which
// only the checked promotionUrl may do.
func directsCustomers(name, value string) bool {
	lowerName := strings.ToLower(name)
	lowerValue := strings.ToLower(strings.TrimSpace(value))
	for _, word := range []string{"url", "uri", "link", "href"} {
		if strings.Contains(lowerName, word) {
			return true
		}
	}
	return strings.Contains(lowerValue, "://") || strings.HasPrefix(lowerValue, "www.") || strings.HasPrefix(lowerValue, "//")
}

func (a promotionArguments) check() error {
	if err := checkText("promotion_id", a.PromotionID, maxPromotionIDRunes); err != nil {
		return err
	}
	if !sourceLanguageForm.MatchString(a.ContentLanguage) {
		return invalidArgument("content_language must be a two-letter lower case language code")
	}
	if !countryForm.MatchString(a.TargetCountry) {
		return invalidArgument("target_country must be a two-letter upper case territory code")
	}
	if len(a.RedemptionChannel) == 0 || len(a.RedemptionChannel) > len(redemptionChannels) {
		return invalidArgument("redemption_channel needs ONLINE, IN_STORE, or both")
	}
	for i, c := range a.RedemptionChannel {
		if !in(c, redemptionChannels) || slices.Contains(a.RedemptionChannel[:i], c) {
			return invalidArgument("redemption_channel needs ONLINE, IN_STORE, or both, each at most once")
		}
	}
	if a.VersionNumber != nil && (*a.VersionNumber < 0 || *a.VersionNumber > maxJSONInteger) {
		return badMember("version_number")
	}
	if a.Attributes == nil {
		return invalidArgument("attributes is required")
	}
	if err := a.Attributes.check(); err != nil {
		return err
	}
	if len(a.CustomAttributes) > maxPromotionCustomAttributes {
		return invalidArgument("custom_attributes takes at most " + strconv.Itoa(maxPromotionCustomAttributes) + " entries")
	}
	for _, c := range a.CustomAttributes {
		if err := checkText("custom attribute name", c.Name, maxPromotionCodeRune); err != nil {
			return err
		}
		if (c.Value == "") == (len(c.GroupValues) == 0) {
			return invalidArgument("a custom attribute has either a value or groupValues")
		}
		if c.Value != "" {
			if err := checkText("custom attribute value", c.Value, maxPromotionTextRune); err != nil {
				return err
			}
		}
		if len(c.GroupValues) > maxCustomMembers {
			return invalidArgument("groupValues takes at most " + strconv.Itoa(maxCustomMembers) + " entries")
		}
		refused := directsCustomers(c.Name, c.Value)
		for _, g := range c.GroupValues {
			if err := checkText("custom attribute name", g.Name, maxPromotionCodeRune); err != nil {
				return err
			}
			if err := checkText("custom attribute value", g.Value, maxPromotionTextRune); err != nil {
				return err
			}
			refused = refused || directsCustomers(g.Name, g.Value)
		}
		if refused {
			return invalidArgument("custom attributes must not carry an address: only promotionUrl may direct customers")
		}
	}
	return nil
}

func handlePromotionsInsert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "insert promotion"
	var args promotionArguments
	bound, err := bind(op, resolved, raw, &args)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := bound.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	if !validID(args.DataSource) {
		return nil, invalidArgument("data_source must be a decimal data source ID")
	}
	if err := args.check(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(promotionInsertBody{
		Promotion: promotionBody{PromotionID: args.PromotionID, ContentLanguage: args.ContentLanguage,
			TargetCountry: args.TargetCountry, RedemptionChannel: args.RedemptionChannel,
			VersionNumber: args.VersionNumber, Attributes: args.Attributes, CustomAttributes: args.CustomAttributes},
		DataSource: "accounts/" + id + "/dataSources/" + args.DataSource,
	})
	if err != nil {
		return nil, provider.Fail(op, "the request could not be built")
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, promotionsInsertID, needsProof, id); err != nil {
		return nil, err
	}
	var out promotion
	r := request{method: http.MethodPost, path: promotionsPath(id) + ":insert", body: body, change: true}
	if err := cl.send(ctx, op, promotionsInsertID, r, &out); err != nil {
		return nil, err
	}
	return promotionAccepted{Accepted: true, Promotion: out}, nil
}

// promotionAccepted is the answer of the insert tool: Google accepted the request. It says nothing about the
// processing that follows.
type promotionAccepted struct {
	Accepted  bool      `json:"accepted"`
	Promotion promotion `json:"promotion"`
}

// promotionSummary phrases the confirmation from the validated arguments alone.
func promotionSummary(raw json.RawMessage) string {
	var a promotionArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Insert promotion " + strconv.Quote(clipText(a.PromotionID, 60)) + " (" + clipText(a.TargetCountry, 4) + ", " +
		clipText(a.ContentLanguage, 4) + ")"
	if a.Account != "" {
		out += " in account " + a.Account
	}
	if a.DataSource != "" {
		out += " with data source " + clipText(a.DataSource, 20)
	}
	if a.Attributes != nil && a.Attributes.LongTitle != "" {
		out += ", titled " + strconv.Quote(clipText(a.Attributes.LongTitle, 80))
	}
	return out + "; a promotion with the same ID, country, and language is REPLACED, the promotion shows publicly " +
		"on Google once approved, it cannot be deleted through Qatlas, and Google processes it afterwards"
}
