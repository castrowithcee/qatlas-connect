package googleads

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	campaignDraftsCreateID          = Provider + ".campaign-drafts.create"
	campaignDraftsUpdateID          = Provider + ".campaign-drafts.update"
	campaignDraftsRemoveID          = Provider + ".campaign-drafts.remove"
	campaignDraftsPromoteID         = Provider + ".campaign-drafts.promote"
	campaignDraftsListAsyncErrorsID = Provider + ".campaign-drafts.list-async-errors"

	campaignDraftsService    = "/campaignDrafts"
	campaignDraftsCollection = "campaignDrafts"

	// campaignDraftNameMax is Qatlas's own bound: the reference of v26 documents no length for CampaignDraft.name,
	// only that it is required, not empty and free of NUL, LF and CR.
	campaignDraftNameMax = 255

	campaignDraftNamePattern = `^customers/[0-9]{10}/campaignDrafts/[1-9][0-9]{0,18}~[1-9][0-9]{0,18}$`
	baseCampaignNamePattern  = `^customers/[0-9]{10}/campaigns/[1-9][0-9]{0,18}$`
)

// campaignDraftIDs splits customers/ID/campaignDrafts/BASE~DRAFT of exactly this account into both IDs.
func campaignDraftIDs(name, customerID string) (base, draft string, ok bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+campaignDraftsCollection+"/")
	if !ok {
		return "", "", false
	}
	base, draft, ok = strings.Cut(rest, "~")
	return base, draft, ok && checkID("", base) == nil && checkID("", draft) == nil
}

// campaignDraftIDOfName is the ID a result of this family carries: both parts, as in the resource name.
func campaignDraftIDOfName(name, customerID string) (string, bool) {
	base, draft, ok := campaignDraftIDs(name, customerID)
	return base + "~" + draft, ok
}

func campaignDraftsCheckCount(argument string, n int) error {
	if n < 1 || n > maxListOperations {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(maxListOperations) + " entries")
	}
	return nil
}

// campaignDraftNameOf checks a draft argument: a draft of the account of the call.
func campaignDraftNameOf(argument, name, customerID string) error {
	if _, _, ok := campaignDraftIDs(name, customerID); !ok {
		return badRequest(argument + " must be customers/" + queryNumber(customerID) +
			"/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID of customer_id")
	}
	return nil
}

// --- arguments ---

type (
	campaignDraftNew struct {
		BaseCampaign string `json:"base_campaign"`
		Name         string `json:"name"`
	}
	campaignDraftEdit struct {
		CampaignDraft string `json:"campaign_draft"`
		Name          string `json:"name"`
	}
	campaignDraftsCreateArguments struct {
		CustomerID     string             `json:"customer_id"`
		CampaignDrafts []campaignDraftNew `json:"campaign_drafts"`
		ValidateOnly   bool               `json:"validate_only"`
	}
	campaignDraftsUpdateArguments struct {
		CustomerID     string              `json:"customer_id"`
		CampaignDrafts []campaignDraftEdit `json:"campaign_drafts"`
		ValidateOnly   bool                `json:"validate_only"`
	}
	campaignDraftsRemoveArguments struct {
		CustomerID     string   `json:"customer_id"`
		CampaignDrafts []string `json:"campaign_drafts"`
		ValidateOnly   bool     `json:"validate_only"`
	}
	campaignDraftsPromoteArguments struct {
		CustomerID    string `json:"customer_id"`
		CampaignDraft string `json:"campaign_draft"`
		ValidateOnly  bool   `json:"validate_only"`
	}
	campaignDraftsListAsyncErrorsArguments struct {
		CustomerID    string `json:"customer_id"`
		CampaignDraft string `json:"campaign_draft"`
		PageSize      *int   `json:"page_size"`
		Cursor        string `json:"cursor"`
	}
)

