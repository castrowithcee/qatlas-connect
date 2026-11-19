package googleroute

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Qatlas bounds of an optimization request. An invoke may take 60 seconds in all, and a token request and the
// reading of the answer come on top of the time Google gets. The timeout therefore stays well below that, and
// a longer optimization belongs to the long-running method. The size bound keeps the request and, with the
// answer bound, its result within what the tool can hand back.
const (
	toursDefaultTimeout = 30 * time.Second
	toursMaxTimeout     = 45 * time.Second
	maxToursBodyBytes   = 2 << 20
)

// toursEnums are the enumerations of the request itself. SolvingMode lists TRANSFORM_AND_RETURN_REQUEST, which
// Google marks experimental.
var (
	solvingModes = []string{"DEFAULT_SOLVE", "VALIDATE_ONLY", "DETECT_SOME_INFEASIBLE_SHIPMENTS",
		"TRANSFORM_AND_RETURN_REQUEST"}
	searchModes = []string{"RETURN_FAST", "CONSUME_ALL_AVAILABLE_TIME"}
)

// topArg is one argument of the request. arg is the name in snake_case, wire the member of Google's body.
type topArg struct {
	arg, wire string
	n         *node
	required  bool
}

func top(arg, wire string, n *node) topArg { return topArg{arg: arg, wire: wire, n: n} }

// toursArgs are the arguments every optimization method takes besides project. allowLargeDeadline is not one of
// them: only the long-running method has it, through toursRequest.extra.
var toursArgs = []topArg{
	{arg: "model", wire: "model", n: tShipmentModel, required: true},
	top("timeout", "timeout", tDuration),
	top("solving_mode", "solvingMode", enum("SolvingMode", solvingModes...)),
	top("search_mode", "searchMode", enum("SearchMode", searchModes...)),
	top("injected_first_solution_routes", "injectedFirstSolutionRoutes", array(tShipmentRoute)),
	top("injected_solution_constraint", "injectedSolutionConstraint", tInjectedSolutionConstraint),
	top("refresh_details_routes", "refreshDetailsRoutes", array(tShipmentRoute)),
	top("interpret_injected_solutions_using_labels", "interpretInjectedSolutionsUsingLabels", tBool),
	top("consider_road_traffic", "considerRoadTraffic", tBool),
	top("populate_polylines", "populatePolylines", tBool),
	top("populate_transition_polylines", "populateTransitionPolylines", tBool),
	top("use_geodesic_distances", "useGeodesicDistances", tBool),
	top("geodesic_meters_per_second", "geodesicMetersPerSecond", tDouble),
	top("label", "label", tText),
	top("max_validation_errors", "maxValidationErrors", tInt32),
}

// toursRequest describes one optimization method: the arguments it adds to the shared ones and the time it
// allows. The long-running method reuses it with its own values. A method with largeTimeout allows that longer
// solving time instead of maxTimeout while its boolean argument largeSwitch (from extra) is true.
type toursRequest struct {
	extra          []topArg
	defaultTimeout time.Duration
	maxTimeout     time.Duration
	largeSwitch    string
	largeTimeout   time.Duration
}

// optimizeTours is the synchronous method.
var optimizeTours = toursRequest{defaultTimeout: toursDefaultTimeout, maxTimeout: toursMaxTimeout}

func (r toursRequest) args() []topArg {
	return append(append([]topArg(nil), toursArgs...), r.extra...)
}

const projectArgumentPattern = `^([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])$`

// schema is the input schema of the method: project and the members of the request.
func (r toursRequest) schema() json.RawMessage {
	properties := map[string]any{"project": map[string]any{"type": "string", "pattern": projectArgumentPattern,
		"minLength": 6, "maxLength": 30}}
	required := []string{"project"}
	for _, a := range r.args() {
		s := a.n.schema()
		switch a.arg {
		case "max_validation_errors":
			s["minimum"], s["maximum"] = 0, maxValidationErrorsLimit
		}
		properties[a.arg] = s
		if a.required {
			required = append(required, a.arg)
		}
	}
	out, err := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": required,
		"additionalProperties": false})
	if err != nil {
		panic("googleroute: the optimization schema cannot be encoded: " + err.Error())
	}
	return out
}

