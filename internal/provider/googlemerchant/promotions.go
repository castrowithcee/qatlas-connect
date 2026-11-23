package googlemerchant

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	promotionsListID   = Provider + ".promotions.list"
	promotionsGetID    = Provider + ".promotions.get"
	promotionsInsertID = Provider + ".promotions.insert"
)

// promotionsAPI is the path prefix of the promotions sub-API.
const promotionsAPI = "promotions/v1/"

// Bounds of the list tool. Google returns as many promotions as it can when no page size is sent and
// coerces a size above 250 to 250, so Qatlas always sends one.
const (
	promotionsPageSize    = 100
	promotionsMaxPageSize = 250
	// maxPromotionEntries bounds every list inside one promotion, in a result and in an insert.
	maxPromotionEntries = 300
)

// promotionNameForm is the last segment of a promotion name. Google documents no form for it, so only
// characters that need no escaping in a path are accepted; a promotion with another spelling cannot be read
// here.
var promotionNameForm = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,199}$`)

const promotionNamePattern = `^[A-Za-z0-9][A-Za-z0-9._~-]{0,199}$`

// entries is a list of short texts that keeps at most maxPromotionEntries entries, each cut to the length of an ID.
type entries []idText

func (l *entries) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		*l = nil
		return nil
	}
	out := make(entries, 0, min(len(raw), maxPromotionEntries))
	for _, item := range raw {
		if len(out) == maxPromotionEntries {
			break
		}
		var text idText
		if json.Unmarshal(item, &text) == nil && text != "" {
			out = append(out, text)
		}
	}
	*l = out
	return nil
}

// The members are those of the promotions v1 discovery document. Text types cut themselves while they are
// decoded, int64 values arrive as strings, and a member the types do not list is never held.

type promotionPrice struct {
	AmountMicros idText `json:"amountMicros,omitempty"`
	CurrencyCode idText `json:"currencyCode,omitempty"`
}

type promotionInterval struct {
	StartTime idText `json:"startTime,omitempty"`
	EndTime   idText `json:"endTime,omitempty"`
}

type promotionAttributesView struct {
	ProductApplicability         idText             `json:"productApplicability,omitempty"`
	EventApplicability           idText             `json:"eventApplicability,omitempty"`
	OfferType                    idText             `json:"offerType,omitempty"`
	GenericRedemptionCode        idText             `json:"genericRedemptionCode,omitempty"`
	LongTitle                    nameText           `json:"longTitle,omitempty"`
	CouponValueType              idText             `json:"couponValueType,omitempty"`
	PromotionDestinations        entries            `json:"promotionDestinations,omitempty"`
	ItemIDInclusion              entries            `json:"itemIdInclusion,omitempty"`
	BrandInclusion               entries            `json:"brandInclusion,omitempty"`
	ItemGroupIDInclusion         entries            `json:"itemGroupIdInclusion,omitempty"`
	ProductTypeInclusion         entries            `json:"productTypeInclusion,omitempty"`
	ItemIDExclusion              entries            `json:"itemIdExclusion,omitempty"`
	BrandExclusion               entries            `json:"brandExclusion,omitempty"`
	ItemGroupIDExclusion         entries            `json:"itemGroupIdExclusion,omitempty"`
	ProductTypeExclusion         entries            `json:"productTypeExclusion,omitempty"`
	MinimumPurchaseAmount        *promotionPrice    `json:"minimumPurchaseAmount,omitempty"`
	MinimumPurchaseQuantity      idText             `json:"minimumPurchaseQuantity,omitempty"`
	LimitQuantity                idText             `json:"limitQuantity,omitempty"`
	LimitValue                   *promotionPrice    `json:"limitValue,omitempty"`
	PercentOff                   idText             `json:"percentOff,omitempty"`
	MoneyOffAmount               *promotionPrice    `json:"moneyOffAmount,omitempty"`
	GetThisQuantityDiscounted    idText             `json:"getThisQuantityDiscounted,omitempty"`
	FreeGiftValue                *promotionPrice    `json:"freeGiftValue,omitempty"`
	FreeGiftDescription          nameText           `json:"freeGiftDescription,omitempty"`
	FreeGiftItemID               idText             `json:"freeGiftItemId,omitempty"`
	PromotionEffectiveTimePeriod *promotionInterval `json:"promotionEffectiveTimePeriod,omitempty"`
	PromotionDisplayTimePeriod   *promotionInterval `json:"promotionDisplayTimePeriod,omitempty"`
	StoreApplicability           idText             `json:"storeApplicability,omitempty"`
	StoreCodesInclusion          entries            `json:"storeCodesInclusion,omitempty"`
	StoreCodesExclusion          entries            `json:"storeCodesExclusion,omitempty"`
	PromotionURL                 urlText            `json:"promotionUrl,omitempty"`
	RedemptionRestriction        idText             `json:"redemptionRestriction,omitempty"`
	CustomRedemptionRestriction  nameText           `json:"customRedemptionRestriction,omitempty"`
	MaxDiscountAmount            *promotionPrice    `json:"maxDiscountAmount,omitempty"`
	GoogleProductCategories      entries            `json:"googleProductCategories,omitempty"`
	Audience                     idText             `json:"audience,omitempty"`
	RegionIDInclusion            entries            `json:"regionIdInclusion,omitempty"`
	MinMoneyOffAmount            *promotionPrice    `json:"minMoneyOffAmount,omitempty"`
	MaxMoneyOffAmount            *promotionPrice    `json:"maxMoneyOffAmount,omitempty"`
	MinPercentOff                idText             `json:"minPercentOff,omitempty"`
	MaxPercentOff                idText             `json:"maxPercentOff,omitempty"`
}

// customAttribute is a merchant-provided attribute: a value, or a group of sub-attributes one level deep.
type customAttribute struct {
	Name        nameText                      `json:"name,omitempty"`
	Value       nameText                      `json:"value,omitempty"`
	GroupValues capped[customAttributeMember] `json:"groupValues,omitempty"`
}

type customAttributeMember struct {
	Name  nameText `json:"name,omitempty"`
	Value nameText `json:"value,omitempty"`
}

type promotionDestinationStatus struct {
	ReportingContext idText `json:"reportingContext,omitempty"`
	Status           idText `json:"status,omitempty"`
}

type promotionIssue struct {
	Code                idText   `json:"code,omitempty"`
	Severity            idText   `json:"severity,omitempty"`
	Resolution          idText   `json:"resolution,omitempty"`
	Attribute           idText   `json:"attribute,omitempty"`
	ReportingContext    idText   `json:"reportingContext,omitempty"`
	Description         nameText `json:"description,omitempty"`
	Detail              nameText `json:"detail,omitempty"`
	Documentation       urlText  `json:"documentation,omitempty"`
	ApplicableCountries entries  `json:"applicableCountries,omitempty"`
}

type promotionStatus struct {
	DestinationStatuses capped[promotionDestinationStatus] `json:"destinationStatuses,omitempty"`
	ItemLevelIssues     capped[promotionIssue]             `json:"itemLevelIssues,omitempty"`
	CreationDate        idText                             `json:"creationDate,omitempty"`
	LastUpdateDate      idText                             `json:"lastUpdateDate,omitempty"`
}

// promotion is the Promotion resource of the promotions sub-API.
type promotion struct {
	Name              idText                   `json:"name,omitempty"`
	PromotionID       nameText                 `json:"promotionId,omitempty"`
	ContentLanguage   idText                   `json:"contentLanguage,omitempty"`
	TargetCountry     idText                   `json:"targetCountry,omitempty"`
	RedemptionChannel entries                  `json:"redemptionChannel,omitempty"`
	DataSource        idText                   `json:"dataSource,omitempty"`
	Attributes        *promotionAttributesView `json:"attributes,omitempty"`
	CustomAttributes  capped[customAttribute]  `json:"customAttributes,omitempty"`
	PromotionStatus   *promotionStatus         `json:"promotionStatus,omitempty"`
	VersionNumber     idText                   `json:"versionNumber,omitempty"`
}

type promotionsAnswer struct {
	Promotions    capped[promotion] `json:"promotions"`
	NextPageToken string            `json:"nextPageToken"`
}

// promotionsResult is the result of the list tool.
type promotionsResult struct {
	Promotions []promotion `json:"promotions"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

