package httpbind

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awsquery"
)

func testMulti() MultiPager {
	return MultiPager{Truncated: "~More", SizeParam: "max", MaxSize: 100, Positions: []Position{
		{In: "KeyMarker", Out: "~NextKey", Required: true}, {In: "VersionMarker", Out: "~NextVersion"}}}
}

func TestMultiPagerCheck(t *testing.T) {
	if err := testMulti().Check(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*MultiPager){
		"one position": func(p *MultiPager) { p.Positions = p.Positions[:1] },
		"five positions": func(p *MultiPager) {
			p.Positions = append(p.Positions, Position{In: "A", Out: "a"}, Position{In: "B", Out: "b"}, Position{In: "C", Out: "c"})
		},
		"no truncation member": func(p *MultiPager) { p.Truncated = "" },
		"first optional":       func(p *MultiPager) { p.Positions[0].Required = false },
		"repeated name":        func(p *MultiPager) { p.Positions[1].In = "KeyMarker" },
		"size without max":     func(p *MultiPager) { p.MaxSize = 0 },
	} {
		p := testMulti()
		p.Positions = append([]Position(nil), p.Positions...)
		mutate(&p)
		if err := p.Check(); !errors.Is(err, ErrShape) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestMultiPagerCursorIsBoundAndValidated(t *testing.T) {
	p := testMulti()
	binding := awsquery.Binding("tool", map[string]any{"b": "x"})
	page, err := p.Start(binding, 10, "")
	if err != nil || page.Params()["max"] != "10" || len(page.Params()) != 1 {
		t.Fatalf("first page = %v, %v", page.Params(), err)
	}
	next, failure := page.Next("op", map[string]any{"~More": true, "~NextKey": "k é", "~NextVersion": "v"})
	if failure != nil || next == "" {
		t.Fatalf("next = %q, %v", next, failure)
	}
	again, err := p.Start(binding, 0, next)
	if err != nil || again.Params()["KeyMarker"] != "k é" || again.Params()["VersionMarker"] != "v" {
		t.Fatalf("second page = %v, %v", again.Params(), err)
	}
	// Anything but the cursor of this list and these arguments is refused, with the same message.
	var wrong []string
	wrong = append(wrong, next+"x", next[:len(next)-2], "!!", strings.Repeat("A", 5000))
	forged := provider.EncodeCursor(binding, `{"KeyMarker":"a","Evil":"b"}`)
	wrong = append(wrong, forged,
		provider.EncodeCursor(binding, `{"VersionMarker":"v"}`), provider.EncodeCursor(binding, `{}`),
		provider.EncodeCursor(binding, `not json`), provider.EncodeCursor(binding, `{"KeyMarker":""}`),
		provider.EncodeCursor(binding, `{"KeyMarker":"`+strings.Repeat("x", 1025)+`"}`),
		provider.EncodeCursor(awsquery.Binding("tool", map[string]any{"b": "y"}), `{"KeyMarker":"a"}`))
	for _, c := range wrong {
		_, err := p.Start(binding, 0, c)
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("cursor %.30q: err = %v", c, err)
		}
	}
	for _, size := range []int{-1, 101} {
		if _, err := p.Start(binding, size, ""); err == nil {
			t.Errorf("page size %d accepted", size)
		}
	}
	noSize := p
	noSize.SizeParam, noSize.MaxSize = "", 0
	if _, err := noSize.Start(binding, 5, ""); err == nil {
		t.Error("a page size without a parameter was accepted")
	}
}

func TestMultiPagerNext(t *testing.T) {
	p := testMulti()
	page, _ := p.Start(nil, 0, "")
	for name, c := range map[string]struct {
		out  map[string]any
		more bool
		bad  bool
	}{
		"end":                    {map[string]any{"~More": false, "~NextKey": "k"}, false, false},
		"end without the member": {map[string]any{}, false, false},
		"more":                   {map[string]any{"~More": true, "~NextKey": "k"}, true, false},
		"empty optional":         {map[string]any{"~More": true, "~NextKey": "k", "~NextVersion": ""}, true, false},
		"flag of the wrong type": {map[string]any{"~More": "true", "~NextKey": "k"}, false, true},
		"no key":                 {map[string]any{"~More": true}, false, true},
		"empty key":              {map[string]any{"~More": true, "~NextKey": ""}, false, true},
		"key not text":           {map[string]any{"~More": true, "~NextKey": int64(1)}, false, true},
		"version not text":       {map[string]any{"~More": true, "~NextKey": "k", "~NextVersion": true}, false, true},
		"key too long":           {map[string]any{"~More": true, "~NextKey": strings.Repeat("k", 1025)}, false, true},
	} {
		next, failure := page.Next("op", c.out)
		if (failure != nil) != c.bad || (next != "") != c.more {
			t.Errorf("%s: next = %q, failure = %v", name, next, failure)
		}
	}
	// Two keys that together would not fit a cursor are an error, never a cut value.
	huge := map[string]any{"~More": true, "~NextKey": strings.Repeat("é", 512), "~NextVersion": strings.Repeat("é", 512)}
	if next, failure := page.Next("op", huge); failure == nil && len(next) > awsquery.MaxCursorLength {
		t.Errorf("a cursor of %d bytes", len(next))
	}
}

// ApplyPage sets the members of a multi-member page like those of a single token.
func TestApplyPageWithAMultiPage(t *testing.T) {
	in := &Shape{Kind: KindStruct, Fields: []Field{
		{Key: "~KeyMarker", Name: "KeyMarker", Shape: &Shape{Kind: KindString}, Loc: LocQuery},
		{Key: "~VersionMarker", Name: "VersionMarker", Shape: &Shape{Kind: KindString}, Loc: LocQuery},
		{Key: "~max", Name: "max", Shape: &Shape{Kind: KindInteger}, Loc: LocQuery}}}
	p := testMulti()
	binding := awsquery.Binding("tool", nil)
	first, _ := p.Start(binding, 0, "")
	next, _ := first.Next("op", map[string]any{"~More": true, "~NextKey": "k", "~NextVersion": "v"})
	page, err := p.Start(binding, 7, next)
	if err != nil {
		t.Fatal(err)
	}
	args, err := ApplyPage(in, map[string]any{}, page)
	if err != nil || args["~KeyMarker"] != "k" || args["~VersionMarker"] != "v" || args["~max"].(interface{ String() string }).String() != "7" {
		t.Errorf("args = %v, %v", args, err)
	}
	if _, err := ApplyPage(&Shape{Kind: KindStruct}, nil, page); !errors.Is(err, ErrShape) {
		t.Errorf("an input without the members: %v", err)
	}
}

func TestStringPrefixesAreRemovedFromArguments(t *testing.T) {
	s := &Shape{Kind: KindString, Prefixes: []string{"/hostedzone/"}}
	for in, want := range map[string]string{"Z1": "Z1", "/hostedzone/Z1": "Z1"} {
		if got, ok := Text(s, in, FormatDefault); !ok || got != want {
			t.Errorf("Text(%q) = %q, %t", in, got, ok)
		}
	}
	for _, in := range []string{"/hostedzone/a/b", "a/b", "/hostedzone//x", "/other/Z1"} {
		if _, ok := Text(s, in, FormatDefault); ok {
			t.Errorf("Text(%q) accepted", in)
		}
	}
	// A shape without prefixes is unchanged, and an answer is never changed.
	if got, ok := Text(&Shape{Kind: KindString}, "/hostedzone/a/b", FormatDefault); !ok || got != "/hostedzone/a/b" {
		t.Errorf("plain string = %q, %t", got, ok)
	}
	if v, _, err := ParseText(s, "/hostedzone/Z1", FormatDefault); err != nil || v != "/hostedzone/Z1" {
		t.Errorf("answer = %v, %v", v, err)
	}
}

// A request cannot set a header the protocol owns, but an answer may be read from one.
func TestReservedHeadersAreReadable(t *testing.T) {
	str := &Shape{Kind: KindString}
	in := &Operation{Method: "PUT", URI: "/x", Input: &Shape{Kind: KindStruct, Fields: []Field{{Key: "t", Name: "Content-Type", Shape: str, Loc: LocHeader}}}}
	if err := in.Validate(); !errors.Is(err, ErrShape) {
		t.Errorf("a reserved request header: %v", err)
	}
	out := &Operation{Method: "GET", URI: "/x", Output: &Shape{Kind: KindStruct, Fields: []Field{
		{Key: "t", Name: "Content-Type", Shape: str, Loc: LocHeader}, {Key: "n", Name: "Content-Length", Shape: &Shape{Kind: KindInteger}, Loc: LocHeader}}}}
	if err := out.Validate(); err != nil {
		t.Fatal(err)
	}
	got, _, err := out.Decode(nil, 200, http.Header{"Content-Type": {"text/plain"}, "Content-Length": {"12"}}, nil)
	if err != nil || got["t"] != "text/plain" || got["n"] != int64(12) {
		t.Errorf("answer = %v, %v", got, err)
	}
	if !ReservedHeader("Content-Length") || ReservedHeader("x-amz-meta-a") {
		t.Error("ReservedHeader is wrong")
	}
}

func TestDocumentBody(t *testing.T) {
	str := &Shape{Kind: KindString}
	for name, c := range map[string]struct {
		out  *Shape
		want bool
	}{
		"none":           {nil, false},
		"headers only":   {&Shape{Kind: KindStruct, Fields: []Field{{Key: "h", Name: "X", Shape: str, Loc: LocHeader}}}, false},
		"body members":   {&Shape{Kind: KindStruct, Fields: []Field{{Key: "a", Name: "A", Shape: str}}}, true},
		"struct payload": {&Shape{Kind: KindStruct, Fields: []Field{{Key: "p", Name: "P", Shape: &Shape{Kind: KindStruct}, Loc: LocPayload}}}, true},
		"blob payload":   {&Shape{Kind: KindStruct, Fields: []Field{{Key: "p", Name: "P", Shape: &Shape{Kind: KindBlob}, Loc: LocPayload}}}, false},
		"string payload": {&Shape{Kind: KindStruct, Fields: []Field{{Key: "p", Name: "P", Shape: str, Loc: LocPayload}}}, false},
	} {
		if got := (&Operation{Method: "GET", URI: "/x", Output: c.out}).DocumentBody(); got != c.want {
			t.Errorf("%s: DocumentBody = %t", name, got)
		}
	}
}
