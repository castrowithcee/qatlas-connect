package googleads

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	campaignBidModifiersCreateID = Provider + ".campaign-bid-modifiers.create"
	campaignBidModifiersUpdateID = Provider + ".campaign-bid-modifiers.update"
	campaignBidModifiersRemoveID = Provider + ".campaign-bid-modifiers.remove"

	campaignBidModifiersService    = "/campaignBidModifiers"
	campaignBidModifiersCollection = "campaignBidModifiers"
)

// A campaign bid modifier is named customers/ID/campaignBidModifiers/CAMPAIGN_ID~CRITERION_ID. Its criterion is
// the interaction type, which v26 documents with one value, CALLS, for Search campaigns; the reference says
// nothing about a range of bid_modifier, so the only check of the number is that it is finite and not negative.
// The reference marks campaign as output only although a create has no other way to name the campaign; the
// create sends it, built from the checked ID, and Google's validation is the judge.

var cbmInteractionTypes = enumOf("campaign_bid_modifier.interaction_type.type")

type (
	cbmEntry struct {
		CampaignID      string   `json:"campaign_id"`
		CriterionID     string   `json:"criterion_id"`
		InteractionType string   `json:"interaction_type"`
		BidModifier     *float64 `json:"bid_modifier"`
	}
	cbmArguments struct {
		CustomerID          string     `json:"customer_id"`
		Entries             []cbmEntry `json:"campaign_bid_modifiers"`
		ValidateOnly        bool       `json:"validate_only"`
		ResponseContentType string     `json:"response_content_type"`
	}
)

func cbmName(customerID, campaignID, criterionID string) string {
	return customerName(customerID) + "/" + campaignBidModifiersCollection + "/" + campaignID + "~" + criterionID
}

func cbmCampaignName(customerID, campaignID string) string {
	return customerName(customerID) + "/campaigns/" + campaignID
}

// cbmIDsOfName returns the campaign and criterion IDs of a resource name that belongs to the account.
func cbmIDsOfName(name, customerID string) (campaignID, criterionID string, ok bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+campaignBidModifiersCollection+"/")
	if !ok {
		return "", "", false
	}
	campaignID, criterionID, ok = strings.Cut(rest, "~")
	if !ok || cbmCheckID(campaignID) != nil || cbmCheckID(criterionID) != nil {
		return "", "", false
	}
	return campaignID, criterionID, true
}

func cbmCheckID(value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil || !numericIDPattern.MatchString(value) {
		return badRequest("an ID must be a positive number")
	}
	return nil
}

func cbmCheckCount(n int) error {
	if n < 1 || n > maxListOperations {
		return badRequest("campaign_bid_modifiers must hold 1 to " + strconv.Itoa(maxListOperations) + " entries")
	}
	return nil
}

func cbmCheckBid(value *float64) error {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return badRequest("bid_modifier must be a number of at least 0, the factor that multiplies the bid")
	}
	return nil
}

func cbmCheckHead(a cbmArguments) error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return err
	}
	return cbmCheckCount(len(a.Entries))
}

func cbmCheckCampaign(e cbmEntry) error {
	if err := cbmCheckID(e.CampaignID); err != nil {
		return badRequest("campaign_id must be the ID of an existing campaign, a positive number")
	}
	return nil
}

func cbmCheckCriterion(e cbmEntry) error {
	if err := cbmCheckID(e.CriterionID); err != nil {
		return badRequest("criterion_id must be the criterion ID of an existing campaign bid modifier, a positive number")
	}
	return nil
}

// operations builds the operations of a call: tool names the kind. want holds the resource names an update or a
// remove names, and campaigns the campaign of each creation, in order.
func (a cbmArguments) operations(tool string) (ops []any, want []string, err error) {
	if err := cbmCheckHead(a); err != nil {
		return nil, nil, err
	}
	var seen []string
	for _, e := range a.Entries {
		if err := cbmCheckCampaign(e); err != nil {
			return nil, nil, err
		}
		if tool == campaignBidModifiersCreateID {
			if !slices.Contains(cbmInteractionTypes, e.InteractionType) {
				return nil, nil, badRequest("interaction_type must be " + strings.Join(cbmInteractionTypes, " or "))
			}
			if err := cbmCheckBid(e.BidModifier); err != nil {
				return nil, nil, err
			}
			if e.CriterionID != "" {
				return nil, nil, badRequest("a new campaign bid modifier takes no criterion_id")
			}
			key := e.CampaignID + "/" + e.InteractionType
			if slices.Contains(seen, key) {
				return nil, nil, badRequest("campaign_bid_modifiers names each campaign and interaction_type at most once")
			}
			seen = append(seen, key)
			want = append(want, cbmCampaignName(a.CustomerID, e.CampaignID))
			ops = append(ops, map[string]any{"create": map[string]any{
				"campaign":        cbmCampaignName(a.CustomerID, e.CampaignID),
				"interactionType": map[string]any{"type": e.InteractionType},
				"bidModifier":     *e.BidModifier}})
			continue
		}
		if err := cbmCheckCriterion(e); err != nil {
			return nil, nil, err
		}
		name := cbmName(a.CustomerID, e.CampaignID, e.CriterionID)
		if slices.Contains(want, name) {
			return nil, nil, badRequest("campaign_bid_modifiers names each campaign bid modifier at most once")
		}
		want = append(want, name)
		if tool == campaignBidModifiersRemoveID {
			ops = append(ops, map[string]any{"remove": name})
			continue
		}
		if err := cbmCheckBid(e.BidModifier); err != nil {
			return nil, nil, err
		}
		ops = append(ops, map[string]any{"update": map[string]any{"resourceName": name, "bidModifier": *e.BidModifier},
			"updateMask": "bidModifier"})
	}
	return ops, want, nil
}

