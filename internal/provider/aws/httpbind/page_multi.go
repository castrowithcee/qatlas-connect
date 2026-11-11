package httpbind

import (
	"encoding/json"
	"strconv"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awsquery"
)

// maxPositionValue bounds one member of a position. An S3 key is at most 1024 bytes; the wrapped cursor of a
// whole position must still fit awsquery.MaxCursorLength.
const maxPositionValue = 1024

// Position is one member of the place a list continues at: the request member that carries it and the
// output member that reports it.
type Position struct {
	// In is the wire name of the request member (a query parameter or a body member).
	In string
	// Out is the Key of the output member.
	Out string
	// Required says that an answer which reports more entries must carry the member. Optional members, such as
	// the identifier of a record set that has none, are left out of the request when the answer has none.
	Required bool
	// Start is the key of the tool argument that offers this member as the place a list starts at; empty for none.
	Start string
}

// MultiPager pages a list whose next page starts at a position of several members: S3 KeyMarker and
// VersionIdMarker, or Route 53 StartRecordName, StartRecordType and StartRecordIdentifier. The members only
// make sense together, so they travel in one cursor, bound to the tool and its arguments like the token of a
// Pager. IsTruncated (Truncated) decides the end: a full page says nothing about it.
type MultiPager struct {
	Positions []Position
	// Truncated is the Key of the boolean output member that says whether the list continues.
	Truncated string
	// SizeParam and MaxSize are as in Pager.
	SizeParam string
	MaxSize   int
}

// Check validates the description of a multi-member pager.
func (p MultiPager) Check() error {
	if len(p.Positions) < 2 || len(p.Positions) > 4 || p.Truncated == "" || !p.Positions[0].Required {
		return shapeErr("a multi-member pager needs two to four positions, the first required, and a truncation member")
	}
	seen := map[string]bool{}
	for i, pos := range p.Positions {
		if pos.In == "" || pos.Out == "" || seen[pos.In] || (pos.Start == "") != (p.Positions[0].Start == "") {
			return shapeErr("a position has no names or repeats one, or only some offer a start argument")
		}
		for _, other := range p.Positions[:i] {
			if pos.Start != "" && pos.Start == other.Start {
				return shapeErr("two positions share a start argument")
			}
		}
		seen[pos.In] = true
	}
	if (p.SizeParam == "") != (p.MaxSize == 0) || p.MaxSize < 0 {
		return shapeErr("page size and its maximum go together")
	}
	return nil
}

// MultiPage is one request of a multi-member list.
type MultiPage struct {
	pager   MultiPager
	binding []byte
	size    int
	at      map[string]string
}

// Start reads the paging arguments of a request. A cursor that is malformed, altered, issued for other
// arguments or that holds a member the pager does not know is refused with an InvalidRequestError.
func (p MultiPager) Start(binding []byte, pageSize int, cursor string) (MultiPage, error) {
	page := MultiPage{pager: p, binding: binding, size: pageSize}
	if pageSize != 0 {
		switch {
		case p.SizeParam == "":
			return MultiPage{}, &provider.InvalidRequestError{Message: "page_size is not supported by this operation"}
		case pageSize < 1 || pageSize > p.MaxSize:
			return MultiPage{}, &provider.InvalidRequestError{Message: "page_size must be between 1 and " + strconv.Itoa(p.MaxSize)}
		}
	}
	if cursor != "" {
		refuse := func() (MultiPage, error) {
			return MultiPage{}, &provider.InvalidRequestError{
				Message: "cursor is not a next_cursor of this list; start the list again without cursor"}
		}
		inner, ok := provider.DecodeCursor(binding, cursor, awsquery.MaxCursorLength)
		if !ok {
			return refuse()
		}
		var at map[string]string
		if json.Unmarshal([]byte(inner), &at) != nil || !p.validPosition(at) {
			return refuse()
		}
		page.at = at
	}
	return page, nil
}

