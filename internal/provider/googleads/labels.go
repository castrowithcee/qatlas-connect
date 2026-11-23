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
	labelsCreateID = Provider + ".labels.create"
	labelsUpdateID = Provider + ".labels.update"
	labelsRemoveID = Provider + ".labels.remove"

	labelsService = "/labels"
)

// Bounds of the label fields as the reference of v26 documents them: Label.name is required on create and
// 1 to 80 characters, TextLabel.description at most 200 characters, TextLabel.background_color matches
// labelColorPattern. Label.status is output only, so no tool offers it.
const (
	labelNameMax        = 80
	labelDescriptionMax = 200
	// labelsMaxOperations is Qatlas's own bound per call; Google accepts up to 10,000 operations per request.
	labelsMaxOperations = 50
)

var (
	labelColorPattern = regexp.MustCompile(`^#([a-fA-F0-9]{6}|[a-fA-F0-9]{3})$`)
	labelIDPattern    = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
)

// labelName is the resource name of a label, built from checked IDs alone.
func labelName(customerID, labelID string) string {
	return customerName(customerID) + "/labels/" + labelID
}

// labelIDOfName returns the label ID of a resource name that belongs to the account.
func labelIDOfName(name, customerID string) (string, bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/labels/")
	if !ok || !labelIDPattern.MatchString(rest) {
		return "", false
	}
	if _, err := strconv.ParseInt(rest, 10, 64); err != nil {
		return "", false
	}
	return rest, true
}

func labelsText(argument, value string, minRunes, maxRunes int) error {
	n := utf8.RuneCountInString(value)
	if n < minRunes || n > maxRunes {
		return badRequest(argument + " must be " + strconv.Itoa(minRunes) + " to " + strconv.Itoa(maxRunes) + " characters")
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return badRequest(argument + " must not hold control characters")
		}
	}
	return nil
}

func labelsCheckColor(value string) error {
	if !labelColorPattern.MatchString(value) {
		return badRequest("background_color must be a hex color such as #1A2B3C or #abc")
	}
	return nil
}

func labelsCheckID(argument, value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil || !labelIDPattern.MatchString(value) {
		return badRequest(argument + " must be the ID of an existing label, a positive number")
	}
	return nil
}

func labelsCheckCount(argument string, n int) error {
	if n < 1 || n > labelsMaxOperations {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(labelsMaxOperations) + " entries")
	}
	return nil
}

// labelTextLabel builds the TextLabel with the fields that were given.
func labelTextLabel(description, color *string) map[string]any {
	text := map[string]any{}
	if color != nil {
		text["backgroundColor"] = *color
	}
	if description != nil {
		text["description"] = *description
	}
	return text
}

// --- arguments ---

type (
	labelsNewLabel struct {
		Name            string  `json:"name"`
		Description     *string `json:"description"`
		BackgroundColor *string `json:"background_color"`
	}
	labelsEditLabel struct {
		LabelID         string  `json:"label_id"`
		Name            *string `json:"name"`
		Description     *string `json:"description"`
		BackgroundColor *string `json:"background_color"`
	}
	labelsCreateArguments struct {
		CustomerID          string           `json:"customer_id"`
		Labels              []labelsNewLabel `json:"labels"`
		ValidateOnly        bool             `json:"validate_only"`
		ResponseContentType string           `json:"response_content_type"`
	}
	labelsUpdateArguments struct {
		CustomerID          string            `json:"customer_id"`
		Labels              []labelsEditLabel `json:"labels"`
		ValidateOnly        bool              `json:"validate_only"`
		ResponseContentType string            `json:"response_content_type"`
	}
	labelsRemoveArguments struct {
		CustomerID   string   `json:"customer_id"`
		LabelIDs     []string `json:"label_ids"`
		ValidateOnly bool     `json:"validate_only"`
	}
)

func labelsCheckFields(name, description, color *string) error {
	if name != nil {
		if err := labelsText("name", *name, 1, labelNameMax); err != nil {
			return err
		}
	}
	if description != nil {
		if err := labelsText("description", *description, 0, labelDescriptionMax); err != nil {
			return err
		}
	}
	if color != nil {
		return labelsCheckColor(*color)
	}
	return nil
}

