package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	biddingStrategiesCreateID = Provider + ".bidding-strategies.create"
	biddingStrategiesUpdateID = Provider + ".bidding-strategies.update"
	biddingStrategiesRemoveID = Provider + ".bidding-strategies.remove"

	biddingStrategiesService = "/biddingStrategies"
	biddingStrategiesTree    = "bidding_strategy"

	// maxBiddingStrategies is Qatlas's own bound per call; Google's reference names none.
	maxBiddingStrategies = 10
	// biddingStrategyNameMax is the documented bound of BiddingStrategy.name in UTF-8 bytes, after trimming.
	biddingStrategyNameMax = 255
)

// The schemes of the BiddingStrategy resource of v26 that the tools offer. enhanced_cpc is not offered: Google
// no longer lets it be created (ManualCpc with enhanced_cpc_enabled replaces it), and
// TargetSpend.target_spend_micros is deprecated. The members of the bidding schemes are the ones campaigns
// offer, so both tools check them alike.
var biddingStrategySchemes = []string{"maximize_conversions", "maximize_conversion_value", "target_cpa",
	"target_impression_share", "target_roas", "target_spend"}

func checkBiddingStrategyName(argument, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > biddingStrategyNameMax || strings.ContainsFunc(value, unicode.IsControl) {
		return badRequest(argument + " must be text of 1 to " + strconv.Itoa(biddingStrategyNameMax) +
			" bytes after trimming, without control characters")
	}
	return nil
}

func biddingStrategySchemeNodes() []node {
	var out []node
	for _, n := range biddingNodes() {
		if !slices.Contains(biddingStrategySchemes, n.name) {
			continue
		}
		// target_roas and target_impression_share have a member Google documents as required.
		if n.name != "target_roas" && n.name != "target_impression_share" {
			n.emptyOK = true
		}
		out = append(out, n)
	}
	return out
}

// biddingStrategyTree holds the members of a bidding strategy that the tools offer.
var biddingStrategyTree = func() node {
	name := textNode("name", "Name of the strategy, 1 to 255 bytes after trimming; unique in the account", biddingStrategyNameMax)
	name.check = checkBiddingStrategyName
	members := []node{
		name,
		createOnly(patternNode("currency_code", "ISO 4217 currency of the strategy; only a strategy of a manager account takes it, "+
			"a serving account always uses its own currency; fixed after creation", `^[A-Z]{3}$`, 3)),
		wholeNode("aligned_campaign_budget_id", "ID of the campaign budget of this account that the strategy is aligned with: "+
			"campaigns added to the strategy must then use that budget", 1, math64),
	}
	return oneOf(objectNode(biddingStrategiesTree, "", append(members, biddingStrategySchemeNodes()...)...), biddingStrategySchemes...)
}()

// biddingStrategyOutputTree is the strategy Google returns with MUTABLE_RESOURCE: the members the tools offer,
// the resource name, and the read-only members of the resource.
var biddingStrategyOutputTree = func() node {
	t := biddingStrategyTree
	t.fields = append(slices.Clone(t.fields),
		biddingStrategyResourceName,
		wholeNode("id", "", 0, math64), enumNode("status", "", nil), enumNode("type", "", nil),
		textNode("effective_currency_code", "", 3),
		wholeNode("campaign_count", "", 0, math64), wholeNode("non_removed_campaign_count", "", 0, math64))
	return t
}()

var biddingStrategyResourceName = resourceNode("resource_name", "Bidding strategy of this account, customers/ID/biddingStrategies/N",
	"biddingStrategies")

const biddingStrategySensitivity = "portfolio bidding strategies of a Google Ads account: names, target costs and returns, " +
	"bid limits, currency"

