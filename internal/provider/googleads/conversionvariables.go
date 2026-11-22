package googleads

import (
	"context"
	"encoding/json"
	"regexp"
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
	conversionCustomVariablesCreateID = Provider + ".conversion-custom-variables.create"
	conversionCustomVariablesUpdateID = Provider + ".conversion-custom-variables.update"

	conversionVariableService    = "/conversionCustomVariables"
	conversionVariableCollection = "conversionCustomVariables"

	// The limits of ConversionCustomVariable in v26: the name up to 100 characters, the tag up to 100 bytes.
	maxVariableName = 100
	maxVariableTag  = 100
)

var (
	// variableTagPattern is the documented form of a tag: lowercase letters, digits and underscores.
	variableTagPattern = regexp.MustCompile(`^[a-z0-9_]{1,100}$`)
	// variableStatuses are the values a request may set. ACTIVATION_NEEDED is documented as not settable.
	variableStatuses = []string{"ENABLED", "PAUSED"}
)

type (
	variableEntry struct {
		ID     string  `json:"conversion_custom_variable_id"`
		Name   *string `json:"name"`
		Tag    string  `json:"tag"`
		Status string  `json:"status"`
	}
	variableArguments struct {
		CustomerID          string          `json:"customer_id"`
		Variables           []variableEntry `json:"conversion_custom_variables"`
		ValidateOnly        bool            `json:"validate_only"`
		ResponseContentType string          `json:"response_content_type"`
	}
)

// checkVariableName applies the documented rule of the name: up to 100 characters, no space before or after.
func checkVariableName(name string) error {
	if name == "" || utf8.RuneCountInString(name) > maxVariableName || strings.TrimSpace(name) != name ||
		strings.ContainsFunc(name, unicode.IsControl) {
		return badRequest("name must be 1 to 100 characters without spaces before or after and without control characters")
	}
	return nil
}

