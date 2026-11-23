package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	campaignCriteriaCreateID = Provider + ".campaign-criteria.create"
	campaignCriteriaUpdateID = Provider + ".campaign-criteria.update"
	campaignCriteriaRemoveID = Provider + ".campaign-criteria.remove"

	campaignCriteriaService    = "/campaignCriteria"
	campaignCriteriaCollection = "campaignCriteria"

	// maxCriteria is Qatlas's own bound per call; Google documents none for this service.
	maxCriteria = 50
)

const campaignCriteriaSensitivity = "targeting of the campaigns of a Google Ads account: locations, languages, audiences, " +
	"keywords, devices, schedules, bid adjustments"

// criterionPairPattern is the form of the ID of a criterion below a parent: the parent ID and the criterion ID.
var criterionPairPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}~[1-9][0-9]{0,18}$`)

// ownedCriterionName reports whether a name is customers/ID/<collection>/PARENT~CRITERION of exactly the account
// and returns the parent ID.
func ownedCriterionName(name, customerID, collection string) (string, bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+collection+"/")
	if !ok || !criterionPairPattern.MatchString(rest) {
		return "", false
	}
	parent, _, _ := strings.Cut(rest, "~")
	return parent, true
}

func criterionNameSchema(collection string) schema {
	return schema{"type": "string", "maxLength": 100,
		"pattern": `^customers/[0-9]{1,10}/` + collection + `/[0-9]{1,19}~[0-9]{1,19}$`}
}

var campaignCriterionTypes = baseCriterionTypes("campaign_criterion")

// campaignCriterionTree holds the members of a campaign criterion the tools offer. The members of a type and
// the campaign are fixed when the criterion is created.
var campaignCriterionTree = func() node {
	members := []node{
		createOnly(resourceNode("campaign", "Campaign of this account, customers/ID/campaigns/N", "campaigns")),
		createOnly(flagNode("negative", "true excludes what the criterion names; false or left out targets it. An excluded criterion has no bid_modifier and no status and cannot be changed afterwards")),
		{name: "bid_modifier", kind: kindNumber, bounded: true, low: 0, high: 10,
			desc: "Factor for the bids when the criterion matches, from 0.1 to 10.0; 0 opts out of a device type and is accepted for a device criterion only"},
		enumNode("status", "ENABLED or PAUSED; a criterion is removed with campaign-criteria.remove", []string{"ENABLED", "PAUSED"}),
	}
	for _, t := range campaignCriterionTypes {
		members = append(members, t.node)
	}
	return oneOf(objectNode("campaign_criterion", "", members...), typeNames(campaignCriterionTypes)...)
}()

// campaignCriterionOutputTree is the criterion Google returns with MUTABLE_RESOURCE.
var campaignCriterionOutputTree = func() node {
	t := campaignCriterionTree
	t.fields = slices.Clone(t.fields)
	// A webpage criterion also carries what Google measures: the share of pages it covers and sample addresses.
	for i, f := range t.fields {
		if f.name == "webpage" {
			f.fields = append(slices.Clone(f.fields), node{name: "coverage_percentage", kind: kindNumber},
				objectNode("sample", "", listOf(textNode("sample_urls", "", maxURLArgument), maxListItems)))
			t.fields[i] = f
		}
	}
	t.fields = append(t.fields, textNode("resource_name", "", maxNameArgument),
		wholeNode("criterion_id", "", 0, math64), textNode("display_name", "", maxNameArgument),
		enumNode("type", "", nil))
	return t
}()

type criteriaArguments struct {
	CustomerID          string   `json:"customer_id"`
	Criteria            []any    `json:"criteria"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readCriteriaArguments(op string, raw json.RawMessage) (criteriaArguments, error) {
	var a criteriaArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

func criteriaCount(n int) error {
	if n < 1 || n > maxCriteria {
		return badRequest(fmt.Sprintf("criteria must hold one to %d entries", maxCriteria))
	}
	return nil
}

// criterionCreateOperations checks every criterion to create and builds its operation. campaigns are the
// campaigns named, one per operation, for the check of the answer.
func criterionCreateOperations(customerID string, items []any) ([]any, []string, error) {
	if err := criteriaCount(len(items)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(items))
	campaigns := make([]string, 0, len(items))
	for i, item := range items {
		at := pathOfMember{snake: fmt.Sprintf("criteria[%d]", i)}
		in, ok := item.(map[string]any)
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		if _, found := in["campaign"]; !found {
			return nil, nil, badRequest(at.snake + ".campaign is required")
		}
		var given []criterionType
		for _, t := range campaignCriterionTypes {
			if _, ok := in[t.node.name]; ok {
				given = append(given, t)
			}
		}
		if len(given) != 1 {
			return nil, nil, badRequest(at.snake + " must name exactly one criterion type: " +
				strings.Join(typeNames(campaignCriterionTypes), ", "))
		}
		if negative, _ := in["negative"].(bool); negative {
			if _, ok := in["bid_modifier"]; ok {
				return nil, nil, badRequest(at.snake + ": an excluded criterion has no bid_modifier")
			}
			if _, ok := in["status"]; ok {
				return nil, nil, badRequest(at.snake + ": an excluded criterion has no status")
			}
		}
		if err := checkBidModifier(at.snake, in["bid_modifier"], given[0].node.name == "device"); err != nil {
			return nil, nil, err
		}
		typeIn, _ := in[given[0].node.name].(map[string]any)
		typeAt := at.snake + "." + given[0].node.name
		if typeIn != nil {
			if err := requireMembers(typeIn, typeAt, given[0].required...); err != nil {
				return nil, nil, err
			}
			if given[0].rules != nil {
				if err := given[0].rules(typeIn); err != nil {
					return nil, nil, err
				}
			}
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(campaignCriterionTree, in, at)
		if err != nil {
			return nil, nil, err
		}
		campaigns = append(campaigns, stringOf(in["campaign"]))
		ops = append(ops, map[string]any{"create": body})
	}
	return ops, campaigns, nil
}

// checkBidModifier holds the factor to the range Google documents: 0.1 to 10.0, and 0 for a device only.
func checkBidModifier(at string, value any, device bool) error {
	number, ok := value.(json.Number)
	if !ok {
		return nil
	}
	factor, err := number.Float64()
	if err != nil {
		return badRequest(at + ".bid_modifier must be a number")
	}
	if factor == 0 && device {
		return nil
	}
	if factor < 0.1 || factor > 10 {
		return badRequest(at + ".bid_modifier must be from 0.1 to 10.0, and 0 only for a device criterion")
	}
	return nil
}

// criterionUpdateOperations builds one update with a field mask per criterion. Google changes only the bid
// modifier and the status of a criterion.
func criterionUpdateOperations(customerID string, items []any) ([]any, []string, error) {
	if err := criteriaCount(len(items)); err != nil {
		return nil, nil, err
	}
	mutable := campaignCriterionTree.mutable()
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i, item := range items {
		at := pathOfMember{snake: fmt.Sprintf("criteria[%d]", i)}
		in, ok := item.(map[string]any)
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		name, _ := in["resource_name"].(string)
		if _, ok := ownedCriterionName(name, customerID, campaignCriteriaCollection); !ok {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) +
				"/campaignCriteria/CAMPAIGN~CRITERION of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("a criterion appears in criteria only once")
		}
		fields := map[string]any{}
		for key, value := range in {
			if key != "resource_name" {
				fields[key] = value
			}
		}
		if len(fields) == 0 {
			return nil, nil, badRequest(at.snake + " must give bid_modifier or status")
		}
		if err := checkBidModifier(at.snake, fields["bid_modifier"], true); err != nil {
			return nil, nil, err
		}
		b := builder{customerID: customerID}
		body, err := b.object(mutable, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")})
	}
	return ops, names, nil
}

func criterionRemoveOperations(customerID string, resourceNames []string) ([]any, error) {
	if err := criteriaCount(len(resourceNames)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if _, ok := ownedCriterionName(name, customerID, campaignCriteriaCollection); !ok {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/campaignCriteria/CAMPAIGN~CRITERION "+
				"of the account of customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(resourceNames[:i], name) {
			return nil, badRequest("a criterion appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	campaignCriterionResult struct {
		ResourceName      string         `json:"resource_name"`
		CampaignCriterion map[string]any `json:"campaign_criterion,omitempty"`
	}
	campaignCriteriaChanged struct {
		CustomerID   string                    `json:"customer_id"`
		ValidateOnly bool                      `json:"validate_only"`
		Results      []campaignCriterionResult `json:"results"`
	}
	campaignCriteriaAnswer struct {
		Results []struct {
			ResourceName      string          `json:"resourceName"`
			CampaignCriterion json.RawMessage `json:"campaignCriterion"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
)

// campaignCriteriaOf maps the results of a change. One result per operation, in the order sent, each a criterion
// of the account; for an update and a remove it is the criterion asked for, for a creation one of the campaign
// asked for. Anything else is an invalid answer with an unknown outcome, since the change may have been made.
func campaignCriteriaOf(op string, answer campaignCriteriaAnswer, customerID string, validateOnly bool, expected, campaigns []string,
	count int) (*campaignCriteriaChanged, error) {
	out := &campaignCriteriaChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []campaignCriterionResult{}}
	invalid := func(what string) error { return unsureResult(op, "answered with "+what) }
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, invalid("a partial failure although none was asked for")
	}
	if validateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != count {
		return nil, invalid("another number of results than operations")
	}
	for i, r := range answer.Results {
		parent, ok := ownedCriterionName(r.ResourceName, customerID, campaignCriteriaCollection)
		if !ok || (expected != nil && r.ResourceName != expected[i]) ||
			(campaigns != nil && customerName(customerID)+"/campaigns/"+parent != campaigns[i]) {
			return nil, invalid("a result for another criterion, campaign or account than the one asked for")
		}
		item := campaignCriterionResult{ResourceName: r.ResourceName}
		if len(r.CampaignCriterion) > 0 && string(r.CampaignCriterion) != "null" {
			raw, ok := decodeObject(r.CampaignCriterion)
			if ok {
				var members map[string]any
				if members, ok = outputMembers(campaignCriterionOutputTree.fields, raw, customerID); ok {
					name, _ := members["resource_name"].(string)
					ok = (name == "" || name == r.ResourceName) && constantsBound(campaignCriterionOutputTree, members)
					item.CampaignCriterion = members
				}
			}
			if !ok {
				return nil, invalid("a criterion that is not the one asked for")
			}
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- descriptors ---

func campaignCriteriaInputSchema(create bool) schema {
	var item schema
	if create {
		item = campaignCriterionTree.inputSchema()
		item["required"] = []string{"campaign"}
		delete(item, "description")
	} else {
		item = campaignCriterionTree.mutable().inputSchema()
		props := item["properties"].(schema)
		props["resource_name"] = criterionNameSchema(campaignCriteriaCollection)
		item["required"] = []string{"resource_name"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"criteria":      schema{"type": "array", "minItems": 1, "maxItems": maxCriteria, "items": item},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "criteria")
}

var campaignCriteriaOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "campaign_criterion": campaignCriterionOutputTree.outputSchema()},
		"resource_name"))}, "customer_id", "validate_only", "results")

var campaignCriteriaFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per criterion, in the order given, with its resource_name and, with " +
		"MUTABLE_RESOURCE, the criterion after the change with the members the tool offers; empty after validate_only"}}

const campaignCriteriaNote = " A criterion narrows or widens who sees the ads of a campaign, and a bid_modifier changes what a " +
	"click costs, so the call is confirmed per call. References in the arguments to the campaign, audiences, user " +
	"lists, interests and sets must belong to customer_id, and a temporary negative ID is refused; Google's own " +
	"constants (locations, languages, carriers, mobile devices, operating systems, topics, app categories) are " +
	"accepted only in their fixed form, such as geoTargetConstants/N. Language criteria are not accepted by Search campaigns, by AI Max for Search or by the " +
	"Search placements of Performance Max: Google refuses them there, so a campaign of these kinds shows the refusal " +
	"(validate_only finds it without a change), while existing language criteria stay readable and can be removed. " +
	"Whether another type applies to the channel of a campaign is Google's to decide in the same way. Addresses of " +
	"placements and webpage conditions are targets: Qatlas never calls them. " +
	"Up to 50 criteria per call, one atomic request without partial failure."

var campaignCriteriaCustomerArgument = campaignCustomerArgument

var campaignCriteriaCreate = capability.Descriptor{
	ID: campaignCriteriaCreateID, Version: 1, Title: "Create Google Ads campaign criteria",
	Description: "Create up to 50 campaign criteria in a Google Ads account (CampaignCriterionService.MutateCampaignCriteria " +
		"with create operations): Google applies all of them or none. Each entry names the campaign and exactly one " +
		"criterion type (keyword, mobile_app_category, mobile_application, location, device, proximity, language, " +
		"ip_block, content_label, carrier, user_interest, operating_system_version, mobile_device, location_group, " +
		"custom_affinity, custom_audience, combined_audience, age_range, gender, income_range, parental_status, " +
		"user_list, youtube_video, youtube_channel, topic, ad_schedule, life_event, extended_demographic, brand_list, " +
		"placement, webpage, keyword_theme, local_service_id, video_lineup, listing_scope) " +
		"with the members Google documents for it, and optionally negative (excludes), bid_modifier and status." +
		campaignCriteriaNote + " Not offered: the criterion type webpage_list, which Google does not make publicly " +
		"available, the feed item sets of location_group, the deprecated product dimensions of listing_scope, and what " +
		"Google fills itself." + mutateNote,
	Tags: []string{"google", "ads", "campaign-criteria", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, campaignCriteriaSensitivity),
	InputSchema: encoded(campaignCriteriaInputSchema(true)), OutputSchema: encoded(campaignCriteriaOutputSchema),
	Arguments: []capability.Argument{campaignCriteriaCustomerArgument,
		{Name: "criteria", Required: true, Description: "One to 50 criteria, each an object with campaign, one criterion " +
			"type object and optionally negative, bid_modifier and status"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: campaignCriteriaFields,
	Examples: []capability.Example{{Description: "Validate a location with a bid modifier without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","criteria":[{"campaign":"customers/1234567890/campaigns/21",` +
			`"location":{"geo_target_constant":"geoTargetConstants/2276"},"bid_modifier":1.2}],"validate_only":true}`)}},
}

var campaignCriteriaUpdate = capability.Descriptor{
	ID: campaignCriteriaUpdateID, Version: 1, Title: "Change Google Ads campaign criteria",
	Description: "Change up to 50 campaign criteria of a Google Ads account (CampaignCriterionService.MutateCampaignCriteria " +
		"with update operations and a field mask that names exactly the members given): Google applies all of them " +
		"or none. Google changes only bid_modifier and status of a criterion that targets; the type, the campaign and " +
		"negative are fixed, and an excluded criterion cannot be changed at all, so it is removed and created again." +
		campaignCriteriaNote + mutateNote,
	Tags: []string{"google", "ads", "campaign-criteria", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, campaignCriteriaSensitivity),
	InputSchema: encoded(campaignCriteriaInputSchema(false)), OutputSchema: encoded(campaignCriteriaOutputSchema),
	Arguments: []capability.Argument{campaignCriteriaCustomerArgument,
		{Name: "criteria", Required: true, Description: "One to 50 entries: resource_name of the criterion, " +
			"customers/ID/campaignCriteria/CAMPAIGN~CRITERION, and bid_modifier or status"}, validateOnlyArgument,
		responseContentTypeArgument},
	Fields: campaignCriteriaFields,
	Examples: []capability.Example{{Description: "Raise the bid for a criterion", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
		`"criteria":[{"resource_name":"customers/1234567890/campaignCriteria/21~7","bid_modifier":1.5}]}`)}},
}

var campaignCriteriaRemove = capability.Descriptor{
	ID: campaignCriteriaRemoveID, Version: 1, Title: "Remove Google Ads campaign criteria",
	Description: "Remove up to 50 campaign criteria of a Google Ads account (CampaignCriterionService.MutateCampaignCriteria " +
		"with remove operations): Google applies all of them or none. A removed criterion no longer targets or " +
		"excludes anything and cannot be restored, only created again; this is also how a language criterion of a " +
		"Search campaign is taken away. Resource names must be criteria of customer_id. Offers no " +
		"response_content_type: a removed criterion has no mutable resource to show." + mutateNote,
	Tags: []string{"google", "ads", "campaign-criteria", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, campaignCriteriaSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxCriteria,
			"items": criterionNameSchema(campaignCriteriaCollection)},
		"validate_only": validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(campaignCriteriaOutputSchema),
	Arguments: []capability.Argument{campaignCriteriaCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 50 criteria of this account, " +
			"customers/ID/campaignCriteria/CAMPAIGN~CRITERION"}, validateOnlyArgument},
	Fields: campaignCriteriaFields,
	Examples: []capability.Example{{Description: "Validate the removal of a criterion without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/campaignCriteria/21~7"],"validate_only":true}`)}},
}

// --- handlers ---

func handleCampaignCriteriaCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return campaignCriteriaChange(ctx, resolved, secrets, red, raw, "create campaign criteria", campaignCriteriaCreateID)
}

func handleCampaignCriteriaUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return campaignCriteriaChange(ctx, resolved, secrets, red, raw, "update campaign criteria", campaignCriteriaUpdateID)
}

func handleCampaignCriteriaRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return campaignCriteriaChange(ctx, resolved, secrets, red, raw, "remove campaign criteria", campaignCriteriaRemoveID)
}

func campaignCriteriaChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readCriteriaArguments(op, raw)
	if err != nil {
		return nil, err
	}
	conn, err := connectionOf(resolved)
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
	var expected, campaigns []string
	switch tool {
	case campaignCriteriaCreateID:
		ops, campaigns, err = criterionCreateOperations(a.CustomerID, a.Criteria)
	case campaignCriteriaUpdateID:
		ops, expected, err = criterionUpdateOperations(a.CustomerID, a.Criteria)
	default:
		ops, err = criterionRemoveOperations(a.CustomerID, a.ResourceNames)
		expected = a.ResourceNames
	}
	if err != nil {
		return nil, err
	}
	body := mutateOperationsBody(ops, a.ValidateOnly, a.ResponseContentType)
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer campaignCriteriaAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: campaignCriteriaService, body: body},
		&answer); err != nil {
		return nil, err
	}
	return campaignCriteriaOf(op, answer, a.CustomerID, a.ValidateOnly, expected, campaigns, len(ops))
}

// --- confirmation ---

// campaignCriteriaSummary phrases the change for the confirmation, from the arguments alone.
func campaignCriteriaSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readCriteriaArguments("", raw)
		verb := map[string]string{campaignCriteriaCreateID: "Create", campaignCriteriaUpdateID: "Change",
			campaignCriteriaRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		var parts []string
		if tool == campaignCriteriaRemoveID {
			parts = a.ResourceNames
		}
		for _, item := range a.Criteria {
			in, _ := item.(map[string]any)
			part := stringOf(in["resource_name"])
			var facts []string
			if campaign := stringOf(in["campaign"]); campaign != "" {
				facts = append(facts, "campaign "+campaign)
			}
			for _, t := range campaignCriterionTypes {
				if _, ok := in[t.node.name]; ok {
					part = t.node.name
				}
			}
			if negative, _ := in["negative"].(bool); negative {
				facts = append(facts, "excluded")
			}
			for _, key := range []string{"bid_modifier", "status"} {
				switch v := in[key].(type) {
				case string:
					facts = append(facts, key+" "+v)
				case json.Number:
					facts = append(facts, key+" "+v.String())
				}
			}
			parts = append(parts, part+" ("+strings.Join(facts, ", ")+")")
		}
		summary := verb + " campaign criteria of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + "."
		if tool == campaignCriteriaRemoveID {
			return summary + " A removed criterion cannot be restored."
		}
		return summary + " Criteria change who sees the ads, and a bid modifier changes what a click costs."
	}
}
