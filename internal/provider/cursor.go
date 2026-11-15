package provider

import "github.com/castrowithcee/qatlas-kit/httpx"

// CursorBinding binds a cursor to the target and the normalized arguments of the request that produced it.
// Parts that do not change the sequence of a list, such as the page size, stay out, so a continuation stays
// exact whatever batch size the next request asks for.
func CursorBinding(parts ...any) []byte { return httpx.CursorBinding(parts...) }

// EncodeCursor wraps the cursor a provider reported into the opaque cursor handed to the caller. Its prefix
// is a checksum over the binding and the provider part, so both are covered.
func EncodeCursor(binding []byte, inner string) string { return httpx.EncodeCursor(binding, inner) }

// DecodeCursor returns the provider cursor a bound cursor continues after. A cursor that is malformed,
// altered, cut short, longer than max, or issued for another binding is refused with ok false.
func DecodeCursor(binding []byte, cursor string, max int) (inner string, ok bool) {
	return httpx.DecodeCursor(binding, cursor, max)
}
