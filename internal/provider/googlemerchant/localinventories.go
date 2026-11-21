package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	localInventoriesListID   = Provider + ".local-inventories.list"
	localInventoriesInsertID = Provider + ".local-inventories.insert"
	localInventoriesDeleteID = Provider + ".local-inventories.delete"
)

// inventoriesAPI is the path prefix of the inventories sub-API.
const inventoriesAPI = "inventories/v1/"

// Google sends 25000 entries when pageSize is left out, which passes the size of an answer this provider
// reads: the tool always sends a page size and stops at 250.
const (
	defaultLocalPage = 100
	maxLocalPage     = 250
)

// Bounds of the arguments. Google documents only the ones marked so; the others are Qatlas' own and keep an
// answer, a body, and a confirmation small.
const (
	maxStoreCodeLen        = 64
	maxInstoreLocationByte = 20    // documented: 20 bytes
	maxCustomAttributeSize = 10240 // documented: name plus value, characters
	maxLocalCustomAttrs    = 100
	maxLocalLoyalty        = 20
	maxLocalLabelRunes     = 100
	maxLocalCustomName     = 100
	maxQuantity            = 9007199254740991
)

// A store code is the store ID of a Business Profile. Google documents no form for it, so this is Qatlas' narrow
// form, chosen so that a code can never alter a path: no / ? # % or space, and no leading dot. A store with
// another code can be neither inserted nor deleted nor listed by code through this tool.
const (
	storeCodePattern = `[A-Za-z0-9_-][A-Za-z0-9._-]{0,63}`
	storeCodeRule    = "store_code must be 1 to 64 ASCII letters, digits, dots, underscores, and hyphens, not starting with a dot"
)

var (
	storeCodeForm = regexp.MustCompile(`^` + storeCodePattern + `$`)
	microsForm    = regexp.MustCompile(`^[0-9]{1,18}$`)
)

var (
	localAvailabilities = []string{"IN_STOCK", "LIMITED_AVAILABILITY", "ON_DISPLAY_TO_ORDER", "OUT_OF_STOCK"}
	localPickupMethods  = []string{"BUY", "RESERVE", "SHIP_TO_STORE", "NOT_SUPPORTED"}
	localPickupSlas     = []string{"SAME_DAY", "NEXT_DAY", "TWO_DAY", "THREE_DAY", "FOUR_DAY", "FIVE_DAY", "SIX_DAY",
		"SEVEN_DAY", "MULTI_WEEK"}
)

// Output nodes, written from the LocalInventory resource of the inventories API.
var (
	nInventoryLoyaltyProgram = obj(
		f("cashbackForFutureUse", nPrice), f("loyaltyPoints", tInt), f("memberPriceEffectiveInterval", nInterval),
		f("price", nPrice), f("programLabel", tName), f("shippingLabel", tName), f("tierLabel", tName))
	nLocalInventoryAttributes = obj(
		f("availability", tID), f("customAttributes", list(nCustomAttribute)), f("instoreProductLocation", tName),
		f("localShippingLabel", tName), f("loyaltyPrograms", list(nInventoryLoyaltyProgram)), f("pickupMethod", tID),
		f("pickupSla", tID), f("price", nPrice), f("quantity", tInt), f("salePrice", nPrice),
		f("salePriceEffectiveDate", nInterval))
	nLocalInventory = obj(
		f("account", tInt), f("base64EncodedName", tName), f("localInventoryAttributes", nLocalInventoryAttributes),
		f("name", tName), f("storeCode", tName))
)

type priceInput struct {
	AmountMicros string `json:"amountMicros"`
	CurrencyCode string `json:"currencyCode"`
}

type intervalInput struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

type loyaltyInput struct {
	ProgramLabel                 string         `json:"programLabel"`
	TierLabel                    string         `json:"tierLabel"`
	Price                        *priceInput    `json:"price"`
	CashbackForFutureUse         *priceInput    `json:"cashbackForFutureUse"`
	LoyaltyPoints                string         `json:"loyaltyPoints"`
	MemberPriceEffectiveInterval *intervalInput `json:"memberPriceEffectiveInterval"`
	ShippingLabel                string         `json:"shippingLabel"`
}

