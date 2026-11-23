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
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	customConversionGoalsCreateID = Provider + ".custom-conversion-goals.create"
	customConversionGoalsUpdateID = Provider + ".custom-conversion-goals.update"
	customConversionGoalsRemoveID = Provider + ".custom-conversion-goals.remove"

	customConversionGoalService = "/customConversionGoals"

	// The reference names no length or size limit for these two; Qatlas keeps its own bounds and Google validates
	// the rest.
	maxCustomGoalNameLength = 200
	maxCustomGoalActions    = 50
)

// CustomConversionGoalService has no partial_failure, and status is left out: ENABLED is the only value to
// send and REMOVED belongs to the remove tool.
type (
	customGoalItem struct {
		CustomConversionGoalID string   `json:"custom_conversion_goal_id"`
		Name                   *string  `json:"name"`
		ConversionActions      []string `json:"conversion_actions"`
	}
	customGoalsArguments struct {
		CustomerID              string          `json:"customer_id"`
		CustomConversionGoals   json.RawMessage `json:"custom_conversion_goals"`
		CustomConversionGoalIDs []string        `json:"custom_conversion_goal_ids"`
		ValidateOnly            bool            `json:"validate_only"`
		ResponseContentType     string          `json:"response_content_type"`
	}
)

func customConversionGoalName(customerID, id string) string {
	return customerName(customerID) + customConversionGoalService + "/" + id
}

func customConversionGoalIDOf(name, customerID string) (string, bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+customConversionGoalService+"/")
	if !ok || !conversionActionIDPattern.MatchString(rest) {
		return "", false
	}
	_, err := strconv.ParseInt(rest, 10, 64)
	return rest, err == nil
}

func checkCustomGoalID(argument, value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil || !conversionActionIDPattern.MatchString(value) {
		return badRequest(argument + " must be the numeric ID of an existing custom conversion goal")
	}
	return nil
}

// check refuses what no request may start with. The actions must be resource names of the checked account.
func (i customGoalItem) check(customerID string, create bool) error {
	if i.Name != nil {
		if n := utf8.RuneCountInString(*i.Name); n < 1 || n > maxCustomGoalNameLength || strings.TrimSpace(*i.Name) == "" ||
			strings.IndexFunc(*i.Name, unicode.IsControl) >= 0 {
			return badRequest("name must be 1 to 200 characters without control characters")
		}
	} else if create {
		return badRequest("every custom conversion goal needs a name")
	}
	if i.ConversionActions == nil {
		if create {
			return badRequest("every custom conversion goal needs conversion_actions")
		}
		return nil
	}
	if n := len(i.ConversionActions); n < 1 || n > maxCustomGoalActions {
		return badRequest("conversion_actions holds one to 50 conversion actions")
	}
	for k, name := range i.ConversionActions {
		if !ownedName(name, customerID, "conversionActions") {
			return badRequest("conversion_actions names conversion actions of the account as customers/" +
				queryNumber(customerID) + "/conversionActions/ID")
		}
		if slices.Contains(i.ConversionActions[:k], name) {
			return badRequest("conversion_actions names each conversion action once")
		}
	}
	return nil
}

// --- schemas ---

var (
	customGoalProperties = func() schema {
		return schema{
			"name": schema{"type": "string", "minLength": 1, "maxLength": maxCustomGoalNameLength},
			"conversion_actions": schema{"type": "array", "minItems": 1, "maxItems": maxCustomGoalActions,
				"uniqueItems": true, "items": schema{"type": "string",
					"pattern": `^customers/[0-9]+/conversionActions/[1-9][0-9]{0,18}$`}},
		}
	}

	customGoalCreateItemSchema = object(customGoalProperties(), "name", "conversion_actions")
	customGoalUpdateItemSchema = func() schema {
		p := customGoalProperties()
		p["custom_conversion_goal_id"] = schema{"type": "string", "pattern": conversionActionIDPattern.String()}
		s := object(p, "custom_conversion_goal_id")
		s["minProperties"] = 2
		return s
	}()

	customGoalResourceSchema = object(schema{"resource_name": stringSchema, "id": stringSchema, "name": stringSchema,
		"status": stringSchema, "conversion_actions": arrayOf(stringSchema)})

	customGoalOutputSchema = encoded(object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"custom_conversion_goal_id": stringSchema, "resource_name": stringSchema,
			"custom_conversion_goal": customGoalResourceSchema}, "custom_conversion_goal_id", "resource_name"))},
		"customer_id", "validate_only", "results"))
)