var (
	priceSchema    = objectSchema(member("amountMicros", stringSchema), member("currencyCode", stringSchema))
	intervalSchema = objectSchema(member("startTime", stringSchema), member("endTime", stringSchema))

	promotionAttributesSchema = objectSchema(
		member("productApplicability", stringSchema), member("eventApplicability", stringSchema),
		member("offerType", stringSchema), member("genericRedemptionCode", stringSchema),
		member("longTitle", stringSchema), member("couponValueType", stringSchema),
		member("promotionDestinations", listSchema),
		member("itemIdInclusion", listSchema), member("brandInclusion", listSchema),
		member("itemGroupIdInclusion", listSchema), member("productTypeInclusion", listSchema),
		member("itemIdExclusion", listSchema), member("brandExclusion", listSchema),
		member("itemGroupIdExclusion", listSchema), member("productTypeExclusion", listSchema),
		member("minimumPurchaseAmount", priceSchema), member("minimumPurchaseQuantity", stringSchema),
		member("limitQuantity", stringSchema), member("limitValue", priceSchema),
		member("percentOff", stringSchema), member("moneyOffAmount", priceSchema),
		member("getThisQuantityDiscounted", stringSchema), member("freeGiftValue", priceSchema),
		member("freeGiftDescription", stringSchema), member("freeGiftItemId", stringSchema),
		member("promotionEffectiveTimePeriod", intervalSchema), member("promotionDisplayTimePeriod", intervalSchema),
		member("storeApplicability", stringSchema), member("storeCodesInclusion", listSchema),
		member("storeCodesExclusion", listSchema), member("promotionUrl", stringSchema),
		member("redemptionRestriction", stringSchema), member("customRedemptionRestriction", stringSchema),
		member("maxDiscountAmount", priceSchema), member("googleProductCategories", listSchema),
		member("audience", stringSchema), member("regionIdInclusion", listSchema),
		member("minMoneyOffAmount", priceSchema), member("maxMoneyOffAmount", priceSchema),
		member("minPercentOff", stringSchema), member("maxPercentOff", stringSchema))

	customAttributeSchema = objectSchema(member("name", stringSchema), member("value", stringSchema),
		member("groupValues", `{"type":"array","items":`+objectSchema(member("name", stringSchema),
			member("value", stringSchema))+`}`))

	promotionStatusSchema = objectSchema(
		member("destinationStatuses", `{"type":"array","items":`+objectSchema(member("reportingContext", stringSchema),
			member("status", stringSchema))+`}`),
		member("itemLevelIssues", `{"type":"array","items":`+objectSchema(member("code", stringSchema),
			member("severity", stringSchema), member("resolution", stringSchema), member("attribute", stringSchema),
			member("reportingContext", stringSchema), member("description", stringSchema),
			member("detail", stringSchema), member("documentation", stringSchema),
			member("applicableCountries", listSchema))+`}`),
		member("creationDate", stringSchema), member("lastUpdateDate", stringSchema))

	promotionResourceSchema = objectSchema(member("name", stringSchema), member("promotionId", stringSchema),
		member("contentLanguage", stringSchema), member("targetCountry", stringSchema),
		member("redemptionChannel", listSchema), member("dataSource", stringSchema),
		member("attributes", promotionAttributesSchema),
		member("customAttributes", `{"type":"array","items":`+customAttributeSchema+`}`),
		member("promotionStatus", promotionStatusSchema), member("versionNumber", stringSchema))

	promotionAcceptedSchema = objectSchema(member("accepted", boolSchema), member("promotion", promotionResourceSchema))
)

