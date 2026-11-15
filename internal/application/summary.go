package application

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxSummaryRunes bounds the confirmation summary of a tool, counted in runes after neutralizing.
const maxSummaryRunes = 500

// confirmationSummary returns the text the tool derives from the validated arguments, or "" without one. It
// runs before any secret is read or handler runs. A panic or an empty answer yields "", so the refusal stays
// what it is without a summary and the confirmation is never bypassed. The text is redacted before it is cut.
func (c *Core) confirmationSummary(id string, arguments json.RawMessage) (summary string) {
	defer func() {
		if recover() != nil {
			summary = ""
		}
	}()
	fn := c.registry.ConfirmationSummary(id)
	if fn == nil {
		return ""
	}
	text := fn(append(json.RawMessage(nil), arguments...))
	if c.redactor != nil {
		text = c.redactor.Apply(text)
	}
	return boundSummary(neutralize(text))
}

// neutralize makes text safe to print: invalid UTF-8 becomes U+FFFD, and control, format (bidi controls,
// zero-width characters), and line or paragraph separator runes, ANSI escape included, become visible escapes.
func neutralize(text string) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(text, "�") {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp):
			if r > 0xFFFF {
				fmt.Fprintf(&b, `\U%08x`, r)
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// boundSummary trims text and cuts it at a rune boundary, marking a cut with an ellipsis.
func boundSummary(text string) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= maxSummaryRunes {
		return text
	}
	return string([]rune(text)[:maxSummaryRunes-1]) + "…"
}
