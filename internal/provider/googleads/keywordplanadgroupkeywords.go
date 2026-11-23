package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	keywordPlanAdGroupKeywordsCreateID = Provider + ".keyword-plan-ad-group-keywords.create"
	keywordPlanAdGroupKeywordsUpdateID = Provider + ".keyword-plan-ad-group-keywords.update"
	keywordPlanAdGroupKeywordsRemoveID = Provider + ".keyword-plan-ad-group-keywords.remove"

	keywordPlanAdGroupKeywordsService = "/keywordPlanAdGroupKeywords"
)

// Bounds of KeywordPlanAdGroupKeyword as the v26 reference documents them: positive and negative keywords are
// supported, negative is immutable, and cpc_bid_micros does not apply to a negative keyword. The reference marks
// no field required and states no text length or word count. MutateKeywordPlanAdGroupKeywordsRequest has no
// response_content_type and its result holds the resource name only.
const (
	// keywordPlanAdGroupKeywordMax is Qatlas's own cap on the text, since the reference states none.
	keywordPlanAdGroupKeywordMax = 80
	keywordPlanAdGroupKeywordCap = "no more than 10000 positive keywords and 1000 negative keywords per plan, the " +
		"negative ones counted with the campaign negative keywords"
)

var keywordPlanAdGroupKeywordMatchTypes = []string{"BROAD", "EXACT", "PHRASE"}

// keywordPlanAdGroupKeywordName is the resource name of a plan keyword, built from checked IDs alone.
func keywordPlanAdGroupKeywordName(customerID, id string) string {
	return customerName(customerID) + "/keywordPlanAdGroupKeywords/" + id
}

func keywordPlanAdGroupKeywordsCheckText(value string) error {
	n := utf8.RuneCountInString(value)
	if n < 1 || n > keywordPlanAdGroupKeywordMax || strings.TrimSpace(value) == "" {
		return badRequest("text must be 1 to " + strconv.Itoa(keywordPlanAdGroupKeywordMax) + " characters and not blank")
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return badRequest("text must not hold control characters")
		}
	}
	return nil
}

func keywordPlanAdGroupKeywordsCheckMatchType(value string) error {
	if !slices.Contains(keywordPlanAdGroupKeywordMatchTypes, value) {
		return badRequest("match_type must be one of " + strings.Join(keywordPlanAdGroupKeywordMatchTypes, ", "))
	}
	return nil
}

// --- arguments ---

type (
	keywordPlanAdGroupKeywordNew struct {
		KeywordPlanAdGroupID string       `json:"keyword_plan_ad_group_id"`
		Text                 string       `json:"text"`
		MatchType            string       `json:"match_type"`
		Negative             bool         `json:"negative"`
		CPCBidMicros         *json.Number `json:"cpc_bid_micros"`
	}
	keywordPlanAdGroupKeywordEdit struct {
		KeywordPlanAdGroupKeywordID string       `json:"keyword_plan_ad_group_keyword_id"`
		Text                        *string      `json:"text"`
		MatchType                   *string      `json:"match_type"`
		CPCBidMicros                *json.Number `json:"cpc_bid_micros"`
	}
	keywordPlanAdGroupKeywordsCreateArguments struct {
		CustomerID                 string                         `json:"customer_id"`
		KeywordPlanAdGroupKeywords []keywordPlanAdGroupKeywordNew `json:"keyword_plan_ad_group_keywords"`
		ValidateOnly               bool                           `json:"validate_only"`
	}
	keywordPlanAdGroupKeywordsUpdateArguments struct {
		CustomerID                 string                          `json:"customer_id"`
		KeywordPlanAdGroupKeywords []keywordPlanAdGroupKeywordEdit `json:"keyword_plan_ad_group_keywords"`
		ValidateOnly               bool                            `json:"validate_only"`
	}
	keywordPlanAdGroupKeywordsRemoveArguments struct {
		CustomerID                   string   `json:"customer_id"`
		KeywordPlanAdGroupKeywordIDs []string `json:"keyword_plan_ad_group_keyword_ids"`
		ValidateOnly                 bool     `json:"validate_only"`
	}
)

func (a keywordPlanAdGroupKeywordsCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_ad_group_keywords", len(a.KeywordPlanAdGroupKeywords)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanAdGroupKeywords))
	for _, k := range a.KeywordPlanAdGroupKeywords {
		if !numericIDPattern.MatchString(k.KeywordPlanAdGroupID) {
			return nil, badRequest("keyword_plan_ad_group_id must be the ID of an existing keyword plan ad group, a positive number")
		}
		if err := keywordPlanAdGroupKeywordsCheckText(k.Text); err != nil {
			return nil, err
		}
		if err := keywordPlanAdGroupKeywordsCheckMatchType(k.MatchType); err != nil {
			return nil, err
		}
		create := map[string]any{"keywordPlanAdGroup": keywordPlanAdGroupName(a.CustomerID, k.KeywordPlanAdGroupID),
			"text": k.Text, "matchType": k.MatchType, "negative": k.Negative}
		if k.CPCBidMicros != nil {
			if k.Negative {
				return nil, badRequest("cpc_bid_micros does not apply to a negative keyword")
			}
			bid, err := campaignBudgetsMicros("cpc_bid_micros", k.CPCBidMicros)
			if err != nil {
				return nil, err
			}
			create["cpcBidMicros"] = bid
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, nil
}

func (a keywordPlanAdGroupKeywordsUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_ad_group_keywords", len(a.KeywordPlanAdGroupKeywords)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanAdGroupKeywords))
	names := make([]string, 0, len(a.KeywordPlanAdGroupKeywords))
	for _, k := range a.KeywordPlanAdGroupKeywords {
		if !numericIDPattern.MatchString(k.KeywordPlanAdGroupKeywordID) {
			return nil, nil, badRequest("keyword_plan_ad_group_keyword_id must be the ID of an existing keyword plan ad group keyword, a positive number")
		}
		name := keywordPlanAdGroupKeywordName(a.CustomerID, k.KeywordPlanAdGroupKeywordID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("keyword_plan_ad_group_keywords names each keyword_plan_ad_group_keyword_id at most once")
		}
		update := map[string]any{"resourceName": name}
		var mask []string
		if k.Text != nil {
			if err := keywordPlanAdGroupKeywordsCheckText(*k.Text); err != nil {
				return nil, nil, err
			}
			update["text"] = *k.Text
			mask = append(mask, "text")
		}
		if k.MatchType != nil {
			if err := keywordPlanAdGroupKeywordsCheckMatchType(*k.MatchType); err != nil {
				return nil, nil, err
			}
			update["matchType"] = *k.MatchType
			mask = append(mask, "match_type")
		}
		if k.CPCBidMicros != nil {
			bid, err := campaignBudgetsMicros("cpc_bid_micros", k.CPCBidMicros)
			if err != nil {
				return nil, nil, err
			}
			update["cpcBidMicros"] = bid
			mask = append(mask, "cpc_bid_micros")
		}
		if len(mask) == 0 {
			return nil, nil, badRequest("each entry of keyword_plan_ad_group_keywords needs at least one field to change")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a keywordPlanAdGroupKeywordsRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_ad_group_keyword_ids", len(a.KeywordPlanAdGroupKeywordIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanAdGroupKeywordIDs))
	names := make([]string, 0, len(a.KeywordPlanAdGroupKeywordIDs))
	for _, id := range a.KeywordPlanAdGroupKeywordIDs {
		if !numericIDPattern.MatchString(id) {
			return nil, nil, badRequest("keyword_plan_ad_group_keyword_ids must be IDs of existing keyword plan ad group keywords, positive numbers")
		}
		name := keywordPlanAdGroupKeywordName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("keyword_plan_ad_group_keyword_ids names each keyword plan ad group keyword at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	keywordPlanAdGroupKeywordsMutated struct {
		KeywordPlanAdGroupKeywordID string `json:"keyword_plan_ad_group_keyword_id"`
		ResourceName                string `json:"resource_name"`
	}
	keywordPlanAdGroupKeywordsChanged struct {
		CustomerID   string                              `json:"customer_id"`
		ValidateOnly bool                                `json:"validate_only"`
		Results      []keywordPlanAdGroupKeywordsMutated `json:"results"`
	}
)

// keywordPlanAdGroupKeywordsResults maps the results. mutateList has bound them to the account and, for an update
// and a remove, to the names asked for; a create must name distinct keywords.
func keywordPlanAdGroupKeywordsResults(op string, results []listResult) ([]keywordPlanAdGroupKeywordsMutated, error) {
	out := []keywordPlanAdGroupKeywordsMutated{}
	for _, r := range results {
		for _, previous := range out {
			if previous.KeywordPlanAdGroupKeywordID == r.ID {
				return nil, unsureResult(op, "answered with the same keyword plan ad group keyword twice")
			}
		}
		out = append(out, keywordPlanAdGroupKeywordsMutated{KeywordPlanAdGroupKeywordID: r.ID, ResourceName: r.ResourceName})
	}
	return out, nil
}

// --- descriptors ---

var (
	keywordPlanAdGroupKeywordIDSchema   = schema{"type": "string", "pattern": numericIDPattern.String()}
	keywordPlanAdGroupKeywordTextSchema = schema{"type": "string", "minLength": 1, "maxLength": keywordPlanAdGroupKeywordMax}

	keywordPlanAdGroupKeywordsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"keyword_plan_ad_group_keyword_id": stringSchema, "resource_name": stringSchema},
			"keyword_plan_ad_group_keyword_id", "resource_name"))},
		"customer_id", "validate_only", "results")
)

