package googlesearchconsole

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// What Google returns is third-party content. The answer types carry their bounds in the types of their
// members: a text type cuts itself while it is decoded, and a member the type does not list is skipped by
// the decoder and never held. Lists are cut after decoding and the result says so.

// Bounds of what a result shows. An account holds at most 1000 properties.
const (
	maxShortRunes = 100
	maxURLRunes   = 2048
	maxSites      = 1000
	maxSitemaps   = 500
	maxContents   = 20
)

// Text types. Each cuts to its limit and marks the cut with …; a value that is not a string is dropped.
type (
	short string // enumerations, timestamps, counts: 100 characters
	text  string // property and sitemap addresses: 2048 characters, shown and never fetched
)

func (s *short) UnmarshalJSON(b []byte) error { *s = short(clipJSON(b, maxShortRunes)); return nil }
func (s *text) UnmarshalJSON(b []byte) error  { *s = text(clipJSON(b, maxURLRunes)); return nil }

// clipJSON decodes a JSON string and cuts it to limit characters; a value that is no string is empty.
func clipJSON(b []byte, limit int) string {
	var value string
	if json.Unmarshal(b, &value) != nil {
		return ""
	}
	value = strings.ToValidUTF8(value, "")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}

// flag is a boolean that drops a value that is not one instead of failing the whole answer.
type flag struct{ set, value bool }

func (f *flag) UnmarshalJSON(b []byte) error {
	var value bool
	if json.Unmarshal(b, &value) == nil {
		*f = flag{set: true, value: value}
	}
	return nil
}

func (f flag) MarshalJSON() ([]byte, error) { return json.Marshal(f.value) }

func (f flag) IsZero() bool { return !f.set }

// The Search Console API as its discovery document documents it (revision 20261115). The deprecated
// indexed count of a sitemap content is left out.
type (
	site struct {
		SiteURL         text  `json:"siteUrl,omitempty"`
		PermissionLevel short `json:"permissionLevel,omitempty"`
	}
	sitesAnswer struct {
		SiteEntry []site `json:"siteEntry"`
	}
	sitesResult struct {
		SiteEntry []site `json:"siteEntry"`
		Truncated bool   `json:"truncated,omitempty"`
	}
	sitemap struct {
		Path            text             `json:"path,omitempty"`
		LastSubmitted   short            `json:"lastSubmitted,omitempty"`
		IsPending       flag             `json:"isPending,omitzero"`
		IsSitemapsIndex flag             `json:"isSitemapsIndex,omitzero"`
		Type            short            `json:"type,omitempty"`
		LastDownloaded  short            `json:"lastDownloaded,omitempty"`
		Warnings        short            `json:"warnings,omitempty"`
		Errors          short            `json:"errors,omitempty"`
		Contents        []sitemapContent `json:"contents,omitempty"`
	}
	sitemapContent struct {
		Type      short `json:"type,omitempty"`
		Submitted short `json:"submitted,omitempty"`
	}
	sitemapsAnswer struct {
		Sitemap []sitemap `json:"sitemap"`
	}
	sitemapsResult struct {
		Sitemap   []sitemap `json:"sitemap"`
		Truncated bool      `json:"truncated,omitempty"`
	}
)

// bound cuts the contents of a sitemap.
func (s sitemap) bound() sitemap {
	if len(s.Contents) > maxContents {
		s.Contents = s.Contents[:maxContents]
	}
	return s
}

// The output schemas, written once for the types above.
const (
	siteSchema = `{"type":"object","properties":{"siteUrl":{"type":"string"},"permissionLevel":{"type":"string"}},` +
		`"additionalProperties":false}`
	sitemapSchema = `{"type":"object","properties":{"path":{"type":"string"},"lastSubmitted":{"type":"string"},` +
		`"isPending":{"type":"boolean"},"isSitemapsIndex":{"type":"boolean"},"type":{"type":"string"},` +
		`"lastDownloaded":{"type":"string"},"warnings":{"type":"string"},"errors":{"type":"string"},` +
		`"contents":{"type":"array","items":{"type":"object","properties":{"type":{"type":"string"},` +
		`"submitted":{"type":"string"}},"additionalProperties":false}}},"additionalProperties":false}`
	sitesSchema = `{"type":"object","properties":{"siteEntry":{"type":"array","items":` + siteSchema + `},` +
		`"truncated":{"type":"boolean"}},"required":["siteEntry"],"additionalProperties":false}`
	sitemapsSchema = `{"type":"object","properties":{"sitemap":{"type":"array","items":` + sitemapSchema + `},` +
		`"truncated":{"type":"boolean"}},"required":["sitemap"],"additionalProperties":false}`
)
