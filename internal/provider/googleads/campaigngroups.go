package googleads

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	campaignGroupsCreateID = Provider + ".campaign-groups.create"
	campaignGroupsUpdateID = Provider + ".campaign-groups.update"
	campaignGroupsRemoveID = Provider + ".campaign-groups.remove"

	campaignGroupsService = "/campaignGroups"
)

// Bounds of the campaign group fields as the reference of v26 documents them: CampaignGroup.name is required
// on create, must not be empty and must not hold NUL, LF or CR; the reference states no length. The status is
// ENABLED or REMOVED; ENABLED is the default of a new group and REMOVED is what the remove operation does, so
// the only status a tool takes is ENABLED.
const (
	// campaignGroupNameMax is Qatlas's own bound, since the reference states none.
	campaignGroupNameMax = 255
	// campaignGroupsMaxOperations is Qatlas's own bound per call; Google accepts up to 10,000 operations per request.
	campaignGroupsMaxOperations = 50
	campaignGroupEnabled        = "ENABLED"
)

var campaignGroupIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

// campaignGroupName is the resource name of a campaign group, built from checked IDs alone.
func campaignGroupName(customerID, groupID string) string {
	return customerName(customerID) + "/campaignGroups/" + groupID
}

// campaignGroupIDOfName returns the campaign group ID of a resource name that belongs to the account.
func campaignGroupIDOfName(name, customerID string) (string, bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/campaignGroups/")
	if !ok || !campaignGroupIDPattern.MatchString(rest) {
		return "", false
	}
	if _, err := strconv.ParseInt(rest, 10, 64); err != nil {
		return "", false
	}
	return rest, true
}

func campaignGroupsCheckName(value string) error {
	n := utf8.RuneCountInString(value)
	if n < 1 || n > campaignGroupNameMax {
		return badRequest("name must be 1 to " + strconv.Itoa(campaignGroupNameMax) + " characters")
	}
	if !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\n\r") {
		return badRequest("name must not hold a null, line feed, or carriage return character")
	}
	return nil
}

func campaignGroupsCheckStatus(value string) error {
	if value != campaignGroupEnabled {
		return badRequest("status must be " + campaignGroupEnabled + "; remove a campaign group with campaign-groups.remove")
	}
	return nil
}

func campaignGroupsCheckID(argument, value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil || !campaignGroupIDPattern.MatchString(value) {
		return badRequest(argument + " must be the ID of an existing campaign group, a positive number")
	}
	return nil
}

func campaignGroupsCheckCount(argument string, n int) error {
	if n < 1 || n > campaignGroupsMaxOperations {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(campaignGroupsMaxOperations) + " entries")
	}
	return nil
}

// --- arguments ---

type (
	campaignGroupsNewGroup struct {
		Name   string  `json:"name"`
		Status *string `json:"status"`
	}
	campaignGroupsEditGroup struct {
		CampaignGroupID string  `json:"campaign_group_id"`
		Name            *string `json:"name"`
		Status          *string `json:"status"`
	}
	campaignGroupsCreateArguments struct {
		CustomerID          string                   `json:"customer_id"`
		CampaignGroups      []campaignGroupsNewGroup `json:"campaign_groups"`
		ValidateOnly        bool                     `json:"validate_only"`
		ResponseContentType string                   `json:"response_content_type"`
	}
	campaignGroupsUpdateArguments struct {
		CustomerID          string                    `json:"customer_id"`
		CampaignGroups      []campaignGroupsEditGroup `json:"campaign_groups"`
		ValidateOnly        bool                      `json:"validate_only"`
		ResponseContentType string                    `json:"response_content_type"`
	}
	campaignGroupsRemoveArguments struct {
		CustomerID       string   `json:"customer_id"`
		CampaignGroupIDs []string `json:"campaign_group_ids"`
		ValidateOnly     bool     `json:"validate_only"`
	}
)

