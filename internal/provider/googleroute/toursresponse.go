package googleroute

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// The answer of an optimization follows the OptimizeToursResponse of the reference (REST v1). Its types are
// the nodes of the request model where the shape is the same, so a route reads the same in both directions.
// Everything in it is untrusted third-party data and personal data: text is cleaned and cut, lists are cut,
// and what does not fit the documented shape is dropped. The request was billed already, so an answer that
// does not fit is shown as far as it fits and never refused.
//
// A cut is never silent. The result carries complete, and when it is false a truncation object that counts
// what was cut and names the lists. A polyline is kept whole or left out, because a cut line draws a wrong
// route.

// Bounds of an answer. Google documents none of them. The reading of the body is bounded by maxResponseBytes,
// these bounds keep the result usable for a client within that.
const (
	maxResponseItems = 1000
	maxResponseLabel = 256
	maxEnumText      = 100
	maxRouteToken    = 8192
	maxPlaceID       = 256

	// A polyline is kept whole up to maxResponsePolylineBytes, and all polylines of an answer together up to
	// maxResponsePolylineTotal. A later one that does not fit is left out and counted.
	maxResponsePolylineBytes = 256 << 10
	maxResponsePolylineTotal = 1 << 20

	// maxCutLists bounds the lists the truncation names, maxUnknownNames the unknown members it names.
	maxCutLists     = 20
	maxUnknownNames = 10

	// fieldReferenceDepth is how deep a validation error may nest subField. Google documents a recursive
	// type, and the node tree is finite.
	fieldReferenceDepth = 5
)

// listCaps are the bounds of lists by the name of their element. Every other list is cut at maxResponseItems.
var listCaps = map[string]int{
	"ShipmentRoute":                500,
	"Visit":                        2000,
	"Transition":                   2001,
	"Break":                        100,
	"SkippedShipment":              1000,
	"Reason":                       20,
	"OptimizeToursValidationError": 1000,
	"FieldReference":               20,
}

func fieldReference(depth int) *node {
	props := []prop{p("name", tText), p("index", tInt32), p("key", tText)}
	if depth > 0 {
		props = append(props, p("subField", fieldReference(depth-1)))
	}
	return object("FieldReference", props...)
}

var (
	tValidationError = object("OptimizeToursValidationError", p("code", tInt32), p("displayName", tText),
		p("fields", array(fieldReference(fieldReferenceDepth))), p("errorMessage", tText),
		p("offendingValues", tText))

	tMetrics = object("Metrics", p("aggregatedRouteMetrics", tAggregatedMetrics),
		p("skippedMandatoryShipmentCount", tInt32), p("usedVehicleCount", tInt32),
		p("earliestVehicleStartTime", tTimestamp), p("latestVehicleEndTime", tTimestamp),
		p("costs", dict(tDouble)), p("totalCost", tDouble))

	// tProcessedRequest is the modified request Google returns for TRANSFORM_AND_RETURN_REQUEST, which Google
	// marks experimental. It has the members of the request body. parent is listed because the RPC message
	// has it and the reference does not say whether the REST answer echoes it.
	tProcessedRequest = processedRequest()

	tToursResponse = object("OptimizeToursResponse",
		p("routes", array(tShipmentRoute)), p("requestLabel", tText),
		p("skippedShipments", array(tSkippedShipment)), p("validationErrors", array(tValidationError)),
		p("processedRequest", tProcessedRequest), p("metrics", tMetrics))
)

func processedRequest() *node {
	props := []prop{p("parent", tText)}
	for _, a := range toursArgs {
		props = append(props, p(a.wire, a.n))
	}
	props = append(props, p("allowLargeDeadlineDespiteInterruptionRisk", tBool))
	return object("OptimizeToursRequest", props...)
}

// truncation says what an answer lost. Every count is zero for a complete answer, and the object is then
// left out of the result.
type truncation struct {
	// TextsCut counts strings cut at their bound.
	TextsCut int `json:"textsCut"`
	// ListsCut counts lists and maps that were cut, ItemsOmitted the entries they lost.
	ListsCut     int `json:"listsCut"`
	ItemsOmitted int `json:"itemsOmitted"`
	// PolylinesOmitted counts polylines left out because they were too long, did not hold encoded points or
	// did not fit the total.
	PolylinesOmitted int `json:"polylinesOmitted"`
	// ValuesDropped counts values of another type or form than the reference documents, and identifiers that
	// are useless when cut and were too long.
	ValuesDropped int `json:"valuesDropped"`
	// UnknownFields counts members the reference does not document, which are not passed on.
	UnknownFields     int       `json:"unknownFields"`
	UnknownFieldNames []string  `json:"unknownFieldNames,omitempty"`
	Lists             []listCut `json:"lists,omitempty"`
}

// listCut names one list or map that was cut: its place in the result, what it keeps and what Google sent.
type listCut struct {
	Path  string `json:"path"`
	Kept  int    `json:"kept"`
	Total int    `json:"total"`
}