type biddingStrategyArguments struct {
	CustomerID          string   `json:"customer_id"`
	Strategies          []any    `json:"bidding_strategies"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readBiddingStrategyArguments(op string, raw json.RawMessage) (biddingStrategyArguments, error) {
	var a biddingStrategyArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

func biddingStrategyCount(argument string, n int) error {
	if n == 0 || n > maxBiddingStrategies {
		return badRequest(argument + " must hold one to " + strconv.Itoa(maxBiddingStrategies) + " bidding strategies")
	}
	return nil
}

// biddingStrategySchemeOf returns the scheme member of the strategy that was given, if any.
func biddingStrategySchemeOf(in map[string]any) (string, bool) {
	for _, name := range biddingStrategySchemes {
		if _, ok := in[name]; ok {
			return name, true
		}
	}
	return "", false
}

// biddingStrategyRequired holds the members of a scheme that Google documents as required on creation.
var biddingStrategyRequired = map[string]string{"target_roas": "target_roas", "target_impression_share": "cpc_bid_ceiling_micros"}

func biddingStrategyCreateOperations(customerID string, items []any) ([]any, error) {
	if err := biddingStrategyCount("bidding_strategies", len(items)); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("bidding_strategies[%d]", i)}
		if !ok {
			return nil, badRequest(at.snake + " must be an object")
		}
		if _, found := in["name"]; !found {
			return nil, badRequest(at.snake + ".name is required")
		}
		scheme, found := biddingStrategySchemeOf(in)
		if !found {
			return nil, badRequest(at.snake + " needs one of " + strings.Join(biddingStrategySchemes, ", "))
		}
		if member, needed := biddingStrategyRequired[scheme]; needed {
			if fields, _ := in[scheme].(map[string]any); fields != nil {
				if _, given := fields[member]; !given {
					return nil, badRequest(at.snake + "." + scheme + "." + member + " is required")
				}
			}
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(biddingStrategyTree, in, at)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"create": body})
	}
	return out, nil
}

func biddingStrategyUpdateOperations(customerID string, items []any) ([]any, []string, error) {
	if err := biddingStrategyCount("bidding_strategies", len(items)); err != nil {
		return nil, nil, err
	}
	tree := biddingStrategyTree.mutable()
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("bidding_strategies[%d]", i)}
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		name, _ := in["resource_name"].(string)
		if !ownedResource(name, customerID, "biddingStrategies") {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) +
				"/biddingStrategies/N of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("a bidding strategy appears in bidding_strategies only once")
		}
		fields := map[string]any{}
		for key, value := range in {
			if key != "resource_name" {
				fields[key] = value
			}
		}
		if len(fields) == 0 {
			return nil, nil, badRequest(at.snake + " must give at least one member to change")
		}
		b := builder{customerID: customerID}
		body, err := b.object(tree, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")})
	}
	return ops, names, nil
}

func biddingStrategyRemoveOperations(customerID string, resourceNames []string) ([]any, error) {
	if err := biddingStrategyCount("resource_names", len(resourceNames)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if !ownedResource(name, customerID, "biddingStrategies") {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/biddingStrategies/N of the account "+
				"of customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(resourceNames[:i], name) {
			return nil, badRequest("a bidding strategy appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	biddingStrategyResult struct {
		ResourceName    string         `json:"resource_name"`
		BiddingStrategy map[string]any `json:"bidding_strategy,omitempty"`
	}
	biddingStrategiesChanged struct {
		CustomerID   string                  `json:"customer_id"`
		ValidateOnly bool                    `json:"validate_only"`
		Results      []biddingStrategyResult `json:"results"`
	}
	biddingStrategyAnswer struct {
		Results []struct {
			ResourceName    string          `json:"resourceName"`
			BiddingStrategy json.RawMessage `json:"biddingStrategy"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
)

// biddingStrategiesOf maps the results of a change: one result per operation, in the order sent, each a
// strategy of the account, and for an update and a remove the one asked for. Anything else is an answer with an
// unknown outcome, since the change may have been made.
func biddingStrategiesOf(op string, answer biddingStrategyAnswer, customerID string, validateOnly bool,
	expected []string, count int) (*biddingStrategiesChanged, error) {
	out := &biddingStrategiesChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []biddingStrategyResult{}}
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(op, "answered with a partial failure although none was asked for")
	}
	if validateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != count {
		return nil, unsureResult(op, "answered with another number of results than operations")
	}
	for i, r := range answer.Results {
		if !ownedResource(r.ResourceName, customerID, "biddingStrategies") || (expected != nil && r.ResourceName != expected[i]) ||
			slices.ContainsFunc(out.Results, func(p biddingStrategyResult) bool { return p.ResourceName == r.ResourceName }) {
			return nil, unsureResult(op, "answered with a result for another bidding strategy or account than the one asked for")
		}
		item := biddingStrategyResult{ResourceName: r.ResourceName}
		if len(r.BiddingStrategy) > 0 && string(r.BiddingStrategy) != "null" {
			raw, ok := decodeObject(r.BiddingStrategy)
			if ok {
				var members map[string]any
				if members, ok = outputMembers(biddingStrategyOutputTree.fields, raw, customerID); ok {
					name, _ := members["resource_name"].(string)
					ok = name == "" || name == r.ResourceName
					item.BiddingStrategy = members
				}
			}
			if !ok {
				return nil, unsureResult(op, "answered with a bidding strategy that is not the one asked for")
			}
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- descriptors ---

func biddingStrategyInputSchema(create bool) schema {
	var item schema
	if create {
		item = biddingStrategyTree.inputSchema()
		delete(item, "description")
		item["required"] = []string{"name"}
		branches := make([]any, 0, len(biddingStrategySchemes))
		props := item["properties"].(schema)
		for _, name := range biddingStrategySchemes {
			branches = append(branches, schema{"required": []string{name}})
			if member, ok := biddingStrategyRequired[name]; ok {
				props[name].(schema)["required"] = []string{member}
			}
		}
		item["anyOf"] = branches
	} else {
		item = biddingStrategyTree.mutable().inputSchema()
		item["properties"].(schema)["resource_name"] = biddingStrategyResourceName.inputSchema()
		item["required"] = []string{"resource_name"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"bidding_strategies": schema{"type": "array", "minItems": 1, "maxItems": maxBiddingStrategies, "items": item},
		"validate_only":      validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "bidding_strategies")
}

var biddingStrategyOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema,
		"bidding_strategy": biddingStrategyOutputTree.outputSchema()}, "resource_name"))},
	"customer_id", "validate_only", "results")

var biddingStrategyFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per strategy, in the order given, with its resource_name and, with " +
		"MUTABLE_RESOURCE, the strategy after the change: the members the tools offer, id, status, type, " +
		"effective_currency_code, campaign_count and non_removed_campaign_count as Google returns them; empty " +
		"after validate_only"}}

var biddingStrategyCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer " +
	"ID of the account without hyphens; a target of the connection or an account below a manager target. A " +
	"manager account needs a manager target that names it"}

const biddingStrategyNote = "A portfolio bidding strategy sets the bids of every campaign attached to it, so its target " +
	"cost or return, its bid limits, and its scheme decide how much those campaigns spend; the call is confirmed " +
	"per call. Amounts are whole micros of the currency of the strategy (1,000,000 micros are one unit) and are " +
	"checked before anything is sent. A strategy has one scheme: maximize_conversions, " +
	"maximize_conversion_value, target_cpa, target_impression_share, target_roas or target_spend. Not offered: " +
	"enhanced_cpc, which Google no longer lets be created, target_spend.target_spend_micros, which is deprecated, " +
	"and the members Google fills itself. Up to 10 strategies per call, one atomic request without partial failure " +
	"(BiddingStrategyService.MutateBiddingStrategies)."

var biddingStrategiesCreate = capability.Descriptor{
	ID: biddingStrategiesCreateID, Version: 1, Title: "Create Google Ads bidding strategies",
	Description: "Create up to 10 portfolio bidding strategies in a Google Ads account. Each needs a name and exactly " +
		"one scheme with its members; currency_code is for strategies of a manager account only. " +
		biddingStrategyNote + mutateNote,
	Tags: []string{"google", "ads", "bidding-strategies", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, biddingStrategySensitivity),
	InputSchema: encoded(biddingStrategyInputSchema(true)), OutputSchema: encoded(biddingStrategyOutputSchema),
	Arguments: []capability.Argument{biddingStrategyCustomerArgument,
		{Name: "bidding_strategies", Required: true, Description: "One to 10 strategies, each with name (required), " +
			"exactly one scheme member (required) and optionally aligned_campaign_budget_id and currency_code. " +
			"target_roas needs its target_roas (0.01 to 1000) and target_impression_share its cpc_bid_ceiling_micros"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: biddingStrategyFields,
	Examples: []capability.Example{{Description: "Validate a target CPA strategy without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","bidding_strategies":[{"name":"Portfolio CPA",` +
			`"target_cpa":{"target_cpa_micros":2000000,"cpc_bid_ceiling_micros":900000}}],"validate_only":true}`)}},
}

var biddingStrategiesUpdate = capability.Descriptor{
	ID: biddingStrategiesUpdateID, Version: 1, Title: "Change Google Ads bidding strategies",
	Description: "Change up to 10 portfolio bidding strategies of a Google Ads account (an update with a field mask " +
		"that names exactly the members given). Each entry names the strategy by resource_name and the members to " +
		"change, nested as the resource nests them. Giving a scheme member sets the members of that scheme given; " +
		"the currency of a strategy is fixed after creation. " + biddingStrategyNote + mutateNote,
	Tags: []string{"google", "ads", "bidding-strategies", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, biddingStrategySensitivity),
	InputSchema: encoded(biddingStrategyInputSchema(false)), OutputSchema: encoded(biddingStrategyOutputSchema),
	Arguments: []capability.Argument{biddingStrategyCustomerArgument,
		{Name: "bidding_strategies", Required: true, Description: "One to 10 entries: resource_name of the strategy " +
			"and at least one member to change; at most one scheme member per entry"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: biddingStrategyFields,
	Examples: []capability.Example{{Description: "Raise the target CPA of a strategy",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","bidding_strategies":[{"resource_name":` +
			`"customers/1234567890/biddingStrategies/31","target_cpa":{"target_cpa_micros":2500000}}]}`)}},
}