// operations checks the arguments and builds the operations of the request.
func (a labelsCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	if err := labelsCheckCount("labels", len(a.Labels)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.Labels))
	for _, l := range a.Labels {
		if err := labelsCheckFields(&l.Name, l.Description, l.BackgroundColor); err != nil {
			return nil, err
		}
		create := map[string]any{"name": l.Name}
		if text := labelTextLabel(l.Description, l.BackgroundColor); len(text) > 0 {
			create["textLabel"] = text
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, nil
}

func (a labelsUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	if err := labelsCheckCount("labels", len(a.Labels)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.Labels))
	names := make([]string, 0, len(a.Labels))
	for _, l := range a.Labels {
		if err := labelsCheckID("label_id", l.LabelID); err != nil {
			return nil, nil, err
		}
		name := labelName(a.CustomerID, l.LabelID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("labels names each label_id at most once")
		}
		if l.Name == nil && l.Description == nil && l.BackgroundColor == nil {
			return nil, nil, badRequest("each entry of labels needs at least one of name, description and background_color")
		}
		if err := labelsCheckFields(l.Name, l.Description, l.BackgroundColor); err != nil {
			return nil, nil, err
		}
		update := map[string]any{"resourceName": name}
		var mask []string
		if l.Name != nil {
			update["name"] = *l.Name
			mask = append(mask, "name")
		}
		if l.BackgroundColor != nil {
			mask = append(mask, "textLabel.backgroundColor")
		}
		if l.Description != nil {
			mask = append(mask, "textLabel.description")
		}
		if text := labelTextLabel(l.Description, l.BackgroundColor); len(text) > 0 {
			update["textLabel"] = text
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a labelsRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := labelsCheckCount("label_ids", len(a.LabelIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.LabelIDs))
	names := make([]string, 0, len(a.LabelIDs))
	for _, id := range a.LabelIDs {
		if err := labelsCheckID("label_ids", id); err != nil {
			return nil, nil, err
		}
		name := labelName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("label_ids names each label at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	labelsLabelResult struct {
		ResourceName    string `json:"resource_name"`
		ID              string `json:"id,omitempty"`
		Name            string `json:"name,omitempty"`
		Status          string `json:"status,omitempty"`
		Description     string `json:"description,omitempty"`
		BackgroundColor string `json:"background_color,omitempty"`
	}
	labelsMutated struct {
		LabelID      string             `json:"label_id"`
		ResourceName string             `json:"resource_name"`
		Label        *labelsLabelResult `json:"label,omitempty"`
	}
	labelsChanged struct {
		CustomerID   string          `json:"customer_id"`
		ValidateOnly bool            `json:"validate_only"`
		Results      []labelsMutated `json:"results"`
	}
	labelsAnswer struct {
		Results []struct {
			ResourceName string          `json:"resourceName"`
			Label        json.RawMessage `json:"label"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
	labelsWire struct {
		ResourceName string   `json:"resourceName"`
		ID           *wireInt `json:"id"`
		Name         string   `json:"name"`
		Status       string   `json:"status"`
		TextLabel    *struct {
			BackgroundColor string `json:"backgroundColor"`
			Description     string `json:"description"`
		} `json:"textLabel"`
	}
)

func labelsLabelOf(raw json.RawMessage, name, labelID string) (*labelsLabelResult, bool) {
	var w labelsWire
	if json.Unmarshal(raw, &w) != nil || w.ResourceName != name {
		return nil, false
	}
	out := &labelsLabelResult{ResourceName: name, Name: boundedText(w.Name, maxText), Status: code(w.Status)}
	if w.ID != nil {
		if string(*w.ID) != labelID {
			return nil, false
		}
		out.ID = labelID
	}
	if w.TextLabel != nil {
		out.Description = boundedText(w.TextLabel.Description, maxText)
		out.BackgroundColor = code(w.TextLabel.BackgroundColor)
	}
	return out, true
}

// labelsResults binds the answer to the checked account. With want names (update, remove) the results must be
// exactly these in order; a create names the new labels, which must belong to the account and be distinct.
// Everything else leaves the outcome open, since the request was sent.
func labelsResults(op, customerID string, answer labelsAnswer, count int, want []string, validateOnly bool) ([]labelsMutated, error) {
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(op, "answered with a partial failure although the request asked for none")
	}
	out := []labelsMutated{}
	if validateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != count {
		return nil, unsureResult(op, "answered with another number of results than the operations asked for")
	}
	for i, r := range answer.Results {
		id, ok := labelIDOfName(r.ResourceName, customerID)
		if want != nil && r.ResourceName != want[i] || !ok {
			return nil, unsureResult(op, "answered with a result for another label than the one asked for")
		}
		for _, previous := range out {
			if previous.LabelID == id {
				return nil, unsureResult(op, "answered with the same label twice")
			}
		}
		item := labelsMutated{LabelID: id, ResourceName: r.ResourceName}
		if len(r.Label) > 0 && string(r.Label) != "null" {
			label, ok := labelsLabelOf(r.Label, r.ResourceName, id)
			if !ok {
				return nil, unsureResult(op, "answered with a label that is not the one asked for")
			}
			item.Label = label
		}
		out = append(out, item)
	}
	return out, nil
}

// --- descriptors ---

var (
	labelNameSchema = schema{"type": "string", "minLength": 1, "maxLength": labelNameMax}
	// Control characters are refused in code, which a JSON schema pattern cannot say compactly.
	labelDescriptionSchema = schema{"type": "string", "maxLength": labelDescriptionMax}
	labelColorSchema       = schema{"type": "string", "pattern": labelColorPattern.String()}
	labelIDSchema          = schema{"type": "string", "pattern": labelIDPattern.String()}

	labelsLabelSchema = object(schema{"resource_name": stringSchema, "id": stringSchema, "name": stringSchema,
		"status": stringSchema, "description": stringSchema, "background_color": stringSchema}, "resource_name")
	labelsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"label_id": stringSchema, "resource_name": stringSchema,
			"label": labelsLabelSchema}, "label_id", "resource_name"))}, "customer_id", "validate_only", "results")
)

const labelsSensitivity = "labels of a Google Ads account: their names, descriptions and colors, which the " +
	"confirmation names; no data of other people"

const labelsNote = " Labels are a way to organize campaigns, ad groups, ads and keywords; this tool does not " +
	"attach a label to anything or read labels (read them with reports.search on the label resource). The " +
	"request goes to LabelService.MutateLabels with up to 50 operations, atomic and without partial_failure: " +
	"Google applies all of them or none. A manager account has labels of its own, which a manager target that " +
	"names it reaches."

var labelsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: label_id, resource_name " +
		"(customers/ID/labels/LABEL_ID, as Google answered, always of the account of the call), and with " +
		"MUTABLE_RESOURCE the label after the change (id, name, status, description, background_color) as " +
		"Google returns it. Empty after validate_only"},
}

var labelsArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

var labelsCreate = capability.Descriptor{
	ID: labelsCreateID, Version: 1, Title: "Create labels in a Google Ads account",
	Description: "Create one or more labels in a Google Ads account: a name of 1 to 80 characters, optionally " +
		"a description of up to 200 characters and a background color as hex (#RRGGBB or #RGB). Google decides " +
		"whether a name may be used twice and which color a label without one gets." + labelsNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "labels", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, labelsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"labels": schema{"type": "array", "minItems": 1, "maxItems": labelsMaxOperations,
			"items": object(schema{"name": labelNameSchema, "description": labelDescriptionSchema,
				"background_color": labelColorSchema}, "name")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "labels")),
	OutputSchema: encoded(labelsOutputSchema),
	Arguments: append(slices.Clone(labelsArgumentDocs),
		capability.Argument{Name: "labels", Required: true, Description: "1 to 50 labels, each with name (required, " +
			"1 to 80 characters), description (up to 200 characters) and background_color (hex, #RRGGBB or #RGB). " +
			"Text must not hold control characters"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: labelsFields,
	Examples: []capability.Example{{Description: "Create a label with a color",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","labels":[{"name":"Seasonal","background_color":"#1A2B3C"}]}`)}},
}

var labelsUpdate = capability.Descriptor{
	ID: labelsUpdateID, Version: 1, Title: "Change labels of a Google Ads account",
	Description: "Change the name, the description or the background color of existing labels of a Google Ads " +
		"account (an update with a field mask that names exactly the fields given). Only the label_id selects a " +
		"label: its resource name is built from customer_id and label_id. An empty description clears it; the " +
		"status of a label cannot be set." + labelsNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "labels", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, labelsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"labels": schema{"type": "array", "minItems": 1, "maxItems": labelsMaxOperations,
			"items": object(schema{"label_id": labelIDSchema, "name": labelNameSchema,
				"description": labelDescriptionSchema, "background_color": labelColorSchema}, "label_id")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "labels")),
	OutputSchema: encoded(labelsOutputSchema),
	Arguments: append(slices.Clone(labelsArgumentDocs),
		capability.Argument{Name: "labels", Required: true, Description: "1 to 50 entries, each label_id once, with at " +
			"least one of name (1 to 80 characters), description (up to 200, empty clears it) and background_color " +
			"(hex, #RRGGBB or #RGB). label_id is the id of the label as reports.search returns it"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: labelsFields,
	Examples: []capability.Example{{Description: "Rename a label",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","labels":[{"label_id":"112233","name":"Archive"}]}`)}},
}

var labelsRemove = capability.Descriptor{
	ID: labelsRemoveID, Version: 1, Title: "Remove labels from a Google Ads account",
	Description: "Remove existing labels of a Google Ads account (a remove of customers/ID/labels/LABEL_ID, built " +
		"from customer_id and label_id). Google marks a removed label REMOVED; no tool of this provider brings it " +
		"back." + labelsNote + mutateListListedNote,
	Tags: []string{"google", "ads", "labels", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, labelsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"label_ids": schema{"type": "array", "minItems": 1, "maxItems": labelsMaxOperations, "uniqueItems": true,
			"items": labelIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "label_ids")),
	OutputSchema: encoded(labelsOutputSchema),
	Arguments: append(slices.Clone(labelsArgumentDocs),
		capability.Argument{Name: "label_ids", Required: true, Description: "1 to 50 IDs of existing labels, each once, " +
			"as reports.search returns them"},
		validateOnlyArgument),
	Fields: labelsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a label without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","label_ids":["112233"],"validate_only":true}`)}},
}

// --- handlers ---

func handleLabelsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create labels"
	var args labelsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return labelsSend(ctx, resolved, secrets, red, op, labelsCreateID, args.CustomerID, ops, nil, args.ValidateOnly,
		args.ResponseContentType)
}

func handleLabelsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update labels"
	var args labelsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return labelsSend(ctx, resolved, secrets, red, op, labelsUpdateID, args.CustomerID, ops, names, args.ValidateOnly,
		args.ResponseContentType)
}

func handleLabelsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove labels"
	var args labelsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return labelsSend(ctx, resolved, secrets, red, op, labelsRemoveID, args.CustomerID, ops, names, args.ValidateOnly, "")
}

func labelsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
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
	var answer labelsAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: customerID, service: labelsService, body: body},
		&answer); err != nil {
		return nil, err
	}
	results, err := labelsResults(op, customerID, answer, len(ops), want, validateOnly)
	if err != nil {
		return nil, err
	}
	return labelsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: results}, nil
}

