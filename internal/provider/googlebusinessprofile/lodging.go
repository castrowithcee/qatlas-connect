package googlebusinessprofile

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	lodgingGetID           = Provider + ".lodging.get"
	lodgingGoogleUpdatedID = Provider + ".lodging.get-google-updated"
	lodgingUpdateID        = Provider + ".lodging.update"
)

const (
	notFoundLodging = "Google holds no lodging data for this location; check the location, that it is a business " +
		"with accommodations, and that the consenting account manages it"

	lodgingSuffix = "/lodging"

	// maxReadPaths and maxChangePaths bound a mask. A change that names more fields cannot be confirmed anyway.
	maxReadPaths   = 100
	maxChangePaths = 20

	// maxConfirmRunes is the length of the confirmation text the core keeps; it cuts a longer one, which
	// would hide what a person confirms, so a change whose full text does not fit is refused.
	maxConfirmRunes = 500
)

const lodgingNote = "The labels and codes of guest units are untrusted content of whoever edits the profile: texts and " +
	"lists are cut."

var lodgingRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "public business data: the amenities, rooms, policies, services, and facilities of a lodging " +
		"location",
}

// lodgingNow is the clock of the update time a change asserts; tests replace it.
var lodgingNow = time.Now

// changeFields are the groups a caller may send: every field of the lodging except the ones Google only answers
// and the metadata, which has its own argument.
var changeFields = slices.DeleteFunc(slices.Clone(lodgingFields), func(f field) bool {
	return f.readOnly || f.name == "metadata"
})

const pathPattern = `^[a-z][A-Za-z0-9]*(\.[a-z][A-Za-z0-9]*){0,5}$`

