package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	autofeedSettingsGetID         = Provider + ".autofeed-settings.get"
	autofeedSettingsUpdateID      = Provider + ".autofeed-settings.update"
	automaticImprovementsGetID    = Provider + ".automatic-improvements.get"
	automaticImprovementsUpdateID = Provider + ".automatic-improvements.update"
)

var settingsChangeRisk = capability.Risk{
	Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
	Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "merchant-account-data",
}

// The documented members of the two settings resources. Flags are pointers so that false is shown.
type autofeedSettings struct {
	Name           idText `json:"name,omitempty"`
	EnableProducts *bool  `json:"enableProducts,omitempty"`
	Eligible       *bool  `json:"eligible,omitempty"`
}

type itemUpdatesSettings struct {
	AllowPriceUpdates              *bool `json:"allowPriceUpdates,omitempty"`
	AllowAvailabilityUpdates       *bool `json:"allowAvailabilityUpdates,omitempty"`
	AllowStrictAvailabilityUpdates *bool `json:"allowStrictAvailabilityUpdates,omitempty"`
	AllowConditionUpdates          *bool `json:"allowConditionUpdates,omitempty"`
}

type itemUpdates struct {
	AccountItemUpdatesSettings              *itemUpdatesSettings `json:"accountItemUpdatesSettings,omitempty"`
	EffectiveAllowAvailabilityUpdates       *bool                `json:"effectiveAllowAvailabilityUpdates,omitempty"`
	EffectiveAllowPriceUpdates              *bool                `json:"effectiveAllowPriceUpdates,omitempty"`
	EffectiveAllowConditionUpdates          *bool                `json:"effectiveAllowConditionUpdates,omitempty"`
	EffectiveAllowStrictAvailabilityUpdates *bool                `json:"effectiveAllowStrictAvailabilityUpdates,omitempty"`
}

type shippingImprovements struct {
	AllowShippingImprovements *bool `json:"allowShippingImprovements,omitempty"`
}

type imageImprovementsSettings struct {
	AllowAutomaticImageImprovements *bool `json:"allowAutomaticImageImprovements,omitempty"`
}

type imageImprovements struct {
	AccountImageImprovementsSettings         *imageImprovementsSettings `json:"accountImageImprovementsSettings,omitempty"`
	EffectiveAllowAutomaticImageImprovements *bool                      `json:"effectiveAllowAutomaticImageImprovements,omitempty"`
}

type automaticImprovements struct {
	Name                 idText                `json:"name,omitempty"`
	ItemUpdates          *itemUpdates          `json:"itemUpdates,omitempty"`
	ShippingImprovements *shippingImprovements `json:"shippingImprovements,omitempty"`
	ImageImprovements    *imageImprovements    `json:"imageImprovements,omitempty"`
}

// settingsAccepted is the result of an update: Google accepted the request. What the account shows after the
// processing is read with the get tool.
type settingsAccepted struct {
	Accepted bool     `json:"accepted"`
	Name     string   `json:"name"`
	Updated  []string `json:"updated"`
}

const (
	autofeedSettingsSchema = `{"type":"object","properties":{"name":{"type":"string"},"enableProducts":{"type":"boolean"},` +
		`"eligible":{"type":"boolean"}}}`
	settingsAcceptedSchema = `{"type":"object","properties":{"accepted":{"type":"boolean"},"name":{"type":"string"},` +
		`"updated":{"type":"array","items":{"type":"string"}}}}`
)

func flagMembers(names ...string) []string {
	members := make([]string, len(names))
	for i, n := range names {
		members[i] = member(n, boolSchema)
	}
	return members
}

var automaticImprovementsSchema = objectSchema(member("name", stringSchema),
	member("itemUpdates", objectSchema(append([]string{member("accountItemUpdatesSettings", objectSchema(flagMembers(
		"allowPriceUpdates", "allowAvailabilityUpdates", "allowStrictAvailabilityUpdates", "allowConditionUpdates")...))},
		flagMembers("effectiveAllowAvailabilityUpdates", "effectiveAllowPriceUpdates", "effectiveAllowConditionUpdates",
			"effectiveAllowStrictAvailabilityUpdates")...)...)),
	member("shippingImprovements", objectSchema(flagMembers("allowShippingImprovements")...)),
	member("imageImprovements", objectSchema(
		member("accountImageImprovementsSettings", objectSchema(flagMembers("allowAutomaticImageImprovements")...)),
		member("effectiveAllowAutomaticImageImprovements", boolSchema))))

