package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	regionalInventoriesListID   = Provider + ".regional-inventories.list"
	regionalInventoriesInsertID = Provider + ".regional-inventories.insert"
	regionalInventoriesDeleteID = Provider + ".regional-inventories.delete"
)

// Google sends 25000 entries when pageSize is left out and allows 100000, which passes the size of an answer this
// provider reads: the tool always sends a page size and stops at the same bound as the local inventories.
const (
	defaultRegionalPage = defaultLocalPage
	maxRegionalPage     = maxLocalPage
)

// The reference allows IN_STOCK and OUT_OF_STOCK; the unspecified value is not a state to write.
var regionalAvailabilities = []string{"IN_STOCK", "OUT_OF_STOCK"}

// Output nodes, written from the RegionalInventory resource of the inventories API.
var (
	nRegionalInventoryAttributes = obj(
		f("availability", tID), f("loyaltyPrograms", list(nInventoryLoyaltyProgram)), f("price", nPrice),
		f("salePrice", nPrice), f("salePriceEffectiveDate", nInterval))
	nRegionalInventory = obj(
		f("account", tInt), f("base64EncodedName", tName), f("name", tName), f("region", tName),
		f("regionalInventoryAttributes", nRegionalInventoryAttributes))
)

type regionalInventoryArguments struct {
	Account                string         `json:"account"`
	Product                string         `json:"product"`
	Region                 string         `json:"region"`
	Price                  *priceInput    `json:"price"`
	SalePrice              *priceInput    `json:"sale_price"`
	SalePriceEffectiveDate *intervalInput `json:"sale_price_effective_date"`
	LoyaltyPrograms        []loyaltyInput `json:"loyalty_programs"`
	Availability           string         `json:"availability"`
	PageSize               int            `json:"page_size"`
	Cursor                 string         `json:"cursor"`
}

const regionRule = "region must be the ID of a region of the account: 1 to 64 ASCII letters, digits, underscores, " +
	"and hyphens, starting with a letter or digit"

func checkRegionID(id string) error {
	if !validRegionID(id) {
		return invalidArgument(regionRule)
	}
	return nil
}

// regionalInventoryBody builds the RegionalInventory the insert sends. Only what the caller gave is in it: the
// insert replaces the whole entry of the region, so a member left out is removed at Google, not kept.
func regionalInventoryBody(a regionalInventoryArguments) (map[string]any, error) {
	if err := checkRegionID(a.Region); err != nil {
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
	if a.Availability != "" {
		if !oneOf(a.Availability, regionalAvailabilities) {
			return nil, invalidArgument("availability must be one of " + strings.Join(regionalAvailabilities, ", "))
		}
		attrs["availability"] = a.Availability
	}
	body := map[string]any{"region": a.Region}
	if len(attrs) > 0 {
		body["regionalInventoryAttributes"] = attrs
	}
	return body, nil
}

// regionalInventoryCall is a settled call: the account is checked against the binding and the product against
// its documented forms, so nothing was read or requested yet.
type regionalInventoryCall struct {
	op, tool   string
	args       regionalInventoryArguments
	account    string
	needsProof bool
	scope      scope
	segment    string
}

func prepareRegionalInventory(op, tool string, resolved *config.Resolved, raw json.RawMessage) (regionalInventoryCall, error) {
	c := regionalInventoryCall{op: op, tool: tool}
	var err error
	c.account, c.needsProof, c.scope, err = settle(op, resolved, raw, &c.args, func() string { return c.args.Account })
	if err != nil {
		return c, err
	}
	c.segment, err = productSegment(c.args.Product)
	return c, err
}

func (c regionalInventoryCall) parent() string {
	return inventoriesAPI + "accounts/" + c.account + "/products/" + c.segment
}

func (c regionalInventoryCall) client(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (*client, error) {
	return openFor(ctx, c.op, c.tool, c.account, c.needsProof, resolved, secrets, red)
}

type regionalInventoriesResult struct {
	RegionalInventories []map[string]any `json:"regional_inventories"`
	NextCursor          string           `json:"next_cursor,omitempty"`
}

type regionalInventoryChange struct {
	Accepted          bool           `json:"accepted"`
	RegionalInventory map[string]any `json:"regional_inventory"`
}

type regionalInventoryDeleted struct {
	Accepted bool   `json:"accepted"`
	Name     string `json:"name"`
}

func handleRegionalInventoriesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareRegionalInventory("list regional inventories", regionalInventoriesListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if c.args.PageSize < 0 || c.args.PageSize > maxRegionalPage {
		return nil, invalidArgument("page_size must be 1 to " + strconv.Itoa(maxRegionalPage))
	}
	size := c.args.PageSize
	if size == 0 {
		size = defaultRegionalPage
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
		RegionalInventories []json.RawMessage `json:"regionalInventories"`
		NextPageToken       string            `json:"nextPageToken"`
	}
	if err := cl.get(ctx, c.op, c.tool, c.parent()+"/regionalInventories", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.RegionalInventories) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := regionalInventoriesResult{RegionalInventories: cleanObjects(nRegionalInventory, answer.RegionalInventories)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleRegionalInventoriesInsert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareRegionalInventory("insert regional inventory", regionalInventoriesInsertID, resolved, raw)
	if err != nil {
		return nil, err
	}
	body, err := regionalInventoryBody(c.args)
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
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodPost, path: c.parent() + "/regionalInventories:insert",
		body: encoded, change: true}, &answer); err != nil {
		return nil, err
	}
	return regionalInventoryChange{Accepted: true, RegionalInventory: cleanObject(nRegionalInventory, answer)}, nil
}

func handleRegionalInventoriesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareRegionalInventory("delete regional inventory", regionalInventoriesDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if err := checkRegionID(c.args.Region); err != nil {
		return nil, err
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := c.parent() + "/regionalInventories/" + c.args.Region
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodDelete, path: path, change: true}, nil); err != nil {
		return nil, err
	}
	return regionalInventoryDeleted{Accepted: true, Name: path[len(inventoriesAPI):]}, nil
}

// The summaries phrase the confirmation from the validated arguments alone.
func regionalInventoryInsertSummary(raw json.RawMessage) string {
	var a regionalInventoryArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Insert the regional inventory of region " + strconv.Quote(a.Region) + " for product " + strconv.Quote(a.Product)
	if a.Account != "" {
		out += " in account " + a.Account
	}
	var given []string
	for name, set := range map[string]bool{"price": a.Price != nil, "sale price": a.SalePrice != nil,
		"availability": a.Availability != "", "loyalty programs": len(a.LoyaltyPrograms) > 0} {
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
	return out + ". This replaces the whole regional inventory of that region, so every attribute not given here is removed. " +
		"Google shows it after up to 30 minutes, and Qatlas never repeats the call"
}

func regionalInventoryDeleteSummary(raw json.RawMessage) string {
	var a regionalInventoryArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Delete the regional inventory of region " + strconv.Quote(a.Region) + " for product " + strconv.Quote(a.Product)
	if a.Account != "" {
		out += " in account " + a.Account
	}
	return out + "; Google removes it after up to an hour, and Qatlas offers no way to undo it"
}
