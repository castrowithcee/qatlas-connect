package main

import (
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// The parsers read the public field reference of the Google Ads API. They know the layout of the pages only
// as far as the catalog needs it and fail on a page that does not have it, so a layout change stops the
// generator instead of producing a thinner catalog.

// fromClauseMarker is the text of the comment that the reference puts behind a segment or metric that stays
// usable when the resource is in the FROM clause. The page filter hides every entry without it.
const fromClauseMarker = "Yes, it is"

// listEntry is one name of a segment or metric table of a resource page.
type listEntry struct {
	name string
	// fromOK: the entry can be selected when the page's resource is in the FROM clause.
	fromOK bool
}

// typePackage is the package prefix of a type URL, such as google.ads.googleads.v26.enums.
var typePackage = regexp.MustCompile(`^(?:[a-z_]+\.)+v\d+\.(?:enums\.|common\.|resources\.)?`)

// fieldDetail is what a field table of a page says that the machine-readable index does not.
type fieldDetail struct {
	dataType string
	// typeName is the enum or message type behind the field, without the package prefix; empty if none.
	typeName string
	enum     []string
}

// pageDoc is the parsed content of one resource, segment or metric page.
type pageDoc struct {
	attributed []string
	segments   []listEntry
	metrics    []listEntry
	details    map[string]fieldDetail
}

func parseHTML(r io.Reader) (*html.Node, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	return doc, nil
}

func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func isElem(n *html.Node, tag string) bool { return n.Type == html.ElementNode && n.Data == tag }

// walk calls f for every node below n in document order; f returns false to skip the children of a node.
func walk(n *html.Node, f func(*html.Node) bool) {
	if !f(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, f)
	}
}

// text is the visible text below n with white space, including the no-break space, collapsed.
func text(n *html.Node) string {
	var b strings.Builder
	walk(n, func(n *html.Node) bool {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		return true
	})
	return strings.Join(strings.Fields(strings.ReplaceAll(b.String(), " ", " ")), " ")
}

func findAll(n *html.Node, match func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	walk(n, func(n *html.Node) bool {
		if n.Type == html.ElementNode && match(n) {
			out = append(out, n)
		}
		return true
	})
	return out
}

// parseOverview returns the resource names of the overview page: the links below the two resource groups of
// the navigation, "Resources with metrics" and "Resources without metrics". The first group also links the
// metrics page, which is no resource.
func parseOverview(r io.Reader) ([]string, error) {
	doc, err := parseHTML(r)
	if err != nil {
		return nil, err
	}
	groups := map[string]bool{"Resources with metrics": false, "Resources without metrics": false}
	var names []string
	seen := map[string]bool{}
	for _, title := range findAll(doc, func(n *html.Node) bool { return hasClass(n, "devsite-nav-title-no-path") }) {
		label := text(title)
		if _, ok := groups[label]; !ok {
			continue
		}
		groups[label] = true
		section := nextElement(title.Parent, "ul")
		if section == nil {
			section = nextElement(title, "ul")
		}
		if section == nil {
			return nil, fmt.Errorf("overview: group %q has no list", label)
		}
		for _, a := range findAll(section, func(n *html.Node) bool { return isElem(n, "a") }) {
			name := path.Base(strings.TrimSuffix(attr(a, "href"), "/"))
			if name == "" || name == "." || name == "metrics" || name == "segments" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	for label, found := range groups {
		if !found {
			return nil, fmt.Errorf("overview: group %q not found", label)
		}
	}
	return names, nil
}

// nextElement is the first element of the given tag among the following siblings of n.
func nextElement(n *html.Node, tag string) *html.Node {
	for s := n.NextSibling; s != nil; s = s.NextSibling {
		if isElem(s, tag) {
			return s
		}
	}
	return nil
}

// parsePage reads a resource page, or the segments or metrics page, which hold the same field tables.
func parsePage(r io.Reader) (*pageDoc, error) {
	doc, err := parseHTML(r)
	if err != nil {
		return nil, err
	}
	p := &pageDoc{details: map[string]fieldDetail{}}
	for _, t := range findAll(doc, func(n *html.Node) bool { return isElem(n, "table") && hasClass(n, "columns") }) {
		head := text(firstElem(t, "thead"))
		switch {
		case strings.HasSuffix(head, "Attributed resources"):
			for _, a := range tableLinks(t) {
				p.attributed = append(p.attributed, path.Base(attr(a, "href")))
			}
		case strings.HasSuffix(head, "Segments"):
			p.segments = listEntries(t)
		case strings.HasSuffix(head, "Metrics"):
			p.metrics = listEntries(t)
		}
	}
	for _, t := range findAll(doc, func(n *html.Node) bool { return isElem(n, "table") && hasClass(n, "api-fields-table") }) {
		h2 := firstElem(t, "h2")
		if attr(h2, "id") == "" || nearestTable(h2) != t {
			continue // the wrapper tables of the page
		}
		name := attr(h2, "id")
		d, err := parseDetail(t)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", name, err)
		}
		if _, dup := p.details[name]; dup {
			return nil, fmt.Errorf("field %s appears twice", name)
		}
		p.details[name] = d
	}
	return p, nil
}

func nearestTable(n *html.Node) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if isElem(p, "table") {
			return p
		}
	}
	return nil
}

func firstElem(n *html.Node, tag string) *html.Node {
	if n == nil {
		return &html.Node{}
	}
	if found := findAll(n, func(n *html.Node) bool { return isElem(n, tag) }); len(found) > 0 {
		return found[0]
	}
	return &html.Node{}
}

func tableLinks(t *html.Node) []*html.Node {
	var out []*html.Node
	for _, body := range findAll(t, func(n *html.Node) bool { return isElem(n, "tbody") }) {
		out = append(out, findAll(body, func(n *html.Node) bool { return isElem(n, "a") })...)
	}
	return out
}

func listEntries(t *html.Node) []listEntry {
	var out []listEntry
	for _, a := range tableLinks(t) {
		e := listEntry{name: strings.TrimPrefix(attr(a, "href"), "#")}
		for c := a.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.CommentNode && strings.TrimSpace(c.Data) == fromClauseMarker {
				e.fromOK = true
			}
		}
		out = append(out, e)
	}
	return out
}

// parseDetail reads the Data Type and Type URL rows of one field table. The enum values are the entries
// below the ENUM marker.
func parseDetail(t *html.Node) (fieldDetail, error) {
	var d fieldDetail
	for _, row := range findAll(t, func(n *html.Node) bool { return isElem(n, "tr") }) {
		cells := directCells(row)
		if len(cells) != 2 {
			continue
		}
		switch text(cells[0]) {
		case "Data Type":
			codes := findAll(cells[1], func(n *html.Node) bool { return isElem(n, "code") })
			if len(codes) == 0 {
				return d, fmt.Errorf("data type without a value")
			}
			d.dataType = text(codes[0])
			for _, c := range codes[1:] {
				d.enum = append(d.enum, text(c))
			}
		case "Type URL":
			full := text(cells[1])
			d.typeName = typePackage.ReplaceAllString(full, "")
		}
	}
	if d.dataType == "" {
		return d, fmt.Errorf("no data type")
	}
	return d, nil
}

func directCells(tr *html.Node) []*html.Node {
	var out []*html.Node
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if isElem(c, "td") || isElem(c, "th") {
			out = append(out, c)
		}
	}
	return out
}
