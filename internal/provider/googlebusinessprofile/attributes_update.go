package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
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

const attributesUpdateID = Provider + ".attributes.update"

const (
	attributeNamePattern = `^attributes/[A-Za-z0-9_]{1,100}$`
	maxAttributeChanges  = 50
	maxAttributeValues   = 50
	// maxMetadataPages bounds the read of the attribute metadata of a location that an update checks its
	// attributes against: 10 pages of 200 attributes.
	maxMetadataPages = 10
)

var attributeNameForm = regexp.MustCompile(attributeNamePattern)

var attributeInputProperties = []string{
	"name", `{"type":"string","maxLength":110,"pattern":"` + attributeNamePattern + `"}`,
	"values", `{"type":"array","maxItems":50,"items":{"anyOf":[` + booleanSchema + `,{"type":"string","minLength":1,"maxLength":100}]}}`,
	"uriValues", `{"type":"array","maxItems":50,"items":{"type":"object","properties":{"uri":{"type":"string",` +
		`"minLength":1,"maxLength":2048}},"required":["uri"],"additionalProperties":false}}`,
	"repeatedEnumValue", `{"type":"object","properties":{"setValues":{"type":"array","maxItems":50,"items":` +
		`{"type":"string","minLength":1,"maxLength":100}},"unsetValues":{"type":"array","maxItems":50,"items":` +
		`{"type":"string","minLength":1,"maxLength":100}}},"additionalProperties":false}`,
}

func attributeInputSchema(previous bool) string {
	pairs := slices.Clone(attributeInputProperties)
	if previous {
		pairs = append(pairs, "valueType", `{"type":"string","enum":["BOOL","ENUM","URL","REPEATED_ENUM"]}`)
	}
	members := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		members = append(members, `"`+pairs[i]+`":`+pairs[i+1])
	}
	return `{"type":"object","properties":{` + strings.Join(members, ",") + `},"required":["name"],` +
		`"additionalProperties":false}`
}

var attributesUpdate = capability.Descriptor{
	ID: attributesUpdateID, Version: 1, Title: "Change Business Profile attributes",
	Description: "Set or remove attributes of one business location. Attributes are public profile data, and a " +
		"URI value appears as a link on the public profile; the tool never fetches it, and the confirmation shows " +
		"it completely. attribute_mask is required and names every attribute that changes as attributes/ATTRIBUTE; " +
		"each attribute in attributes must be in it, and a name in the mask without an entry in attributes " +
		"removes that attribute. previous_attributes is required and only compared: give the attributes of the " +
		"mask as last read with attributes.get, none for an attribute that is not set. The tool reads the " +
		"attributes first and changes nothing unless they still are exactly that, so the confirmation shows the " +
		"old and the new values; the whole change must fit into that confirmation of 500 characters, so a " +
		"larger change is refused and goes in several calls. Every attribute must be offered for the location " +
		"(attributes.list with location), must not be deprecated, and its value must match the value type: " +
		"values with one boolean for BOOL, one listed value for ENUM, uriValues for URL, and repeatedEnumValue " +
		"with listed values for REPEATED_ENUM. Google counts at most 10 changes per minute and profile. " +
		attributesNote,
	Tags: []string{"google", "business-profile", "attributes", "update"}, Provider: Provider,
	Risk: capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true,
		DataSensitivity: "public business profile attributes: amenities, payment types, and links such as booking " +
			"or menu addresses that appear on the public profile"},
	InputSchema: inputSchema([]string{"attributes", "attribute_mask", "previous_attributes"},
		"location", locationArgSchema,
		"attributes", `{"type":"array","maxItems":50,"items":`+attributeInputSchema(false)+`}`,
		"attribute_mask", `{"type":"array","minItems":1,"maxItems":50,"uniqueItems":true,"items":{"type":"string",`+
			`"maxLength":110,"pattern":"`+attributeNamePattern+`"}}`,
		"previous_attributes", `{"type":"array","maxItems":50,"items":`+attributeInputSchema(true)+`}`),
	OutputSchema: json.RawMessage(attributesResultSchema),
	Arguments: []capability.Argument{
		{Name: "location", Target: "location", Description: "Location as locations/LOCATION; optional when the " +
			"connection is bound to a location, and then it must be that location"},
		{Name: "attributes", Required: true, Description: "The new attributes: name (attributes/ATTRIBUTE) and one " +
			"of values, uriValues (uri), or repeatedEnumValue (setValues, unsetValues), as the value type needs; " +
			"empty when the change only removes attributes"},
		{Name: "attribute_mask", Required: true, Description: "Names of all attributes the call changes or removes, " +
			"as attributes/ATTRIBUTE; every attribute in attributes is in it"},
		{Name: "previous_attributes", Required: true, Description: "The attributes of the mask as last read with " +
			"attributes.get (valueType may stay); only compared with what Google holds, never sent, and the change " +
			"is refused when it differs; leave out an attribute that is not set"},
	},
	Fields: attributesGet.Fields,
	Examples: []capability.Example{{Description: "Switch one boolean attribute on", Arguments: json.RawMessage(
		`{"location":"locations/2000002","attribute_mask":["attributes/has_wifi"],` +
			`"attributes":[{"name":"attributes/has_wifi","values":[true]}],"previous_attributes":[]}`)}},
}

