package googleads

import (
	"context"
	"encoding/json"
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
	remarketingActionsCreateID = Provider + ".remarketing-actions.create"
	remarketingActionsUpdateID = Provider + ".remarketing-actions.update"

	remarketingService    = "/remarketingActions"
	remarketingCollection = "remarketingActions"
)

// A remarketing action has only one field a request can set: its name. Its tag snippets are output only, and
// Google answers a change with the resource name alone: MutateRemarketingActionsRequest has no
// response_content_type. The snippets are website code of the provider and never pass through these tools; a
// report on remarketing_action reads them as untrusted text.

type (
	remarketingEntry struct {
		ID   string `json:"remarketing_action_id"`
		Name string `json:"name"`
	}
	remarketingArguments struct {
		CustomerID       string             `json:"customer_id"`
		RemarketingItems []remarketingEntry `json:"remarketing_actions"`
		ValidateOnly     bool               `json:"validate_only"`
	}
)

// operations checks the entries and builds the operations. The name is the one mutable field; Google
// documents no bound for it, so the bound of 1024 bytes is Qatlas's own.
func (a remarketingArguments) operations(create bool) ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if n := len(a.RemarketingItems); n == 0 || n > maxListOperations {
		return nil, nil, badRequest("remarketing_actions must hold one to 10 entries")
	}
	var ops []any
	var names, seen []string
	for _, e := range a.RemarketingItems {
		if strings.TrimSpace(e.Name) == "" || len(e.Name) > maxNameArgument || strings.ContainsFunc(e.Name, unicode.IsControl) {
			return nil, nil, badRequest("name must be text of 1 to 1024 bytes without control characters")
		}
		if create {
			if e.ID != "" {
				return nil, nil, badRequest("a new remarketing action takes no remarketing_action_id")
			}
			ops = append(ops, map[string]any{"create": map[string]any{"name": e.Name}})
			continue
		}
		if !numericIDPattern.MatchString(e.ID) {
			return nil, nil, badRequest("every entry needs the remarketing_action_id of an existing remarketing " +
				"action, a positive number")
		}
		if slices.Contains(seen, e.ID) {
			return nil, nil, badRequest("a remarketing action appears in one entry only")
		}
		seen = append(seen, e.ID)
		name := customerName(a.CustomerID) + "/" + remarketingCollection + "/" + e.ID
		names = append(names, name)
		ops = append(ops, map[string]any{"update": map[string]any{"resourceName": name, "name": e.Name},
			"updateMask": "name"})
	}
	return ops, names, nil
}

type (
	remarketingResult struct {
		ResourceName        string `json:"resource_name"`
		RemarketingActionID string `json:"remarketing_action_id"`
	}
	remarketingChanged struct {
		CustomerID   string              `json:"customer_id"`
		ValidateOnly bool                `json:"validate_only"`
		Results      []remarketingResult `json:"results"`
	}
)

var (
	remarketingOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema, "remarketing_action_id": stringSchema},
			"resource_name", "remarketing_action_id"))}, "customer_id", "validate_only", "results")

	remarketingNameSchema = schema{"type": "string", "minLength": 1, "maxLength": maxNameArgument}

	remarketingFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "One result per entry, in the order of the entries, empty after validate_only: " +
			"resource_name (customers/ID/remarketingActions/N, as Google answered) and remarketing_action_id (its " +
			"number). Google answers nothing else, and the tag snippets are not part of the answer"}}
)

const remarketingEffectNote = "A remarketing action is the website tracking code of an account: it adds visitors " +
	"to remarketing lists and counts events on the pages of a website. A request can set only its name; Google " +
	"generates the tag snippets, which belong to the person who installs them on a website. The tool neither " +
	"takes nor returns snippets: they are output only, and reports.search reads them from remarketing_action, as " +
	"untrusted text. The remarketing actions are read with reports.search on remarketing_action."

var remarketingCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit " +
	"customer ID of the account without hyphens; a target of the connection or an account below a manager target"}