// --- confirmation ---

// labelsQuote shows a label text in the confirmation, cut and without control characters.
func labelsQuote(value string) string { return strconv.Quote(boundedText(value, labelNameMax*4)) }

func labelsLead(validateOnly bool) string {
	if validateOnly {
		return "Only validate: "
	}
	return ""
}

func labelsCreateSummary(raw json.RawMessage) string {
	var a labelsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, l := range a.Labels[:min(len(a.Labels), 10)] {
		part := labelsQuote(l.Name)
		if l.BackgroundColor != nil {
			part += " " + labelsQuote(*l.BackgroundColor)
		}
		parts = append(parts, part)
	}
	more := ""
	if len(a.Labels) > len(parts) {
		more = " and " + strconv.Itoa(len(a.Labels)-len(parts)) + " more"
	}
	return labelsLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.Labels)) + " label(s) in Google Ads account " +
		a.CustomerID + ": " + strings.Join(parts, ", ") + more + "."
}

func labelsUpdateSummary(raw json.RawMessage) string {
	var a labelsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, l := range a.Labels[:min(len(a.Labels), 10)] {
		part := "label " + boundedText(l.LabelID, maxCodeText)
		if l.Name != nil {
			part += " name " + labelsQuote(*l.Name)
		}
		if l.Description != nil {
			part += " description " + labelsQuote(*l.Description)
		}
		if l.BackgroundColor != nil {
			part += " color " + labelsQuote(*l.BackgroundColor)
		}
		parts = append(parts, part)
	}
	more := ""
	if len(a.Labels) > len(parts) {
		more = " and " + strconv.Itoa(len(a.Labels)-len(parts)) + " more"
	}
	return labelsLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.Labels)) + " label(s) of Google Ads account " +
		a.CustomerID + ": " + strings.Join(parts, "; ") + more + "."
}

func labelsRemoveSummary(raw json.RawMessage) string {
	var a labelsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.LabelIDs[:min(len(a.LabelIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	more := ""
	if len(a.LabelIDs) > len(shown) {
		more = " and " + strconv.Itoa(len(a.LabelIDs)-len(shown)) + " more"
	}
	return labelsLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.LabelIDs)) + " label(s) from Google Ads account " +
		a.CustomerID + ": " + strings.Join(shown, ", ") + more + ". Google marks them removed; no tool brings them back."
}
