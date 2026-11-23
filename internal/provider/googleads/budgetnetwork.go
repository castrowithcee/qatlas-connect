package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	customerBudgetNetworkSettingsCreateID = Provider + ".customer-budget-network-settings.create"
	customerBudgetNetworkSettingsUpdateID = Provider + ".customer-budget-network-settings.update"

	budgetNetworkService = "/customerBudgetNetworkSettings"
	// maxCapMillis is the largest cap Google accepts: 100,000 milli-percent are 100 percent.
	maxCapMillis = 100_000
)

// The values of the enums in v26 that a request may name. UNSPECIFIED and UNKNOWN are no values to send.
var (
	advertisingChannelTypes = []string{"SEARCH", "DISPLAY", "SHOPPING", "HOTEL", "VIDEO", "MULTI_CHANNEL", "LOCAL",
		"SMART", "PERFORMANCE_MAX", "LOCAL_SERVICES", "TRAVEL", "DEMAND_GEN"}
	networkChannelTypes = []string{"OWNED_AND_OPERATED", "SEARCH_PARTNER", "GOOGLE_DISPLAY"}
	priorityTiers       = []string{"TIER1", "TIER2"}
)

type (
	budgetCap struct {
		NetworkChannel   string `json:"network_channel"`
		CapPercentMillis *int64 `json:"cap_percent_millis"`
	}
	channelPriority struct {
		PriorityTier    string   `json:"priority_tier"`
		NetworkChannels []string `json:"network_channels"`
	}
	networkSetting struct {
		AdvertisingChannelType string            `json:"advertising_channel_type"`
		BudgetCaps             []budgetCap       `json:"budget_caps"`
		ChannelPriorities      []channelPriority `json:"channel_priorities"`
	}
	budgetNetworkArguments struct {
		CustomerID            string           `json:"customer_id"`
		BudgetNetworkSettings []networkSetting `json:"budget_network_settings"`
		ValidateOnly          bool             `json:"validate_only"`
		ResponseContentType   string           `json:"response_content_type"`
	}
)

// settings checks the structure that Google documents (each advertising channel type once, each network
// channel once in the caps and once across the tiers, caps from 0 to 100,000 milli-percent) and builds the
// wire form. Every other rule, such as which channel a tier may hold, is Google's to validate.
func (a budgetNetworkArguments) settings() (map[string]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	return networkSettingsBody(a.BudgetNetworkSettings)
}

// networkSettingsBody checks the entries and builds the BudgetNetworkSettings message, which the customer
// setting and a campaign budget share.
func networkSettingsBody(settings []networkSetting) (map[string]any, error) {
	if n := len(settings); n == 0 || n > len(advertisingChannelTypes) {
		return nil, badRequest("budget_network_settings must hold one to 12 entries")
	}
	var seenTypes []string
	wire := []any{}
	for _, s := range settings {
		if !slices.Contains(advertisingChannelTypes, s.AdvertisingChannelType) || slices.Contains(seenTypes, s.AdvertisingChannelType) {
			return nil, badRequest("every entry of budget_network_settings needs its own advertising_channel_type of " +
				"the API, and a type appears at most once")
		}
		seenTypes = append(seenTypes, s.AdvertisingChannelType)
		if len(s.BudgetCaps) > len(networkChannelTypes) || len(s.ChannelPriorities) > len(priorityTiers) {
			return nil, badRequest("an entry holds at most one cap per network channel and one entry per priority tier")
		}
		entry := map[string]any{"advertisingChannelType": s.AdvertisingChannelType}
		var caps []any
		var capped []string
		for _, c := range s.BudgetCaps {
			if !slices.Contains(networkChannelTypes, c.NetworkChannel) || slices.Contains(capped, c.NetworkChannel) {
				return nil, badRequest("budget_caps names each network channel at most once")
			}
			capped = append(capped, c.NetworkChannel)
			item := map[string]any{"networkChannel": c.NetworkChannel}
			if c.CapPercentMillis != nil {
				if *c.CapPercentMillis < 0 || *c.CapPercentMillis > maxCapMillis {
					return nil, badRequest("cap_percent_millis must be 0 to 100000 (milli-percent, 100000 is 100 percent)")
				}
				item["capPercentMillis"] = *c.CapPercentMillis
			}
			caps = append(caps, item)
		}
		if caps != nil {
			entry["budgetCaps"] = caps
		}
		var tiers []any
		var usedTiers []string
		for _, p := range s.ChannelPriorities {
			if !slices.Contains(priorityTiers, p.PriorityTier) || slices.Contains(usedTiers, p.PriorityTier) ||
				len(p.NetworkChannels) > len(networkChannelTypes) {
				return nil, badRequest("channel_priorities names each priority tier TIER1 and TIER2 at most once")
			}
			usedTiers = append(usedTiers, p.PriorityTier)
			channels := []any{}
			for _, ch := range p.NetworkChannels {
				if !slices.Contains(networkChannelTypes, ch) {
					return nil, badRequest("network_channels may name only OWNED_AND_OPERATED, SEARCH_PARTNER and GOOGLE_DISPLAY")
				}
				channels = append(channels, ch)
			}
			tiers = append(tiers, map[string]any{"priorityTier": p.PriorityTier, "networkChannels": channels})
		}
		if tiers != nil {
			entry["channelPriorities"] = tiers
			var all []string
			for _, p := range s.ChannelPriorities {
				all = append(all, p.NetworkChannels...)
			}
			for i, ch := range all {
				if slices.Contains(all[:i], ch) {
					return nil, badRequest("a network channel appears in one priority tier only")
				}
			}
		}
		wire = append(wire, entry)
	}
	return map[string]any{"settings": wire}, nil
}

