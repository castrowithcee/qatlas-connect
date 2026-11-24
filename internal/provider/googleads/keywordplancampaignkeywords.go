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
	keywordPlanCampaignKeywordsCreateID = Provider + ".keyword-plan-campaign-keywords.create"
	keywordPlanCampaignKeywordsUpdateID = Provider + ".keyword-plan-campaign-keywords.update"
	keywordPlanCampaignKeywordsRemoveID = Provider + ".keyword-plan-campaign-keywords.remove"

	keywordPlanCampaignKeywordsService = "/keywordPlanCampaignKeywords"
)

// Bounds of KeywordPlanCampaignKeyword as the v26 reference documents them: only negative keywords are supported
// and negative is immutable and must be true, so the tool always sends it and offers no argument for it; text and
// match_type are the fields to set. The reference states no text length or word count.
// MutateKeywordPlanCampaignKeywordsRequest has no response_content_type and its result holds the resource name only.
const (
	// keywordPlanCampaignKeywordMax is Qatlas's own cap on the text, since the reference states none.
	keywordPlanCampaignKeywordMax = 80
	keywordPlanCampaignKeywordCap = "no more than 1000 negative keywords per plan, campaign and ad group keywords together"
)

var keywordPlanMatchTypes = []string{"BROAD", "EXACT", "PHRASE"}

// keywordPlanCampaignKeywordName is the resource name of a plan campaign keyword, built from checked IDs alone.
func keywordPlanCampaignKeywordName(customerID, id string) string {
	return customerName(customerID) + "/keywordPlanCampaignKeywords/" + id
}

func keywordPlanCampaignKeywordsCheckText(value string) error {
	n := utf8.RuneCountInString(value)
	if n < 1 || n > keywordPlanCampaignKeywordMax || strings.TrimSpace(value) == "" {
		return badRequest("text must be 1 to " + strconv.Itoa(keywordPlanCampaignKeywordMax) + " characters and not blank")
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return badRequest("text must not hold control characters")
		}
	}
	return nil
}

func keywordPlanCampaignKeywordsCheckMatchType(value string) error {
	if !slices.Contains(keywordPlanMatchTypes, value) {
		return badRequest("match_type must be one of " + strings.Join(keywordPlanMatchTypes, ", "))
	}
	return nil
}

// --- arguments ---

type (
	keywordPlanCampaignKeywordNew struct {
		KeywordPlanCampaignID string `json:"keyword_plan_campaign_id"`
		Text                  string `json:"text"`
		MatchType             string `json:"match_type"`
	}
	keywordPlanCampaignKeywordEdit struct {
		KeywordPlanCampaignKeywordID string  `json:"keyword_plan_campaign_keyword_id"`
		Text                         *string `json:"text"`
		MatchType                    *string `json:"match_type"`
	}
	keywordPlanCampaignKeywordsCreateArguments struct {
		CustomerID                  string                          `json:"customer_id"`
		KeywordPlanCampaignKeywords []keywordPlanCampaignKeywordNew `json:"keyword_plan_campaign_keywords"`
		ValidateOnly                bool                            `json:"validate_only"`
	}
	keywordPlanCampaignKeywordsUpdateArguments struct {
		CustomerID                  string                           `json:"customer_id"`
		KeywordPlanCampaignKeywords []keywordPlanCampaignKeywordEdit `json:"keyword_plan_campaign_keywords"`
		ValidateOnly                bool                             `json:"validate_only"`
	}
	keywordPlanCampaignKeywordsRemoveArguments struct {
		CustomerID                    string   `json:"customer_id"`
		KeywordPlanCampaignKeywordIDs []string `json:"keyword_plan_campaign_keyword_ids"`
		ValidateOnly                  bool     `json:"validate_only"`
	}
)

func (a keywordPlanCampaignKeywordsCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_campaign_keywords", len(a.KeywordPlanCampaignKeywords)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanCampaignKeywords))
	for _, k := range a.KeywordPlanCampaignKeywords {
		if !numericIDPattern.MatchString(k.KeywordPlanCampaignID) {
			return nil, badRequest("keyword_plan_campaign_id must be the ID of an existing keyword plan campaign, a positive number")
		}
		if err := keywordPlanCampaignKeywordsCheckText(k.Text); err != nil {
			return nil, err
		}
		if err := keywordPlanCampaignKeywordsCheckMatchType(k.MatchType); err != nil {
			return nil, err
		}
		ops = append(ops, map[string]any{"create": map[string]any{
			"keywordPlanCampaign": keywordPlanCampaignName(a.CustomerID, k.KeywordPlanCampaignID),
			"text":                k.Text, "matchType": k.MatchType, "negative": true}})
	}
	return ops, nil
}

func (a keywordPlanCampaignKeywordsUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_campaign_keywords", len(a.KeywordPlanCampaignKeywords)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanCampaignKeywords))
	names := make([]string, 0, len(a.KeywordPlanCampaignKeywords))
	for _, k := range a.KeywordPlanCampaignKeywords {
		if !numericIDPattern.MatchString(k.KeywordPlanCampaignKeywordID) {
			return nil, nil, badRequest("keyword_plan_campaign_keyword_id must be the ID of an existing keyword plan campaign keyword, a positive number")
		}
		name := keywordPlanCampaignKeywordName(a.CustomerID, k.KeywordPlanCampaignKeywordID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("keyword_plan_campaign_keywords names each keyword_plan_campaign_keyword_id at most once")
		}
		update := map[string]any{"resourceName": name}
		var mask []string
		if k.Text != nil {
			if err := keywordPlanCampaignKeywordsCheckText(*k.Text); err != nil {
				return nil, nil, err
			}
			update["text"] = *k.Text
			mask = append(mask, "text")
		}
		if k.MatchType != nil {
			if err := keywordPlanCampaignKeywordsCheckMatchType(*k.MatchType); err != nil {
				return nil, nil, err
			}
			update["matchType"] = *k.MatchType
			mask = append(mask, "match_type")
		}
		if len(mask) == 0 {
			return nil, nil, badRequest("each entry of keyword_plan_campaign_keywords needs at least one field to change")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a keywordPlanCampaignKeywordsRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_campaign_keyword_ids", len(a.KeywordPlanCampaignKeywordIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanCampaignKeywordIDs))
	names := make([]string, 0, len(a.KeywordPlanCampaignKeywordIDs))
	for _, id := range a.KeywordPlanCampaignKeywordIDs {
		if !numericIDPattern.MatchString(id) {
			return nil, nil, badRequest("keyword_plan_campaign_keyword_ids must be IDs of existing keyword plan campaign keywords, positive numbers")
		}
		name := keywordPlanCampaignKeywordName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("keyword_plan_campaign_keyword_ids names each keyword plan campaign keyword at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	keywordPlanCampaignKeywordsMutated struct {
		KeywordPlanCampaignKeywordID string `json:"keyword_plan_campaign_keyword_id"`
		ResourceName                 string `json:"resource_name"`
	}
	keywordPlanCampaignKeywordsChanged struct {
		CustomerID   string                               `json:"customer_id"`
		ValidateOnly bool                                 `json:"validate_only"`
		Results      []keywordPlanCampaignKeywordsMutated `json:"results"`
	}
)

// keywordPlanCampaignKeywordsResults maps the results. mutateList has bound them to the account and, for an
// update and a remove, to the names asked for; a create must name distinct keywords.
func keywordPlanCampaignKeywordsResults(op string, results []listResult) ([]keywordPlanCampaignKeywordsMutated, error) {
	out := []keywordPlanCampaignKeywordsMutated{}
	for _, r := range results {
		for _, previous := range out {
			if previous.KeywordPlanCampaignKeywordID == r.ID {
				return nil, unsureResult(op, "answered with the same keyword plan campaign keyword twice")
			}
		}
		out = append(out, keywordPlanCampaignKeywordsMutated{KeywordPlanCampaignKeywordID: r.ID, ResourceName: r.ResourceName})
	}
	return out, nil
}

// --- descriptors ---

var (
	keywordPlanCampaignKeywordIDSchema   = schema{"type": "string", "pattern": numericIDPattern.String()}
	keywordPlanCampaignKeywordTextSchema = schema{"type": "string", "minLength": 1, "maxLength": keywordPlanCampaignKeywordMax}

	keywordPlanCampaignKeywordsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"keyword_plan_campaign_keyword_id": stringSchema, "resource_name": stringSchema},
			"keyword_plan_campaign_keyword_id", "resource_name"))},
		"customer_id", "validate_only", "results")
)

const keywordPlanCampaignKeywordsSensitivity = "negative keywords of the campaigns of the keyword plans of a Google " +
	"Ads account: their text and match type, which the confirmation names; no data of other people"

const keywordPlanCampaignKeywordsNote = " A keyword plan campaign keyword is a negative keyword of the campaign of a " +
	"saved Keyword Planner plan: Google supports only negative ones, so the tool always sets negative and offers no " +
	"positive keyword. A plan holds " + keywordPlanCampaignKeywordCap + ". The tool sets the text and match type, " +
	"adds no ad group or ad group keyword, runs no forecast, and does not read the keywords (read them with " +
	"reports.search on the keyword_plan_campaign_keyword resource). Google may block the keyword planning services " +
	"at the Explorer access level, so the Cloud project of the credential then needs Basic access. The request goes " +
	"to KeywordPlanCampaignKeywordService.MutateKeywordPlanCampaignKeywords with up to 50 operations, atomic and " +
	"without partial_failure: Google applies all of them or none."

var keywordPlanCampaignKeywordsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: keyword_plan_campaign_keyword_id " +
		"and resource_name (customers/ID/keywordPlanCampaignKeywords/KEYWORD_ID, as Google answered, always of the " +
		"account of the call). Google answers the resource name only, so there is no response_content_type. Empty " +
		"after validate_only"},
}

const keywordPlanCampaignKeywordsFieldDoc = "text (1 to 80 characters, not blank, no control characters) and " +
	"match_type (BROAD, EXACT, or PHRASE)"

var keywordPlanCampaignKeywordProperties = schema{
	"text": keywordPlanCampaignKeywordTextSchema, "match_type": recEnumSchema(keywordPlanMatchTypes),
}

func keywordPlanCampaignKeywordItem(extra schema, required ...string) schema {
	properties := schema{}
	for k, v := range keywordPlanCampaignKeywordProperties {
		properties[k] = v
	}
	for k, v := range extra {
		properties[k] = v
	}
	return object(properties, required...)
}

var keywordPlanCampaignKeywordsCreate = capability.Descriptor{
	ID: keywordPlanCampaignKeywordsCreateID, Version: 1,
	Title: "Create keyword plan campaign keywords in a Google Ads account",
	Description: "Create negative keywords for the campaigns of keyword plans of a Google Ads account: the " +
		"keyword_plan_campaign_id of the plan campaign (its resource name is built from customer_id and " +
		"keyword_plan_campaign_id), the text, and the match type." + keywordPlanCampaignKeywordsNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-campaign-keywords", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, keywordPlanCampaignKeywordsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_campaign_keywords": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"items": keywordPlanCampaignKeywordItem(schema{"keyword_plan_campaign_id": keywordPlanCampaignIDSchema},
				"keyword_plan_campaign_id", "text", "match_type")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_campaign_keywords")),
	OutputSchema: encoded(keywordPlanCampaignKeywordsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_campaign_keywords", Required: true, Description: "1 to 50 " +
			"negative keywords, each with keyword_plan_campaign_id (the id of an existing plan campaign as " +
			"reports.search returns it), " + keywordPlanCampaignKeywordsFieldDoc + ", all required"},
		validateOnlyArgument),
	Fields: keywordPlanCampaignKeywordsFields,
	Examples: []capability.Example{{Description: "Add a negative phrase match keyword to a keyword plan campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_campaign_keywords":[{"keyword_plan_campaign_id":"445566",` +
			`"text":"free trial","match_type":"PHRASE"}]}`)}},
}

var keywordPlanCampaignKeywordsUpdate = capability.Descriptor{
	ID: keywordPlanCampaignKeywordsUpdateID, Version: 1,
	Title: "Change keyword plan campaign keywords of a Google Ads account",
	Description: "Change existing negative keywords of keyword plan campaigns of a Google Ads account (an update " +
		"with a field mask that names exactly the fields given). Only the keyword_plan_campaign_keyword_id " +
		"selects a keyword: its resource name is built from customer_id and keyword_plan_campaign_keyword_id. " +
		"The campaign of a keyword is not changed." + keywordPlanCampaignKeywordsNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-campaign-keywords", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, keywordPlanCampaignKeywordsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_campaign_keywords": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"items": keywordPlanCampaignKeywordItem(schema{"keyword_plan_campaign_keyword_id": keywordPlanCampaignKeywordIDSchema},
				"keyword_plan_campaign_keyword_id")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_campaign_keywords")),
	OutputSchema: encoded(keywordPlanCampaignKeywordsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_campaign_keywords", Required: true, Description: "1 to 50 entries, " +
			"each keyword_plan_campaign_keyword_id once (the id of the keyword as reports.search returns it), with " +
			"at least one of " + keywordPlanCampaignKeywordsFieldDoc},
		validateOnlyArgument),
	Fields: keywordPlanCampaignKeywordsFields,
	Examples: []capability.Example{{Description: "Change the match type of a negative keyword of a keyword plan campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_campaign_keywords":[{"keyword_plan_campaign_keyword_id":"778899","match_type":"EXACT"}]}`)}},
}