// attributeInput is an attribute as an argument. ValueType is only accepted in previous_attributes.
type attributeInput struct {
	Name              string         `json:"name"`
	ValueType         string         `json:"valueType,omitempty"`
	Values            []any          `json:"values,omitempty"`
	URIValues         []uriInput     `json:"uriValues,omitempty"`
	RepeatedEnumValue *repeatedInput `json:"repeatedEnumValue,omitempty"`
}

type uriInput struct {
	URI string `json:"uri"`
}

type repeatedInput struct {
	SetValues   []string `json:"setValues,omitempty"`
	UnsetValues []string `json:"unsetValues,omitempty"`
}

type attributesUpdateArguments struct {
	Location           string           `json:"location"`
	Attributes         []attributeInput `json:"attributes"`
	AttributeMask      []string         `json:"attribute_mask"`
	PreviousAttributes []attributeInput `json:"previous_attributes"`
}

// canon is a spelling of the value of an attribute in which equal values are equal strings: the value
// members as sets, and nothing for an attribute without a value.
func canon(values []any, uris, set, unset []string) string {
	encoded := make([]string, 0, len(values))
	for _, v := range values {
		raw, _ := json.Marshal(v)
		encoded = append(encoded, string(raw))
	}
	for _, s := range [][]string{encoded, uris, set, unset} {
		slices.Sort(s)
	}
	if len(encoded)+len(uris)+len(set)+len(unset) == 0 {
		return ""
	}
	raw, _ := json.Marshal([4][]string{encoded, uris, set, unset})
	return string(raw)
}

func (a attributeInput) canon() string {
	uris := make([]string, 0, len(a.URIValues))
	for _, u := range a.URIValues {
		uris = append(uris, u.URI)
	}
	var set, unset []string
	if a.RepeatedEnumValue != nil {
		set = slices.Clone(a.RepeatedEnumValue.SetValues)
		unset = slices.Clone(a.RepeatedEnumValue.UnsetValues)
	}
	return canon(a.Values, uris, set, unset)
}

func (a attribute) canon() string {
	uris := make([]string, 0, len(a.URIValues))
	for _, u := range a.URIValues {
		uris = append(uris, string(u.URI))
	}
	var set, unset []string
	if a.RepeatedEnumValue != nil {
		for _, s := range a.RepeatedEnumValue.SetValues {
			set = append(set, string(s))
		}
		for _, s := range a.RepeatedEnumValue.UnsetValues {
			unset = append(unset, string(s))
		}
	}
	return canon([]any(a.Values), uris, set, unset)
}

// plainText accepts a non-empty text of at most limit characters without control, format, or separator
// characters, which could hide what a confirmation shows.
func plainText(s string, limit int) bool {
	if s == "" || utf8.RuneCountInString(s) > limit || !utf8.ValidString(s) {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) })
}

