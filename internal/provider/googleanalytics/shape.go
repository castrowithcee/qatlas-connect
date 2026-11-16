package googleanalytics

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// What Google returns is third-party content. The answer types carry their bounds in the types of their
// members: a text cuts itself while it is decoded, a code or a resource name that is not in its documented
// form is dropped, and a member the type does not list is skipped by the decoder and never held.

// Bounds of what a result shows. A display name has at most 100 UTF-16 code units at Google.
const (
	maxTextRunes = 300
	maxCodeBytes = 64
)

type (
	// text is a name a person chose, cut to maxTextRunes characters.
	text string
	// code is an enumeration value or a timestamp, kept unchanged when it is printable ASCII of at most
	// maxCodeBytes without blanks or quotes, and dropped otherwise.
	code string
	// resourceName is accounts/ID, properties/ID or accountSummaries/ID; any other value is dropped.
	resourceName string
)

func (t *text) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		*t = text(clip(value, maxTextRunes))
	}
	return nil
}

func (c *code) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) != nil || len(value) > maxCodeBytes {
		return nil
	}
	for i := 0; i < len(value); i++ {
		if value[i] <= ' ' || value[i] > '~' || value[i] == '"' || value[i] == '\\' {
			return nil
		}
	}
	*c = code(value)
	return nil
}

func (r *resourceName) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) != nil {
		return nil
	}
	for _, prefix := range []string{accountPrefix, propertyPrefix, summaryPrefix} {
		if isName(prefix, value) {
			*r = resourceName(value)
		}
	}
	return nil
}

// clip cuts a string to at most limit characters and marks the cut.
func clip(value string, limit int) string {
	value = strings.ToValidUTF8(value, "")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}