// customInput is a custom attribute: exactly one of a value or a group of sub-attributes. A sub-attribute has
// only a name and a value; Google allows groups inside groups, Qatlas stops after one level.
type customInput struct {
	Name        string      `json:"name"`
	Value       string      `json:"value"`
	GroupValues []subCustom `json:"group_values"`
}

type subCustom struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Nested is read only to refuse a second level.
	Nested json.RawMessage `json:"group_values"`
}

type localInventoryArguments struct {
	Account                string         `json:"account"`
	Product                string         `json:"product"`
	StoreCode              string         `json:"store_code"`
	Price                  *priceInput    `json:"price"`
	SalePrice              *priceInput    `json:"sale_price"`
	SalePriceEffectiveDate *intervalInput `json:"sale_price_effective_date"`
	LoyaltyPrograms        []loyaltyInput `json:"loyalty_programs"`
	CustomAttributes       []customInput  `json:"custom_attributes"`
	Availability           string         `json:"availability"`
	Quantity               *int64         `json:"quantity"`
	PickupMethod           string         `json:"pickup_method"`
	PickupSla              string         `json:"pickup_sla"`
	InstoreProductLocation *string        `json:"instore_product_location"`
	LocalShippingLabel     *string        `json:"local_shipping_label"`
	PageSize               int            `json:"page_size"`
	Cursor                 string         `json:"cursor"`
}