var remarketingActionsCreate = capability.Descriptor{
	ID: remarketingActionsCreateID, Version: 1, Title: "Create remarketing actions of a Google Ads account",
	Description: "Create one to 10 remarketing actions in a Google Ads account " +
		"(RemarketingActionService.MutateRemarketingActions with create operations): each needs a name. " +
		remarketingEffectNote + mutateListNote,
	Tags: []string{"google", "ads", "remarketing", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent,
		"website tracking definitions of a Google Ads account: remarketing action names"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"remarketing_actions": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"name": remarketingNameSchema}, "name")},
		"validate_only": validateOnlySchema}, "customer_id", "remarketing_actions")),
	OutputSchema: encoded(remarketingOutputSchema),
	Arguments: []capability.Argument{remarketingCustomerArgument,
		{Name: "remarketing_actions", Required: true, Description: "One to 10 entries, each with name (1 to 1024 bytes, " +
			"no control characters)"}, validateOnlyArgument},
	Fields: remarketingFields,
	Examples: []capability.Example{{Description: "Validate a new remarketing action without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","remarketing_actions":[{"name":"Shop visitors"}],"validate_only":true}`)}},
}

var remarketingActionsUpdate = capability.Descriptor{
	ID: remarketingActionsUpdateID, Version: 1, Title: "Rename remarketing actions of a Google Ads account",
	Description: "Rename one to 10 existing remarketing actions of a Google Ads account " +
		"(RemarketingActionService.MutateRemarketingActions with update operations and the mask name): the name " +
		"is the only field that can change. " + remarketingEffectNote + mutateListNote,
	Tags: []string{"google", "ads", "remarketing", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
		"website tracking definitions of a Google Ads account: remarketing action names"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"remarketing_actions": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"remarketing_action_id": schema{"type": "string", "pattern": numericIDPattern.String()},
				"name": remarketingNameSchema}, "remarketing_action_id", "name")},
		"validate_only": validateOnlySchema}, "customer_id", "remarketing_actions")),
	OutputSchema: encoded(remarketingOutputSchema),
	Arguments: []capability.Argument{remarketingCustomerArgument,
		{Name: "remarketing_actions", Required: true, Description: "One to 10 entries, each with " +
			"remarketing_action_id (the number at the end of the resource name that reports.search or create returns, " +
			"each action once) and the new name (1 to 1024 bytes, no control characters)"}, validateOnlyArgument},
	Fields: remarketingFields,
	Examples: []capability.Example{{Description: "Rename a remarketing action", Arguments: json.RawMessage(
		`{"customer_id":"1234567890","remarketing_actions":[{"remarketing_action_id":"123","name":"All visitors"}]}`)}},
}

func handleRemarketingActionsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return remarketingActionsChange(ctx, resolved, secrets, red, raw, true)
}

func handleRemarketingActionsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return remarketingActionsChange(ctx, resolved, secrets, red, raw, false)
}

func remarketingActionsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, create bool) (any, error) {
	op, tool := "update remarketing actions", remarketingActionsUpdateID
	if create {
		op, tool = "create remarketing actions", remarketingActionsCreateID
	}
	var args remarketingArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	operations, names, err := args.operations(create)
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: args.CustomerID,
		service: remarketingService, collection: remarketingCollection, resourceKey: "remarketingAction",
		operations: operations, names: names, validateOnly: args.ValidateOnly})
	if err != nil {
		return nil, err
	}
	out := remarketingChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly, Results: []remarketingResult{}}
	for _, r := range results {
		out.Results = append(out.Results, remarketingResult{ResourceName: r.ResourceName, RemarketingActionID: r.ID})
	}
	return out, nil
}

// remarketingActionsSummary phrases the change for the confirmation, from the arguments alone.
func remarketingActionsSummary(create bool) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a remarketingArguments
		_ = json.Unmarshal(raw, &a)
		var parts []string
		for _, e := range a.RemarketingItems {
			if create {
				parts = append(parts, "create "+strconv.Quote(e.Name))
			} else {
				parts = append(parts, "rename action "+e.ID+" to "+strconv.Quote(e.Name))
			}
		}
		lead := "Remarketing actions of Google Ads account " + a.CustomerID + ": "
		if a.ValidateOnly {
			lead = "Only validate, remarketing actions of Google Ads account " + a.CustomerID + ": "
		}
		return lead + strings.Join(parts, "; ") + "."
	}
}