func (a campaignDraftsCreateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := campaignDraftsCheckCount("campaign_drafts", len(a.CampaignDrafts)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.CampaignDrafts))
	bases := make([]string, 0, len(a.CampaignDrafts))
	for _, d := range a.CampaignDrafts {
		base, ok := numericIDOf(d.BaseCampaign, a.CustomerID, "campaigns")
		if !ok || checkID("", base) != nil {
			return nil, nil, badRequest("base_campaign must be customers/" + queryNumber(a.CustomerID) +
				"/campaigns/N, a campaign of customer_id")
		}
		if err := labelsText("name", d.Name, 1, campaignDraftNameMax); err != nil {
			return nil, nil, err
		}
		bases = append(bases, base)
		ops = append(ops, map[string]any{"create": map[string]any{"baseCampaign": d.BaseCampaign, "name": d.Name}})
	}
	return ops, bases, nil
}

func (a campaignDraftsUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := campaignDraftsCheckCount("campaign_drafts", len(a.CampaignDrafts)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.CampaignDrafts))
	names := make([]string, 0, len(a.CampaignDrafts))
	for _, d := range a.CampaignDrafts {
		if err := campaignDraftNameOf("campaign_draft", d.CampaignDraft, a.CustomerID); err != nil {
			return nil, nil, err
		}
		if slices.Contains(names, d.CampaignDraft) {
			return nil, nil, badRequest("campaign_drafts names each campaign_draft at most once")
		}
		if err := labelsText("name", d.Name, 1, campaignDraftNameMax); err != nil {
			return nil, nil, err
		}
		names = append(names, d.CampaignDraft)
		ops = append(ops, map[string]any{"update": map[string]any{"resourceName": d.CampaignDraft, "name": d.Name},
			"updateMask": "name"})
	}
	return ops, names, nil
}

func (a campaignDraftsRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := campaignDraftsCheckCount("campaign_drafts", len(a.CampaignDrafts)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.CampaignDrafts))
	for i, name := range a.CampaignDrafts {
		if err := campaignDraftNameOf("campaign_drafts", name, a.CustomerID); err != nil {
			return nil, nil, err
		}
		if slices.Contains(a.CampaignDrafts[:i], name) {
			return nil, nil, badRequest("campaign_drafts names each campaign draft at most once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, a.CampaignDrafts, nil
}

// --- results ---

type (
	campaignDraftMutated struct {
		BaseCampaignID string `json:"base_campaign_id"`
		DraftID        string `json:"draft_id"`
		ResourceName   string `json:"resource_name"`
	}
	campaignDraftsChanged struct {
		CustomerID   string                 `json:"customer_id"`
		ValidateOnly bool                   `json:"validate_only"`
		Results      []campaignDraftMutated `json:"results"`
	}
	campaignDraftPromoted struct {
		CustomerID    string      `json:"customer_id"`
		CampaignDraft string      `json:"campaign_draft"`
		ValidateOnly  bool        `json:"validate_only"`
		Operation     string      `json:"operation,omitempty"`
		Done          bool        `json:"done"`
		Error         *asyncError `json:"error,omitempty"`
	}
)

// --- descriptors ---

var (
	campaignDraftNameSchema = schema{"type": "string", "maxLength": 100, "pattern": campaignDraftNamePattern,
		"description": "Campaign draft of this account, customers/ID/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID"}
	campaignDraftTextSchema = schema{"type": "string", "minLength": 1, "maxLength": campaignDraftNameMax}

	campaignDraftsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"base_campaign_id": stringSchema, "draft_id": stringSchema,
			"resource_name": stringSchema}, "base_campaign_id", "draft_id", "resource_name"))},
		"customer_id", "validate_only", "results")
)

const campaignDraftsSensitivity = "campaign drafts of a Google Ads account: their names and the base campaigns they " +
	"belong to, which the confirmation names; no data of other people"