// TakeStart removes the start arguments of a list from args and returns the position they name, by wire name, nil
// when none is given. A start argument is a string within the bounds of a position, and each needs the ones before
// it: a later member of a position means nothing without the earlier ones.
func (p MultiPager) TakeStart(args map[string]any) (map[string]string, error) {
	var start map[string]string
	missing := ""
	for _, pos := range p.Positions {
		if pos.Start == "" {
			return nil, nil
		}
		raw, set := args[pos.Start]
		delete(args, pos.Start)
		if !set {
			if missing == "" {
				missing = pos.Start
			}
			continue
		}
		text, ok := raw.(string)
		if !ok || !usablePosition(text) {
			return nil, &provider.InvalidRequestError{Message: pos.Start + " must be a non-empty text of at most " + strconv.Itoa(maxPositionValue) + " bytes"}
		}
		if missing != "" {
			return nil, &provider.InvalidRequestError{Message: pos.Start + " needs " + missing}
		}
		if start == nil {
			start = map[string]string{}
		}
		start[pos.In] = text
	}
	return start, nil
}

// StartAt is Start for a list that begins at the position start (from TakeStart) instead of at its first
// entry. It cannot be combined with a cursor, which already holds a position.
func (p MultiPager) StartAt(binding []byte, pageSize int, cursor string, start map[string]string) (MultiPage, error) {
	if len(start) > 0 && cursor != "" {
		return MultiPage{}, &provider.InvalidRequestError{Message: "cursor continues a list and cannot be combined with a start argument; leave one out"}
	}
	page, err := p.Start(binding, pageSize, cursor)
	if err == nil && len(start) > 0 {
		page.at = start
	}
	return page, err
}

// validPosition accepts a position of known members whose required ones are all there.
func (p MultiPager) validPosition(at map[string]string) bool {
	if len(at) == 0 {
		return false
	}
	known := 0
	for _, pos := range p.Positions {
		v, set := at[pos.In]
		if !set {
			if pos.Required {
				return false
			}
			continue
		}
		known++
		if !usablePosition(v) {
			return false
		}
	}
	return known == len(at)
}

func usablePosition(v string) bool {
	return v != "" && len(v) <= maxPositionValue && utf8.ValidString(v)
}

// Params returns the request members this page sets, by wire name.
func (pg MultiPage) Params() map[string]string {
	params := map[string]string{}
	if pg.size > 0 {
		params[pg.pager.SizeParam] = strconv.Itoa(pg.size)
	}
	for k, v := range pg.at {
		params[k] = v
	}
	return params
}

// Next returns the cursor of the following page, or "" at the end of the list. An answer that says it is
// truncated but lacks a required member, holds a member of the wrong type, or repeats the position it was
// asked with is invalid: ending the list there would hide entries and continuing would loop.
func (pg MultiPage) Next(op string, out map[string]any) (string, *provider.Error) {
	invalid := func() (string, *provider.Error) {
		return "", provider.InvalidResponse(op, "AWS returned an invalid response")
	}
	more, isBool := out[pg.pager.Truncated].(bool)
	if _, set := out[pg.pager.Truncated]; set && !isBool {
		return invalid()
	}
	if !more {
		return "", nil
	}
	next := map[string]string{}
	for _, pos := range pg.pager.Positions {
		raw, set := out[pos.Out]
		if !set || raw == nil {
			if pos.Required {
				return invalid()
			}
			continue
		}
		text, isString := raw.(string)
		if !isString || (text == "" && !pos.Required) {
			if !isString {
				return invalid()
			}
			continue
		}
		if !usablePosition(text) {
			return invalid()
		}
		next[pos.In] = text
	}
	if len(next) == 0 || sameSet(next, pg.at) {
		return invalid()
	}
	inner, err := json.Marshal(next)
	if err != nil {
		return invalid()
	}
	cursor := provider.EncodeCursor(pg.binding, string(inner))
	if len(cursor) > awsquery.MaxCursorLength {
		return invalid()
	}
	return cursor, nil
}

func sameSet(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