// --- results ---

type (
	cbmObject struct {
		ResourceName    string   `json:"resource_name"`
		Campaign        string   `json:"campaign,omitempty"`
		CriterionID     string   `json:"criterion_id,omitempty"`
		InteractionType string   `json:"interaction_type,omitempty"`
		BidModifier     *float64 `json:"bid_modifier,omitempty"`
	}
	cbmMutated struct {
		CampaignID          string     `json:"campaign_id"`
		CriterionID         string     `json:"criterion_id"`
		ResourceName        string     `json:"resource_name"`
		CampaignBidModifier *cbmObject `json:"campaign_bid_modifier,omitempty"`
	}
	cbmChanged struct {
		CustomerID   string       `json:"customer_id"`
		ValidateOnly bool         `json:"validate_only"`
		Results      []cbmMutated `json:"results"`
	}
	cbmAnswer struct {
		Results []struct {
			ResourceName        string          `json:"resourceName"`
			CampaignBidModifier json.RawMessage `json:"campaignBidModifier"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
	cbmWire struct {
		ResourceName    string   `json:"resourceName"`
		Campaign        string   `json:"campaign"`
		CriterionID     *wireInt `json:"criterionId"`
		BidModifier     *float64 `json:"bidModifier"`
		InteractionType *struct {
			Type string `json:"type"`
		} `json:"interactionType"`
	}
)

func cbmObjectOf(raw json.RawMessage, customerID, name, campaignID, criterionID string) (*cbmObject, bool) {
	var w cbmWire
	if json.Unmarshal(raw, &w) != nil || w.ResourceName != name {
		return nil, false
	}
	out := &cbmObject{ResourceName: name}
	if w.Campaign != "" {
		if w.Campaign != cbmCampaignName(customerID, campaignID) {
			return nil, false
		}
		out.Campaign = w.Campaign
	}
	if w.CriterionID != nil {
		if string(*w.CriterionID) != criterionID {
			return nil, false
		}
		out.CriterionID = criterionID
	}
	if w.BidModifier != nil {
		if math.IsNaN(*w.BidModifier) || math.IsInf(*w.BidModifier, 0) {
			return nil, false
		}
		out.BidModifier = w.BidModifier
	}
	if w.InteractionType != nil {
		out.InteractionType = code(w.InteractionType.Type)
	}
	return out, true
}

// cbmResults binds the answer to the checked account. An update and a remove must answer exactly the names
// asked for, in order; a create must answer one distinct modifier of the campaign asked for per entry. Anything
// else leaves the outcome open, since the request was sent.
func cbmResults(op, tool, customerID string, answer cbmAnswer, a cbmArguments, want []string) ([]cbmMutated, error) {
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(op, "answered with a partial failure although the request asked for none")
	}
	out := []cbmMutated{}
	if a.ValidateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != len(a.Entries) {
		return nil, unsureResult(op, "answered with another number of results than the operations asked for")
	}
	for i, r := range answer.Results {
		campaignID, criterionID, ok := cbmIDsOfName(r.ResourceName, customerID)
		if !ok {
			return nil, unsureResult(op, "answered with a result for another object than the one asked for")
		}
		if tool == campaignBidModifiersCreateID {
			ok = campaignID == a.Entries[i].CampaignID
		} else {
			ok = r.ResourceName == want[i]
		}
		if !ok {
			return nil, unsureResult(op, "answered with a result for another campaign bid modifier than the one asked for")
		}
		for _, previous := range out {
			if previous.ResourceName == r.ResourceName {
				return nil, unsureResult(op, "answered with the same campaign bid modifier twice")
			}
		}
		item := cbmMutated{CampaignID: campaignID, CriterionID: criterionID, ResourceName: r.ResourceName}
		if len(r.CampaignBidModifier) > 0 && string(r.CampaignBidModifier) != "null" {
			object, ok := cbmObjectOf(r.CampaignBidModifier, customerID, r.ResourceName, campaignID, criterionID)
			if !ok {
				return nil, unsureResult(op, "answered with a campaign bid modifier that is not the one asked for")
			}
			item.CampaignBidModifier = object
		}
		out = append(out, item)
	}
	return out, nil
}

// --- descriptors ---

var (
	cbmIDSchema  = schema{"type": "string", "pattern": numericIDPattern.String()}
	cbmBidSchema = schema{"type": "number", "minimum": 0}

	cbmOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"campaign_id": stringSchema, "criterion_id": stringSchema,
			"resource_name": stringSchema, "campaign_bid_modifier": object(schema{"resource_name": stringSchema,
				"campaign": stringSchema, "criterion_id": stringSchema, "interaction_type": stringSchema,
				"bid_modifier": numberSchema}, "resource_name")},
			"campaign_id", "criterion_id", "resource_name"))}, "customer_id", "validate_only", "results")
)

const cbmSensitivity = "bid adjustments of the campaigns of a Google Ads account, which change what the campaigns " +
	"spend; no data of other people"

const cbmNote = " A campaign bid modifier multiplies the bids of one campaign when its criterion matches: 1.5 raises " +
	"them by half, 0.5 halves them, 0 opts the criterion out. Google documents one criterion for the campaign " +
	"level, the interaction type CALLS, which only Search campaigns support; device, location and the other " +
	"criteria are campaign criteria, not part of this tool. Smart bidding can ignore or reinterpret a modifier. " +
	"A change moves the spend of a running campaign, so it is confirmed per call. The request goes to " +
	"CampaignBidModifierService.MutateCampaignBidModifiers with up to 10 operations (Qatlas's own bound), atomic " +
	"and without partial_failure: Google applies all of them or none. The campaign is named by campaign_id, and " +
	"the resource names are built from customer_id, campaign_id and criterion_id, never taken from a caller."

var cbmFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per entry, in the order of the arguments: campaign_id, criterion_id, resource_name " +
		"(customers/ID/campaignBidModifiers/CAMPAIGN_ID~CRITERION_ID, as Google answered, always of the account of " +
		"the call), and with MUTABLE_RESOURCE the modifier after the change (resource_name, campaign, criterion_id, " +
		"interaction_type, bid_modifier) as Google returns it. Empty after validate_only"},
}

var cbmCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit " +
	"customer ID of the account without hyphens; a target of the connection or an account below a manager target. " +
	"A manager account needs a manager target that names it"}

var campaignBidModifiersCreate = capability.Descriptor{
	ID: campaignBidModifiersCreateID, Version: 1, Title: "Create campaign bid modifiers in a Google Ads account",
	Description: "Create one to 10 campaign bid modifiers: for each entry a campaign (campaign_id), the " +
		"interaction_type CALLS and the bid_modifier factor." + cbmNote + mutateNote,
	Tags: []string{"google", "ads", "bid-modifiers", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, cbmSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_bid_modifiers": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"campaign_id": cbmIDSchema,
				"interaction_type": schema{"type": "string", "enum": cbmInteractionTypes},
				"bid_modifier":     cbmBidSchema}, "campaign_id", "interaction_type", "bid_modifier")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "campaign_bid_modifiers")),
	OutputSchema: encoded(cbmOutputSchema),
	Arguments: []capability.Argument{cbmCustomerArgument,
		{Name: "campaign_bid_modifiers", Required: true, Description: "1 to 10 entries, each with campaign_id (the ID of " +
			"an existing campaign of the account, as reports.search returns it), interaction_type (CALLS) and " +
			"bid_modifier (a number of at least 0). A campaign and interaction type appears once"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: cbmFields,
	Examples: []capability.Example{{Description: "Validate a bid raise of 20 percent for calls without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_bid_modifiers":[{"campaign_id":"21",` +
			`"interaction_type":"CALLS","bid_modifier":1.2}],"validate_only":true}`)}},
}