const campaignDraftsNote = " A draft is a copy of a base campaign in which changes are prepared without affecting " +
	"ad serving. This tool changes the name only: the status is set by Google and cannot be written, and a draft's " +
	"campaign is changed with the campaign tools on its draft campaign (read drafts with reports.search on the " +
	"campaign_draft resource). The request goes to CampaignDraftService.MutateCampaignDrafts with up to 10 " +
	"operations, atomic and without partial_failure: Google applies all of them or none. Google offers no " +
	"response_content_type here, so a result holds the resource name only."

var campaignDraftsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: base_campaign_id, draft_id and " +
		"resource_name (customers/ID/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID, as Google answered, always of the " +
		"account of the call). Empty after validate_only"},
}

var campaignDraftsCreate = capability.Descriptor{
	ID: campaignDraftsCreateID, Version: 1, Title: "Create campaign drafts in a Google Ads account",
	Description: "Create one or more campaign drafts in a Google Ads account. Each entry names the base campaign " +
		"(a campaign of customer_id, customers/ID/campaigns/N, immutable afterwards) and the name of the draft " +
		"(1 to 255 characters, no control characters)." + campaignDraftsNote + " Offered by a connection with the " +
		"create effect or whose tools list names it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "campaign-drafts", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, campaignDraftsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_drafts": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"base_campaign": schema{"type": "string", "maxLength": 60,
				"pattern": baseCampaignNamePattern}, "name": campaignDraftTextSchema}, "base_campaign", "name")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "campaign_drafts")),
	OutputSchema: encoded(campaignDraftsOutputSchema),
	Arguments: append(slices.Clone(experimentsArgumentDocs),
		capability.Argument{Name: "campaign_drafts", Required: true, Description: "1 to 10 drafts, each with " +
			"base_campaign (customers/ID/campaigns/N of customer_id) and name (1 to 255 characters)"},
		validateOnlyArgument),
	Fields: campaignDraftsFields,
	Examples: []capability.Example{{Description: "Create a draft of a campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_drafts":[{"base_campaign":` +
			`"customers/1234567890/campaigns/21","name":"Spring changes"}]}`)}},
}

var campaignDraftsUpdate = capability.Descriptor{
	ID: campaignDraftsUpdateID, Version: 1, Title: "Rename campaign drafts of a Google Ads account",
	Description: "Rename existing campaign drafts of a Google Ads account (an update with a field mask that names " +
		"the name alone). The base campaign cannot be changed. Each entry names the draft by campaign_draft, " +
		"customers/ID/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID of customer_id." + campaignDraftsNote + " Offered by a " +
		"connection with the update effect or whose tools list names it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "campaign-drafts", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, campaignDraftsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_drafts": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"campaign_draft": campaignDraftNameSchema, "name": campaignDraftTextSchema},
				"campaign_draft", "name")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "campaign_drafts")),
	OutputSchema: encoded(campaignDraftsOutputSchema),
	Arguments: append(slices.Clone(experimentsArgumentDocs),
		capability.Argument{Name: "campaign_drafts", Required: true, Description: "1 to 10 entries, each " +
			"campaign_draft once (customers/ID/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID, as reports.search returns " +
			"it) with the new name (1 to 255 characters)"},
		validateOnlyArgument),
	Fields: campaignDraftsFields,
	Examples: []capability.Example{{Description: "Rename a draft",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_drafts":[{"campaign_draft":` +
			`"customers/1234567890/campaignDrafts/21~5","name":"Spring changes v2"}]}`)}},
}