func customGoalInputSchema(items schema, idList bool) json.RawMessage {
	properties := schema{"customer_id": customerIDArgument, "validate_only": validateOnlySchema,
		"response_content_type": responseContentTypeSchema}
	required := []string{"customer_id"}
	if idList {
		properties["custom_conversion_goal_ids"] = schema{"type": "array", "minItems": 1,
			"maxItems": maxListOperations, "uniqueItems": true,
			"items": schema{"type": "string", "pattern": conversionActionIDPattern.String()}}
		delete(properties, "response_content_type")
		required = append(required, "custom_conversion_goal_ids")
	} else {
		properties["custom_conversion_goals"] = schema{"type": "array", "minItems": 1,
			"maxItems": maxListOperations, "items": items}
		required = append(required, "custom_conversion_goals")
	}
	return encoded(object(properties, required...))
}

var (
	customGoalSettingsDoc = "name (1 to 200 characters) and conversion_actions (one to 50 resource names " +
		"customers/ID/conversionActions/ID of conversion actions of the same account, each once; on an update they " +
		"replace the whole list)"

	customGoalFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the request: results is then empty"},
		{Name: "results", Description: "One entry per operation, in the order of the request: " +
			"custom_conversion_goal_id, resource_name (customers/ID/customConversionGoals/ID, as Google answered, " +
			"always of the account of the call), and with MUTABLE_RESOURCE the custom_conversion_goal as Google " +
			"returns it: id, name, status and conversion_actions"},
	}
)

const customGoalNote = " A custom conversion goal bundles conversion actions and steers the bidding of every " +
	"campaign that uses it, so a change can change what those campaigns optimize and bid for. Up to 10 entries go " +
	"in one request; the service has no partial_failure. The goal's status is not offered: a goal is removed with " +
	"custom-conversion-goals.remove. Custom conversion goals are read with reports.search on custom_conversion_goal."

// --- descriptors ---

var customConversionGoalsCreate = capability.Descriptor{
	ID: customConversionGoalsCreateID, Version: 1, Title: "Create custom conversion goals of a Google Ads account",
	Description: "Create one to 10 custom conversion goals in a Google Ads account (CustomConversionGoalService." +
		"MutateCustomConversionGoals with create operations). Each needs a name and conversion_actions: " +
		customGoalSettingsDoc + "." + customGoalNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent,
		"conversion goal settings of a Google Ads account"),
	InputSchema: customGoalInputSchema(customGoalCreateItemSchema, false), OutputSchema: customGoalOutputSchema,
	Arguments: append([]capability.Argument{customerArgumentDoc,
		{Name: "custom_conversion_goals", Required: true, Description: "One to 10 custom conversion goals to create, " +
			"each with " + customGoalSettingsDoc}}, conversionResponseDoc...),
	Fields: customGoalFields,
	Examples: []capability.Example{{Description: "Validate a goal of two conversion actions without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","custom_conversion_goals":[{"name":"Leads",` +
			`"conversion_actions":["customers/1234567890/conversionActions/5544332211",` +
			`"customers/1234567890/conversionActions/5544332212"]}],"validate_only":true}`)}},
}

var customConversionGoalsUpdate = capability.Descriptor{
	ID: customConversionGoalsUpdateID, Version: 1, Title: "Change custom conversion goals of a Google Ads account",
	Description: "Change one to 10 existing custom conversion goals of a Google Ads account " +
		"(CustomConversionGoalService.MutateCustomConversionGoals with update operations and a mask of exactly the " +
		"given settings). Each entry names the custom_conversion_goal_id and at least one of: " +
		customGoalSettingsDoc + "." + customGoalNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
		"conversion goal settings of a Google Ads account"),
	InputSchema: customGoalInputSchema(customGoalUpdateItemSchema, false), OutputSchema: customGoalOutputSchema,
	Arguments: append([]capability.Argument{customerArgumentDoc,
		{Name: "custom_conversion_goals", Required: true, Description: "One to 10 entries, each with " +
			"custom_conversion_goal_id (as reports.search returns it) and at least one of: " + customGoalSettingsDoc}},
		conversionResponseDoc...),
	Fields: customGoalFields,
	Examples: []capability.Example{{Description: "Rename a goal, validated only",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","custom_conversion_goals":[` +
			`{"custom_conversion_goal_id":"7788990011","name":"Qualified leads"}],"validate_only":true}`)}},
}

