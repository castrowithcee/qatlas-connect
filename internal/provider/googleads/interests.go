package googleads

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	customInterestsCreateID = Provider + ".custom-interests.create"
	customInterestsUpdateID = Provider + ".custom-interests.update"

	customInterestService    = "/customInterests"
	customInterestCollection = "customInterests"
)

// Qatlas' own bound of the members of a custom interest; Google documents none.
const customInterestMaxMembers = 100

var (
	customInterestTypes       = []string{"CUSTOM_AFFINITY", "CUSTOM_INTENT"}
	customInterestMemberTypes = []string{"KEYWORD", "URL"}
)

// customInterestMemberArgument is a keyword or a URL of a custom interest. Both are data for Google; Qatlas never fetches a
// URL.
type customInterestMemberArgument struct {
	MemberType string `json:"member_type"`
	Parameter  string `json:"parameter"`
}

func checkCustomInterestMembers(argument string, members []customInterestMemberArgument) ([]any, error) {
	if len(members) > customInterestMaxMembers {
		return nil, badRequest(fmt.Sprintf("%s must hold at most %d members", argument, customInterestMaxMembers))
	}
	out := make([]any, 0, len(members))
	for _, m := range members {
		if !slices.Contains(customInterestMemberTypes, m.MemberType) {
			return nil, badRequest(argument + ": member_type must be KEYWORD or URL")
		}
		if !audienceText(m.Parameter, maxURLArgument) {
			return nil, badRequest(argument + ": parameter must be text of 1 to 2048 bytes without control characters")
		}
		out = append(out, map[string]any{"memberType": m.MemberType, "parameter": m.Parameter})
	}
	return out, nil
}

type customInterestCreateArguments struct {
	CustomerID      string `json:"customer_id"`
	CustomInterests []struct {
		Name        string                         `json:"name"`
		Type        string                         `json:"type"`
		Description string                         `json:"description"`
		Members     []customInterestMemberArgument `json:"members"`
	} `json:"custom_interests"`
	ValidateOnly bool `json:"validate_only"`
}

func (a customInterestCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := audienceCheckCount("custom_interests", len(a.CustomInterests)); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(a.CustomInterests))
	for i, c := range a.CustomInterests {
		at := fmt.Sprintf("custom_interests[%d]", i)
		if !audienceText(c.Name, maxNameArgument) {
			return nil, badRequest(at + ".name must be text of 1 to 1024 bytes without control characters")
		}
		create := map[string]any{"name": c.Name}
		if c.Type != "" {
			if !slices.Contains(customInterestTypes, c.Type) {
				return nil, badRequest(at + ".type must be CUSTOM_AFFINITY or CUSTOM_INTENT")
			}
			create["type"] = c.Type
		}
		if c.Description != "" {
			if err := audienceCheckDescription(at+".description", c.Description); err != nil {
				return nil, err
			}
			create["description"] = c.Description
		}
		members, err := checkCustomInterestMembers(at+".members", c.Members)
		if err != nil {
			return nil, err
		}
		if len(members) > 0 {
			create["members"] = members
		}
		out = append(out, map[string]any{"create": create})
	}
	return out, nil
}

type customInterestUpdateArguments struct {
	CustomerID      string `json:"customer_id"`
	CustomInterests []struct {
		ID          string                          `json:"id"`
		Name        *string                         `json:"name"`
		Type        *string                         `json:"type"`
		Description *string                         `json:"description"`
		Members     *[]customInterestMemberArgument `json:"members"`
	} `json:"custom_interests"`
	ValidateOnly bool `json:"validate_only"`
}