const keywordPlanAdGroupKeywordsSensitivity = "keywords of the ad groups of the keyword plans of a Google Ads " +
	"account: their text, match type, polarity and bids, which the confirmation names; no data of other people"

const keywordPlanAdGroupKeywordsNote = " A keyword plan ad group keyword is a positive or negative keyword of an ad " +
	"group of a saved Keyword Planner plan. A plan holds " + keywordPlanAdGroupKeywordCap + ". The tool sets the " +
	"text, match type, polarity and bid, adds no plan ad group or campaign keyword, runs no forecast, and does not " +
	"read the keywords (read them with reports.search on the keyword_plan_ad_group_keyword resource). Google may " +
	"block the keyword planning services at the Explorer access level, so the Cloud project of the credential then " +
	"needs Basic access. The request goes to KeywordPlanAdGroupKeywordService.MutateKeywordPlanAdGroupKeywords with " +
	"up to 50 operations, atomic and without partial_failure: Google applies all of them or none."

var keywordPlanAdGroupKeywordsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: keyword_plan_ad_group_keyword_id " +
		"and resource_name (customers/ID/keywordPlanAdGroupKeywords/KEYWORD_ID, as Google answered, always of the " +
		"account of the call). Google answers the resource name only, so there is no response_content_type. Empty " +
		"after validate_only"},
}

const keywordPlanAdGroupKeywordsFieldDoc = "text (1 to 80 characters, not blank, no control characters), " +
	"match_type (BROAD, EXACT, or PHRASE), and cpc_bid_micros (whole micros of the account currency; it overrides " +
	"the bid of the plan ad group and does not apply to a negative keyword)"

var keywordPlanAdGroupKeywordProperties = schema{
	"text": keywordPlanAdGroupKeywordTextSchema, "match_type": recEnumSchema(keywordPlanAdGroupKeywordMatchTypes),
	"cpc_bid_micros": campaignBudgetMicrosSchema,
}

func keywordPlanAdGroupKeywordItem(extra schema, required ...string) schema {
	properties := schema{}
	for k, v := range keywordPlanAdGroupKeywordProperties {
		properties[k] = v
	}
	for k, v := range extra {
		properties[k] = v
	}
	return object(properties, required...)
}

var keywordPlanAdGroupKeywordsCreate = capability.Descriptor{
	ID: keywordPlanAdGroupKeywordsCreateID, Version: 1,
	Title: "Create keyword plan ad group keywords in a Google Ads account",
	Description: "Create keywords in the ad groups of keyword plans of a Google Ads account: the " +
		"keyword_plan_ad_group_id of the plan ad group (its resource name is built from customer_id and " +
		"keyword_plan_ad_group_id), the text, the match type, whether it is negative, and optionally a bid." +
		keywordPlanAdGroupKeywordsNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-ad-group-keywords", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, keywordPlanAdGroupKeywordsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_ad_group_keywords": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"items": keywordPlanAdGroupKeywordItem(schema{"keyword_plan_ad_group_id": keywordPlanAdGroupIDSchema,
				"negative": booleanSchema}, "keyword_plan_ad_group_id", "text", "match_type")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_ad_group_keywords")),
	OutputSchema: encoded(keywordPlanAdGroupKeywordsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_ad_group_keywords", Required: true, Description: "1 to 50 " +
			"keywords, each with keyword_plan_ad_group_id (the id of an existing plan ad group as reports.search " +
			"returns it), " + keywordPlanAdGroupKeywordsFieldDoc + ", and negative (default false, cannot be " +
			"changed later). keyword_plan_ad_group_id, text and match_type are required"},
		validateOnlyArgument),
	Fields: keywordPlanAdGroupKeywordsFields,
	Examples: []capability.Example{{Description: "Add a positive exact match keyword with its own bid to a keyword plan ad group",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_ad_group_keywords":[{"keyword_plan_ad_group_id":"778899",` +
			`"text":"running shoes","match_type":"EXACT","cpc_bid_micros":800000}]}`)}},
}