// The setting as Google returns it.
type (
	networkSettingsWire struct {
		Settings []struct {
			AdvertisingChannelType string `json:"advertisingChannelType"`
			BudgetCaps             []struct {
				NetworkChannel   string   `json:"networkChannel"`
				CapPercentMillis *wireInt `json:"capPercentMillis"`
			} `json:"budgetCaps"`
			ChannelPriorities []struct {
				PriorityTier    string   `json:"priorityTier"`
				NetworkChannels []string `json:"networkChannels"`
			} `json:"channelPriorities"`
		} `json:"settings"`
	}
	budgetNetworkResource struct {
		ResourceName          string                `json:"resource_name"`
		BudgetNetworkSettings []networkSettingEntry `json:"budget_network_settings"`
	}
	networkSettingEntry struct {
		AdvertisingChannelType string      `json:"advertising_channel_type"`
		BudgetCaps             []capEntry  `json:"budget_caps"`
		ChannelPriorities      []tierEntry `json:"channel_priorities"`
	}
	capEntry struct {
		NetworkChannel   string `json:"network_channel"`
		CapPercentMillis *int64 `json:"cap_percent_millis,omitempty"`
	}
	tierEntry struct {
		PriorityTier    string   `json:"priority_tier"`
		NetworkChannels []string `json:"network_channels"`
	}
	budgetNetworkChanged struct {
		CustomerID                   string                 `json:"customer_id"`
		ValidateOnly                 bool                   `json:"validate_only"`
		ResourceName                 string                 `json:"resource_name,omitempty"`
		CustomerBudgetNetworkSetting *budgetNetworkResource `json:"customer_budget_network_setting,omitempty"`
	}
)

// networkResourceOf maps the setting of a mutate answer. It must be the setting of the account of the call.
func networkResourceOf(raw json.RawMessage, customerID string) (*budgetNetworkResource, bool) {
	var w struct {
		ResourceName          string              `json:"resourceName"`
		BudgetNetworkSettings networkSettingsWire `json:"budgetNetworkSettings"`
	}
	if json.Unmarshal(raw, &w) != nil || w.ResourceName != networkSettingName(customerID) {
		return nil, false
	}
	entries, ok := networkEntriesOf(w.BudgetNetworkSettings)
	if !ok {
		return nil, false
	}
	return &budgetNetworkResource{ResourceName: w.ResourceName, BudgetNetworkSettings: entries}, true
}

// networkEntriesOf maps the BudgetNetworkSettings message of an answer, cut and without control characters.
func networkEntriesOf(settings networkSettingsWire) ([]networkSettingEntry, bool) {
	entries := []networkSettingEntry{}
	for _, s := range settings.Settings[:min(len(settings.Settings), maxListItems)] {
		entry := networkSettingEntry{AdvertisingChannelType: code(s.AdvertisingChannelType), BudgetCaps: []capEntry{},
			ChannelPriorities: []tierEntry{}}
		for _, c := range s.BudgetCaps[:min(len(s.BudgetCaps), maxListItems)] {
			item := capEntry{NetworkChannel: code(c.NetworkChannel)}
			if c.CapPercentMillis != nil {
				n, err := strconv.ParseInt(string(*c.CapPercentMillis), 10, 64)
				if err != nil {
					return nil, false
				}
				item.CapPercentMillis = &n
			}
			entry.BudgetCaps = append(entry.BudgetCaps, item)
		}
		for _, p := range s.ChannelPriorities[:min(len(s.ChannelPriorities), maxListItems)] {
			entry.ChannelPriorities = append(entry.ChannelPriorities, tierEntry{PriorityTier: code(p.PriorityTier),
				NetworkChannels: boundedList(p.NetworkChannels, maxCodeText)})
		}
		entries = append(entries, entry)
	}
	return entries, true
}

