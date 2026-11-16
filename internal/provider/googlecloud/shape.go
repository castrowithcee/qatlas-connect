package googlecloud

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Everything Google returns is untrusted text: control characters become spaces, and strings, maps and lists
// are cut to the bounds below. A cut string ends with an ellipsis.
const (
	maxText      = 1024
	maxNameText  = 256
	maxLabels    = 100
	maxLabelText = 128
	maxListItems = 100
	maxNodes     = 2000
	maxDepth     = 6
)

// boundedText replaces control characters and cuts the text to at most limit bytes on a rune boundary.
func boundedText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return ' '
		}
		return r
	}, value)
	if len(value) <= limit {
		return value
	}
	cut := limit - len("…")
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return strings.TrimRight(value[:cut], " ") + "…"
}

// boundedMap keeps at most maxLabels entries, the first by key order, with bounded keys and values.
func boundedMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, min(len(keys), maxLabels))
	for _, key := range keys[:min(len(keys), maxLabels)] {
		out[boundedText(key, maxLabelText)] = boundedText(in[key], maxLabelText)
	}
	return out
}

func boundedList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(in), maxListItems))
	for _, item := range in[:min(len(in), maxListItems)] {
		out = append(out, boundedText(item, maxNameText))
	}
	return out
}

// boundedValue decodes provider data of an open shape, such as the metadata and the response of an
// operation, and bounds it: text is cut, objects and lists are cut, nesting stops at maxDepth, and the whole
// value holds at most maxNodes values. What exceeds a bound is dropped.
func boundedValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	budget := maxNodes
	return boundNode(value, 0, &budget)
}

func boundNode(value any, depth int, budget *int) any {
	if *budget <= 0 {
		return nil
	}
	*budget--
	switch value := value.(type) {
	case string:
		return boundedText(value, maxText)
	case map[string]any:
		if depth >= maxDepth {
			return nil
		}
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := map[string]any{}
		for _, key := range keys[:min(len(keys), maxListItems)] {
			if node := boundNode(value[key], depth+1, budget); node != nil {
				out[boundedText(key, maxLabelText)] = node
			}
		}
		return out
	case []any:
		if depth >= maxDepth {
			return nil
		}
		out := make([]any, 0, min(len(value), maxListItems))
		for _, item := range value[:min(len(value), maxListItems)] {
			if node := boundNode(item, depth+1, budget); node != nil {
				out = append(out, node)
			}
		}
		return out
	}
	return value
}