const (
	settingsAcceptedNote = " Only the acceptance of the request is reported: Google processes the change with a " +
		"delay, so read the settings again after a while to see the result."
	autofeedEffectNote = "Turning the autofeed on lets Google take the products it crawls from the website of " +
		"the account into Google Shopping automatically, without any product being submitted: they become public " +
		"offers. When the autofeed is turned on for the first time, products usually appear at once, and after " +
		"turning it on again it can take up to 24 hours."
	improvementsEffectNote = "Turning an improvement on lets Google change the public offers of the account by " +
		"itself: item updates let Google overwrite price, availability, and condition with what it finds on the " +
		"website, image improvements replace images of disapproved offers with corrected ones, and shipping " +
		"improvements change the delivery times shown to shoppers."
	improvementsFieldsNote = "Each of item_updates, image_improvements, and shipping_improvements that is given " +
		"replaces that whole setting: item_updates flags that are left out are not kept. Item updates and image " +
		"improvements are set at the level of the account; their effective values, which can be inherited from an " +
		"advanced account, are shown by the get tool. Shipping improvements are not available for advanced accounts."
)

var (
	settingsGetFields = []capability.Field{{Name: "name", Description: "accounts/ACCOUNT/autofeedSettings"},
		{Name: "enableProducts", Description: "whether product crawling through the autofeed is on"},
		{Name: "eligible", Description: "whether the account can be enrolled into an autofeed"}}
	improvementsGetFields = []capability.Field{{Name: "name", Description: "accounts/ACCOUNT/automaticImprovements"},
		{Name: "itemUpdates", Description: "accountItemUpdatesSettings (allowPriceUpdates, allowAvailabilityUpdates, " +
			"allowStrictAvailabilityUpdates, allowConditionUpdates) and the effective values of the four"},
		{Name: "imageImprovements", Description: "accountImageImprovementsSettings (allowAutomaticImageImprovements) " +
			"and effectiveAllowAutomaticImageImprovements"},
		{Name: "shippingImprovements", Description: "allowShippingImprovements"}}
	acceptedFields = []capability.Field{{Name: "accepted", Description: "true when Google accepted the request"},
		{Name: "name", Description: "the settings resource of the request"},
		{Name: "updated", Description: "the members of the settings that the request named"}}
)

var autofeedSettingsGet = capability.Descriptor{
	ID: autofeedSettingsGetID, Version: 1, Title: "Get the autofeed settings of a Merchant Center account",
	Description: "Read whether the autofeed of a Merchant Center account is on (enableProducts) and whether the " +
		"account is eligible for it. With the autofeed on, Google takes crawled products into Google Shopping " +
		"automatically.",
	Tags: []string{"google", "merchant", "autofeed"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema)), OutputSchema: json.RawMessage(autofeedSettingsSchema),
	Arguments: []capability.Argument{accountArgument}, Fields: settingsGetFields,
	Examples: []capability.Example{{Description: "Autofeed settings of one account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var autofeedSettingsUpdate = capability.Descriptor{
	ID: autofeedSettingsUpdateID, Version: 1, Title: "Turn the autofeed of a Merchant Center account on or off",
	Description: "Turn the autofeed of a Merchant Center account on or off (enableProducts). " + autofeedEffectNote +
		" The account must meet Google's conditions (see eligible of the get tool) and must not be a marketplace." +
		settingsAcceptedNote + " Offered only by a connection whose tools list names it.",
	Tags: []string{"google", "merchant", "autofeed", "update"}, Risk: settingsChangeRisk, Provider: Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema([]string{"enable_products"}, member("account", accountArgSchema), member("enable_products", boolSchema)),
	OutputSchema:          json.RawMessage(settingsAcceptedSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "enable_products", Description: "true turns product crawling through the autofeed on, false turns it off", Required: true}},
	Fields: acceptedFields,
	Examples: []capability.Example{{Description: "Turn the autofeed on",
		Arguments: json.RawMessage(`{"account":"1000001","enable_products":true}`)}},
}