// operations checks the entries and builds the operations. A creation needs name and tag; an update names an
// existing variable by its numeric ID and gives name, status or both, never the tag, which is immutable.
func (a variableArguments) operations(create bool) ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	if n := len(a.Variables); n == 0 || n > maxListOperations {
		return nil, nil, badRequest("conversion_custom_variables must hold one to 10 entries")
	}
	var ops []any
	var names, seen []string
	for _, v := range a.Variables {
		if v.Status != "" && !slices.Contains(variableStatuses, v.Status) {
			return nil, nil, badRequest("status must be ENABLED or PAUSED; ACTIVATION_NEEDED cannot be set")
		}
		if v.Name != nil {
			if err := checkVariableName(*v.Name); err != nil {
				return nil, nil, err
			}
		}
		if create {
			if v.ID != "" {
				return nil, nil, badRequest("a new variable takes no conversion_custom_variable_id")
			}
			if v.Name == nil || !variableTagPattern.MatchString(v.Tag) {
				return nil, nil, badRequest("every entry needs a name and a tag of 1 to 100 lowercase letters, digits " +
					"and underscores")
			}
			if slices.Contains(seen, v.Tag) {
				return nil, nil, badRequest("a tag appears in one entry only")
			}
			seen = append(seen, v.Tag)
			item := map[string]any{"name": *v.Name, "tag": v.Tag}
			if v.Status != "" {
				item["status"] = v.Status
			}
			ops = append(ops, map[string]any{"create": item})
			continue
		}
		if !numericIDPattern.MatchString(v.ID) || v.Tag != "" {
			return nil, nil, badRequest("every entry needs the conversion_custom_variable_id of an existing variable, " +
				"a positive number, and takes no tag: the tag cannot be changed")
		}
		if v.Name == nil && v.Status == "" {
			return nil, nil, badRequest("every entry needs a name, a status or both to change")
		}
		if slices.Contains(seen, v.ID) {
			return nil, nil, badRequest("a variable appears in one entry only")
		}
		seen = append(seen, v.ID)
		name := customerName(a.CustomerID) + "/" + conversionVariableCollection + "/" + v.ID
		update := map[string]any{"resourceName": name}
		var mask []string
		if v.Name != nil {
			update["name"], mask = *v.Name, append(mask, "name")
		}
		if v.Status != "" {
			update["status"], mask = v.Status, append(mask, "status")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

// The variable as Google returns it after a change with MUTABLE_RESOURCE.
type (
	variableWire struct {
		ResourceName  string   `json:"resourceName"`
		ID            *wireInt `json:"id"`
		Name          string   `json:"name"`
		Status        string   `json:"status"`
		Tag           string   `json:"tag"`
		OwnerCustomer string   `json:"ownerCustomer"`
	}
	conversionVariable struct {
		ResourceName  string `json:"resource_name"`
		ID            string `json:"id,omitempty"`
		Name          string `json:"name,omitempty"`
		Status        string `json:"status,omitempty"`
		Tag           string `json:"tag,omitempty"`
		OwnerCustomer string `json:"owner_customer,omitempty"`
	}
	variableResult struct {
		ResourceName               string              `json:"resource_name"`
		ConversionCustomVariableID string              `json:"conversion_custom_variable_id"`
		ConversionCustomVariable   *conversionVariable `json:"conversion_custom_variable,omitempty"`
	}
	variablesChanged struct {
		CustomerID   string           `json:"customer_id"`
		ValidateOnly bool             `json:"validate_only"`
		Results      []variableResult `json:"results"`
	}
)

// variableOf maps the variable of a result. It must be the one of the result and belong to the account of the
// call.
func variableOf(raw json.RawMessage, name, id, customerID string) (*conversionVariable, bool) {
	var w variableWire
	if json.Unmarshal(raw, &w) != nil || (w.ResourceName != "" && w.ResourceName != name) ||
		(w.OwnerCustomer != "" && w.OwnerCustomer != customerName(customerID)) {
		return nil, false
	}
	out := &conversionVariable{ResourceName: name, Name: boundedText(w.Name, maxText), Status: code(w.Status),
		Tag: boundedText(w.Tag, maxText), OwnerCustomer: w.OwnerCustomer}
	if w.ID != nil {
		if string(*w.ID) != id {
			return nil, false
		}
		out.ID = id
	}
	return out, true
}

var (
	variableObjectSchema = object(schema{"resource_name": stringSchema, "id": stringSchema, "name": stringSchema,
		"status": stringSchema, "tag": stringSchema, "owner_customer": stringSchema}, "resource_name")
	variablesOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema, "conversion_custom_variable_id": stringSchema,
			"conversion_custom_variable": variableObjectSchema}, "resource_name", "conversion_custom_variable_id"))},
		"customer_id", "validate_only", "results")

	variableNameSchema   = schema{"type": "string", "minLength": 1, "maxLength": maxVariableName}
	variableStatusSchema = schema{"type": "string", "enum": variableStatuses}

	variablesFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "One result per entry, in the order of the entries, empty after validate_only: " +
			"resource_name (customers/ID/conversionCustomVariables/N, as Google answered), " +
			"conversion_custom_variable_id (its number), and with MUTABLE_RESOURCE the variable after the change " +
			"(resource_name, id, name, status, tag, owner_customer, as far as Google returns them)"}}
)

const variablesEffectNote = "A conversion custom variable is a named value, identified by its tag, that conversion " +
	"events can carry and that reports can split conversions by. The name is up to 100 characters and the tag " +
	"up to 100 bytes of lowercase letters, digits and underscores; both must be unique in the account and have " +
	"no space before or after. The tag is set when the variable is created and cannot be changed afterwards. " +
	"The status is ENABLED or PAUSED: a paused variable collects no conversion data; ACTIVATION_NEEDED cannot be " +
	"set, Google sets it on a new variable until it is activated. The variables are read with reports.search on " +
	"conversion_custom_variable."

var variablesInputArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit " +
	"customer ID of the account without hyphens; a target of the connection or an account below a manager target"}