// operations checks the arguments and builds the operations of the request.
func (a campaignGroupsCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	if err := campaignGroupsCheckCount("campaign_groups", len(a.CampaignGroups)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.CampaignGroups))
	for _, g := range a.CampaignGroups {
		if err := campaignGroupsCheckName(g.Name); err != nil {
			return nil, err
		}
		create := map[string]any{"name": g.Name}
		if g.Status != nil {
			if err := campaignGroupsCheckStatus(*g.Status); err != nil {
				return nil, err
			}
			create["status"] = *g.Status
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, nil
}

func (a campaignGroupsUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	if err := campaignGroupsCheckCount("campaign_groups", len(a.CampaignGroups)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.CampaignGroups))
	names := make([]string, 0, len(a.CampaignGroups))
	for _, g := range a.CampaignGroups {
		if err := campaignGroupsCheckID("campaign_group_id", g.CampaignGroupID); err != nil {
			return nil, nil, err
		}
		name := campaignGroupName(a.CustomerID, g.CampaignGroupID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("campaign_groups names each campaign_group_id at most once")
		}
		if g.Name == nil && g.Status == nil {
			return nil, nil, badRequest("each entry of campaign_groups needs at least one of name and status")
		}
		update := map[string]any{"resourceName": name}
		var mask []string
		if g.Name != nil {
			if err := campaignGroupsCheckName(*g.Name); err != nil {
				return nil, nil, err
			}
			update["name"] = *g.Name
			mask = append(mask, "name")
		}
		if g.Status != nil {
			if err := campaignGroupsCheckStatus(*g.Status); err != nil {
				return nil, nil, err
			}
			update["status"] = *g.Status
			mask = append(mask, "status")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a campaignGroupsRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := campaignGroupsCheckCount("campaign_group_ids", len(a.CampaignGroupIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.CampaignGroupIDs))
	names := make([]string, 0, len(a.CampaignGroupIDs))
	for _, id := range a.CampaignGroupIDs {
		if err := campaignGroupsCheckID("campaign_group_ids", id); err != nil {
			return nil, nil, err
		}
		name := campaignGroupName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("campaign_group_ids names each campaign group at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	campaignGroupsGroupResult struct {
		ResourceName string `json:"resource_name"`
		ID           string `json:"id,omitempty"`
		Name         string `json:"name,omitempty"`
		Status       string `json:"status,omitempty"`
	}
	campaignGroupsMutated struct {
		CampaignGroupID string                     `json:"campaign_group_id"`
		ResourceName    string                     `json:"resource_name"`
		CampaignGroup   *campaignGroupsGroupResult `json:"campaign_group,omitempty"`
	}
	campaignGroupsChanged struct {
		CustomerID   string                  `json:"customer_id"`
		ValidateOnly bool                    `json:"validate_only"`
		Results      []campaignGroupsMutated `json:"results"`
	}
	campaignGroupsAnswer struct {
		Results []struct {
			ResourceName  string          `json:"resourceName"`
			CampaignGroup json.RawMessage `json:"campaignGroup"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
	campaignGroupsWire struct {
		ResourceName string   `json:"resourceName"`
		ID           *wireInt `json:"id"`
		Name         string   `json:"name"`
		Status       string   `json:"status"`
	}
)

func campaignGroupsGroupOf(raw json.RawMessage, name, groupID string) (*campaignGroupsGroupResult, bool) {
	var w campaignGroupsWire
	if json.Unmarshal(raw, &w) != nil || w.ResourceName != name {
		return nil, false
	}
	out := &campaignGroupsGroupResult{ResourceName: name, Name: boundedText(w.Name, maxText), Status: code(w.Status)}
	if w.ID != nil {
		if string(*w.ID) != groupID {
			return nil, false
		}
		out.ID = groupID
	}
	return out, true
}

// campaignGroupsResults binds the answer to the checked account. With want names (update, remove) the results
// must be exactly these in order; a create names the new groups, which must belong to the account and be
// distinct. Everything else leaves the outcome open, since the request was sent.
func campaignGroupsResults(op, customerID string, answer campaignGroupsAnswer, count int, want []string,
	validateOnly bool) ([]campaignGroupsMutated, error) {
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(op, "answered with a partial failure although the request asked for none")
	}
	out := []campaignGroupsMutated{}
	if validateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != count {
		return nil, unsureResult(op, "answered with another number of results than the operations asked for")
	}
	for i, r := range answer.Results {
		id, ok := campaignGroupIDOfName(r.ResourceName, customerID)
		if want != nil && r.ResourceName != want[i] || !ok {
			return nil, unsureResult(op, "answered with a result for another campaign group than the one asked for")
		}
		for _, previous := range out {
			if previous.CampaignGroupID == id {
				return nil, unsureResult(op, "answered with the same campaign group twice")
			}
		}
		item := campaignGroupsMutated{CampaignGroupID: id, ResourceName: r.ResourceName}
		if len(r.CampaignGroup) > 0 && string(r.CampaignGroup) != "null" {
			group, ok := campaignGroupsGroupOf(r.CampaignGroup, r.ResourceName, id)
			if !ok {
				return nil, unsureResult(op, "answered with a campaign group that is not the one asked for")
			}
			item.CampaignGroup = group
		}
		out = append(out, item)
	}
	return out, nil
}

// --- descriptors ---

var (
	campaignGroupNameSchema   = schema{"type": "string", "minLength": 1, "maxLength": campaignGroupNameMax}
	campaignGroupStatusSchema = schema{"type": "string", "enum": []string{campaignGroupEnabled}}
	campaignGroupIDSchema     = schema{"type": "string", "pattern": campaignGroupIDPattern.String()}

	campaignGroupsGroupSchema = object(schema{"resource_name": stringSchema, "id": stringSchema, "name": stringSchema,
		"status": stringSchema}, "resource_name")
	campaignGroupsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"campaign_group_id": stringSchema, "resource_name": stringSchema,
			"campaign_group": campaignGroupsGroupSchema}, "campaign_group_id", "resource_name"))},
		"customer_id", "validate_only", "results")
)

const campaignGroupsSensitivity = "campaign groups of a Google Ads account: their names, which the " +
	"confirmation names; no data of other people"

const campaignGroupsNote = " A campaign group collects campaigns for reporting; this tool does not assign a " +
	"campaign to a group or read groups (read them with reports.search on the campaign_group resource). The " +
	"request goes to CampaignGroupService.MutateCampaignGroups with up to 50 operations, atomic and without " +
	"partial_failure: Google applies all of them or none."

var campaignGroupsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: campaign_group_id, " +
		"resource_name (customers/ID/campaignGroups/GROUP_ID, as Google answered, always of the account of the " +
		"call), and with MUTABLE_RESOURCE the campaign group after the change (id, name, status) as Google " +
		"returns it. Empty after validate_only"},
}

var campaignGroupsArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

var campaignGroupsCreate = capability.Descriptor{
	ID: campaignGroupsCreateID, Version: 1, Title: "Create campaign groups in a Google Ads account",
	Description: "Create one or more campaign groups in a Google Ads account: a name of 1 to " +
		strconv.Itoa(campaignGroupNameMax) + " characters without null, line feed, or carriage return. A new " +
		"group is ENABLED, which status may also be given. Google decides whether a name may be used twice." +
		campaignGroupsNote + " Offered by a connection with the create effect or whose tools list names it; no " +
		"profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "campaign-groups", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, campaignGroupsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_groups": schema{"type": "array", "minItems": 1, "maxItems": campaignGroupsMaxOperations,
			"items": object(schema{"name": campaignGroupNameSchema, "status": campaignGroupStatusSchema}, "name")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "campaign_groups")),
	OutputSchema: encoded(campaignGroupsOutputSchema),
	Arguments: append(slices.Clone(campaignGroupsArgumentDocs),
		capability.Argument{Name: "campaign_groups", Required: true, Description: "1 to 50 campaign groups, each " +
			"with name (required, 1 to 255 characters, no null, line feed, or carriage return) and status " +
			"(ENABLED, the default)"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: campaignGroupsFields,
	Examples: []capability.Example{{Description: "Create a campaign group",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_groups":[{"name":"Spring sale"}]}`)}},
}

var campaignGroupsUpdate = capability.Descriptor{
	ID: campaignGroupsUpdateID, Version: 1, Title: "Change campaign groups of a Google Ads account",
	Description: "Change the name or the status of existing campaign groups of a Google Ads account (an update " +
		"with a field mask that names exactly the fields given). Only the campaign_group_id selects a group: its " +
		"resource name is built from customer_id and campaign_group_id. The only status a tool sets is ENABLED; " +
		"campaign-groups.remove removes a group." + campaignGroupsNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "campaign-groups", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, campaignGroupsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_groups": schema{"type": "array", "minItems": 1, "maxItems": campaignGroupsMaxOperations,
			"items": object(schema{"campaign_group_id": campaignGroupIDSchema, "name": campaignGroupNameSchema,
				"status": campaignGroupStatusSchema}, "campaign_group_id")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "campaign_groups")),
	OutputSchema: encoded(campaignGroupsOutputSchema),
	Arguments: append(slices.Clone(campaignGroupsArgumentDocs),
		capability.Argument{Name: "campaign_groups", Required: true, Description: "1 to 50 entries, each " +
			"campaign_group_id once, with at least one of name (1 to 255 characters) and status (ENABLED). " +
			"campaign_group_id is the id of the group as reports.search returns it"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: campaignGroupsFields,
	Examples: []capability.Example{{Description: "Rename a campaign group",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_groups":[{"campaign_group_id":"112233","name":"Autumn sale"}]}`)}},
}

var campaignGroupsRemove = capability.Descriptor{
	ID: campaignGroupsRemoveID, Version: 1, Title: "Remove campaign groups from a Google Ads account",
	Description: "Remove existing campaign groups of a Google Ads account (a remove of " +
		"customers/ID/campaignGroups/GROUP_ID, built from customer_id and campaign_group_id). Google marks a " +
		"removed group REMOVED; no tool of this provider brings it back." + campaignGroupsNote + mutateListListedNote,
	Tags: []string{"google", "ads", "campaign-groups", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, campaignGroupsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_group_ids": schema{"type": "array", "minItems": 1, "maxItems": campaignGroupsMaxOperations,
			"uniqueItems": true, "items": campaignGroupIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "campaign_group_ids")),
	OutputSchema: encoded(campaignGroupsOutputSchema),
	Arguments: append(slices.Clone(campaignGroupsArgumentDocs),
		capability.Argument{Name: "campaign_group_ids", Required: true, Description: "1 to 50 IDs of existing " +
			"campaign groups, each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: campaignGroupsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a campaign group without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_group_ids":["112233"],"validate_only":true}`)}},
}

// --- handlers ---

func handleCampaignGroupsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create campaign groups"
	var args campaignGroupsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignGroupsSend(ctx, resolved, secrets, red, op, campaignGroupsCreateID, args.CustomerID, ops, nil,
		args.ValidateOnly, args.ResponseContentType)
}

func handleCampaignGroupsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update campaign groups"
	var args campaignGroupsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignGroupsSend(ctx, resolved, secrets, red, op, campaignGroupsUpdateID, args.CustomerID, ops, names,
		args.ValidateOnly, args.ResponseContentType)
}

func handleCampaignGroupsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove campaign groups"
	var args campaignGroupsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignGroupsSend(ctx, resolved, secrets, red, op, campaignGroupsRemoveID, args.CustomerID, ops, names,
		args.ValidateOnly, "")
}

func campaignGroupsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, customerID string, ops []any, want []string, validateOnly bool, content string) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"operations": ops}
	if validateOnly {
		body["validateOnly"] = true
	}
	if content != "" {
		body["responseContentType"] = content
	}
	var answer campaignGroupsAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: customerID, service: campaignGroupsService,
		body: body}, &answer); err != nil {
		return nil, err
	}
	results, err := campaignGroupsResults(op, customerID, answer, len(ops), want, validateOnly)
	if err != nil {
		return nil, err
	}
	return campaignGroupsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: results}, nil
}

// --- confirmation ---

// campaignGroupsQuote shows a name in the confirmation, cut and without control characters.
func campaignGroupsQuote(value string) string {
	return strconv.Quote(boundedText(value, campaignGroupNameMax*4))
}

func campaignGroupsLead(validateOnly bool) string {
	if validateOnly {
		return "Only validate: "
	}
	return ""
}

func campaignGroupsMore(total, shown int) string {
	if total > shown {
		return " and " + strconv.Itoa(total-shown) + " more"
	}
	return ""
}

func campaignGroupsCreateSummary(raw json.RawMessage) string {
	var a campaignGroupsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, g := range a.CampaignGroups[:min(len(a.CampaignGroups), 10)] {
		parts = append(parts, campaignGroupsQuote(g.Name))
	}
	return campaignGroupsLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.CampaignGroups)) +
		" campaign group(s) in Google Ads account " + a.CustomerID + ": " + strings.Join(parts, ", ") +
		campaignGroupsMore(len(a.CampaignGroups), len(parts)) + "."
}

func campaignGroupsUpdateSummary(raw json.RawMessage) string {
	var a campaignGroupsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, g := range a.CampaignGroups[:min(len(a.CampaignGroups), 10)] {
		part := "campaign group " + boundedText(g.CampaignGroupID, maxCodeText)
		if g.Name != nil {
			part += " name " + campaignGroupsQuote(*g.Name)
		}
		if g.Status != nil {
			part += " status " + campaignGroupsQuote(*g.Status)
		}
		parts = append(parts, part)
	}
	return campaignGroupsLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.CampaignGroups)) +
		" campaign group(s) of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		campaignGroupsMore(len(a.CampaignGroups), len(parts)) + "."
}

func campaignGroupsRemoveSummary(raw json.RawMessage) string {
	var a campaignGroupsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.CampaignGroupIDs[:min(len(a.CampaignGroupIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	return campaignGroupsLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.CampaignGroupIDs)) +
		" campaign group(s) from Google Ads account " + a.CustomerID + ": " + strings.Join(shown, ", ") +
		campaignGroupsMore(len(a.CampaignGroupIDs), len(shown)) + ". Google marks them removed; no tool brings them back."
}
