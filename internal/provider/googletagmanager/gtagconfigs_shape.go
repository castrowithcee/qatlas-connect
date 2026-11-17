package googletagmanager

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// gtagConfigBody holds the members of a Google tag configuration a caller can set.
type gtagConfigBody struct {
	Type      string      `json:"type,omitempty"`
	Parameter []parameter `json:"parameter,omitempty"`
}

// gtagConfigEntity is a GtagConfig of the API: the members of gtagConfigBody and those Google assigns.
type gtagConfigEntity struct {
	Path          idText  `json:"path,omitempty"`
	AccountID     idText  `json:"accountId,omitempty"`
	ContainerID   idText  `json:"containerId,omitempty"`
	WorkspaceID   idText  `json:"workspaceId,omitempty"`
	GtagConfigID  idText  `json:"gtagConfigId,omitempty"`
	Fingerprint   idText  `json:"fingerprint,omitempty"`
	TagManagerURL urlText `json:"tagManagerUrl,omitempty"`
	gtagConfigBody
}

type (
	gtagConfigsAnswer struct {
		GtagConfig    []gtagConfigEntity `json:"gtagConfig"`
		NextPageToken string             `json:"nextPageToken"`
	}
	gtagConfigsResult struct {
		GtagConfigs []gtagConfigEntity `json:"gtagConfigs"`
		NextCursor  string             `json:"next_cursor,omitempty"`
	}
	deleteGtagConfigResult struct {
		GtagConfig string `json:"gtag_config"`
		Deleted    bool   `json:"deleted"`
	}
)

// checkInput refuses a configuration the schema would refuse as well, for a handler reached without the schema.
func (b gtagConfigBody) checkInput() error {
	if err := checkTrees(b.Parameter); err != nil {
		return err
	}
	if b.Type == "" {
		return invalidArgument("type is required")
	}
	return nil
}

// checkAnswer refuses an answer that holds more parameter entries than a request may carry back.
func (c gtagConfigEntity) checkAnswer(op string) error {
	if countNodes(c.Parameter) > maxParamNodes {
		return provider.InvalidResponse(op, subject+" returned a Google tag configuration with more than "+
			strconv.Itoa(maxParamNodes)+" parameter entries")
	}
	return nil
}

// sameParameters reports whether two parameter lists hold the same entries in the same order. An absent list
// and an empty one are the same.
func sameParameters(a, b []parameter) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	return reflect.DeepEqual(normalized(a), normalized(b))
}

// normalized gives every empty nested list the nil value, so that DeepEqual compares meaning.
func normalized(in []parameter) []parameter {
	if len(in) == 0 {
		return nil
	}
	out := make([]parameter, len(in))
	for i, p := range in {
		p.List, p.Map = normalized(p.List), normalized(p.Map)
		out[i] = p
	}
	return out
}

// targetIDs collects the parameters that name a destination: type template and a key that ends in Id or ID.
// Each is identified by the keys on the way to it; an unkeyed entry counts as its position and a repeated key
// as its occurrence, so the same parameter has the same name in the old and the new list.
func targetIDs(trees []parameter) (names []string, values map[string]string) {
	values = map[string]string{}
	var walk func(level []parameter, prefix string)
	walk = func(level []parameter, prefix string) {
		seen := map[string]int{}
		for i, p := range level {
			segment := "[" + strconv.Itoa(i) + "]"
			if p.Key != "" {
				segment = p.Key
				if n := seen[p.Key]; n > 0 {
					segment += "#" + strconv.Itoa(n)
				}
				seen[p.Key]++
			}
			name := prefix + segment
			if p.Type == "template" && (strings.HasSuffix(p.Key, "Id") || strings.HasSuffix(p.Key, "ID")) {
				names = append(names, name)
				values[name] = p.Value
			}
			walk(p.List, name+"/")
			walk(p.Map, name+"/")
		}
	}
	walk(trees, "")
	return names, values
}

// targetIDChanges phrases how the target IDs differ between the previous and the new parameters.
func targetIDChanges(previous, current []parameter) []string {
	oldNames, oldValues := targetIDs(previous)
	newNames, newValues := targetIDs(current)
	var out []string
	for _, name := range newNames {
		old, had := oldValues[name]
		if had && old == newValues[name] {
			continue
		}
		out = append(out, name+": "+quoted(old, had)+" -> "+quoted(newValues[name], true))
	}
	for _, name := range oldNames {
		if _, has := newValues[name]; !has {
			out = append(out, name+": "+quoted(oldValues[name], true)+" -> (none)")
		}
	}
	return out
}

func quoted(value string, present bool) string {
	if !present {
		return "(none)"
	}
	return strconv.Quote(value)
}

// targetIDList phrases the target IDs of parameters that are set without a previous state.
func targetIDList(current []parameter) []string {
	names, values := targetIDs(current)
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = name + ": " + strconv.Quote(values[name])
	}
	return out
}

// previousParameters reads the previous_parameter argument of an update; absent or null is not given.
func previousParameters(raw json.RawMessage) ([]parameter, bool, error) {
	var in struct {
		Previous *[]parameter `json:"previous_parameter"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, false, provider.Fail("update Google tag configuration", "the validated arguments could not be read")
	}
	if in.Previous == nil {
		return nil, false, nil
	}
	return *in.Previous, true, nil
}

func gtagConfigMembers(input bool) []string {
	typ := stringSchema
	if input {
		typ = `{"type":"string","minLength":1}`
	}
	return []string{member("type", typ),
		member("parameter", `{"type":"array","items":`+parameterSchema(maxParamDepth, input)+`}`)}
}

var gtagConfigSchema = objectSchema(append([]string{member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("workspaceId", stringSchema), member("gtagConfigId", stringSchema),
	member("fingerprint", stringSchema), member("tagManagerUrl", stringSchema)}, gtagConfigMembers(false)...)...)