var campaignDraftsRemove = capability.Descriptor{
	ID: campaignDraftsRemoveID, Version: 1, Title: "Remove campaign drafts from a Google Ads account",
	Description: "Remove existing campaign drafts of a Google Ads account (a remove of customers/ID/campaignDrafts/" +
		"BASE_CAMPAIGN_ID~DRAFT_ID). The changes prepared in a removed draft are lost; the base campaign stays. " +
		"Google marks a removed draft REMOVED; no tool of this provider brings it back." + campaignDraftsNote +
		mutateListListedNote,
	Tags: []string{"google", "ads", "campaign-drafts", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, campaignDraftsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_drafts": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"uniqueItems": true, "items": campaignDraftNameSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "campaign_drafts")),
	OutputSchema: encoded(campaignDraftsOutputSchema),
	Arguments: append(slices.Clone(experimentsArgumentDocs),
		capability.Argument{Name: "campaign_drafts", Required: true, Description: "1 to 10 resource names of " +
			"existing drafts of the account, customers/ID/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID, each once, as " +
			"reports.search returns them"},
		validateOnlyArgument),
	Fields: campaignDraftsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a draft without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_drafts":["customers/1234567890/` +
			`campaignDrafts/21~5"],"validate_only":true}`)}},
}

var campaignDraftsPromote = capability.Descriptor{
	ID: campaignDraftsPromoteID, Version: 1, Title: "Promote a campaign draft of a Google Ads account",
	Description: "Promote the changes of one campaign draft into its base campaign (CampaignDraftService." +
		"PromoteCampaignDraft, POST customers/ID/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID:promote). The base campaign " +
		"then serves with those changes, which can change its targeting, ads and spend. A promotion that fails " +
		"partway cannot be retried safely: Google then marks the draft PROMOTE_FAILED and the problem is fixed in the " +
		"base campaign. Google answers with a long-running operation and only a done state: the tool returns its " +
		"name (untrusted provider text, cut) and done, and no tool polls an operation. The outcome is the status of " +
		"the draft (PROMOTING, PROMOTED or PROMOTE_FAILED), read with reports.search on campaign_draft.status; the " +
		"errors of a failed promotion are read with campaign-drafts.list-async-errors. validate_only asks Google to " +
		"validate the promotion without making it. The request is sent once and never repeated." + mutateNote,
	Tags: []string{"google", "ads", "campaign-drafts", "promote"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectExecute, capability.IdempotencyNonIdempotent, campaignDraftsSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "campaign_draft": campaignDraftNameSchema,
		"validate_only": validateOnlySchema}, "customer_id", "campaign_draft")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "campaign_draft": stringSchema,
		"validate_only": booleanSchema, "operation": stringSchema, "done": booleanSchema,
		"error": object(schema{"code": integerSchema, "message": stringSchema}, "code")},
		"customer_id", "campaign_draft", "validate_only", "done")),
	Arguments: append(slices.Clone(experimentsArgumentDocs),
		capability.Argument{Name: "campaign_draft", Required: true, Description: "The draft of this account to " +
			"promote, customers/ID/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID, as reports.search returns it"},
		validateOnlyArgument),
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "campaign_draft", Description: "The draft that was promoted"},
		{Name: "validate_only", Description: "true when Google only validated the promotion"},
		{Name: "operation", Description: "Name of the long-running operation as Google answered, an untrusted " +
			"provider text that is cut; absent when Google gave none"},
		{Name: "done", Description: "true when Google reports the operation as done; the outcome is the draft's status"},
		{Name: "error", Description: "code and message (cut, untrusted) when the operation reports an error; the " +
			"details of the status are not returned"},
	},
	Examples: []capability.Example{{Description: "Validate the promotion of a draft without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_draft":"customers/1234567890/` +
			`campaignDrafts/21~5","validate_only":true}`)}},
}