var campaignBidModifiersUpdate = capability.Descriptor{
	ID: campaignBidModifiersUpdateID, Version: 1, Title: "Change campaign bid modifiers of a Google Ads account",
	Description: "Change the bid_modifier factor of one to 10 existing campaign bid modifiers (an update with the " +
		"field mask bidModifier): the criterion of a modifier cannot change, so only the factor is offered." +
		cbmNote + mutateNote,
	Tags: []string{"google", "ads", "bid-modifiers", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, cbmSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_bid_modifiers": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"campaign_id": cbmIDSchema, "criterion_id": cbmIDSchema,
				"bid_modifier": cbmBidSchema}, "campaign_id", "criterion_id", "bid_modifier")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "campaign_bid_modifiers")),
	OutputSchema: encoded(cbmOutputSchema),
	Arguments: []capability.Argument{cbmCustomerArgument,
		{Name: "campaign_bid_modifiers", Required: true, Description: "1 to 10 entries, each with campaign_id, " +
			"criterion_id (both as reports.search returns them for campaign_bid_modifier, or as create answered) and " +
			"the new bid_modifier (a number of at least 0). Each modifier appears once"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: cbmFields,
	Examples: []capability.Example{{Description: "Halve the bids of calls for a campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_bid_modifiers":[{"campaign_id":"21",` +
			`"criterion_id":"3","bid_modifier":0.5}]}`)}},
}

var campaignBidModifiersRemove = capability.Descriptor{
	ID: campaignBidModifiersRemoveID, Version: 1, Title: "Remove campaign bid modifiers from a Google Ads account",
	Description: "Remove one to 10 existing campaign bid modifiers (a remove of " +
		"customers/ID/campaignBidModifiers/CAMPAIGN_ID~CRITERION_ID, built from customer_id, campaign_id and " +
		"criterion_id): the bids of the campaign no longer carry the factor." + cbmNote + mutateNote,
	Tags: []string{"google", "ads", "bid-modifiers", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, cbmSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_bid_modifiers": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"campaign_id": cbmIDSchema, "criterion_id": cbmIDSchema},
				"campaign_id", "criterion_id")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "campaign_bid_modifiers")),
	OutputSchema: encoded(cbmOutputSchema),
	Arguments: []capability.Argument{cbmCustomerArgument,
		{Name: "campaign_bid_modifiers", Required: true, Description: "1 to 10 entries, each with campaign_id and " +
			"criterion_id of an existing campaign bid modifier, as reports.search returns them. Each modifier appears once"},
		validateOnlyArgument},
	Fields: cbmFields,
	Examples: []capability.Example{{Description: "Validate the removal of a modifier without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_bid_modifiers":[{"campaign_id":"21",` +
			`"criterion_id":"3"}],"validate_only":true}`)}},
}