var customConversionGoalsRemove = capability.Descriptor{
	ID: customConversionGoalsRemoveID, Version: 1, Title: "Remove custom conversion goals of a Google Ads account",
	Description: "Remove one to 10 custom conversion goals of a Google Ads account (CustomConversionGoalService." +
		"MutateCustomConversionGoals with remove operations of customers/ID/customConversionGoals/ID). Campaigns " +
		"that use a removed goal lose it as a bidding signal, and Google may refuse the removal while a campaign " +
		"still uses it. The tool builds the names from customer_id and the numeric IDs and takes no other " +
		"settings. The service has no partial_failure, and the removal answers the resource names only, so there is " +
		"no response_content_type." + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent,
		"conversion goal settings of a Google Ads account"),
	InputSchema: customGoalInputSchema(nil, true), OutputSchema: customGoalOutputSchema,
	Arguments: []capability.Argument{customerArgumentDoc,
		{Name: "custom_conversion_goal_ids", Required: true, Description: "One to 10 distinct numeric IDs of custom " +
			"conversion goals of the account, as reports.search returns them"}, validateOnlyArgument},
	Fields: customGoalFields,
	Examples: []capability.Example{{Description: "Validate the removal of a goal",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","custom_conversion_goal_ids":["7788990011"],"validate_only":true}`)}},
}

// --- handlers ---

func handleCustomConversionGoalsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return customGoalsChange(ctx, resolved, secrets, red, raw, customConversionGoalsCreateID)
}

func handleCustomConversionGoalsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return customGoalsChange(ctx, resolved, secrets, red, raw, customConversionGoalsUpdateID)
}

func handleCustomConversionGoalsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return customGoalsChange(ctx, resolved, secrets, red, raw, customConversionGoalsRemoveID)
}

type (
	customGoalsResult struct {
		CustomerID   string                  `json:"customer_id"`
		ValidateOnly bool                    `json:"validate_only"`
		Results      []customGoalResultEntry `json:"results"`
	}
	customGoalResultEntry struct {
		CustomConversionGoalID string              `json:"custom_conversion_goal_id"`
		ResourceName           string              `json:"resource_name"`
		CustomConversionGoal   *customGoalResource `json:"custom_conversion_goal,omitempty"`
	}
	customGoalResource struct {
		ResourceName      string   `json:"resource_name"`
		ID                string   `json:"id,omitempty"`
		Name              string   `json:"name,omitempty"`
		Status            string   `json:"status,omitempty"`
		ConversionActions []string `json:"conversion_actions,omitempty"`
	}
)

func customGoalsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, tool string) (any, error) {
	op := map[string]string{customConversionGoalsCreateID: "create custom conversion goals",
		customConversionGoalsUpdateID: "update custom conversion goals",
		customConversionGoalsRemoveID: "remove custom conversion goals"}[tool]
	var args customGoalsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(args.ResponseContentType); err != nil {
		return nil, err
	}
	operations, names, err := args.operations(tool)
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Results []struct {
			ResourceName         string          `json:"resourceName"`
			CustomConversionGoal json.RawMessage `json:"customConversionGoal"`
		} `json:"results"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: args.CustomerID,
		service: customConversionGoalService,
		body:    mutateOperationsBody(operations, args.ValidateOnly, args.ResponseContentType)}, &answer); err != nil {
		return nil, err
	}
	// The request was sent: an answer that does not fit it leaves the outcome open.
	invalid := func(what string) error {
		return unsure(provider.InvalidResponse(op, subject+" answered with "+what), !args.ValidateOnly)
	}
	out := customGoalsResult{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly,
		Results: []customGoalResultEntry{}}
	if args.ValidateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != len(operations) {
		return nil, invalid("a number of results that is not the number of operations")
	}
	seen := map[string]bool{}
	for i, r := range answer.Results {
		id, ok := customConversionGoalIDOf(r.ResourceName, args.CustomerID)
		if !ok || seen[id] || (names[i] != "" && r.ResourceName != names[i]) {
			return nil, invalid("a result for another custom conversion goal or account than the one asked for")
		}
		seen[id] = true
		item := customGoalResultEntry{CustomConversionGoalID: id, ResourceName: r.ResourceName}
		if len(r.CustomConversionGoal) > 0 && string(r.CustomConversionGoal) != "null" {
			goal, ok := customGoalOf(r.CustomConversionGoal, r.ResourceName, id)
			if !ok {
				return nil, invalid("a custom conversion goal that is not the one asked for")
			}
			item.CustomConversionGoal = goal
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// operations checks the arguments of the tool and builds the operations. names holds the resource name each
// operation must be answered with, empty for a create, whose ID Google assigns.
func (a customGoalsArguments) operations(tool string) (operations []any, names []string, err error) {
	if tool == customConversionGoalsRemoveID {
		if n := len(a.CustomConversionGoalIDs); n < 1 || n > maxListOperations {
			return nil, nil, badRequest("custom_conversion_goal_ids names one to 10 custom conversion goals")
		}
		for i, id := range a.CustomConversionGoalIDs {
			if err := checkCustomGoalID("custom_conversion_goal_ids", id); err != nil {
				return nil, nil, err
			}
			if slices.Contains(a.CustomConversionGoalIDs[:i], id) {
				return nil, nil, badRequest("custom_conversion_goal_ids names each custom conversion goal once")
			}
			name := customConversionGoalName(a.CustomerID, id)
			operations = append(operations, map[string]any{"remove": name})
			names = append(names, name)
		}
		return operations, names, nil
	}
	create := tool == customConversionGoalsCreateID
	var items []customGoalItem
	if json.Unmarshal(a.CustomConversionGoals, &items) != nil || len(items) < 1 || len(items) > maxListOperations {
		return nil, nil, badRequest("custom_conversion_goals holds one to 10 custom conversion goals")
	}
	for i, item := range items {
		if create && item.CustomConversionGoalID != "" {
			return nil, nil, badRequest("a custom conversion goal to create takes no custom_conversion_goal_id")
		}
		if err := item.check(a.CustomerID, create); err != nil {
			return nil, nil, err
		}
		resource := map[string]any{}
		var mask []string
		if item.Name != nil {
			resource["name"] = *item.Name
			mask = append(mask, "name")
		}
		if item.ConversionActions != nil {
			resource["conversionActions"] = item.ConversionActions
			mask = append(mask, "conversionActions")
		}
		if create {
			operations = append(operations, map[string]any{"create": resource})
			names = append(names, "")
			continue
		}
		if err := checkCustomGoalID("custom_conversion_goal_id", item.CustomConversionGoalID); err != nil {
			return nil, nil, err
		}
		for _, earlier := range items[:i] {
			if earlier.CustomConversionGoalID == item.CustomConversionGoalID {
				return nil, nil, badRequest("custom_conversion_goals names each custom conversion goal once")
			}
		}
		if len(mask) == 0 {
			return nil, nil, badRequest("every entry of custom_conversion_goals needs at least one setting to change")
		}
		name := customConversionGoalName(a.CustomerID, item.CustomConversionGoalID)
		resource["resourceName"] = name
		operations = append(operations, map[string]any{"update": resource, "updateMask": strings.Join(mask, ",")})
		names = append(names, name)
	}
	return operations, names, nil
}

// customGoalOf maps the custom conversion goal of a mutate answer. It must be the one of the result.
func customGoalOf(raw json.RawMessage, resourceName, id string) (*customGoalResource, bool) {
	var w struct {
		ResourceName      string   `json:"resourceName"`
		ID                *wireInt `json:"id"`
		Name              string   `json:"name"`
		Status            string   `json:"status"`
		ConversionActions []string `json:"conversionActions"`
	}
	if json.Unmarshal(raw, &w) != nil || (w.ResourceName != "" && w.ResourceName != resourceName) {
		return nil, false
	}
	out := &customGoalResource{ResourceName: resourceName, Name: boundedText(w.Name, maxText), Status: code(w.Status)}
	if w.ID != nil {
		if string(*w.ID) != id {
			return nil, false
		}
		out.ID = id
	}
	for _, name := range w.ConversionActions[:min(len(w.ConversionActions), maxListItems)] {
		out.ConversionActions = append(out.ConversionActions, boundedText(name, maxText))
	}
	return out, true
}

// customGoalsSummary phrases the change for the confirmation, from the arguments alone.
func customGoalsSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a customGoalsArguments
		_ = json.Unmarshal(raw, &a)
		where := " in Google Ads account " + a.CustomerID
		prefix := ""
		if a.ValidateOnly {
			prefix = "Only validate: "
		}
		if tool == customConversionGoalsRemoveID {
			return prefix + "Remove " + strconv.Itoa(len(a.CustomConversionGoalIDs)) + " custom conversion goal(s) (" +
				strings.Join(a.CustomConversionGoalIDs, ", ") + ")" + where +
				": campaigns that use them lose them as a bidding signal."
		}
		var items []customGoalItem
		_ = json.Unmarshal(a.CustomConversionGoals, &items)
		var parts []string
		for _, item := range items {
			part := item.CustomConversionGoalID
			if item.Name != nil {
				part += " " + boundedText(*item.Name, 80)
			}
			if item.ConversionActions != nil {
				part += " (" + strconv.Itoa(len(item.ConversionActions)) + " conversion actions)"
			}
			parts = append(parts, strings.TrimSpace(part))
		}
		verb := "Change "
		if tool == customConversionGoalsCreateID {
			verb = "Create "
		}
		return prefix + verb + strconv.Itoa(len(items)) + " custom conversion goal(s)" + where + ": " +
			strings.Join(parts, "; ") + ". This can change what campaigns that use them optimize and bid for."
	}
}