func networkSettingName(customerID string) string {
	return customerName(customerID) + budgetNetworkService
}

var (
	capSchema = object(schema{"network_channel": schema{"type": "string", "enum": networkChannelTypes},
		"cap_percent_millis": schema{"type": "integer", "minimum": 0, "maximum": maxCapMillis}}, "network_channel")
	tierSchema = object(schema{"priority_tier": schema{"type": "string", "enum": priorityTiers},
		"network_channels": schema{"type": "array", "maxItems": len(networkChannelTypes),
			"items": schema{"type": "string", "enum": networkChannelTypes}}}, "priority_tier")
	settingSchema = object(schema{"advertising_channel_type": schema{"type": "string", "enum": advertisingChannelTypes},
		"budget_caps":        schema{"type": "array", "maxItems": len(networkChannelTypes), "items": capSchema},
		"channel_priorities": schema{"type": "array", "maxItems": len(priorityTiers), "items": tierSchema}},
		"advertising_channel_type")

	networkEntriesSchema = arrayOf(object(schema{"advertising_channel_type": stringSchema,
		"budget_caps": arrayOf(object(schema{"network_channel": stringSchema, "cap_percent_millis": integerSchema})),
		"channel_priorities": arrayOf(object(schema{"priority_tier": stringSchema,
			"network_channels": arrayOf(stringSchema)}))}))

	networkOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"resource_name": stringSchema,
		"customer_budget_network_setting": object(schema{"resource_name": stringSchema,
			"budget_network_settings": networkEntriesSchema})},
		"customer_id", "validate_only")

	networkInputSchema = encoded(object(schema{"customer_id": customerIDArgument,
		"budget_network_settings": schema{"type": "array", "minItems": 1, "maxItems": len(advertisingChannelTypes),
			"items": settingSchema},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "budget_network_settings"))

	networkArguments = []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "budget_network_settings", Required: true, Description: "One to 12 entries, one per advertising channel " +
			"type (SEARCH, DISPLAY, SHOPPING, HOTEL, VIDEO, MULTI_CHANNEL, LOCAL, SMART, PERFORMANCE_MAX, " +
			"LOCAL_SERVICES, TRAVEL, DEMAND_GEN; Google refuses the types it does not support here). Each entry: " +
			"advertising_channel_type, budget_caps (per network channel OWNED_AND_OPERATED, SEARCH_PARTNER or " +
			"GOOGLE_DISPLAY at most once: cap_percent_millis 0 to 100000, in milli-percent of the budget, 100000 is " +
			"100 percent, 0 is a complete opt-out; only for channels of the secondary tier) and channel_priorities " +
			"(per tier TIER1, the primary tier, or TIER2: the network_channels in it; a channel in one tier only)"},
		validateOnlyArgument, responseContentTypeArgument}

	networkFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "resource_name", Description: "customers/ID/customerBudgetNetworkSettings, as Google answered; absent " +
			"after validate_only"},
		{Name: "customer_budget_network_setting", Description: "With MUTABLE_RESOURCE: the setting after the change, " +
			"as Google returns it"}}
)

const networkEffectNote = "These account defaults decide how the budget of every campaign budget that has no setting of " +
	"its own is spread over the network channels: which channels spend first (tier TIER1) and which only get what is " +
	"left (TIER2), and how many percent of the budget a secondary channel may spend at most. They change where " +
	"the budget is spent. Each call sets the whole " +
	"budget_network_settings: entries and caps that are left out are gone afterwards. The account must be allowlisted " +
	"by Google for budget network settings. The setting is read with reports.search on customer_budget_network_setting."

var networkExample = json.RawMessage(`{"customer_id":"1234567890","budget_network_settings":[{"advertising_channel_type":` +
	`"SEARCH","channel_priorities":[{"priority_tier":"TIER1","network_channels":["OWNED_AND_OPERATED"]},` +
	`{"priority_tier":"TIER2","network_channels":["SEARCH_PARTNER"]}],"budget_caps":[{"network_channel":` +
	`"SEARCH_PARTNER","cap_percent_millis":20000}]}],"validate_only":true}`)