var keywordPlanAdGroupKeywordsUpdate = capability.Descriptor{
	ID: keywordPlanAdGroupKeywordsUpdateID, Version: 1,
	Title: "Change keyword plan ad group keywords of a Google Ads account",
	Description: "Change existing keywords of keyword plan ad groups of a Google Ads account (an update with a " +
		"field mask that names exactly the fields given). Only the keyword_plan_ad_group_keyword_id selects a " +
		"keyword: its resource name is built from customer_id and keyword_plan_ad_group_keyword_id. The plan ad " +
		"group and the polarity of a keyword are not changed." + keywordPlanAdGroupKeywordsNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-ad-group-keywords", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, keywordPlanAdGroupKeywordsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_ad_group_keywords": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"items": keywordPlanAdGroupKeywordItem(schema{"keyword_plan_ad_group_keyword_id": keywordPlanAdGroupKeywordIDSchema},
				"keyword_plan_ad_group_keyword_id")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_ad_group_keywords")),
	OutputSchema: encoded(keywordPlanAdGroupKeywordsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_ad_group_keywords", Required: true, Description: "1 to 50 entries, " +
			"each keyword_plan_ad_group_keyword_id once (the id of the keyword as reports.search returns it), " +
			"with at least one of " + keywordPlanAdGroupKeywordsFieldDoc},
		validateOnlyArgument),
	Fields: keywordPlanAdGroupKeywordsFields,
	Examples: []capability.Example{{Description: "Change the match type of a keyword plan ad group keyword",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_ad_group_keywords":[{"keyword_plan_ad_group_keyword_id":"112233","match_type":"PHRASE"}]}`)}},
}

var keywordPlanAdGroupKeywordsRemove = capability.Descriptor{
	ID: keywordPlanAdGroupKeywordsRemoveID, Version: 1,
	Title: "Remove keyword plan ad group keywords from a Google Ads account",
	Description: "Remove existing keywords of keyword plan ad groups of a Google Ads account (a remove of " +
		"customers/ID/keywordPlanAdGroupKeywords/KEYWORD_ID, built from customer_id and " +
		"keyword_plan_ad_group_keyword_id). A removed keyword is gone from the account; no tool of this provider " +
		"brings it back." + keywordPlanAdGroupKeywordsNote +
		" Offered by a connection with the delete effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-ad-group-keywords", "remove"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, keywordPlanAdGroupKeywordsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_ad_group_keyword_ids": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"uniqueItems": true, "items": keywordPlanAdGroupKeywordIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_ad_group_keyword_ids")),
	OutputSchema: encoded(keywordPlanAdGroupKeywordsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_ad_group_keyword_ids", Required: true, Description: "1 to 50 IDs of " +
			"existing keyword plan ad group keywords, each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: keywordPlanAdGroupKeywordsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a keyword plan ad group keyword without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_ad_group_keyword_ids":["112233"],"validate_only":true}`)}},
}

// --- handlers ---

func handleKeywordPlanAdGroupKeywordsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create keyword plan ad group keywords"
	var args keywordPlanAdGroupKeywordsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanAdGroupKeywordsSend(ctx, resolved, secrets, red, op, keywordPlanAdGroupKeywordsCreateID,
		args.CustomerID, ops, nil, args.ValidateOnly)
}

func handleKeywordPlanAdGroupKeywordsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update keyword plan ad group keywords"
	var args keywordPlanAdGroupKeywordsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanAdGroupKeywordsSend(ctx, resolved, secrets, red, op, keywordPlanAdGroupKeywordsUpdateID,
		args.CustomerID, ops, names, args.ValidateOnly)
}

func handleKeywordPlanAdGroupKeywordsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove keyword plan ad group keywords"
	var args keywordPlanAdGroupKeywordsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanAdGroupKeywordsSend(ctx, resolved, secrets, red, op, keywordPlanAdGroupKeywordsRemoveID,
		args.CustomerID, ops, names, args.ValidateOnly)
}

func keywordPlanAdGroupKeywordsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, op, tool, customerID string, ops []any, want []string, validateOnly bool) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: customerID,
		service: keywordPlanAdGroupKeywordsService, collection: "keywordPlanAdGroupKeywords", operations: ops,
		names: want, validateOnly: validateOnly})
	if err != nil {
		return nil, err
	}
	out, err := keywordPlanAdGroupKeywordsResults(op, results)
	if err != nil {
		return nil, err
	}
	return keywordPlanAdGroupKeywordsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: out}, nil
}

// --- confirmation ---

// keywordPlanAdGroupKeywordsTextText shows a keyword text in the confirmation, quoted and cut.
func keywordPlanAdGroupKeywordsTextText(value string) string {
	return strconv.Quote(boundedText(value, keywordPlanAdGroupKeywordMax*4))
}

func keywordPlanAdGroupKeywordsBidText(bid *json.Number) string {
	if bid == nil {
		return ""
	}
	return " bid " + boundedText(string(*bid), maxCodeText) + " micros"
}

func keywordPlanAdGroupKeywordsCreateSummary(raw json.RawMessage) string {
	var a keywordPlanAdGroupKeywordsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, k := range a.KeywordPlanAdGroupKeywords[:min(len(a.KeywordPlanAdGroupKeywords), 10)] {
		polarity := "positive"
		if k.Negative {
			polarity = "negative"
		}
		parts = append(parts, polarity+" "+keywordPlanAdGroupKeywordsTextText(k.Text)+" "+
			boundedText(k.MatchType, maxCodeText)+" in keyword plan ad group "+
			boundedText(k.KeywordPlanAdGroupID, maxCodeText)+keywordPlanAdGroupKeywordsBidText(k.CPCBidMicros))
	}
	return keywordPlansLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.KeywordPlanAdGroupKeywords)) +
		" keyword(s) of keyword plan ad groups in Google Ads account " + a.CustomerID + ": " +
		strings.Join(parts, "; ") + keywordPlansMore(len(a.KeywordPlanAdGroupKeywords), len(parts)) + "."
}

func keywordPlanAdGroupKeywordsUpdateSummary(raw json.RawMessage) string {
	var a keywordPlanAdGroupKeywordsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, k := range a.KeywordPlanAdGroupKeywords[:min(len(a.KeywordPlanAdGroupKeywords), 10)] {
		part := "keyword " + boundedText(k.KeywordPlanAdGroupKeywordID, maxCodeText)
		if k.Text != nil {
			part += " text " + keywordPlanAdGroupKeywordsTextText(*k.Text)
		}
		if k.MatchType != nil {
			part += " match type " + boundedText(*k.MatchType, maxCodeText)
		}
		parts = append(parts, part+keywordPlanAdGroupKeywordsBidText(k.CPCBidMicros))
	}
	return keywordPlansLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.KeywordPlanAdGroupKeywords)) +
		" keyword(s) of keyword plan ad groups of Google Ads account " + a.CustomerID + ": " +
		strings.Join(parts, "; ") + keywordPlansMore(len(a.KeywordPlanAdGroupKeywords), len(parts)) + "."
}

func keywordPlanAdGroupKeywordsRemoveSummary(raw json.RawMessage) string {
	var a keywordPlanAdGroupKeywordsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.KeywordPlanAdGroupKeywordIDs[:min(len(a.KeywordPlanAdGroupKeywordIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	return keywordPlansLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.KeywordPlanAdGroupKeywordIDs)) +
		" keyword(s) of keyword plan ad groups from Google Ads account " + a.CustomerID + ": " +
		strings.Join(shown, ", ") + keywordPlansMore(len(a.KeywordPlanAdGroupKeywordIDs), len(shown)) +
		". No tool brings them back."
}
