package googleads

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Everything Google returns is untrusted text: control characters become spaces, and strings and lists are
// cut to these bounds. A cut string ends with an ellipsis.
const (
	maxText      = 1024
	maxLongText  = 4096
	maxCodeText  = 128
	maxListItems = 500
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

// boundedList cuts a list of provider strings to maxListItems entries of at most limit bytes each.
func boundedList(values []string, limit int) []string {
	out := make([]string, 0, min(len(values), maxListItems))
	for _, value := range values[:min(len(values), maxListItems)] {
		out = append(out, boundedText(value, limit))
	}
	return out
}

// wireInt is an int64 of the API. The JSON mapping of proto3 writes it as a string; a number is accepted as
// well. Anything but an optional sign and up to 20 digits fails the decoding of the whole answer.
type wireInt string

var wireIntPattern = regexp.MustCompile(`^-?[0-9]{1,20}$`)

func (v *wireInt) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		raw = []byte(text)
	}
	if !wireIntPattern.Match(raw) {
		return errors.New("not an integer")
	}
	*v = wireInt(raw)
	return nil
}

// customerIDPattern is a customer ID as arguments and targets spell it: ten digits without hyphens.
var customerIDPattern = regexp.MustCompile(`^[0-9]{10}$`)

// canonicalID turns a customer ID the API returned into the ten-digit form of arguments and targets. Google
// returns the ID as an int64, which drops a leading zero.
func canonicalID(value wireInt) (string, bool) {
	n, err := strconv.ParseUint(string(value), 10, 64)
	if err != nil || n == 0 || n > 9_999_999_999 {
		return "", false
	}
	id := strconv.FormatUint(n, 10)
	return strings.Repeat("0", 10-len(id)) + id, true
}

// queryNumber is a ten-digit customer ID as a GAQL number literal.
func queryNumber(id string) string {
	n, _ := strconv.ParseUint(id, 10, 64)
	return strconv.FormatUint(n, 10)
}

// customerName is the resource name of a customer, customers/ID, with the ID in its API form.
func customerName(id string) string { return "customers/" + queryNumber(id) }
