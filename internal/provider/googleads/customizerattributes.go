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
	customizerAttributesCreateID = Provider + ".customizer-attributes.create"
	customizerAttributesRemoveID = Provider + ".customizer-attributes.remove"

	customizerAttributesService = "/customizerAttributes"
)

// Bounds of the customizer attribute fields as the reference of v26 documents them: name is required, 1 to 40
// characters, and unique among the enabled attributes, case insensitive; the type is immutable and one of the
// four below; the status is output only, so no tool offers it. The service has no update operation.
const (
	customizerAttributeNameMax = 40
	// customizerAttributesMaxOperations is Qatlas's own bound per call; Google accepts up to 10,000 operations per request.
	customizerAttributesMaxOperations = 50
)

var customizerAttributeTypes = []string{"TEXT", "NUMBER", "PRICE", "PERCENT"}

// customizerAttributeName is the resource name of a customizer attribute, built from checked IDs alone. The
// customizers that attach a value to an object name their attribute by it.
func customizerAttributeName(customerID, attributeID string) string {
	return customerName(customerID) + "/customizerAttributes/" + attributeID
}

// customizerAttributeIDOfName returns the attribute ID of a resource name that belongs to the account.
func customizerAttributeIDOfName(name, customerID string) (string, bool) {
	return numericIDOf(name, customerID, "customizerAttributes")
}

func customizerAttributesCheckName(value string) error {
	n := utf8.RuneCountInString(value)
	if n < 1 || n > customizerAttributeNameMax {
		return badRequest("name must be 1 to " + strconv.Itoa(customizerAttributeNameMax) + " characters")
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return badRequest("name must not hold control characters")
		}
	}
	return nil
}

func customizerAttributesCheckCount(argument string, n int) error {
	if n < 1 || n > customizerAttributesMaxOperations {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(customizerAttributesMaxOperations) + " entries")
	}
	return nil
}

// --- arguments ---

type (
	customizerAttributesNew struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	customizerAttributesCreateArguments struct {
		CustomerID          string                    `json:"customer_id"`
		CustomizerAttrs     []customizerAttributesNew `json:"customizer_attributes"`
		ValidateOnly        bool                      `json:"validate_only"`
		ResponseContentType string                    `json:"response_content_type"`
	}
	customizerAttributesRemoveArguments struct {
		CustomerID        string   `json:"customer_id"`
		CustomizerAttrIDs []string `json:"customizer_attribute_ids"`
		ValidateOnly      bool     `json:"validate_only"`
	}
)

func (a customizerAttributesCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	if err := customizerAttributesCheckCount("customizer_attributes", len(a.CustomizerAttrs)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.CustomizerAttrs))
	for _, attr := range a.CustomizerAttrs {
		if err := customizerAttributesCheckName(attr.Name); err != nil {
			return nil, err
		}
		if !slices.Contains(customizerAttributeTypes, attr.Type) {
			return nil, badRequest("type must be one of " + strings.Join(customizerAttributeTypes, ", "))
		}
		ops = append(ops, map[string]any{"create": map[string]any{"name": attr.Name, "type": attr.Type}})
	}
	return ops, nil
}