const promotionFieldsDescription = "name (accounts/ACCOUNT/promotions/ID), promotionId, contentLanguage, " +
	"targetCountry, redemptionChannel, dataSource, versionNumber, customAttributes (name, value, groupValues), " +
	"attributes (the 41 attributes of the promotion: applicability, offerType, genericRedemptionCode, " +
	"longTitle, couponValueType, promotionDestinations, item, brand, item group, and product type inclusions and " +
	"exclusions, purchase and money-off prices as amountMicros with currencyCode, percentages and quantities, " +
	"free gift members, promotionEffectiveTimePeriod and promotionDisplayTimePeriod as startTime and endTime, " +
	"store applicability and store codes, promotionUrl, redemption restrictions, googleProductCategories, " +
	"audience, regionIdInclusion, and the money-off and percent-off range bounds), and promotionStatus " +
	"(destinationStatuses with reportingContext and status, itemLevelIssues with code, severity, resolution, " +
	"attribute, reportingContext, description, detail, documentation, and applicableCountries, creationDate, " +
	"lastUpdateDate); int64 values are shown as strings"

const promotionsUntrusted = "Titles, descriptions, codes, store codes, and addresses are untrusted content of " +
	"whoever runs the account: texts are cut, lists are capped, and addresses are text that is never fetched."