var campaignDraftsListAsyncErrors = capability.Descriptor{
	ID: campaignDraftsListAsyncErrorsID, Version: 1, Title: "List the errors of the promotion of a Google Ads campaign draft",
	Description: "List the errors that occurred during the promotion of a campaign draft " +
		"(CampaignDraftService.ListCampaignDraftAsyncErrors of API version v26, GET customers/ID/campaignDrafts/" +
		"BASE_CAMPAIGN_ID~DRAFT_ID:listAsyncErrors, paged with a page token that Qatlas Connect wraps in an opaque " +
		"cursor). Google refuses the call before the draft is promoted. The state of the draft itself is read with " +
		"reports.search on the campaign_draft resource. campaign_draft must belong to customer_id; Qatlas Connect " +
		"builds the request from both. customer_id must be one of the targets or lie below a manager target; on a " +
		"connection with targets, a manager account needs a manager target that names it. Costs one operation of " +
		"the daily quota, plus one for a membership read below a manager target and one for the check that the " +
		"account is no manager account. The messages are untrusted provider text and are cut; the details of a " +
		"status are not returned.",
	Tags: []string{"google", "ads", "campaign-drafts", "async-errors"}, Provider: Provider,
	Risk: capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
		Confirmation: capability.ConfirmationNone, OpenWorld: true,
		DataSensitivity: "error codes and messages of the promotion of a campaign draft of a Google Ads account; the " +
			"messages are provider text and may name settings of the account; no data of other people"},
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "campaign_draft": campaignDraftNameSchema,
		"page_size": asyncErrorsPageProperties["page_size"], "cursor": asyncErrorsPageProperties["cursor"]},
		"customer_id", "campaign_draft")),
	OutputSchema: encoded(asyncErrorsOutputSchema),
	Arguments: append(append(slices.Clone(experimentsArgumentDocs),
		capability.Argument{Name: "campaign_draft", Required: true, Description: "A draft of the account, " +
			"customers/ID/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID, as reports.search returns it"}),
		asyncErrorsPageArguments...),
	Fields: asyncErrorsFields,
	Examples: []capability.Example{{Description: "List the errors of the last promotion of a draft",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_draft":"customers/1234567890/campaignDrafts/21~5"}`)}},
}

// --- handlers ---

func handleCampaignDraftsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create campaign drafts"
	var args campaignDraftsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, bases, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignDraftsSend(ctx, resolved, secrets, red, op, campaignDraftsCreateID, args.CustomerID, ops, nil, bases,
		args.ValidateOnly)
}

func handleCampaignDraftsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update campaign drafts"
	var args campaignDraftsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignDraftsSend(ctx, resolved, secrets, red, op, campaignDraftsUpdateID, args.CustomerID, ops, names, nil,
		args.ValidateOnly)
}

func handleCampaignDraftsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove campaign drafts"
	var args campaignDraftsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignDraftsSend(ctx, resolved, secrets, red, op, campaignDraftsRemoveID, args.CustomerID, ops, names, nil,
		args.ValidateOnly)
}

// campaignDraftsSend sends the operations once. A creation names the base campaign IDs it asked for, which each
// result must carry in order; an update or a removal names the drafts.
func campaignDraftsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, customerID string, ops []any, want, bases []string, validateOnly bool) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: customerID,
		service: campaignDraftsService, collection: campaignDraftsCollection, operations: ops, names: want,
		idOf: func(name string) (string, bool) { return campaignDraftIDOfName(name, customerID) }, validateOnly: validateOnly})
	if err != nil {
		return nil, err
	}
	out := make([]campaignDraftMutated, 0, len(results))
	seen := map[string]bool{}
	for i, r := range results {
		if seen[r.ID] {
			return nil, unsureResult(op, "answered with the same campaign draft twice")
		}
		seen[r.ID] = true
		base, draft, _ := strings.Cut(r.ID, "~")
		if bases != nil && base != bases[i] {
			return nil, unsureResult(op, "answered with a draft of another base campaign than the one asked for")
		}
		out = append(out, campaignDraftMutated{BaseCampaignID: base, DraftID: draft, ResourceName: r.ResourceName})
	}
	return campaignDraftsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: out}, nil
}

