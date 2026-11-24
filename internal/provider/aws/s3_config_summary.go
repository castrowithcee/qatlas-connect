package aws

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// The confirmation texts of the lifecycle and CORS puts. The core cuts at its default of 500 runes, so the lifecycle
// text keeps its own budget below that and reserves room for the count of the rules it leaves out.
const (
	s3SummaryBudget    = 450
	s3SummaryRemainder = 60
	s3SummaryIDRunes   = 40
	s3SummaryTagRunes  = 24
	s3SummaryPrefix    = 48
	s3SummaryOrigins   = 3
	s3SummaryOrigin    = 60
)

func init() {
	registerSummary("aws.s3.lifecycle.put", lifecyclePutSummary)
	registerSummary("aws.s3.cors.put", corsPutSummary)
}

type s3SummaryTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (t s3SummaryTag) text() string {
	return "tag " + summaryClip(t.Key, s3SummaryTagRunes) + "=" + summaryClip(t.Value, s3SummaryTagRunes)
}

type s3SummaryRule struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Filter *struct {
		Prefix  string        `json:"prefix"`
		Tag     *s3SummaryTag `json:"tag"`
		Greater json.Number   `json:"object_size_greater_than"`
		Less    json.Number   `json:"object_size_less_than"`
		And     *struct {
			Prefix  string         `json:"prefix"`
			Tags    []s3SummaryTag `json:"tags"`
			Greater json.Number    `json:"object_size_greater_than"`
			Less    json.Number    `json:"object_size_less_than"`
		} `json:"and"`
	} `json:"filter"`
	Expiration *struct {
		Days json.Number `json:"days"`
		Date string      `json:"date"`
	} `json:"expiration"`
	Noncurrent *struct {
		Days json.Number `json:"noncurrent_days"`
	} `json:"noncurrent_version_expiration"`
}

func (r s3SummaryRule) deletes() bool {
	return r.Expiration != nil && (r.Expiration.Days != "" || r.Expiration.Date != "") ||
		r.Noncurrent != nil && r.Noncurrent.Days != ""
}

// scope names what the filter selects, or "" when the rule covers the whole bucket.
func (r s3SummaryRule) scope() string {
	f := r.Filter
	if f == nil {
		return ""
	}
	var parts []string
	add := func(prefix string, greater, less json.Number) {
		if prefix != "" {
			parts = append(parts, "prefix "+summaryClip(prefix, s3SummaryPrefix))
		}
		if greater != "" {
			parts = append(parts, "size > "+summaryClip(greater.String(), 20))
		}
		if less != "" {
			parts = append(parts, "size < "+summaryClip(less.String(), 20))
		}
	}
	add(f.Prefix, f.Greater, f.Less)
	if f.Tag != nil {
		parts = append(parts, f.Tag.text())
	}
	if a := f.And; a != nil {
		add(a.Prefix, a.Greater, a.Less)
		if len(a.Tags) > 0 {
			tags := a.Tags[0].text()
			if len(a.Tags) > 1 {
				tags += " +" + strconv.Itoa(len(a.Tags)-1) + " more tags"
			}
			parts = append(parts, tags)
		}
	}
	return strings.Join(parts, ", ")
}

// rank puts enabled rules over the whole bucket first and disabled rules last.
func (r s3SummaryRule) rank() int {
	switch {
	case r.Status != "Enabled":
		return 2
	case r.scope() == "":
		return 0
	}
	return 1
}

func (r s3SummaryRule) line() string {
	out := "unnamed rule"
	if r.ID != "" {
		out = summaryClip(r.ID, s3SummaryIDRunes)
	}
	if r.Status != "Enabled" {
		out += " [" + summaryClip(r.Status, 10) + "]"
	}
	if scope := r.scope(); scope == "" {
		out += " [WHOLE BUCKET]"
	} else {
		out += " (" + scope + ")"
	}
	var what []string
	if e := r.Expiration; e != nil && e.Days != "" {
		what = append(what, "objects after "+summaryClip(e.Days.String(), 10)+" days")
	} else if e != nil && e.Date != "" {
		what = append(what, "objects on "+summaryClip(e.Date, 30))
	}
	if n := r.Noncurrent; n != nil && n.Days != "" {
		what = append(what, "noncurrent versions after "+summaryClip(n.Days.String(), 10)+" days")
	}
	return out + " deletes " + strings.Join(what, " and ")
}

func lifecyclePutSummary(raw json.RawMessage) string {
	var a struct {
		summaryTarget
		Config struct {
			Rules []s3SummaryRule `json:"rules"`
		} `json:"lifecycle_configuration"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Replace all lifecycle rules of " + a.bucket() + " with " + strconv.Itoa(len(a.Config.Rules)) +
		" rules; rules not given are removed"
	var deleting []s3SummaryRule
	for _, r := range a.Config.Rules {
		if r.deletes() {
			deleting = append(deleting, r)
		}
	}
	if len(deleting) == 0 {
		return out + ". No rule deletes objects."
	}
	slices.SortStableFunc(deleting, func(x, y s3SummaryRule) int { return x.rank() - y.rank() })
	out += ". " + strconv.Itoa(len(deleting)) + " rules delete data, lost unless versioning keeps it"
	used, listed := len([]rune(out)), 0
	for i, r := range deleting {
		line := "; " + r.line()
		if i == 0 {
			line = ": " + r.line()
		}
		n := len([]rune(line))
		// Only the last rule may use the room of the remainder note, since no note follows it.
		if used+n > s3SummaryBudget-s3SummaryRemainder && (i < len(deleting)-1 || used+n > s3SummaryBudget) {
			break
		}
		out += line
		used += n
		listed++
	}
	if rest := len(deleting) - listed; rest > 0 {
		out += "; " + strconv.Itoa(rest) + " more not listed, check lifecycle_configuration"
	}
	return out
}

func corsPutSummary(raw json.RawMessage) string {
	var a struct {
		summaryTarget
		Config struct {
			Rules []struct {
				Origins []string `json:"allowed_origins"`
			} `json:"cors_rules"`
		} `json:"cors_configuration"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	var origins []string
	wildcard := false
	for _, r := range a.Config.Rules {
		for _, o := range r.Origins {
			wildcard = wildcard || o == "*"
			if !slices.Contains(origins, o) {
				origins = append(origins, o)
			}
		}
	}
	out := "Replace all CORS rules of " + a.bucket() + " with " + strconv.Itoa(len(a.Config.Rules)) +
		" rules; rules not given are removed."
	if wildcard {
		out += " The origin * lets a browser on any website read the bucket's responses; CORS is no permission, the " +
			"bucket policy, ACLs, and public access block still decide who may use the bucket."
	}
	out += " Origins: "
	for i, o := range origins[:min(len(origins), s3SummaryOrigins)] {
		if i > 0 {
			out += ", "
		}
		out += summaryClip(o, s3SummaryOrigin)
	}
	if more := len(origins) - s3SummaryOrigins; more > 0 {
		out += " +" + strconv.Itoa(more) + " more"
	}
	out += "."
	return out
}