// operations builds the updates with the resource names of the account and the fields that were given, and
// returns the resource names and the argument names changed per update.
func (a customInterestUpdateArguments) operations() ([]any, []string, [][]string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := audienceCheckCount("custom_interests", len(a.CustomInterests)); err != nil {
		return nil, nil, nil, err
	}
	var out []any
	var names []string
	var changed [][]string
	for i, c := range a.CustomInterests {
		at := fmt.Sprintf("custom_interests[%d]", i)
		name, err := audienceObjectName(at+".id", a.CustomerID, customInterestCollection, c.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		if slices.Contains(names, name) {
			return nil, nil, nil, badRequest(at + ".id repeats an ID of this call")
		}
		update := map[string]any{"resourceName": name}
		var fields audienceChangedNames
		if c.Name != nil {
			if !audienceText(*c.Name, maxNameArgument) {
				return nil, nil, nil, badRequest(at + ".name must be text of 1 to 1024 bytes without control characters")
			}
			update["name"] = *c.Name
			fields.add("name", "name")
		}
		if c.Type != nil {
			if !slices.Contains(customInterestTypes, *c.Type) {
				return nil, nil, nil, badRequest(at + ".type must be CUSTOM_AFFINITY or CUSTOM_INTENT")
			}
			update["type"] = *c.Type
			fields.add("type", "type")
		}
		if c.Description != nil {
			if err := audienceCheckDescription(at+".description", *c.Description); err != nil {
				return nil, nil, nil, err
			}
			if *c.Description != "" {
				update["description"] = *c.Description
			}
			fields.add("description", "description")
		}
		if c.Members != nil {
			if len(*c.Members) == 0 {
				return nil, nil, nil, badRequest(at + ".members must hold at least one member; it replaces all members")
			}
			members, err := checkCustomInterestMembers(at+".members", *c.Members)
			if err != nil {
				return nil, nil, nil, err
			}
			update["members"] = members
			fields.add("members", "members")
		}
		if len(fields.mask) == 0 {
			return nil, nil, nil, badRequest(at + " must give at least one field to change")
		}
		out = append(out, map[string]any{"update": update, "updateMask": strings.Join(fields.mask, ",")})
		names = append(names, name)
		changed = append(changed, fields.names)
	}
	return out, names, changed, nil
}

type customInterestResult struct {
	ResourceName string   `json:"resource_name"`
	Updated      []string `json:"updated,omitempty"`
}

type customInterestsChanged struct {
	CustomerID   string                 `json:"customer_id"`
	ValidateOnly bool                   `json:"validate_only"`
	Results      []customInterestResult `json:"results"`
}

func customInterestsOf(customerID string, validateOnly bool, results []audienceMutateResult, changed [][]string) customInterestsChanged {
	out := customInterestsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []customInterestResult{}}
	for i, r := range results {
		item := customInterestResult{ResourceName: r.ResourceName}
		if changed != nil {
			item.Updated = changed[i]
		}
		out.Results = append(out.Results, item)
	}
	return out
}

var (
	customInterestMemberSchema = object(schema{"member_type": audienceEnumSchema(customInterestMemberTypes),
		"parameter": schema{"type": "string", "minLength": 1, "maxLength": maxURLArgument}}, "member_type", "parameter")
	customInterestNameSchema = schema{"type": "string", "minLength": 1, "maxLength": maxNameArgument}
	audienceTextSchema       = schema{"type": "string", "maxLength": audienceMaxDescriptionArgument}

	customInterestsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema, "updated": arrayOf(stringSchema)}, "resource_name"))},
		"customer_id", "validate_only", "results")
)

const customInterestsSensitivity = "custom interests of a Google Ads account: keywords and URLs that Google uses to " +
	"find an audience for ads; the confirmation names the interests"

const customInterestsNote = " At most 10 custom interests per call, sent as one atomic request without partial failure. " +
	"Keywords and URLs are data for Google and are never fetched by Qatlas Connect. A custom interest is never " +
	"removed by this tool. CustomInterestService answers the resource names only, so there is no " +
	"response_content_type."

var customInterestsCreate = capability.Descriptor{
	ID: customInterestsCreateID, Version: 1, Title: "Create custom interests in a Google Ads account",
	Description: "Create custom interests, audiences that Google builds from keywords and URLs " +
		"(CustomInterestService.MutateCustomInterests, create): name, type CUSTOM_AFFINITY (the default) or " +
		"CUSTOM_INTENT, description, and members, each a KEYWORD or a URL." + customInterestsNote +
		" On a connection with targets, a manager account needs a manager target that names it." + mutateListNote,
	Tags: []string{"google", "ads", "custom-interests", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, customInterestsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"custom_interests": audienceOperationListSchema(object(schema{"name": customInterestNameSchema,
			"type":        audienceEnumSchema(customInterestTypes),
			"description": audienceTextSchema,
			"members":     schema{"type": "array", "maxItems": customInterestMaxMembers, "items": customInterestMemberSchema}}, "name")),
		"validate_only": validateOnlySchema,
	}, "customer_id", "custom_interests")),
	OutputSchema: encoded(customInterestsOutputSchema),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "custom_interests", Required: true, Description: "1 to 10 custom interests to create. Each has " +
			"name (required, 1 to 1024 bytes), type (CUSTOM_AFFINITY or CUSTOM_INTENT), description (up to 4096 " +
			"bytes) and members (up to 100, each member_type KEYWORD or URL with a parameter of 1 to 2048 bytes)"},
		validateOnlyArgument},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "One entry per custom interest in the order sent: resource_name " +
			"(customers/ID/customInterests/N); empty after validate_only"}},
	Examples: []capability.Example{{Description: "Validate a custom interest of two keywords and one URL",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","custom_interests":[{"name":"Hikers","members":[` +
			`{"member_type":"KEYWORD","parameter":"hiking boots"},{"member_type":"KEYWORD","parameter":"trail map"},` +
			`{"member_type":"URL","parameter":"example.com/hiking"}]}],"validate_only":true}`)}},
}