func handleCampaignDraftsPromote(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "promote campaign draft"
	var args campaignDraftsPromoteArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := campaignDraftNameOf("campaign_draft", args.CampaignDraft, args.CustomerID); err != nil {
		return nil, err
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, campaignDraftsPromoteID)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"campaignDraft": args.CampaignDraft}
	if args.ValidateOnly {
		body["validateOnly"] = true
	}
	encodedBody, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the request body could not be encoded")
	}
	var answer struct {
		Name  string `json:"name"`
		Done  bool   `json:"done"`
		Error *struct {
			Code    *int   `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := c.send(ctx, request{op: op, tool: campaignDraftsPromoteID, method: http.MethodPost,
		path: "/" + args.CampaignDraft + ":promote", body: encodedBody, onAccount: true, change: true}, &answer); err != nil {
		return nil, err
	}
	out := campaignDraftPromoted{CustomerID: args.CustomerID, CampaignDraft: args.CampaignDraft,
		ValidateOnly: args.ValidateOnly, Operation: boundedText(answer.Name, maxText), Done: answer.Done}
	if e := answer.Error; e != nil {
		out.Error = &asyncError{Message: boundedText(e.Message, maxText)}
		if e.Code != nil {
			out.Error.Code = *e.Code
		}
	}
	return out, nil
}

func handleCampaignDraftsListAsyncErrors(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list campaign draft async errors"
	var args campaignDraftsListAsyncErrorsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := campaignDraftNameOf("campaign_draft", args.CampaignDraft, args.CustomerID); err != nil {
		return nil, err
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, campaignDraftsListAsyncErrorsID)
	if err != nil {
		return nil, err
	}
	return env.listAsyncErrors(asyncErrorsSpec{op: op, tool: campaignDraftsListAsyncErrorsID, customerID: args.CustomerID,
		resource: args.CampaignDraft, method: "listAsyncErrors", pageSize: args.PageSize, cursor: args.Cursor})
}

// --- confirmation ---

func campaignDraftsCreateSummary(raw json.RawMessage) string {
	var a campaignDraftsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, d := range a.CampaignDrafts[:min(len(a.CampaignDrafts), 10)] {
		parts = append(parts, campaignGroupsQuote(d.Name)+" of base campaign "+boundedText(d.BaseCampaign, maxCodeText))
	}
	return campaignGroupsLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.CampaignDrafts)) +
		" campaign draft(s) in Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		campaignGroupsMore(len(a.CampaignDrafts), len(parts)) + "."
}

func campaignDraftsUpdateSummary(raw json.RawMessage) string {
	var a campaignDraftsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, d := range a.CampaignDrafts[:min(len(a.CampaignDrafts), 10)] {
		parts = append(parts, boundedText(d.CampaignDraft, maxCodeText)+" to "+campaignGroupsQuote(d.Name))
	}
	return campaignGroupsLead(a.ValidateOnly) + "Rename " + strconv.Itoa(len(a.CampaignDrafts)) +
		" campaign draft(s) of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		campaignGroupsMore(len(a.CampaignDrafts), len(parts)) + "."
}

func campaignDraftsRemoveSummary(raw json.RawMessage) string {
	var a campaignDraftsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, name := range a.CampaignDrafts[:min(len(a.CampaignDrafts), 10)] {
		shown = append(shown, boundedText(name, maxCodeText))
	}
	return campaignGroupsLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.CampaignDrafts)) +
		" campaign draft(s) from Google Ads account " + a.CustomerID + ": " + strings.Join(shown, ", ") +
		campaignGroupsMore(len(a.CampaignDrafts), len(shown)) +
		". The changes prepared in them are lost; no tool brings them back."
}

func campaignDraftsPromoteSummary(raw json.RawMessage) string {
	var a campaignDraftsPromoteArguments
	_ = json.Unmarshal(raw, &a)
	return campaignGroupsLead(a.ValidateOnly) + "Promote campaign draft " + boundedText(a.CampaignDraft, maxCodeText) +
		" of Google Ads account " + a.CustomerID + " into its base campaign. The base campaign then serves with the " +
		"changes of the draft, and a promotion that fails partway cannot be retried safely."
}