var keywordPlanCampaignKeywordsRemove = capability.Descriptor{
	ID: keywordPlanCampaignKeywordsRemoveID, Version: 1,
	Title: "Remove keyword plan campaign keywords from a Google Ads account",
	Description: "Remove existing negative keywords of keyword plan campaigns of a Google Ads account (a remove of " +
		"customers/ID/keywordPlanCampaignKeywords/KEYWORD_ID, built from customer_id and " +
		"keyword_plan_campaign_keyword_id). A removed keyword is gone from the account; no tool of this provider " +
		"brings it back." + keywordPlanCampaignKeywordsNote +
		" Offered by a connection with the delete effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-campaign-keywords", "remove"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, keywordPlanCampaignKeywordsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_campaign_keyword_ids": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"uniqueItems": true, "items": keywordPlanCampaignKeywordIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_campaign_keyword_ids")),
	OutputSchema: encoded(keywordPlanCampaignKeywordsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_campaign_keyword_ids", Required: true, Description: "1 to 50 IDs " +
			"of existing keyword plan campaign keywords, each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: keywordPlanCampaignKeywordsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a negative keyword without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_campaign_keyword_ids":["778899"],"validate_only":true}`)}},
}

// --- handlers ---

func handleKeywordPlanCampaignKeywordsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create keyword plan campaign keywords"
	var args keywordPlanCampaignKeywordsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanCampaignKeywordsSend(ctx, resolved, secrets, red, op, keywordPlanCampaignKeywordsCreateID,
		args.CustomerID, ops, nil, args.ValidateOnly)
}

func handleKeywordPlanCampaignKeywordsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update keyword plan campaign keywords"
	var args keywordPlanCampaignKeywordsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanCampaignKeywordsSend(ctx, resolved, secrets, red, op, keywordPlanCampaignKeywordsUpdateID,
		args.CustomerID, ops, names, args.ValidateOnly)
}

func handleKeywordPlanCampaignKeywordsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove keyword plan campaign keywords"
	var args keywordPlanCampaignKeywordsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanCampaignKeywordsSend(ctx, resolved, secrets, red, op, keywordPlanCampaignKeywordsRemoveID,
		args.CustomerID, ops, names, args.ValidateOnly)
}

func keywordPlanCampaignKeywordsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
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
		service: keywordPlanCampaignKeywordsService, collection: "keywordPlanCampaignKeywords", operations: ops,
		names: want, validateOnly: validateOnly})
	if err != nil {
		return nil, err
	}
	out, err := keywordPlanCampaignKeywordsResults(op, results)
	if err != nil {
		return nil, err
	}
	return keywordPlanCampaignKeywordsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: out}, nil
}

// --- confirmation ---

// keywordPlanCampaignKeywordsTextText shows a keyword text in the confirmation, quoted and cut.
func keywordPlanCampaignKeywordsTextText(value string) string {
	return strconv.Quote(boundedText(value, keywordPlanCampaignKeywordMax*4))
}

func keywordPlanCampaignKeywordsCreateSummary(raw json.RawMessage) string {
	var a keywordPlanCampaignKeywordsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, k := range a.KeywordPlanCampaignKeywords[:min(len(a.KeywordPlanCampaignKeywords), 10)] {
		parts = append(parts, keywordPlanCampaignKeywordsTextText(k.Text)+" "+boundedText(k.MatchType, maxCodeText)+
			" on keyword plan campaign "+boundedText(k.KeywordPlanCampaignID, maxCodeText))
	}
	return keywordPlansLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.KeywordPlanCampaignKeywords)) +
		" negative keyword(s) of keyword plan campaigns in Google Ads account " + a.CustomerID + ": " +
		strings.Join(parts, "; ") + keywordPlansMore(len(a.KeywordPlanCampaignKeywords), len(parts)) + "."
}

func keywordPlanCampaignKeywordsUpdateSummary(raw json.RawMessage) string {
	var a keywordPlanCampaignKeywordsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, k := range a.KeywordPlanCampaignKeywords[:min(len(a.KeywordPlanCampaignKeywords), 10)] {
		part := "keyword " + boundedText(k.KeywordPlanCampaignKeywordID, maxCodeText)
		if k.Text != nil {
			part += " text " + keywordPlanCampaignKeywordsTextText(*k.Text)
		}
		if k.MatchType != nil {
			part += " match type " + boundedText(*k.MatchType, maxCodeText)
		}
		parts = append(parts, part)
	}
	return keywordPlansLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.KeywordPlanCampaignKeywords)) +
		" negative keyword(s) of keyword plan campaigns of Google Ads account " + a.CustomerID + ": " +
		strings.Join(parts, "; ") + keywordPlansMore(len(a.KeywordPlanCampaignKeywords), len(parts)) + "."
}

func keywordPlanCampaignKeywordsRemoveSummary(raw json.RawMessage) string {
	var a keywordPlanCampaignKeywordsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.KeywordPlanCampaignKeywordIDs[:min(len(a.KeywordPlanCampaignKeywordIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	return keywordPlansLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.KeywordPlanCampaignKeywordIDs)) +
		" negative keyword(s) of keyword plan campaigns from Google Ads account " + a.CustomerID + ": " +
		strings.Join(shown, ", ") + keywordPlansMore(len(a.KeywordPlanCampaignKeywordIDs), len(shown)) +
		". No tool brings them back."
}