var customInterestsUpdate = capability.Descriptor{
	ID: customInterestsUpdateID, Version: 1, Title: "Change custom interests of a Google Ads account",
	Description: "Change custom interests by ID (CustomInterestService.MutateCustomInterests, update with its " +
		"field mask); only the fields that are given change. Giving members replaces all members of the interest, " +
		"and the members are never changed one by one." + customInterestsNote +
		" On a connection with targets, a manager account needs a manager target that names it." + mutateListNote,
	Tags: []string{"google", "ads", "custom-interests", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, customInterestsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"custom_interests": audienceOperationListSchema(object(schema{"id": audienceNumberIDSchema, "name": customInterestNameSchema,
			"type":        audienceEnumSchema(customInterestTypes),
			"description": audienceTextSchema,
			"members":     schema{"type": "array", "minItems": 1, "maxItems": customInterestMaxMembers, "items": customInterestMemberSchema}}, "id")),
		"validate_only": validateOnlySchema,
	}, "customer_id", "custom_interests")),
	OutputSchema: encoded(customInterestsOutputSchema),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "custom_interests", Required: true, Description: "1 to 10 updates, each for a different custom " +
			"interest of the account. Each has id (the number of customers/ID/customInterests/N) and at least one " +
			"of name, type, description (empty clears it) and members (1 to 100, replaces all)"},
		validateOnlyArgument},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "One entry per custom interest in the order sent: resource_name " +
			"(customers/ID/customInterests/N) and updated, the fields the change named by argument name; " +
			"empty after validate_only"}},
	Examples: []capability.Example{{Description: "Rename a custom interest",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","custom_interests":[{"id":"123","name":"Trail hikers"}]}`)}},
}

func handleCustomInterestsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create custom interests"
	var args customInterestCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	operations, err := args.operations()
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: customInterestsCreateID}
	results, err := env.audienceMutateList(args.CustomerID, customInterestService, customInterestCollection, operations, nil,
		args.ValidateOnly, "")
	if err != nil {
		return nil, err
	}
	return customInterestsOf(args.CustomerID, args.ValidateOnly, results, nil), nil
}

func handleCustomInterestsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update custom interests"
	var args customInterestUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	operations, names, changed, err := args.operations()
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: customInterestsUpdateID}
	results, err := env.audienceMutateList(args.CustomerID, customInterestService, customInterestCollection, operations, names,
		args.ValidateOnly, "")
	if err != nil {
		return nil, err
	}
	out := customInterestsOf(args.CustomerID, args.ValidateOnly, results, changed)
	return out, nil
}

func customInterestMemberCounts(members []customInterestMemberArgument) string {
	keywords := 0
	for _, m := range members {
		if m.MemberType == "KEYWORD" {
			keywords++
		}
	}
	return audiencePlural(keywords, "keyword") + ", " + audiencePlural(len(members)-keywords, "URL")
}

func audienceValidatePrefix(validateOnly bool, verb string) string {
	if validateOnly {
		return "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
	}
	return verb
}

// customInterestsCreateSummary phrases the change for the confirmation, from the arguments alone.
func customInterestsCreateSummary(raw json.RawMessage) string {
	var a customInterestCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, c := range a.CustomInterests {
		kind := c.Type
		if kind == "" {
			kind = "CUSTOM_AFFINITY"
		}
		parts = append(parts, audienceClipped(c.Name)+" ("+kind+", "+customInterestMemberCounts(c.Members)+")")
	}
	return audienceValidatePrefix(a.ValidateOnly, "Create "+audiencePlural(len(a.CustomInterests), "custom interest")+
		" in Google Ads account "+a.CustomerID+": "+strings.Join(parts, "; ")+".")
}

// customInterestsUpdateSummary phrases the change for the confirmation, from the arguments alone.
func customInterestsUpdateSummary(raw json.RawMessage) string {
	var a customInterestUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, c := range a.CustomInterests {
		var what []string
		if c.Name != nil {
			what = append(what, "set the name to "+audienceClipped(*c.Name))
		}
		if c.Type != nil {
			what = append(what, "set the type to "+boundedText(*c.Type, 40))
		}
		if c.Description != nil {
			what = append(what, "set the description")
		}
		if c.Members != nil {
			what = append(what, "replace all members by "+customInterestMemberCounts(*c.Members))
		}
		parts = append(parts, "custom interest "+boundedText(c.ID, 24)+": "+strings.Join(what, ", "))
	}
	return audienceValidatePrefix(a.ValidateOnly, "Change "+audiencePlural(len(a.CustomInterests), "custom interest")+
		" in Google Ads account "+a.CustomerID+": "+strings.Join(parts, "; ")+".")
}