// checkURI accepts an http or https address of printable ASCII without user info. A non-ASCII address has to
// be written in its punycode and percent-encoded form, so that what the confirmation shows is what is sent.
func checkURI(raw string) bool {
	if raw == "" || len(raw) > maxURLRunes || strings.ContainsFunc(raw, func(r rune) bool { return r <= ' ' || r >= 0x7f }) {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// checkInputs refuses everything that can be refused from the arguments alone.
func (a attributesUpdateArguments) check() (mask []string, body, previous map[string]attributeInput, err error) {
	mask = slices.Clone(a.AttributeMask)
	slices.Sort(mask)
	if len(mask) == 0 || len(mask) > maxAttributeChanges || len(slices.Compact(slices.Clone(mask))) != len(mask) {
		return nil, nil, nil, invalidArgument("attribute_mask is required and names each attribute once")
	}
	for _, name := range mask {
		if !attributeNameForm.MatchString(name) {
			return nil, nil, nil, invalidArgument("attribute_mask names an attribute that is not attributes/ATTRIBUTE")
		}
	}
	body, previous = map[string]attributeInput{}, map[string]attributeInput{}
	for _, item := range a.Attributes {
		if _, dup := body[item.Name]; dup || !slices.Contains(mask, item.Name) {
			return nil, nil, nil, invalidArgument("every attribute in attributes appears once there and is named in attribute_mask")
		}
		if item.ValueType != "" {
			return nil, nil, nil, invalidArgument("valueType is read only and not part of attributes")
		}
		if item.canon() == "" {
			return nil, nil, nil, invalidArgument("an attribute in attributes carries a value; to remove an " +
				"attribute, name it in attribute_mask only")
		}
		if err := checkValueForm(item, true); err != nil {
			return nil, nil, nil, err
		}
		body[item.Name] = item
	}
	for _, item := range a.PreviousAttributes {
		if _, dup := previous[item.Name]; dup || !slices.Contains(mask, item.Name) {
			return nil, nil, nil, invalidArgument("previous_attributes names each attribute of attribute_mask at " +
				"most once and no other")
		}
		if err := checkValueForm(item, false); err != nil {
			return nil, nil, nil, err
		}
		previous[item.Name] = item
	}
	unchanged := true
	for _, name := range mask {
		if previous[name].canon() != body[name].canon() {
			unchanged = false
		}
	}
	if unchanged {
		return nil, nil, nil, invalidArgument("the attributes already have these values; there is nothing to change")
	}
	return mask, body, previous, nil
}

// checkValueForm checks the texts of an attribute and that it carries one kind of value. A new value is
// held to the strict address form; a previous one is only compared.
func checkValueForm(item attributeInput, strict bool) error {
	kinds := 0
	if len(item.Values) > 0 {
		kinds++
	}
	if len(item.URIValues) > 0 {
		kinds++
	}
	if item.RepeatedEnumValue != nil && len(item.RepeatedEnumValue.SetValues)+len(item.RepeatedEnumValue.UnsetValues) > 0 {
		kinds++
	}
	if kinds > 1 {
		return invalidArgument("an attribute carries one kind of value: values, uriValues, or repeatedEnumValue")
	}
	for _, v := range item.Values {
		if s, ok := v.(string); ok && !plainText(s, maxShortRunes) {
			return invalidArgument("a value must be 1 to 100 characters without control characters")
		} else if _, isBool := v.(bool); !ok && !isBool {
			return invalidArgument("a value is a string or a boolean")
		}
	}
	for _, u := range item.URIValues {
		if strict && !checkURI(u.URI) || !strict && !plainText(u.URI, maxURLRunes+1) {
			return invalidArgument("uri must be an http or https address of printable ASCII characters without " +
				"user info, at most 2048 characters")
		}
	}
	if item.RepeatedEnumValue != nil {
		for _, s := range slices.Concat(item.RepeatedEnumValue.SetValues, item.RepeatedEnumValue.UnsetValues) {
			if !plainText(s, maxShortRunes) {
				return invalidArgument("setValues and unsetValues hold values of 1 to 100 characters without " +
					"control characters")
			}
		}
	}
	return nil
}

// checkAgainstMetadata checks a new attribute against the metadata Google lists for the location.
func checkAgainstMetadata(item attributeInput, m attributeMetadata) error {
	if m.Deprecated.value {
		return invalidArgument("an attribute is deprecated and is not changed: Google does not save such a change")
	}
	listed := func(v any) bool {
		for _, entry := range m.ValueMetadata {
			if entry.Value.v == v {
				return true
			}
		}
		return false
	}
	switch string(m.ValueType) {
	case "BOOL", "ENUM":
		if len(item.Values) == 0 || len(item.URIValues) > 0 || item.RepeatedEnumValue != nil ||
			len(item.Values) > 1 && !m.Repeatable.value {
			return invalidArgument("an attribute of the value type BOOL or ENUM takes values, one unless it is repeatable")
		}
		for _, v := range item.Values {
			_, isBool := v.(bool)
			if isBool != (string(m.ValueType) == "BOOL") || string(m.ValueType) == "ENUM" && !listed(v) {
				return invalidArgument("a value does not fit the value type or is not among the values of the attribute")
			}
		}
	case "URL":
		if len(item.URIValues) == 0 || len(item.Values) > 0 || item.RepeatedEnumValue != nil {
			return invalidArgument("an attribute of the value type URL takes uriValues")
		}
	case "REPEATED_ENUM":
		r := item.RepeatedEnumValue
		if r == nil || len(item.Values) > 0 || len(item.URIValues) > 0 {
			return invalidArgument("an attribute of the value type REPEATED_ENUM takes repeatedEnumValue")
		}
		for _, s := range slices.Concat(r.SetValues, r.UnsetValues) {
			if !listed(s) {
				return invalidArgument("a value is not among the values of the attribute")
			}
		}
		for _, s := range r.SetValues {
			if slices.Contains(r.UnsetValues, s) {
				return invalidArgument("a value is both set and unset")
			}
		}
	default:
		return invalidArgument("the value type of an attribute is unknown, so it is not changed")
	}
	return nil
}

// findMetadata reads the attribute metadata of a location page by page until every wanted attribute is found.
func (c *client) findMetadata(ctx context.Context, op, location string, want []string) (map[string]attributeMetadata, error) {
	found := map[string]attributeMetadata{}
	query := pageSizeQuery(url.Values{"parent": {location}}, maxMetadataPage)
	for range maxMetadataPages {
		var answer metadataAnswer
		if err := c.get(ctx, op, attributesUpdateID, request{root: informationRoot, path: "attributes", query: query,
			notFound: notFoundLocation}, &answer); err != nil {
			return nil, err
		}
		if len(answer.AttributeMetadata) > maxMetadataPage {
			return nil, tooMany(op, maxMetadataPage)
		}
		for _, m := range answer.AttributeMetadata {
			// The discovery document calls parent the unique identifier of the attribute, and the mask
			// attributes/ATTRIBUTE; both spellings of that identifier are taken.
			name := "attributes/" + strings.TrimPrefix(string(m.Parent), "attributes/")
			if slices.Contains(want, name) {
				found[name] = m
			}
		}
		if len(found) == len(want) || answer.NextPageToken == "" {
			return found, nil
		}
		if len(answer.NextPageToken) > maxPageToken {
			return nil, provider.InvalidResponse(op, subject+" returned a continuation token that is too long")
		}
		query.Set("pageToken", answer.NextPageToken)
	}
	return found, nil
}

func handleAttributesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "change attributes"
	var args attributesUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	name, err := bound.locationOf(args.Location)
	if err != nil {
		return nil, err
	}
	mask, body, previous, err := args.check()
	if err != nil {
		return nil, err
	}
	if confirmationRunes(attributesSummary(raw)) > maxConfirmRunes {
		return nil, invalidArgument("the change is too large to be shown completely in the confirmation of 500 " +
			"characters; change fewer attributes or shorter values per call")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if bound.account != "" {
		if err := c.proveLocation(ctx, op, bound.account, name); err != nil {
			return nil, err
		}
	}
	metadata, err := c.findMetadata(ctx, op, name, mask)
	if err != nil {
		return nil, err
	}
	for _, attr := range mask {
		m, ok := metadata[attr]
		if !ok {
			return nil, invalidArgument("an attribute in attribute_mask is not offered for this location; list the " +
				"attributes with attributes.list and the location")
		}
		if item, writes := body[attr]; writes {
			if err := checkAgainstMetadata(item, m); err != nil {
				return nil, err
			}
		} else if m.Deprecated.value {
			return nil, invalidArgument("an attribute is deprecated and is not changed: Google does not save such a change")
		}
	}
	var current attributesAnswer
	if err := c.get(ctx, op, attributesUpdateID, request{root: informationRoot, path: attributesPath(name),
		notFound: notFoundLocation}, &current); err != nil {
		return nil, err
	}
	held := map[string]string{}
	for _, entry := range current.Attributes {
		held[string(entry.Name)] = entry.canon()
	}
	for _, attr := range mask {
		if _, listed := held[attr]; !listed && len(current.Attributes) >= maxItems {
			return nil, invalidArgument("an attribute is not among the first 250 attributes of the location, so its " +
				"current value cannot be checked")
		}
		if held[attr] != previous[attr].canon() {
			return nil, invalidArgument("previous_attributes is not what Google holds for these attributes now; " +
				"read them with attributes.get and send those values")
		}
	}
	// Only the attributes that change are sent. valueType and anything else Google answers stay out.
	changes := make([]attributeInput, 0, len(body))
	for _, attr := range mask {
		if item, ok := body[attr]; ok {
			changes = append(changes, item)
		}
	}
	payload, err := json.Marshal(struct {
		Name       string           `json:"name"`
		Attributes []attributeInput `json:"attributes"`
	}{attributesPath(name), changes})
	if err != nil {
		return nil, provider.Fail(op, "the request body could not be built")
	}
	query := url.Values{"attributeMask": {strings.Join(mask, ",")}}
	var out attributesAnswer
	if err := c.patch(ctx, op, attributesUpdateID, change{root: informationRoot, path: attributesPath(name),
		query: query, body: payload, key: name, notFound: notFoundLocation}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// attributesSummary phrases a confirmation from the arguments alone: the location and, for each attribute of
// the mask, the value read before and the value to set, addresses complete. The core neutralizes and bounds
// it; the handler refuses a change whose summary the bound would cut.
func attributesSummary(raw json.RawMessage) string {
	var a attributesUpdateArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	body, previous := map[string]attributeInput{}, map[string]attributeInput{}
	for _, item := range a.Attributes {
		body[item.Name] = item
	}
	for _, item := range a.PreviousAttributes {
		previous[item.Name] = item
	}
	mask := slices.Clone(a.AttributeMask)
	slices.Sort(mask)
	out := "Change the public attributes"
	if a.Location != "" {
		out += " of " + a.Location
	}
	out += ", old to new:"
	for i, name := range mask {
		if i > 0 {
			out += ";"
		}
		next := "removed"
		if item, ok := body[name]; ok {
			next = item.describe()
		}
		out += " " + name + " " + previous[name].describe() + " -> " + next
	}
	return out
}

// describe writes the value of an attribute; an attribute without one is "none".
func (a attributeInput) describe() string {
	var parts []string
	for _, v := range a.Values {
		if text, ok := v.(string); ok {
			parts = append(parts, strconv.Quote(text))
		} else {
			parts = append(parts, strconv.FormatBool(v == true))
		}
	}
	for _, u := range a.URIValues {
		parts = append(parts, "uri "+u.URI)
	}
	if r := a.RepeatedEnumValue; r != nil {
		for _, s := range r.SetValues {
			parts = append(parts, "set "+s)
		}
		for _, s := range r.UnsetValues {
			parts = append(parts, "unset "+s)
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}