var automaticImprovementsGet = capability.Descriptor{
	ID: automaticImprovementsGetID, Version: 1, Title: "Get the automatic improvements of a Merchant Center account",
	Description: "Read which automatic improvements are on for a Merchant Center account: item updates, image " +
		"improvements, and shipping improvements. With them on, Google changes prices, conditions, images, " +
		"availability, and delivery times of the public offers by itself.",
	Tags: []string{"google", "merchant", "automatic-improvements"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema)), OutputSchema: json.RawMessage(automaticImprovementsSchema),
	Arguments: []capability.Argument{accountArgument}, Fields: improvementsGetFields,
	Examples: []capability.Example{{Description: "Automatic improvements of one account", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

const (
	itemUpdatesInput = `{"type":"object","properties":{"allow_price_updates":` + boolSchema +
		`,"allow_availability_updates":` + boolSchema + `,"allow_strict_availability_updates":` + boolSchema +
		`,"allow_condition_updates":` + boolSchema + `},"additionalProperties":false,"minProperties":1}`
)

var automaticImprovementsUpdate = capability.Descriptor{
	ID: automaticImprovementsUpdateID, Version: 1, Title: "Change the automatic improvements of a Merchant Center account",
	Description: "Turn automatic item updates, image improvements, and shipping improvements of a Merchant Center " +
		"account on or off. " + improvementsEffectNote + " " + improvementsFieldsNote + settingsAcceptedNote +
		" Offered only by a connection whose tools list names it.",
	Tags: []string{"google", "merchant", "automatic-improvements", "update"}, Risk: settingsChangeRisk, Provider: Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(nil, member("account", accountArgSchema), member("item_updates", itemUpdatesInput),
		member("image_improvements", boolSchema), member("shipping_improvements", boolSchema)),
	OutputSchema: json.RawMessage(settingsAcceptedSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "item_updates", Description: "object with allow_price_updates, allow_availability_updates, " +
			"allow_strict_availability_updates, allow_condition_updates (booleans, at least one); replaces the item " +
			"update settings of the account"},
		{Name: "image_improvements", Description: "true or false: allow automatic image improvements"},
		{Name: "shipping_improvements", Description: "true or false: allow automatic shipping improvements"}},
	Fields: acceptedFields,
	Examples: []capability.Example{{Description: "Turn off automatic image improvements",
		Arguments: json.RawMessage(`{"account":"1000001","image_improvements":false}`)}},
}

type autofeedUpdateArguments struct {
	EnableProducts *bool `json:"enable_products"`
}

type improvementsUpdateArguments struct {
	ItemUpdates          *itemUpdateArguments `json:"item_updates"`
	ImageImprovements    *bool                `json:"image_improvements"`
	ShippingImprovements *bool                `json:"shipping_improvements"`
}

type itemUpdateArguments struct {
	AllowPriceUpdates              *bool `json:"allow_price_updates"`
	AllowAvailabilityUpdates       *bool `json:"allow_availability_updates"`
	AllowStrictAvailabilityUpdates *bool `json:"allow_strict_availability_updates"`
	AllowConditionUpdates          *bool `json:"allow_condition_updates"`
}

func (a itemUpdateArguments) empty() bool {
	return a.AllowPriceUpdates == nil && a.AllowAvailabilityUpdates == nil &&
		a.AllowStrictAvailabilityUpdates == nil && a.AllowConditionUpdates == nil
}

func autofeedPath(id string) string     { return accountPath(id) + "/autofeedSettings" }
func improvementsPath(id string) string { return accountPath(id) + "/automaticImprovements" }

// settleSettings settles the account against the binding, which needs no secret and no request, and then opens the
// client and, for an account no target names, reads the proof. Every refusal comes before the first secret.
func settleSettings(ctx context.Context, c call, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (*client, string, error) {
	id, needsProof, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return nil, "", err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, "", err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, "", err
		}
	}
	return cl, id, nil
}

func handleAutofeedSettingsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get autofeed settings", autofeedSettingsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	cl, id, err := settleSettings(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out autofeedSettings
	if err := cl.get(ctx, c.op, c.tool, autofeedPath(id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleAutomaticImprovementsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get automatic improvements", automaticImprovementsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	cl, id, err := settleSettings(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out automaticImprovements
	if err := cl.get(ctx, c.op, c.tool, improvementsPath(id), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// patchSettings sends the one PATCH of an update. The mask is built here from fixed names: Google requires
// it, and a member the mask does not name is left alone.
func patchSettings(ctx context.Context, cl *client, c call, path string, mask []string, body any) (any, error) {
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
	return settingsAccepted{Accepted: true, Name: path[len(accountsAPI):], Updated: mask}, nil
}

func handleAutofeedSettingsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update autofeed settings", autofeedSettingsUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var args autofeedUpdateArguments
	if json.Unmarshal(raw, &args) != nil || args.EnableProducts == nil {
		return nil, invalidArgument("enable_products is required and must be true or false")
	}
	cl, id, err := settleSettings(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return patchSettings(ctx, cl, c, autofeedPath(id), []string{"enableProducts"},
		autofeedSettings{EnableProducts: args.EnableProducts})
}

// improvementsBody maps the arguments to the request body and the update mask. The mask names the members
// as a whole: the narrower paths of the reference are not used, so a member the caller gives replaces its
// account level settings and nothing is cleared that the caller did not name.
func improvementsBody(args improvementsUpdateArguments) (automaticImprovements, []string, error) {
	var body automaticImprovements
	var mask []string
	if item := args.ItemUpdates; item != nil {
		if item.empty() {
			return body, nil, invalidArgument("item_updates needs at least one flag")
		}
		body.ItemUpdates = &itemUpdates{AccountItemUpdatesSettings: &itemUpdatesSettings{
			AllowPriceUpdates: item.AllowPriceUpdates, AllowAvailabilityUpdates: item.AllowAvailabilityUpdates,
			AllowStrictAvailabilityUpdates: item.AllowStrictAvailabilityUpdates,
			AllowConditionUpdates:          item.AllowConditionUpdates}}
		mask = append(mask, "itemUpdates")
	}
	if args.ImageImprovements != nil {
		body.ImageImprovements = &imageImprovements{AccountImageImprovementsSettings: &imageImprovementsSettings{
			AllowAutomaticImageImprovements: args.ImageImprovements}}
		mask = append(mask, "imageImprovements")
	}
	if args.ShippingImprovements != nil {
		body.ShippingImprovements = &shippingImprovements{AllowShippingImprovements: args.ShippingImprovements}
		mask = append(mask, "shippingImprovements")
	}
	if len(mask) == 0 {
		return body, nil, invalidArgument("give at least one of item_updates, image_improvements, shipping_improvements")
	}
	return body, mask, nil
}

func handleAutomaticImprovementsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update automatic improvements", automaticImprovementsUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var args improvementsUpdateArguments
	if json.Unmarshal(raw, &args) != nil {
		return nil, invalidArgument("the arguments could not be read")
	}
	body, mask, err := improvementsBody(args)
	if err != nil {
		return nil, err
	}
	cl, id, err := settleSettings(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return patchSettings(ctx, cl, c, improvementsPath(id), mask, body)
}

func flagText(v *bool) string {
	if v != nil && *v {
		return "on"
	}
	return "off"
}

const publicEffect = " This changes what Google shows publicly; Google processes it with a delay."

func autofeedSummary(raw json.RawMessage) string {
	var args struct {
		Account string `json:"account"`
		autofeedUpdateArguments
	}
	_ = json.Unmarshal(raw, &args)
	return "Turn the autofeed of Merchant Center account " + accountText(args.Account) + " " + flagText(args.EnableProducts) +
		". With it on, Google takes crawled products into Google Shopping automatically." + publicEffect
}

func improvementsSummary(raw json.RawMessage) string {
	var args struct {
		Account string `json:"account"`
		improvementsUpdateArguments
	}
	_ = json.Unmarshal(raw, &args)
	var parts []string
	if item := args.ItemUpdates; item != nil {
		for _, f := range []struct {
			name string
			v    *bool
		}{{"price updates", item.AllowPriceUpdates}, {"availability updates", item.AllowAvailabilityUpdates},
			{"strict availability updates", item.AllowStrictAvailabilityUpdates},
			{"condition updates", item.AllowConditionUpdates}} {
			if f.v != nil {
				parts = append(parts, f.name+" "+flagText(f.v))
			}
		}
	}
	if args.ImageImprovements != nil {
		parts = append(parts, "image improvements "+flagText(args.ImageImprovements))
	}
	if args.ShippingImprovements != nil {
		parts = append(parts, "shipping improvements "+flagText(args.ShippingImprovements))
	}
	return "Set the automatic improvements of Merchant Center account " + accountText(args.Account) + ": " +
		strings.Join(parts, ", ") + ". Google may then change prices, availability, conditions, images, and delivery " +
		"times of the public offers by itself." + publicEffect
}

func accountText(id string) string {
	if id == "" {
		return "of the connection"
	}
	return id
}