func maskSchema(maxPaths int) string {
	return `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxPaths) + `,"uniqueItems":true,"items":` +
		`{"type":"string","maxLength":150,"pattern":` + mustJSON(pathPattern) + `}}`
}

var (
	lodgingSchema = membersSchema(lodgingFields, false)

	lodgingReadMask = capability.Argument{Name: "read_mask", Required: true, Description: "Fields of the " +
		"lodging to return as dotted paths of the result, such as pools.hotTub, or a whole group such as pools; at " +
		"least one, at most 100, none inside another. There is no default and no wildcard: name only what the task " +
		"needs. A list, such as guestUnits or services.languagesSpoken, is read as a whole"}
	lodgingLocation = capability.Argument{Name: "location", Target: "location", Description: "Location as " +
		"locations/LOCATION; optional when the connection is bound to a location, and then it must be that location"}

	lodgingFieldsOut = func() []capability.Field {
		out := make([]capability.Field, 0, len(lodgingFields))
		for _, f := range lodgingFields {
			out = append(out, capability.Field{Name: f.name, Description: f.about})
		}
		return out
	}()
)

func readInput() json.RawMessage {
	return inputSchema([]string{"read_mask"}, "location", locationArgSchema, "read_mask", maskSchema(maxReadPaths))
}

var lodgingGet = capability.Descriptor{
	ID: lodgingGetID, Version: 1, Title: "Get the lodging data of a location",
	Description: "Read the lodging data of one business location with the fields read_mask names, as last set by " +
		"the business: amenities, rooms, times, policies, and services, which are public on Google. A field " +
		"Google holds nothing for is left out; so is a group with no field. A connection bound to an account first " +
		"proves from the location list of the account that the location belongs to it, reading at most 2000 " +
		"locations. Google answers not-found for a location without lodging data. " + lodgingNote,
	Tags: []string{"google", "business-profile", "lodging"}, Risk: lodgingRisk, Provider: Provider,
	InputSchema: readInput(), OutputSchema: json.RawMessage(lodgingSchema),
	Arguments: []capability.Argument{lodgingLocation, lodgingReadMask},
	Fields:    lodgingFieldsOut,
	Examples: []capability.Example{{Description: "Pool and parking facts of one location",
		Arguments: json.RawMessage(`{"location":"locations/2000002","read_mask":["pools","parking.freeParking"]}`)}},
}

var lodgingGoogleUpdated = capability.Descriptor{
	ID: lodgingGoogleUpdatedID, Version: 1, Title: "Get the lodging data Google changed",
	Description: "Read the lodging data of one business location as Google itself updated it, with the fields " +
		"read_mask names, and diffMask, the fields Google changed. Repeated fields are named as a whole. " +
		"Otherwise it works like lodging.get. " + lodgingNote,
	Tags: []string{"google", "business-profile", "lodging"}, Risk: lodgingRisk, Provider: Provider,
	InputSchema: readInput(),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"lodging":` + lodgingSchema +
		`,"diffMask":{"type":"string"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{lodgingLocation, lodgingReadMask},
	Fields: []capability.Field{
		{Name: "lodging", Description: "The lodging as Google updated it, with the fields of read_mask"},
		{Name: "diffMask", Description: "Comma-separated field paths Google changed, only paths the lodging has"},
	},
	Examples: []capability.Example{{Description: "Wellness facts Google changed",
		Arguments: json.RawMessage(`{"location":"locations/2000002","read_mask":["wellness"]}`)}},
}

func updateArguments() []capability.Argument {
	args := []capability.Argument{lodgingLocation,
		{Name: "update_mask", Required: true, Description: "The fields to change as dotted paths, such as " +
			"pools.hotTub, or a whole group such as pools; at least one, at most 20, none inside another, and not " +
			"the metadata. Every value given must lie inside a named path. A named path with no value clears " +
			"that field. A list, such as guestUnits or services.languagesSpoken, is replaced as a whole. There is no " +
			"wildcard"},
		{Name: "previous_values", Required: true, Description: "The current values of the named fields as last " +
			"read with lodging.get, in the shape of the groups below ({} if none of them is set); only compared " +
			"with what Google holds, never sent, and the change is refused when it differs"},
		{Name: "metadata", Description: "updateTime, the time the new values are asserted true, in RFC 3339 form; " +
			"default is now"}}
	for _, f := range changeFields {
		args = append(args, capability.Argument{Name: f.name, Description: f.about + "; an object of typed fields " +
			"as the input schema shows, or for a list the entries"})
	}
	return args
}

func updateInput() json.RawMessage {
	pairs := []string{"location", locationArgSchema, "update_mask", maskSchema(maxChangePaths),
		"previous_values", membersSchema(changeFields, true),
		"metadata", membersSchema([]field{timestamp("updateTime")}, true)}
	for _, f := range changeFields {
		pairs = append(pairs, f.name, f.schema(true))
	}
	return inputSchema([]string{"update_mask", "previous_values"}, pairs...)
}

var lodgingUpdate = capability.Descriptor{
	ID: lodgingUpdateID, Version: 1, Title: "Change the lodging data of a location",
	Description: "Change the lodging data of one business location, which is public on Google. Each group of the " +
		"lodging is an argument with typed fields; update_mask names the fields to change as dotted paths and is " +
		"required. previous_values is required and only compared: the tool reads the named fields first and changes " +
		"nothing unless they still hold exactly those values, so the confirmation shows the old and the new value " +
		"of every field. The confirmation names the location, the fields, and the full old and new values; a change " +
		"whose confirmation would exceed 500 characters is refused, so change fewer fields per call. The tool " +
		"sends the metadata update time itself. Google counts at most 10 changes per minute and profile. " +
		lodgingNote,
	Tags: []string{"google", "business-profile", "lodging", "update"}, Provider: Provider,
	Risk: capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true,
		DataSensitivity: lodgingRisk.DataSensitivity},
	InputSchema: updateInput(), OutputSchema: json.RawMessage(lodgingSchema),
	Arguments: updateArguments(),
	Fields:    lodgingFieldsOut,
	Examples: []capability.Example{{Description: "Close the hot tub",
		Arguments: json.RawMessage(`{"location":"locations/2000002","update_mask":["pools.hotTub"],` +
			`"previous_values":{"pools":{"hotTub":true}},"pools":{"hotTub":false}}`)}},
}

type lodgingReadArguments struct {
	Location string   `json:"location"`
	ReadMask []string `json:"read_mask"`
}

// lodgingTarget checks the arguments and the connection before any secret is read, opens the connection, and,
// for a connection bound to an account, proves the location. It returns the client and the name of the location.
func lodgingTarget(ctx context.Context, op string, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, given string) (*client, string, error) {
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, "", err
	}
	name, err := bound.locationOf(given)
	if err != nil {
		return nil, "", err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, "", err
	}
	if bound.account != "" {
		if err := c.proveLocation(ctx, op, bound.account, name); err != nil {
			return nil, "", err
		}
	}
	return c, name, nil
}

func (c *client) readLodging(ctx context.Context, op, tool, path string, mask []string, out any) error {
	return c.get(ctx, op, tool, request{root: lodgingRoot, path: path, query: readMaskQuery(mask),
		notFound: notFoundLodging}, out)
}

func readLodgingArguments(op string, raw json.RawMessage) (lodgingReadArguments, []string, error) {
	var args lodgingReadArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, nil, provider.Fail(op, "the validated arguments could not be read")
	}
	mask, err := maskOf("read_mask", args.ReadMask, readPaths)
	if err == nil && len(mask) > maxReadPaths {
		err = invalidArgument("read_mask names more than 100 field paths")
	}
	return args, mask, err
}

func handleLodgingGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get lodging"
	args, mask, err := readLodgingArguments(op, raw)
	if err != nil {
		return nil, err
	}
	c, name, err := lodgingTarget(ctx, op, resolved, secrets, red, args.Location)
	if err != nil {
		return nil, err
	}
	var answer any
	if err := c.readLodging(ctx, op, lodgingGetID, name+lodgingSuffix, mask, &answer); err != nil {
		return nil, err
	}
	return cleanObject(lodgingFields, answer), nil
}

func handleLodgingGoogleUpdated(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get Google-updated lodging"
	args, mask, err := readLodgingArguments(op, raw)
	if err != nil {
		return nil, err
	}
	c, name, err := lodgingTarget(ctx, op, resolved, secrets, red, args.Location)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Lodging  any    `json:"lodging"`
		DiffMask string `json:"diffMask"`
	}
	if err := c.readLodging(ctx, op, lodgingGoogleUpdatedID, name+lodgingSuffix+":getGoogleUpdated", mask,
		&answer); err != nil {
		return nil, err
	}
	out := map[string]any{}
	if lodging := cleanObject(lodgingFields, answer.Lodging); len(lodging) > 0 {
		out["lodging"] = lodging
	}
	var diff []string
	for _, path := range strings.Split(answer.DiffMask, ",") {
		if slices.Contains(readPaths, path) && !slices.Contains(diff, path) {
			diff = append(diff, path)
		}
	}
	if len(diff) > 0 {
		out["diffMask"] = strings.Join(diff, ",")
	}
	return out, nil
}

// lodgingEdit is one named field of a change: the value the caller says it holds and the value to set; a
// missing value is a field that is not set.
type lodgingEdit struct {
	path           string
	old, new       any
	oldSet, newSet bool
}

type lodgingPlan struct {
	location   string
	mask       []string
	body       map[string]any
	updateTime string
	edits      []lodgingEdit
}

// planLodgingUpdate checks the arguments of a change against the field tables and against each other. It
// reads nothing but its input, so the confirmation can be phrased from it.
func planLodgingUpdate(raw json.RawMessage) (*lodgingPlan, error) {
	var given map[string]json.RawMessage
	if err := json.Unmarshal(raw, &given); err != nil {
		return nil, invalidArgument("the arguments could not be read")
	}
	keys := make([]string, 0, len(given))
	for key := range given {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	plan := &lodgingPlan{body: map[string]any{}}
	var maskGiven []string
	var previous map[string]any
	for _, key := range keys {
		var value any
		if err := json.Unmarshal(given[key], &value); err != nil {
			return nil, invalidArgument(key + " could not be read")
		}
		switch key {
		case "location":
			plan.location, _ = value.(string)
		case "update_mask":
			if err := json.Unmarshal(given[key], &maskGiven); err != nil {
				return nil, invalidArgument("update_mask must be a list of field paths")
			}
		case "previous_values":
			if err := checkObject(changeFields, value, key); err != nil {
				return nil, err
			}
			previous = cleanObject(changeFields, value)
		case "metadata":
			if err := checkObject([]field{timestamp("updateTime")}, value, key); err != nil {
				return nil, err
			}
			plan.updateTime, _ = value.(map[string]any)["updateTime"].(string)
		default:
			f, ok := memberOf(changeFields, key)
			if !ok {
				return nil, invalidArgument(key + " is not a group of the lodging that can be changed")
			}
			if err := check(f, value, key); err != nil {
				return nil, err
			}
			if v, ok := clean(f, value); ok {
				plan.body[key] = v
			}
		}
	}
	if previous == nil {
		return nil, invalidArgument("previous_values is required: the values of the named fields as last read with " +
			"lodging.get, or {} if none is set")
	}
	mask, err := maskOf("update_mask", maskGiven, writePaths)
	if err != nil {
		return nil, err
	}
	if len(mask) > maxChangePaths {
		return nil, invalidArgument("update_mask names more than 20 field paths")
	}
	plan.mask = mask
	for _, c := range []struct {
		name   string
		object map[string]any
	}{{"a value", plan.body}, {"a previous value", previous}} {
		for _, path := range sortedLeaves(c.object) {
			if !covered(mask, path) {
				return nil, invalidArgument(c.name + " is given for " + path + ", which update_mask does not name")
			}
		}
	}
	changes := false
	for _, path := range mask {
		e := lodgingEdit{path: path}
		e.old, e.oldSet = valueAt(previous, path)
		e.new, e.newSet = valueAt(plan.body, path)
		changes = changes || !sameValue(e.old, e.oldSet, e.new, e.newSet)
		plan.edits = append(plan.edits, e)
	}
	if !changes {
		return nil, invalidArgument("every named field already has the value to set; there is nothing to change")
	}
	return plan, nil
}

func sortedLeaves(object map[string]any) []string {
	out := leaves(changeFields, object, "")
	slices.Sort(out)
	return out
}

// confirmation is the full text a person confirms: the location, every field, and its old and new value.
func (p *lodgingPlan) confirmation() string {
	where := p.location
	if where == "" {
		where = "the location of this connection"
	}
	parts := make([]string, 0, len(p.edits))
	for _, e := range p.edits {
		parts = append(parts, e.path+" "+shown(e.old, e.oldSet)+" -> "+shown(e.new, e.newSet))
	}
	return "Change the public lodging data of " + where + ": " + strings.Join(parts, "; ")
}

func shown(value any, set bool) string {
	if !set {
		return "unset"
	}
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil {
		return "?"
	}
	return strings.TrimSpace(b.String())
}

// confirmationRunes counts the characters the core keeps of a text: it writes a control, format, or separator
// character as an escape of up to ten characters, so those are counted as ten.
func confirmationRunes(text string) int {
	n := 0
	for _, r := range strings.ToValidUTF8(text, "�") {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			n += 2
		case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp):
			n += 10
		default:
			n++
		}
	}
	return n
}

const confirmationTooLong = "the confirmation with the full old and new values would exceed 500 characters; " +
	"change fewer fields per call"

// lodgingUpdateSummary phrases the confirmation from the arguments alone; a text that does not fit is not
// shown cut but replaced by the refusal the call will end with.
func lodgingUpdateSummary(raw json.RawMessage) string {
	plan, err := planLodgingUpdate(raw)
	if err != nil {
		return "Change the public lodging data; refused, nothing is changed: " + err.Error()
	}
	text := plan.confirmation()
	if confirmationRunes(text) > maxConfirmRunes {
		return "Change the public lodging data; refused, nothing is changed: " + confirmationTooLong
	}
	return text
}

func handleLodgingUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update lodging"
	plan, err := planLodgingUpdate(raw)
	if err != nil {
		return nil, err
	}
	if confirmationRunes(plan.confirmation()) > maxConfirmRunes {
		return nil, invalidArgument(confirmationTooLong)
	}
	if plan.updateTime == "" {
		plan.updateTime = lodgingNow().UTC().Format(time.RFC3339)
	}
	c, name, err := lodgingTarget(ctx, op, resolved, secrets, red, plan.location)
	if err != nil {
		return nil, err
	}
	var current any
	if err := c.readLodging(ctx, op, lodgingUpdateID, name+lodgingSuffix, plan.mask, &current); err != nil {
		return nil, err
	}
	held := cleanObject(lodgingFields, current)
	for _, e := range plan.edits {
		value, set := valueAt(held, e.path)
		if !sameValue(e.old, e.oldSet, value, set) {
			return nil, invalidArgument("previous_values does not match what Google holds for " + e.path +
				"; read the fields with lodging.get and send exactly those values")
		}
	}
	body := map[string]any{"name": name + lodgingSuffix, "metadata": map[string]any{"updateTime": plan.updateTime}}
	for key, value := range plan.body {
		body[key] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, provider.Fail(op, "the request body could not be built")
	}
	var answer any
	if err := c.patch(ctx, op, lodgingUpdateID, change{root: lodgingRoot, path: name + lodgingSuffix,
		query: url.Values{"updateMask": {strings.Join(plan.mask, ",")}}, body: encoded, key: name,
		notFound: notFoundLodging}, &answer); err != nil {
		return nil, err
	}
	return cleanObject(lodgingFields, answer), nil
}