var biddingStrategiesRemove = capability.Descriptor{
	ID: biddingStrategiesRemoveID, Version: 1, Title: "Remove Google Ads bidding strategies",
	Description: "Remove up to 10 portfolio bidding strategies of a Google Ads account (remove operations of " +
		"BiddingStrategyService.MutateBiddingStrategies): Google applies all of them or none. A removed strategy " +
		"cannot be used again, and Google decides what happens to campaigns still attached to it. Resource names " +
		"must be strategies of customer_id. Offers no response_content_type: a removed strategy has no mutable " +
		"resource to show." + mutateNote,
	Tags: []string{"google", "ads", "bidding-strategies", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, biddingStrategySensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxBiddingStrategies, "uniqueItems": true,
			"items": biddingStrategyResourceName.inputSchema()},
		"validate_only": validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(biddingStrategyOutputSchema),
	Arguments: []capability.Argument{biddingStrategyCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 10 strategies of this account, " +
			"customers/ID/biddingStrategies/N, each once"}, validateOnlyArgument},
	Fields: biddingStrategyFields,
	Examples: []capability.Example{{Description: "Validate the removal of a strategy without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/biddingStrategies/31"],` +
			`"validate_only":true}`)}},
}

// --- handlers ---

func handleBiddingStrategiesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return biddingStrategiesChange(ctx, resolved, secrets, red, raw, "create bidding strategies", biddingStrategiesCreateID)
}

func handleBiddingStrategiesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return biddingStrategiesChange(ctx, resolved, secrets, red, raw, "update bidding strategies", biddingStrategiesUpdateID)
}

func handleBiddingStrategiesRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return biddingStrategiesChange(ctx, resolved, secrets, red, raw, "remove bidding strategies", biddingStrategiesRemoveID)
}

func biddingStrategiesChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readBiddingStrategyArguments(op, raw)
	if err != nil {
		return nil, err
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	var ops []any
	var expected []string
	switch tool {
	case biddingStrategiesCreateID:
		ops, err = biddingStrategyCreateOperations(a.CustomerID, a.Strategies)
	case biddingStrategiesUpdateID:
		ops, expected, err = biddingStrategyUpdateOperations(a.CustomerID, a.Strategies)
	default:
		ops, err = biddingStrategyRemoveOperations(a.CustomerID, a.ResourceNames)
		expected = a.ResourceNames
	}
	if err != nil {
		return nil, err
	}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer biddingStrategyAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: biddingStrategiesService,
		body: mutateOperationsBody(ops, a.ValidateOnly, a.ResponseContentType)}, &answer); err != nil {
		return nil, err
	}
	return biddingStrategiesOf(op, answer, a.CustomerID, a.ValidateOnly, expected, len(ops))
}

// --- confirmation ---

// biddingStrategySummary phrases the change for the confirmation, from the arguments alone: the name, the
// scheme, and the amounts that decide the spend.
func biddingStrategySummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readBiddingStrategyArguments("", raw)
		verb := map[string]string{biddingStrategiesCreateID: "Create", biddingStrategiesUpdateID: "Change",
			biddingStrategiesRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		var parts []string
		for _, name := range a.ResourceNames[:min(len(a.ResourceNames), maxBiddingStrategies)] {
			parts = append(parts, boundedText(name, maxNameArgument))
		}
		for _, item := range a.Strategies[:min(len(a.Strategies), maxBiddingStrategies)] {
			in, _ := item.(map[string]any)
			part := strconv.Quote(boundedText(stringOf(in["name"]), biddingStrategyNameMax*4))
			if tool == biddingStrategiesUpdateID {
				part = boundedText(stringOf(in["resource_name"]), maxNameArgument)
			}
			var facts []string
			var keys []string
			for key := range in {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				switch value := in[key].(type) {
				case map[string]any:
					var inner []string
					for k, v := range value {
						inner = append(inner, boundedText(k, maxCodeText)+" "+boundedText(fmt.Sprint(v), maxCodeText))
					}
					slices.Sort(inner)
					facts = append(facts, boundedText(key, maxCodeText)+" ("+strings.Join(inner, ", ")+")")
				case json.Number:
					facts = append(facts, boundedText(key, maxCodeText)+" "+boundedText(value.String(), maxCodeText))
				}
			}
			if len(facts) > 0 {
				part += " (" + strings.Join(facts, ", ") + ")"
			}
			parts = append(parts, part)
		}
		summary := verb + " bidding strategies of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + "."
		if tool == biddingStrategiesRemoveID {
			return summary + " A removed strategy cannot be used again."
		}
		return summary + " The strategy sets the bids of the campaigns attached to it, so it decides how much they spend."
	}
}