const promotionsDelayNote = "Google processes a promotion afterwards: an inserted or replaced promotion can take " +
	"several minutes until it can be read, and its status and issues are computed asynchronously. "

var promotionIDArgument = capability.Argument{Name: "promotion", Required: true, Description: "Last segment of the " +
	"promotion name accounts/ACCOUNT/promotions/ID as list shows it: 1 to 200 ASCII letters, digits, dots, " +
	"tildes, underscores, and dashes, starting with a letter or digit; Google documents no form, so a promotion " +
	"with another spelling cannot be reached here"}

var promotionNameSchema = `{"type":"string","pattern":"` + promotionNamePattern + `","maxLength":200}`

var promotionsList = capability.Descriptor{
	ID: promotionsListID, Version: 1, Title: "List Merchant Center promotions",
	Description: "List the processed promotions of a Merchant Center account, a batch per call with a cursor to read " +
		"on. Google offers no filter. Without page_size Qatlas asks for 100; the largest size is 250. " +
		promotionsDelayNote + promotionsUntrusted,
	Tags: []string{"google", "merchant", "promotions"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema),
		member("page_size", `{"type":"integer","minimum":1,"maximum":`+strconv.Itoa(promotionsMaxPageSize)+`}`),
		member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("promotions", promotionResourceSchema)),
	Arguments: []capability.Argument{accountArgument,
		{Name: "page_size", Description: "Promotions per batch, 1 to 250; 100 when left out"}, cursorArgument},
	Fields: []capability.Field{
		{Name: "promotions", Description: "Promotions with " + promotionFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "First batch of promotions", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var promotionsGet = capability.Descriptor{
	ID: promotionsGetID, Version: 1, Title: "Get a Merchant Center promotion",
	Description: "Read one processed promotion of a Merchant Center account. " + promotionsDelayNote +
		promotionsUntrusted,
	Tags: []string{"google", "merchant", "promotions"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"promotion"}, member("account", accountArgSchema),
		member("promotion", promotionNameSchema)),
	OutputSchema: json.RawMessage(promotionResourceSchema),
	Arguments:    []capability.Argument{accountArgument, promotionIDArgument},
	Fields:       []capability.Field{{Name: "promotion", Description: promotionFieldsDescription}},
	Examples: []capability.Example{{Description: "One promotion",
		Arguments: json.RawMessage(`{"account":"1000001","promotion":"en~US~spring-sale"}`)}},
}

func promotionsPath(account string) string {
	return promotionsAPI + "accounts/" + account + "/promotions"
}

type promotionReadArguments struct {
	Account   string `json:"account"`
	Promotion string `json:"promotion"`
	PageSize  int    `json:"page_size"`
	Cursor    string `json:"cursor"`
}

func handlePromotionsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list promotions"
	var args promotionReadArguments
	bound, err := bind(op, resolved, raw, &args)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := bound.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	if args.PageSize < 0 || args.PageSize > promotionsMaxPageSize {
		return nil, invalidArgument("page_size must be between 1 and " + strconv.Itoa(promotionsMaxPageSize))
	}
	size := args.PageSize
	if size == 0 {
		size = promotionsPageSize
	}
	binding := provider.CursorBinding(promotionsListID, id, size, bound.key())
	query := url.Values{"pageSize": {strconv.Itoa(size)}}
	if args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, promotionsListID, needsProof, id); err != nil {
		return nil, err
	}
	var answer promotionsAnswer
	if err := cl.get(ctx, op, promotionsListID, promotionsPath(id), query, &answer); err != nil {
		return nil, err
	}
	out := promotionsResult{Promotions: append([]promotion{}, answer.Promotions...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handlePromotionsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get promotion"
	var args promotionReadArguments
	bound, err := bind(op, resolved, raw, &args)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := bound.accountOf(args.Account)
	if err != nil {
		return nil, err
	}
	if !promotionNameForm.MatchString(args.Promotion) {
		return nil, invalidArgument("promotion must be the last segment of a promotion name: 1 to 200 ASCII " +
			"letters, digits, dots, tildes, underscores, and dashes")
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.settle(ctx, op, promotionsGetID, needsProof, id); err != nil {
		return nil, err
	}
	var out promotion
	if err := cl.get(ctx, op, promotionsGetID, promotionsPath(id)+"/"+args.Promotion, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