var conversionCustomVariablesCreate = capability.Descriptor{
	ID: conversionCustomVariablesCreateID, Version: 1, Title: "Create conversion custom variables of a Google Ads account",
	Description: "Create one to 10 conversion custom variables in a Google Ads account " +
		"(ConversionCustomVariableService.MutateConversionCustomVariables with create operations). " +
		variablesEffectNote + mutateListNote,
	Tags: []string{"google", "ads", "conversions", "variables", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent,
		"conversion measurement definitions of a Google Ads account: variable names and tags"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"conversion_custom_variables": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"name": variableNameSchema,
				"tag":    schema{"type": "string", "pattern": variableTagPattern.String()},
				"status": variableStatusSchema}, "name", "tag")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "conversion_custom_variables")),
	OutputSchema: encoded(variablesOutputSchema),
	Arguments: []capability.Argument{variablesInputArgument,
		{Name: "conversion_custom_variables", Required: true, Description: "One to 10 entries, each with name (1 to 100 " +
			"characters, no space before or after), tag (1 to 100 lowercase letters, digits and underscores, each " +
			"tag once) and optionally status (ENABLED or PAUSED; left out, Google decides)"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: variablesFields,
	Examples: []capability.Example{{Description: "Validate a new variable without creating it", Arguments: json.RawMessage(
		`{"customer_id":"1234567890","conversion_custom_variables":[{"name":"Plan","tag":"plan"}],"validate_only":true}`)}},
}

var conversionCustomVariablesUpdate = capability.Descriptor{
	ID: conversionCustomVariablesUpdateID, Version: 1, Title: "Change conversion custom variables of a Google Ads account",
	Description: "Change the name or the status of one to 10 existing conversion custom variables of a Google Ads " +
		"account (ConversionCustomVariableService.MutateConversionCustomVariables with update operations and a " +
		"field mask of exactly the given fields). " + variablesEffectNote + " Pausing a variable stops it from " +
		"collecting conversion data." + mutateListNote,
	Tags: []string{"google", "ads", "conversions", "variables", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
		"conversion measurement definitions of a Google Ads account: variable names and status"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"conversion_custom_variables": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"conversion_custom_variable_id": schema{"type": "string", "pattern": numericIDPattern.String()},
				"name": variableNameSchema, "status": variableStatusSchema}, "conversion_custom_variable_id")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "conversion_custom_variables")),
	OutputSchema: encoded(variablesOutputSchema),
	Arguments: []capability.Argument{variablesInputArgument,
		{Name: "conversion_custom_variables", Required: true, Description: "One to 10 entries, each with " +
			"conversion_custom_variable_id (the number at the end of the resource name that reports.search or " +
			"create returns, each variable once) and name, status (ENABLED or PAUSED) or both. The tag cannot be changed"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: variablesFields,
	Examples: []capability.Example{{Description: "Pause a variable", Arguments: json.RawMessage(
		`{"customer_id":"1234567890","conversion_custom_variables":[{"conversion_custom_variable_id":"123","status":"PAUSED"}]}`)}},
}

func handleConversionCustomVariablesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionVariablesChange(ctx, resolved, secrets, red, raw, true)
}

func handleConversionCustomVariablesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionVariablesChange(ctx, resolved, secrets, red, raw, false)
}

func conversionVariablesChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, create bool) (any, error) {
	op, tool := "update conversion custom variables", conversionCustomVariablesUpdateID
	if create {
		op, tool = "create conversion custom variables", conversionCustomVariablesCreateID
	}
	var args variableArguments
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
		service: conversionVariableService, collection: conversionVariableCollection,
		resourceKey: "conversionCustomVariable", operations: operations, names: names,
		validateOnly: args.ValidateOnly, content: args.ResponseContentType})
	if err != nil {
		return nil, err
	}
	out := variablesChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly, Results: []variableResult{}}
	for _, r := range results {
		item := variableResult{ResourceName: r.ResourceName, ConversionCustomVariableID: r.ID}
		if len(r.Resource) > 0 {
			variable, ok := variableOf(r.Resource, r.ResourceName, r.ID, args.CustomerID)
			if !ok {
				return nil, unsureResult(op, "answered with a variable that is not the one of its result")
			}
			item.ConversionCustomVariable = variable
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// conversionVariablesSummary phrases the change for the confirmation, from the arguments alone.
func conversionVariablesSummary(create bool) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a variableArguments
		_ = json.Unmarshal(raw, &a)
		var parts []string
		for _, v := range a.Variables {
			var part string
			if create {
				part = "create tag " + strconv.Quote(v.Tag)
			} else {
				part = "change variable " + v.ID
			}
			if v.Name != nil {
				part += " name " + strconv.Quote(*v.Name)
			}
			if v.Status != "" {
				part += " status " + v.Status
			}
			parts = append(parts, part)
		}
		lead := "Conversion custom variables of Google Ads account " + a.CustomerID + ": "
		if a.ValidateOnly {
			lead = "Only validate, conversion custom variables of Google Ads account " + a.CustomerID + ": "
		}
		return lead + strings.Join(parts, "; ") + ". Conversion measurement of the account depends on them."
	}
}