func plainText(value string, maxRunes int) bool {
	if value == "" || utf8.RuneCountInString(value) > maxRunes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func oneOf(value string, allowed []string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

func priceBody(name string, in *priceInput) (map[string]any, error) {
	if !microsForm.MatchString(in.AmountMicros) || !currencyForm.MatchString(in.CurrencyCode) {
		return nil, invalidArgument(name + " needs amountMicros as digits (millionths of the currency unit) and " +
			"currencyCode as three capital letters")
	}
	return map[string]any{"amountMicros": in.AmountMicros, "currencyCode": in.CurrencyCode}, nil
}

// intervalBody takes an interval as the API defines it: an open start or end is allowed, and so is a start equal
// to the end. Only the times that are given go into the body. An interval without any time matches any time,
// which leaving the argument out says as well, so it is refused as an empty argument.
func intervalBody(name string, in *intervalInput) (map[string]any, error) {
	out := map[string]any{}
	var start, end time.Time
	var err error
	if in.StartTime != "" {
		if start, err = time.Parse(time.RFC3339, in.StartTime); err != nil {
			return nil, invalidArgument(name + " startTime must be an RFC 3339 time")
		}
		out["startTime"] = in.StartTime
	}
	if in.EndTime != "" {
		if end, err = time.Parse(time.RFC3339, in.EndTime); err != nil {
			return nil, invalidArgument(name + " endTime must be an RFC 3339 time")
		}
		out["endTime"] = in.EndTime
	}
	if len(out) == 0 {
		return nil, invalidArgument(name + " needs startTime or endTime; leave it out for no restriction")
	}
	if len(out) == 2 && end.Before(start) {
		return nil, invalidArgument(name + " endTime must not be before startTime")
	}
	return out, nil
}

func checkStoreCode(code string) error {
	if !storeCodeForm.MatchString(code) {
		return invalidArgument(storeCodeRule)
	}
	return nil
}

// localInventoryBody builds the LocalInventory the insert sends. Only what the caller gave is in it: the
// insert replaces the whole entry of the store, so a member left out is removed at Google, not kept.
func localInventoryBody(a localInventoryArguments) (map[string]any, error) {
	if err := checkStoreCode(a.StoreCode); err != nil {
		return nil, err
	}
	attrs := map[string]any{}
	var err error
	if a.Price != nil {
		if attrs["price"], err = priceBody("price", a.Price); err != nil {
			return nil, err
		}
	}
	if a.SalePrice != nil {
		if attrs["salePrice"], err = priceBody("sale_price", a.SalePrice); err != nil {
			return nil, err
		}
	}
	if a.SalePriceEffectiveDate != nil {
		if a.SalePrice == nil {
			return nil, invalidArgument("sale_price_effective_date needs sale_price, which Google requires with it")
		}
		if attrs["salePriceEffectiveDate"], err = intervalBody("sale_price_effective_date", a.SalePriceEffectiveDate); err != nil {
			return nil, err
		}
	}
	if len(a.LoyaltyPrograms) > maxLocalLoyalty {
		return nil, invalidArgument("loyalty_programs holds at most " + strconv.Itoa(maxLocalLoyalty) + " entries")
	}
	if len(a.LoyaltyPrograms) > 0 {
		programs := make([]map[string]any, len(a.LoyaltyPrograms))
		for i, p := range a.LoyaltyPrograms {
			if programs[i], err = loyaltyBody(p); err != nil {
				return nil, err
			}
		}
		attrs["loyaltyPrograms"] = programs
	}
	if len(a.CustomAttributes) > 0 {
		custom, err := customBody(a.CustomAttributes)
		if err != nil {
			return nil, err
		}
		attrs["customAttributes"] = custom
	}
	if a.Availability != "" {
		if !oneOf(a.Availability, localAvailabilities) {
			return nil, invalidArgument("availability must be one of " + strings.Join(localAvailabilities, ", "))
		}
		attrs["availability"] = a.Availability
	}
	if a.Quantity != nil {
		if *a.Quantity < 0 || *a.Quantity > maxQuantity {
			return nil, invalidArgument("quantity must be a whole number from 0 to " + strconv.FormatInt(maxQuantity, 10))
		}
		attrs["quantity"] = strconv.FormatInt(*a.Quantity, 10)
	}
	if err := pickupBody(a, attrs); err != nil {
		return nil, err
	}
	if a.InstoreProductLocation != nil {
		value := *a.InstoreProductLocation
		if !plainText(value, maxInstoreLocationByte) || len(value) > maxInstoreLocationByte {
			return nil, invalidArgument("instore_product_location must be 1 to 20 bytes without control characters")
		}
		attrs["instoreProductLocation"] = value
	}
	if a.LocalShippingLabel != nil {
		if !plainText(*a.LocalShippingLabel, maxLocalLabelRunes) {
			return nil, invalidArgument("local_shipping_label must be 1 to " + strconv.Itoa(maxLocalLabelRunes) +
				" characters without control characters")
		}
		attrs["localShippingLabel"] = *a.LocalShippingLabel
	}
	body := map[string]any{"storeCode": a.StoreCode}
	if len(attrs) > 0 {
		body["localInventoryAttributes"] = attrs
	}
	return body, nil
}

// pickupBody applies the rule of the reference: a pickup method goes with a pickup SLA unless the method is
// NOT_SUPPORTED, and an SLA never goes without a method.
func pickupBody(a localInventoryArguments, attrs map[string]any) error {
	if a.PickupMethod != "" && !oneOf(a.PickupMethod, localPickupMethods) {
		return invalidArgument("pickup_method must be one of " + strings.Join(localPickupMethods, ", "))
	}
	if a.PickupSla != "" && !oneOf(a.PickupSla, localPickupSlas) {
		return invalidArgument("pickup_sla must be one of " + strings.Join(localPickupSlas, ", "))
	}
	switch {
	case a.PickupSla != "" && a.PickupMethod == "":
		return invalidArgument("pickup_sla needs pickup_method")
	case a.PickupMethod != "" && a.PickupMethod != "NOT_SUPPORTED" && a.PickupSla == "":
		return invalidArgument("pickup_method needs pickup_sla unless it is NOT_SUPPORTED")
	}
	if a.PickupMethod != "" {
		attrs["pickupMethod"] = a.PickupMethod
	}
	if a.PickupSla != "" {
		attrs["pickupSla"] = a.PickupSla
	}
	return nil
}

const customRule = "a custom attribute needs a name of 1 to " + "100" + " characters and exactly one of value or " +
	"group_values; a group holds sub-attributes with name and value; each attribute, with its sub-attributes, has " +
	"at most 10240 characters, and attributes and sub-attributes together are at most 100, without control characters"

// customBody checks the custom attributes and builds them. Every attribute counts once and every
// sub-attribute counts once towards the cap; the 10240 characters apply to an attribute as a whole, name and
// value, or the group name with every sub-attribute.
func customBody(in []customInput) ([]map[string]any, error) {
	out := make([]map[string]any, len(in))
	count := 0
	for i, c := range in {
		count++
		size := utf8.RuneCountInString(c.Name)
		if !plainText(c.Name, maxLocalCustomName) || (c.Value == "") == (len(c.GroupValues) == 0) {
			return nil, invalidArgument(customRule)
		}
		if c.Value != "" {
			if !plainText(c.Value, maxCustomAttributeSize) {
				return nil, invalidArgument(customRule)
			}
			size += utf8.RuneCountInString(c.Value)
			out[i] = map[string]any{"name": c.Name, "value": c.Value}
		} else {
			subs := make([]map[string]string, len(c.GroupValues))
			for j, sub := range c.GroupValues {
				count++
				if sub.Nested != nil || !plainText(sub.Name, maxLocalCustomName) || !plainText(sub.Value, maxCustomAttributeSize) {
					return nil, invalidArgument(customRule)
				}
				size += utf8.RuneCountInString(sub.Name) + utf8.RuneCountInString(sub.Value)
				subs[j] = map[string]string{"name": sub.Name, "value": sub.Value}
			}
			out[i] = map[string]any{"name": c.Name, "groupValues": subs}
		}
		if size > maxCustomAttributeSize || count > maxLocalCustomAttrs {
			return nil, invalidArgument(customRule)
		}
	}
	return out, nil
}

func loyaltyBody(p loyaltyInput) (map[string]any, error) {
	out := map[string]any{}
	var err error
	for name, value := range map[string]string{"programLabel": p.ProgramLabel, "tierLabel": p.TierLabel,
		"shippingLabel": p.ShippingLabel} {
		if value == "" {
			continue
		}
		if !plainText(value, maxLocalLabelRunes) {
			return nil, invalidArgument("loyalty program labels are 1 to " + strconv.Itoa(maxLocalLabelRunes) +
				" characters without control characters")
		}
		out[name] = value
	}
	if p.Price != nil {
		if out["price"], err = priceBody("loyalty price", p.Price); err != nil {
			return nil, err
		}
	}
	if p.CashbackForFutureUse != nil {
		if out["cashbackForFutureUse"], err = priceBody("cashbackForFutureUse", p.CashbackForFutureUse); err != nil {
			return nil, err
		}
	}
	if p.LoyaltyPoints != "" {
		if !microsForm.MatchString(p.LoyaltyPoints) {
			return nil, invalidArgument("loyaltyPoints must be a whole number as digits")
		}
		out["loyaltyPoints"] = p.LoyaltyPoints
	}
	if p.MemberPriceEffectiveInterval != nil {
		if out["memberPriceEffectiveInterval"], err = intervalBody("memberPriceEffectiveInterval", p.MemberPriceEffectiveInterval); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, invalidArgument("a loyalty program needs at least one member")
	}
	return out, nil
}

// localInventoryCall is a settled call: the account is checked against the binding and the product against
// its documented forms, so nothing was read or requested yet.
type localInventoryCall struct {
	op, tool   string
	args       localInventoryArguments
	account    string
	needsProof bool
	scope      scope
	segment    string
}

func prepareLocalInventory(op, tool string, resolved *config.Resolved, raw json.RawMessage) (localInventoryCall, error) {
	c := localInventoryCall{op: op, tool: tool}
	var err error
	c.account, c.needsProof, c.scope, err = settle(op, resolved, raw, &c.args, func() string { return c.args.Account })
	if err != nil {
		return c, err
	}
	c.segment, err = productSegment(c.args.Product)
	return c, err
}

func (c localInventoryCall) parent() string {
	return inventoriesAPI + "accounts/" + c.account + "/products/" + c.segment
}

func (c localInventoryCall) client(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (*client, error) {
	return openFor(ctx, c.op, c.tool, c.account, c.needsProof, resolved, secrets, red)
}

type localInventoriesResult struct {
	LocalInventories []map[string]any `json:"local_inventories"`
	NextCursor       string           `json:"next_cursor,omitempty"`
}

type localInventoryChange struct {
	Accepted       bool           `json:"accepted"`
	LocalInventory map[string]any `json:"local_inventory"`
}

type localInventoryDeleted struct {
	Accepted bool   `json:"accepted"`
	Name     string `json:"name"`
}

func handleLocalInventoriesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareLocalInventory("list local inventories", localInventoriesListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if c.args.PageSize < 0 || c.args.PageSize > maxLocalPage {
		return nil, invalidArgument("page_size must be 1 to " + strconv.Itoa(maxLocalPage))
	}
	size := c.args.PageSize
	if size == 0 {
		size = defaultLocalPage
	}
	binding := provider.CursorBinding(c.tool, c.account, c.segment, size, c.scope.key())
	query, err := pageQuery(binding, size, c.args.Cursor)
	if err != nil {
		return nil, err
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer struct {
		LocalInventories []json.RawMessage `json:"localInventories"`
		NextPageToken    string            `json:"nextPageToken"`
	}
	if err := cl.get(ctx, c.op, c.tool, c.parent()+"/localInventories", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.LocalInventories) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := localInventoriesResult{LocalInventories: cleanObjects(nLocalInventory, answer.LocalInventories)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleLocalInventoriesInsert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareLocalInventory("insert local inventory", localInventoriesInsertID, resolved, raw)
	if err != nil {
		return nil, err
	}
	body, err := localInventoryBody(c.args)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, invalidArgument("the arguments could not be encoded")
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer json.RawMessage
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodPost, path: c.parent() + "/localInventories:insert",
		body: encoded, change: true}, &answer); err != nil {
		return nil, err
	}
	return localInventoryChange{Accepted: true, LocalInventory: cleanObject(nLocalInventory, answer)}, nil
}

func handleLocalInventoriesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareLocalInventory("delete local inventory", localInventoriesDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if err := checkStoreCode(c.args.StoreCode); err != nil {
		return nil, err
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := c.parent() + "/localInventories/" + c.args.StoreCode
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodDelete, path: path, change: true}, nil); err != nil {
		return nil, err
	}
	return localInventoryDeleted{Accepted: true, Name: path[len(inventoriesAPI):]}, nil
}

// The summaries phrase the confirmation from the validated arguments alone.
func localInventoryInsertSummary(raw json.RawMessage) string {
	var a localInventoryArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Insert the local inventory of store " + strconv.Quote(a.StoreCode) + " for product " + strconv.Quote(a.Product)
	if a.Account != "" {
		out += " in account " + a.Account
	}
	var given []string
	for name, set := range map[string]bool{"price": a.Price != nil, "sale price": a.SalePrice != nil,
		"availability": a.Availability != "", "quantity": a.Quantity != nil,
		"pickup": a.PickupMethod != "" || a.PickupSla != "", "loyalty programs": len(a.LoyaltyPrograms) > 0,
		"custom attributes": len(a.CustomAttributes) > 0, "in-store location": a.InstoreProductLocation != nil,
		"shipping label": a.LocalShippingLabel != nil} {
		if set {
			given = append(given, name)
		}
	}
	slices.Sort(given)
	if len(given) == 0 {
		out += " with no attributes"
	} else {
		out += " with " + strings.Join(given, ", ")
	}
	return out + ". This replaces the whole local inventory of that store, so every attribute not given here is removed. " +
		"Google shows it after up to 30 minutes, and Qatlas never repeats the call"
}

func localInventoryDeleteSummary(raw json.RawMessage) string {
	var a localInventoryArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Delete the local inventory of store " + strconv.Quote(a.StoreCode) + " for product " + strconv.Quote(a.Product)
	if a.Account != "" {
		out += " in account " + a.Account
	}
	return out + "; Google removes it after up to an hour, and Qatlas offers no way to undo it"
}