// --- handlers ---

func handleCampaignBidModifiersCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return cbmChange(ctx, resolved, secrets, red, raw, "create campaign bid modifiers", campaignBidModifiersCreateID)
}

func handleCampaignBidModifiersUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return cbmChange(ctx, resolved, secrets, red, raw, "update campaign bid modifiers", campaignBidModifiersUpdateID)
}

func handleCampaignBidModifiersRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return cbmChange(ctx, resolved, secrets, red, raw, "remove campaign bid modifiers", campaignBidModifiersRemoveID)
}

func cbmChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	var a cbmArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	ops, want, err := a.operations(tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	content := a.ResponseContentType
	if tool == campaignBidModifiersRemoveID {
		content = ""
	}
	var answer cbmAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: campaignBidModifiersService,
		body: mutateOperationsBody(ops, a.ValidateOnly, content)}, &answer); err != nil {
		return nil, err
	}
	results, err := cbmResults(op, tool, a.CustomerID, answer, a, want)
	if err != nil {
		return nil, err
	}
	return cbmChanged{CustomerID: a.CustomerID, ValidateOnly: a.ValidateOnly, Results: results}, nil
}

// --- confirmation ---

// cbmSummary phrases the change for the confirmation, from the arguments alone. IDs are cut so that a hostile
// value cannot fill the text.
func cbmSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a cbmArguments
		_ = json.Unmarshal(raw, &a)
		var parts []string
		for _, e := range a.Entries[:min(len(a.Entries), 10)] {
			part := "campaign " + boundedText(e.CampaignID, maxCodeText)
			if e.CriterionID != "" {
				part += " modifier " + boundedText(e.CriterionID, maxCodeText)
			}
			if e.InteractionType != "" {
				part += " " + boundedText(e.InteractionType, maxCodeText)
			}
			if e.BidModifier != nil {
				part += " factor " + strconv.FormatFloat(*e.BidModifier, 'g', -1, 64)
			}
			parts = append(parts, part)
		}
		more := ""
		if len(a.Entries) > len(parts) {
			more = " and " + strconv.Itoa(len(a.Entries)-len(parts)) + " more"
		}
		verb, tail := "Create", " in"
		switch tool {
		case campaignBidModifiersUpdateID:
			verb, tail = "Change", " of"
		case campaignBidModifiersRemoveID:
			verb, tail = "Remove", " from"
		}
		lead := ""
		if a.ValidateOnly {
			lead = "Only validate: "
		}
		text := lead + verb + " " + strconv.Itoa(len(a.Entries)) + " campaign bid modifier(s)" + tail +
			" Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + more + "."
		if !a.ValidateOnly {
			text += " This changes what the campaigns spend."
		}
		return text
	}
}