func (t truncation) none() bool {
	return t.TextsCut == 0 && t.ListsCut == 0 && t.ItemsOmitted == 0 && t.PolylinesOmitted == 0 &&
		t.ValuesDropped == 0 && t.UnknownFields == 0
}

// cutter cleans one answer and keeps the count of what it cuts.
type cutter struct {
	t              truncation
	polylineBudget int
	withType       bool
}

// toursResultOf maps the decoded body of an OptimizeToursResponse to the result of a tool: the members of the
// answer under Google's names, cleaned and cut, with complete and, if not complete, truncation. It returns an
// invalid-response error for a body that is not one JSON object. withType keeps the @type member that the
// response of a long-running operation carries.
func toursResultOf(op string, raw []byte, withType bool) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	c := &cutter{polylineBudget: maxResponsePolylineTotal, withType: withType}
	cleaned, _ := c.clean(tToursResponse, value, "", "")
	result, _ := cleaned.(map[string]any)
	result["complete"] = c.t.none()
	if !c.t.none() {
		result["truncation"] = c.t
	}
	return result, nil
}

func (c *cutter) drop() (any, bool) {
	c.t.ValuesDropped++
	return nil, false
}

func capOf(elem *node) int {
	if limit, ok := listCaps[elem.name]; ok {
		return limit
	}
	return maxResponseItems
}

// textLimit is the bound of a text by the name of its member.
func textLimit(name string) int {
	switch name {
	case "label", "requestLabel", "vehicleLabel", "shipmentLabel", "visitLabel":
		return maxResponseLabel
	case "displayName":
		return maxNameText
	}
	return maxText
}

// wholeText lists the members that are identifiers: useless when cut, so shown whole or not at all.
var wholeText = map[string]int{"routeToken": maxRouteToken, "placeId": maxPlaceID}

func (c *cutter) record(path string, kept, total int) {
	c.t.ListsCut++
	c.t.ItemsOmitted += total - kept
	if len(c.t.Lists) < maxCutLists {
		c.t.Lists = append(c.t.Lists, listCut{Path: boundedText(path, maxNameText), Kept: kept, Total: total})
	}
}

func (c *cutter) unknown(name string) {
	c.t.UnknownFields++
	name = boundedText(name, maxLabelText)
	if len(c.t.UnknownFieldNames) >= maxUnknownNames {
		return
	}
	for _, known := range c.t.UnknownFieldNames {
		if known == name {
			return
		}
	}
	c.t.UnknownFieldNames = append(c.t.UnknownFieldNames, name)
}