func (a customizerAttributesRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := customizerAttributesCheckCount("customizer_attribute_ids", len(a.CustomizerAttrIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.CustomizerAttrIDs))
	names := make([]string, 0, len(a.CustomizerAttrIDs))
	for _, id := range a.CustomizerAttrIDs {
		if !numericIDPattern.MatchString(id) {
			return nil, nil, badRequest("customizer_attribute_ids must be IDs of existing customizer attributes, positive numbers")
		}
		name := customizerAttributeName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("customizer_attribute_ids names each customizer attribute at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	customizerAttributesAttrResult struct {
		ResourceName string `json:"resource_name"`
		ID           string `json:"id,omitempty"`
		Name         string `json:"name,omitempty"`
		Type         string `json:"type,omitempty"`
		Status       string `json:"status,omitempty"`
	}
	customizerAttributesMutated struct {
		CustomizerAttributeID string                          `json:"customizer_attribute_id"`
		ResourceName          string                          `json:"resource_name"`
		CustomizerAttribute   *customizerAttributesAttrResult `json:"customizer_attribute,omitempty"`
	}
	customizerAttributesChanged struct {
		CustomerID   string                        `json:"customer_id"`
		ValidateOnly bool                          `json:"validate_only"`
		Results      []customizerAttributesMutated `json:"results"`
	}
	customizerAttributesWire struct {
		ResourceName string   `json:"resourceName"`
		ID           *wireInt `json:"id"`
		Name         string   `json:"name"`
		Type         string   `json:"type"`
		Status       string   `json:"status"`
	}
)

func customizerAttributesAttrOf(raw json.RawMessage, name, attributeID string) (*customizerAttributesAttrResult, bool) {
	var w customizerAttributesWire
	if json.Unmarshal(raw, &w) != nil || w.ResourceName != name {
		return nil, false
	}
	out := &customizerAttributesAttrResult{ResourceName: name, Name: boundedText(w.Name, maxText),
		Type: code(w.Type), Status: code(w.Status)}
	if w.ID != nil {
		if string(*w.ID) != attributeID {
			return nil, false
		}
		out.ID = attributeID
	}
	return out, true
}

// customizerAttributesResults maps the results. mutateList has bound them to the account and, for a remove, to
// the names asked for; a create must name distinct attributes, and an object shown must be the one named.
func customizerAttributesResults(op string, results []listResult) ([]customizerAttributesMutated, error) {
	out := []customizerAttributesMutated{}
	for _, r := range results {
		for _, previous := range out {
			if previous.CustomizerAttributeID == r.ID {
				return nil, unsureResult(op, "answered with the same customizer attribute twice")
			}
		}
		item := customizerAttributesMutated{CustomizerAttributeID: r.ID, ResourceName: r.ResourceName}
		if len(r.Resource) > 0 {
			attr, ok := customizerAttributesAttrOf(r.Resource, r.ResourceName, r.ID)
			if !ok {
				return nil, unsureResult(op, "answered with a customizer attribute that is not the one asked for")
			}
			item.CustomizerAttribute = attr
		}
		out = append(out, item)
	}
	return out, nil
}

// --- descriptors ---

var (
	customizerAttributeNameSchema = schema{"type": "string", "minLength": 1, "maxLength": customizerAttributeNameMax}
	customizerAttributeTypeSchema = schema{"type": "string", "enum": customizerAttributeTypes}
	customizerAttributeIDSchema   = schema{"type": "string", "pattern": numericIDPattern.String()}

	customizerAttributesAttrSchema = object(schema{"resource_name": stringSchema, "id": stringSchema,
		"name": stringSchema, "type": stringSchema, "status": stringSchema}, "resource_name")
	customizerAttributesOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"customizer_attribute_id": stringSchema, "resource_name": stringSchema,
			"customizer_attribute": customizerAttributesAttrSchema}, "customizer_attribute_id", "resource_name"))},
		"customer_id", "validate_only", "results")
)

const customizerAttributesSensitivity = "customizer attributes of a Google Ads account: their names and types, " +
	"which the confirmation names; no data of other people"

const customizerAttributesNote = " A customizer attribute defines a name and a type for values that customizers " +
	"attach to campaigns, ad groups, keywords or the account and that ads insert with a customizer tag; this tool " +
	"sets no such value and does not read attributes (read them with reports.search on the customizer_attribute " +
	"resource). The service has no update: name and type are immutable. The request goes to " +
	"CustomizerAttributeService.MutateCustomizerAttributes with up to 50 operations, atomic and without " +
	"partial_failure: Google applies all of them or none."

var customizerAttributesFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: customizer_attribute_id, " +
		"resource_name (customers/ID/customizerAttributes/ATTRIBUTE_ID, as Google answered, always of the account " +
		"of the call), and with MUTABLE_RESOURCE the attribute after the change (id, name, type, status) as Google " +
		"returns it. Empty after validate_only"},
}

var customizerAttributesArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