// toursBody is a checked request: the project, the body to send, and what the confirmation names.
type toursBody struct {
	Project   string
	Payload   []byte
	Timeout   time.Duration
	Shipments int
	Vehicles  int
	// Extra holds the values of the arguments of the method that the shared request does not know, by argument.
	Extra map[string]any
}

// body checks the arguments for form, types, enumeration values and the bounds of Qatlas, and builds Google's
// body. It does not check what Google checks: the consistency of the model, of times and of indices is left
// to the API, which reports it.
func (r toursRequest) body(raw json.RawMessage) (toursBody, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var given map[string]any
	if err := decoder.Decode(&given); err != nil || given == nil {
		return toursBody{}, badRequest("the arguments must be a JSON object")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return toursBody{}, badRequest("the arguments must hold exactly one JSON value")
	}
	known := map[string]topArg{}
	for _, a := range r.args() {
		known[a.arg] = a
	}
	for name := range given {
		if _, ok := known[name]; !ok && name != "project" {
			return toursBody{}, badRequest(name + " is not an argument of this tool")
		}
	}
	project, _ := given["project"].(string)
	if !validProject(project) {
		return toursBody{}, badRequest("project must be a Cloud project ID or project number")
	}
	out := toursBody{Project: project, Timeout: r.defaultTimeout}
	limit := r.maxTimeout
	if on, _ := given[r.largeSwitch].(bool); on && r.largeTimeout > 0 {
		limit = r.largeTimeout
	}
	body := map[string]any{}
	for _, a := range r.args() {
		value, ok := given[a.arg]
		if !ok {
			if a.required {
				return toursBody{}, badRequest(a.arg + " is required")
			}
			continue
		}
		if err := checkNode(a.n, value, a.arg); err != nil {
			return toursBody{}, err
		}
		switch a.arg {
		case "timeout":
			d, ok := parseDuration(value.(string))
			if !ok || d <= 0 || d > limit {
				return toursBody{}, badRequest("timeout must be longer than 0 and at most " + durationText(limit) + r.longer(limit))
			}
			out.Timeout = d
		case "max_validation_errors":
			if n, _ := strconv.ParseInt(string(value.(json.Number)), 10, 64); n < 0 || n > maxValidationErrorsLimit {
				return toursBody{}, badRequest("max_validation_errors must be 0 to " + strconv.Itoa(maxValidationErrorsLimit))
			}
		}
		body[a.wire] = value
	}
	if _, ok := body["timeout"]; !ok {
		body["timeout"] = durationText(r.defaultTimeout)
	}
	model, _ := given["model"].(map[string]any)
	out.Shipments = listLen(model["shipments"])
	out.Vehicles = listLen(model["vehicles"])
	for _, a := range r.extra {
		if value, ok := given[a.arg]; ok {
			if out.Extra == nil {
				out.Extra = map[string]any{}
			}
			out.Extra[a.arg] = value
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return toursBody{}, badRequest("the arguments cannot be encoded as a request")
	}
	if len(payload) > maxToursBodyBytes {
		return toursBody{}, badRequest("the request body is larger than " + strconv.Itoa(maxToursBodyBytes>>20) +
			" MiB; send fewer shipments or vehicles, or drop the polylines of injected routes")
	}
	out.Payload = payload
	return out, nil
}

// longer is the hint of a refused timeout on how to get a longer one.
func (r toursRequest) longer(limit time.Duration) string {
	switch {
	case r.largeTimeout == 0:
		return "; a longer optimization needs the long-running tool"
	case limit < r.largeTimeout:
		return "; up to " + durationText(r.largeTimeout) + " needs " + r.largeSwitch
	}
	return ""
}

func listLen(v any) int {
	list, _ := v.([]any)
	return len(list)
}

func durationText(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + "s"
}

var (
	durationForm  = regexp.MustCompile(durationPattern)
	timestampForm = regexp.MustCompile(timestampPattern)
	int64Form     = regexp.MustCompile(int64Pattern)
)

// parseDuration reads a protobuf duration. ok is false for a duration that does not fit a time.Duration, which
// is far beyond any bound of this package.
func parseDuration(s string) (time.Duration, bool) {
	if !durationForm.MatchString(s) {
		return 0, false
	}
	text := strings.TrimSuffix(s, "s")
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	whole, frac, _ := strings.Cut(text, ".")
	seconds, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || seconds > math.MaxInt64/int64(time.Second)-1 {
		return 0, false
	}
	nanos, _ := strconv.ParseInt((frac + "000000000")[:9], 10, 64)
	d := time.Duration(seconds)*time.Second + time.Duration(nanos)
	if negative {
		d = -d
	}
	return d, true
}

// checkNode checks value against the node. The messages name the path and the rule, never the value.
func checkNode(n *node, value any, path string) error {
	wrong := func(want string) error { return badRequest(path + " must be " + want) }
	switch n.kind {
	case kindObject:
		object, ok := value.(map[string]any)
		if !ok {
			return wrong("an object")
		}
		for name, member := range object {
			child := n.member(name)
			if child == nil {
				return badRequest(path + "." + name + " is not a field of " + n.name)
			}
			if err := checkNode(child, member, path+"."+name); err != nil {
				return err
			}
		}
	case kindArray:
		list, ok := value.([]any)
		if !ok {
			return wrong("a list")
		}
		for i, item := range list {
			if err := checkNode(n.elem, item, path+"["+strconv.Itoa(i)+"]"); err != nil {
				return err
			}
		}
	case kindMap:
		object, ok := value.(map[string]any)
		if !ok {
			return wrong("an object")
		}
		if len(object) > maxMapEntries {
			return badRequest(path + " must have at most " + strconv.Itoa(maxMapEntries) + " entries")
		}
		for name, member := range object {
			if err := checkNode(n.elem, member, path+"."+name); err != nil {
				return err
			}
		}
	case kindText, kindLongText:
		text, ok := value.(string)
		limit := maxTextRunes
		if n.kind == kindLongText {
			limit = maxPolylineRunes
		}
		if !ok || !utf8.ValidString(text) {
			return wrong("text")
		}
		if utf8.RuneCountInString(text) > limit {
			return badRequest(path + " must be at most " + strconv.Itoa(limit) + " characters")
		}
	case kindBool:
		if _, ok := value.(bool); !ok {
			return wrong("true or false")
		}
	case kindDouble:
		number, ok := value.(json.Number)
		if !ok {
			return wrong("a number")
		}
		if f, err := strconv.ParseFloat(string(number), 64); err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return wrong("a finite number")
		}
	case kindInt32:
		number, ok := value.(json.Number)
		if !ok {
			return wrong("an integer")
		}
		if i, err := strconv.ParseInt(string(number), 10, 64); err != nil || i < int32Min || i > int32Max {
			return wrong("a 32-bit integer")
		}
	case kindInt64:
		switch v := value.(type) {
		case string:
			if !int64Form.MatchString(v) {
				return wrong("a 64-bit integer, as a number or a decimal string")
			}
			if _, err := strconv.ParseInt(v, 10, 64); err != nil {
				return wrong("a 64-bit integer, as a number or a decimal string")
			}
		case json.Number:
			if _, err := strconv.ParseInt(string(v), 10, 64); err != nil {
				return wrong("a 64-bit integer, as a number or a decimal string")
			}
		default:
			return wrong("a 64-bit integer, as a number or a decimal string")
		}
	case kindDuration:
		text, ok := value.(string)
		if !ok || !durationForm.MatchString(text) {
			return wrong("a duration in seconds with an s, such as 3600s or 0.5s")
		}
	case kindTimestamp:
		text, ok := value.(string)
		if !ok || !timestampForm.MatchString(text) {
			return wrong("an RFC 3339 timestamp such as 2026-11-11T08:00:00Z")
		}
		if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
			return wrong("an RFC 3339 timestamp such as 2026-11-11T08:00:00Z")
		}
	case kindEnum:
		text, ok := value.(string)
		if !ok {
			return wrong("one of " + strings.Join(n.values, ", "))
		}
		for _, allowed := range n.values {
			if text == allowed {
				return nil
			}
		}
		return badRequest(path + " must be one of " + strings.Join(n.values, ", "))
	}
	return nil
}