var customerBudgetNetworkSettingsCreate = capability.Descriptor{
	ID: customerBudgetNetworkSettingsCreateID, Version: 1, Title: "Create the budget network settings of a Google Ads account",
	Description: "Create the default budget network settings of a Google Ads account " +
		"(CustomerBudgetNetworkSettingService.MutateCustomerBudgetNetworkSetting with a create operation): the order " +
		"in which the network channels of each advertising channel type spend, and caps on secondary channels. " +
		networkEffectNote + " Google refuses the creation when the account already has the setting; change it with " +
		"update." + mutateNote,
	Tags: []string{"google", "ads", "budgets", "networks", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent,
		"budget distribution settings of a Google Ads account"),
	InputSchema: networkInputSchema, OutputSchema: encoded(networkOutputSchema), Arguments: networkArguments,
	Fields:   networkFields,
	Examples: []capability.Example{{Description: "Validate a first setting without creating it", Arguments: networkExample}},
}

var customerBudgetNetworkSettingsUpdate = capability.Descriptor{
	ID: customerBudgetNetworkSettingsUpdateID, Version: 1, Title: "Change the budget network settings of a Google Ads account",
	Description: "Replace the default budget network settings of a Google Ads account " +
		"(CustomerBudgetNetworkSettingService.MutateCustomerBudgetNetworkSetting with an update operation and the " +
		"mask budget_network_settings): the order in which the network channels of each advertising channel type " +
		"spend, and caps on secondary channels. " + networkEffectNote + mutateNote,
	Tags: []string{"google", "ads", "budgets", "networks", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
		"budget distribution settings of a Google Ads account"),
	InputSchema: networkInputSchema, OutputSchema: encoded(networkOutputSchema), Arguments: networkArguments,
	Fields:   networkFields,
	Examples: []capability.Example{{Description: "Validate a replacement without changing anything", Arguments: networkExample}},
}

func handleBudgetNetworkCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return budgetNetworkChange(ctx, resolved, secrets, red, raw, true)
}

func handleBudgetNetworkUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return budgetNetworkChange(ctx, resolved, secrets, red, raw, false)
}

func budgetNetworkChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, create bool) (any, error) {
	op, tool := "update budget network settings", customerBudgetNetworkSettingsUpdateID
	if create {
		op, tool = "create budget network settings", customerBudgetNetworkSettingsCreateID
	}
	var args budgetNetworkArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	settings, err := args.settings()
	if err != nil {
		return nil, err
	}
	operation := map[string]any{"create": map[string]any{"budgetNetworkSettings": settings}}
	if !create {
		operation = map[string]any{"update": map[string]any{"resourceName": networkSettingName(args.CustomerID),
			"budgetNetworkSettings": settings}, "updateMask": "budgetNetworkSettings"}
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(args.CustomerID, false)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Result *struct {
			ResourceName string          `json:"resourceName"`
			Setting      json.RawMessage `json:"customerBudgetNetworkSetting"`
		} `json:"result"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: args.CustomerID, service: budgetNetworkService,
		body: mutateBody(operation, args.ValidateOnly, args.ResponseContentType)}, &answer); err != nil {
		return nil, err
	}
	out := budgetNetworkChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly}
	if answer.Result != nil {
		out.ResourceName = answer.Result.ResourceName
	}
	if args.ValidateOnly && out.ResourceName == "" {
		return out, nil
	}
	if out.ResourceName != networkSettingName(args.CustomerID) {
		return nil, unsure(provider.InvalidResponse(op, subject+" answered with a result for another account than the "+
			"one asked for"), true)
	}
	if len(answer.Result.Setting) > 0 && string(answer.Result.Setting) != "null" {
		setting, ok := networkResourceOf(answer.Result.Setting, args.CustomerID)
		if !ok {
			return nil, unsure(provider.InvalidResponse(op, subject+" answered with a setting that is not the one "+
				"asked for"), true)
		}
		out.CustomerBudgetNetworkSetting = setting
	}
	return out, nil
}

// budgetNetworkSummary phrases the change for the confirmation, from the arguments alone.
func budgetNetworkSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a budgetNetworkArguments
		_ = json.Unmarshal(raw, &a)
		var parts []string
		for _, s := range a.BudgetNetworkSettings {
			part := s.AdvertisingChannelType
			for _, p := range s.ChannelPriorities {
				part += " " + p.PriorityTier + "=" + strings.Join(p.NetworkChannels, "+")
			}
			for _, c := range s.BudgetCaps {
				if c.CapPercentMillis != nil {
					part += " cap " + c.NetworkChannel + "=" + strconv.FormatInt(*c.CapPercentMillis, 10) + "/1000%"
				}
			}
			parts = append(parts, part)
		}
		lead := verb
		if a.ValidateOnly {
			lead = "Only validate: " + verb
		}
		return lead + " the default budget network settings of Google Ads account " + a.CustomerID + " (" +
			strings.Join(parts, "; ") + "). This changes how the budget of every campaign budget without a setting " +
			"of its own is spread over the network channels."
	}
}