var customizerAttributesCreate = capability.Descriptor{
	ID: customizerAttributesCreateID, Version: 1, Title: "Create customizer attributes in a Google Ads account",
	Description: "Create one or more customizer attributes in a Google Ads account: a name of 1 to " +
		strconv.Itoa(customizerAttributeNameMax) + " characters, unique among the enabled attributes of the " +
		"account without regard to case, and a type of TEXT, NUMBER, PRICE or PERCENT. A new attribute is ENABLED." +
		customizerAttributesNote + " Offered by a connection with the create effect or whose tools list names it; " +
		"no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "customizers", "customizer-attributes", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, customizerAttributesSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"customizer_attributes": schema{"type": "array", "minItems": 1, "maxItems": customizerAttributesMaxOperations,
			"items": object(schema{"name": customizerAttributeNameSchema, "type": customizerAttributeTypeSchema},
				"name", "type")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "customizer_attributes")),
	OutputSchema: encoded(customizerAttributesOutputSchema),
	Arguments: append(slices.Clone(customizerAttributesArgumentDocs),
		capability.Argument{Name: "customizer_attributes", Required: true, Description: "1 to 50 attributes, each " +
			"with name (required, 1 to 40 characters, no control characters) and type (required: TEXT, NUMBER, " +
			"PRICE or PERCENT; the reference does not mark it required, but an attribute without a type is not " +
			"usable)"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: customizerAttributesFields,
	Examples: []capability.Example{{Description: "Create a text attribute",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","customizer_attributes":[{"name":"Season","type":"TEXT"}]}`)}},
}

// remove is listed because a removed attribute is REMOVED for good, like a removed label, and the values and
// ad texts that use it stop working.
var customizerAttributesRemove = capability.Descriptor{
	ID: customizerAttributesRemoveID, Version: 1, Title: "Remove customizer attributes from a Google Ads account",
	Description: "Remove existing customizer attributes of a Google Ads account (a remove of " +
		"customers/ID/customizerAttributes/ATTRIBUTE_ID, built from customer_id and customizer_attribute_id). " +
		"Google marks a removed attribute REMOVED; no tool of this provider brings it back, and the values that " +
		"customizers set for it and the ads that insert it are affected." + customizerAttributesNote +
		mutateListListedNote,
	Tags: []string{"google", "ads", "customizers", "customizer-attributes", "remove"}, Provider: Provider,
	RequiresToolAllowList: true,
	Risk:                  changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, customizerAttributesSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"customizer_attribute_ids": schema{"type": "array", "minItems": 1, "maxItems": customizerAttributesMaxOperations,
			"uniqueItems": true, "items": customizerAttributeIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "customizer_attribute_ids")),
	OutputSchema: encoded(customizerAttributesOutputSchema),
	Arguments: append(slices.Clone(customizerAttributesArgumentDocs),
		capability.Argument{Name: "customizer_attribute_ids", Required: true, Description: "1 to 50 IDs of existing " +
			"customizer attributes, each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: customizerAttributesFields,
	Examples: []capability.Example{{Description: "Validate the removal of an attribute without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","customizer_attribute_ids":["112233"],"validate_only":true}`)}},
}

// --- handlers ---

func handleCustomizerAttributesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create customizer attributes"
	var args customizerAttributesCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return customizerAttributesSend(ctx, resolved, secrets, red, op, customizerAttributesCreateID, args.CustomerID,
		ops, nil, args.ValidateOnly, args.ResponseContentType)
}

func handleCustomizerAttributesRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove customizer attributes"
	var args customizerAttributesRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return customizerAttributesSend(ctx, resolved, secrets, red, op, customizerAttributesRemoveID, args.CustomerID,
		ops, names, args.ValidateOnly, "")
}

func customizerAttributesSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, op, tool, customerID string, ops []any, want []string, validateOnly bool,
	content string) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: customerID,
		service: customizerAttributesService, collection: "customizerAttributes", resourceKey: "customizerAttribute",
		operations: ops, names: want, validateOnly: validateOnly, content: content})
	if err != nil {
		return nil, err
	}
	out, err := customizerAttributesResults(op, results)
	if err != nil {
		return nil, err
	}
	return customizerAttributesChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: out}, nil
}

// --- confirmation ---

func customizerAttributesLead(validateOnly bool) string {
	if validateOnly {
		return "Only validate: "
	}
	return ""
}

func customizerAttributesMore(total, shown int) string {
	if total > shown {
		return " and " + strconv.Itoa(total-shown) + " more"
	}
	return ""
}

func customizerAttributesCreateSummary(raw json.RawMessage) string {
	var a customizerAttributesCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, attr := range a.CustomizerAttrs[:min(len(a.CustomizerAttrs), 10)] {
		parts = append(parts, strconv.Quote(boundedText(attr.Name, customizerAttributeNameMax*4))+" "+
			boundedText(attr.Type, maxCodeText))
	}
	return customizerAttributesLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.CustomizerAttrs)) +
		" customizer attribute(s) in Google Ads account " + a.CustomerID + ": " + strings.Join(parts, ", ") +
		customizerAttributesMore(len(a.CustomizerAttrs), len(parts)) + "."
}

func customizerAttributesRemoveSummary(raw json.RawMessage) string {
	var a customizerAttributesRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.CustomizerAttrIDs[:min(len(a.CustomizerAttrIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	return customizerAttributesLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.CustomizerAttrIDs)) +
		" customizer attribute(s) from Google Ads account " + a.CustomerID + ": " + strings.Join(shown, ", ") +
		customizerAttributesMore(len(a.CustomizerAttrIDs), len(shown)) +
		". Google marks them removed; no tool brings them back."
}