// clean returns the part of value that the node keeps. It is false when nothing of the value is shown, which
// is silent for a null and counted otherwise.
func (c *cutter) clean(n *node, value any, path, name string) (any, bool) {
	if value == nil {
		return nil, false
	}
	switch n.kind {
	case kindObject:
		members, ok := value.(map[string]any)
		if !ok {
			return c.drop()
		}
		out := map[string]any{}
		for _, field := range n.props {
			raw, present := members[field.name]
			if !present {
				continue
			}
			child := field.name
			if path != "" {
				child = path + "." + field.name
			}
			if cleaned, keep := c.clean(field.n, raw, child, field.name); keep {
				out[field.name] = cleaned
			}
		}
		keys := make([]string, 0, len(members))
		for key := range members {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if n.member(key) != nil {
				continue
			}
			if path == "" && c.withType && key == "@type" {
				if text, ok := members[key].(string); ok {
					out[key] = boundedText(text, maxResponseLabel)
					continue
				}
			}
			c.unknown(key)
		}
		if n.name == "EncodedPolyline" && len(out) == 0 {
			return nil, false
		}
		return out, true
	case kindArray:
		items, ok := value.([]any)
		if !ok {
			return c.drop()
		}
		kept := min(len(items), capOf(n.elem))
		if kept < len(items) {
			c.record(path, kept, len(items))
		}
		out := make([]any, 0, kept)
		for i, item := range items[:kept] {
			if cleaned, keep := c.clean(n.elem, item, path+"["+strconv.Itoa(i)+"]", name); keep {
				out = append(out, cleaned)
			}
		}
		return out, true
	case kindMap:
		entries, ok := value.(map[string]any)
		if !ok {
			return c.drop()
		}
		keys := make([]string, 0, len(entries))
		for key := range entries {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := map[string]any{}
		for _, key := range keys {
			// A key that is cut or cleaned into another one could name a different entry, so such an entry
			// is left out.
			clean := boundedText(key, math.MaxInt32)
			if len(out) >= maxMapEntries || len(clean) > maxLabelText || out[clean] != nil {
				continue
			}
			if cleaned, keep := c.clean(n.elem, entries[key], path+"{"+clean+"}", name); keep {
				out[clean] = cleaned
			}
		}
		if len(out) < len(entries) {
			c.record(path, len(out), len(entries))
		}
		return out, true
	case kindText:
		text, ok := value.(string)
		if !ok {
			return c.drop()
		}
		if limit, whole := wholeText[name]; whole {
			if clean := boundedText(text, math.MaxInt32); text == "" || len(clean) > limit {
				return c.drop()
			}
			return boundedText(text, math.MaxInt32), true
		}
		limit := textLimit(name)
		if len(boundedText(text, math.MaxInt32)) > limit {
			c.t.TextsCut++
		}
		return boundedText(text, limit), true
	case kindEnum:
		text, ok := value.(string)
		if !ok {
			return c.drop()
		}
		if len(text) > maxEnumText {
			c.t.TextsCut++
		}
		return boundedText(text, maxEnumText), true
	case kindLongText:
		text, ok := value.(string)
		if !ok {
			return c.drop()
		}
		if !c.polyline(text) {
			c.t.PolylinesOmitted++
			return nil, false
		}
		c.polylineBudget -= len(text)
		return text, true
	case kindBool:
		flag, ok := value.(bool)
		if !ok {
			return c.drop()
		}
		return flag, true
	case kindDouble:
		switch number := value.(type) {
		case json.Number:
			if f, err := number.Float64(); err == nil && !math.IsInf(f, 0) {
				return number, true
			}
		case string:
			// Protocol buffers write the values that JSON has no number for as text.
			if number == "NaN" || number == "Infinity" || number == "-Infinity" {
				return number, true
			}
		}
		return c.drop()
	case kindInt32:
		if number, ok := value.(json.Number); ok {
			if _, err := strconv.ParseInt(number.String(), 10, 32); err == nil {
				return number, true
			}
		}
		return c.drop()
	case kindInt64:
		switch number := value.(type) {
		case json.Number:
			if _, err := strconv.ParseInt(number.String(), 10, 64); err == nil {
				return number, true
			}
		case string:
			if int64Form.MatchString(number) {
				if _, err := strconv.ParseInt(number, 10, 64); err == nil {
					return number, true
				}
			}
		}
		return c.drop()
	case kindDuration:
		if text, ok := value.(string); ok && durationForm.MatchString(text) {
			return text, true
		}
		return c.drop()
	case kindTimestamp:
		if text, ok := value.(string); ok && timestampForm.MatchString(text) {
			return text, true
		}
		return c.drop()
	}
	return c.drop()
}

// polyline reports whether an encoded polyline is shown: not empty, within its bound and the total, and
// written in the alphabet of the algorithm.
func (c *cutter) polyline(text string) bool {
	if text == "" || len(text) > maxResponsePolylineBytes || len(text) > c.polylineBudget {
		return false
	}
	for i := 0; i < len(text); i++ {
		// The polyline algorithm writes only the characters 63 to 126.
		if text[i] < '?' || text[i] > '~' {
			return false
		}
	}
	return true
}

// outputSchema renders a node as the schema of its cleaned form. Enumerations are plain strings, because
// Google may add values, and the nodes of the request carry no bounds that the answer needs.
func outputSchema(n *node) map[string]any {
	switch n.kind {
	case kindObject:
		properties := make(map[string]any, len(n.props))
		for _, field := range n.props {
			properties[field.name] = outputSchema(field.n)
		}
		return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	case kindArray:
		return map[string]any{"type": "array", "items": outputSchema(n.elem)}
	case kindMap:
		return map[string]any{"type": "object", "additionalProperties": outputSchema(n.elem)}
	case kindBool:
		return map[string]any{"type": "boolean"}
	case kindDouble:
		return map[string]any{"anyOf": []any{
			map[string]any{"type": "number"},
			map[string]any{"type": "string", "enum": []any{"NaN", "Infinity", "-Infinity"}},
		}}
	case kindInt32:
		return map[string]any{"type": "integer"}
	case kindInt64:
		return map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}}}
	}
	return map[string]any{"type": "string"}
}

// toursOutputSchema is the output schema of a result of toursResultOf. withType adds the @type member.
func toursOutputSchema(withType bool) json.RawMessage {
	schema := outputSchema(tToursResponse)
	properties := schema["properties"].(map[string]any)
	if withType {
		properties["@type"] = map[string]any{"type": "string"}
	}
	properties["complete"] = map[string]any{"type": "boolean"}
	properties["truncation"] = map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"textsCut":          map[string]any{"type": "integer"},
		"listsCut":          map[string]any{"type": "integer"},
		"itemsOmitted":      map[string]any{"type": "integer"},
		"polylinesOmitted":  map[string]any{"type": "integer"},
		"valuesDropped":     map[string]any{"type": "integer"},
		"unknownFields":     map[string]any{"type": "integer"},
		"unknownFieldNames": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"lists": map[string]any{"type": "array", "items": map[string]any{"type": "object",
			"additionalProperties": false, "properties": map[string]any{
				"path": map[string]any{"type": "string"}, "kept": map[string]any{"type": "integer"},
				"total": map[string]any{"type": "integer"}}}},
	}}
	schema["required"] = []string{"complete"}
	out, err := json.Marshal(schema)
	if err != nil {
		panic("googleroute: the optimization output schema cannot be encoded: " + err.Error())
	}
	return out
}

// toursOutput is the output schema of tours.optimize.
var toursOutput = toursOutputSchema(false)
