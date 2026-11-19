package aws

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// The confirmation text of the Route 53 record change, one entry per change separated by "; ". The core bounds it to its default of 500 runes, so the
// text keeps its own budget below that and reserves the room for the count of changes it leaves out: the cut of
// the core must never take the remainder note.
const (
	r53SummaryBudget     = 450
	r53SummaryRemainder  = 50
	r53SummaryNameRunes  = 50
	r53SummaryValueRunes = 30
	r53SummaryValues     = 2
	r53SummaryZoneRunes  = 44
	r53SummaryAliasRunes = 40
	r53SummaryAliasZone  = 20
)

func init() {
	registerSummary("aws.route53.record-set.change", recordSetChangeSummary)
}

// summaryClip bounds an untrusted value to n runes.
func summaryClip(v string, n int) string {
	v = summaryValue(v)
	if r := []rune(v); len(r) > n {
		return string(r[:n]) + "…"
	}
	return v
}

type r53SummaryChange struct {
	Action string `json:"action"`
	Record struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		TTL     *int64 `json:"ttl"`
		Records []struct {
			Value string `json:"value"`
		} `json:"resource_records"`
		Alias *struct {
			HostedZoneID string `json:"hosted_zone_id"`
			DNSName      string `json:"dns_name"`
		} `json:"alias_target"`
	} `json:"resource_record_set"`
}

// rank orders the changes: delegation first, then removals, then replacements, then the rest.
func (c r53SummaryChange) rank() int {
	switch {
	case c.Record.Type == "NS" || c.Record.Type == "SOA":
		return 0
	case c.Action == "DELETE":
		return 1
	case c.Action == "UPSERT":
		return 2
	}
	return 3
}

// The apex of the zone is not known here, so every NS and SOA change is marked, not only the one at the apex.
// DELETE and UPSERT replace the plain action word, so a line names its action once.
func (c r53SummaryChange) flags() string {
	var out []string
	switch c.Record.Type {
	case "NS":
		out = append(out, "[NS: delegation if at the apex]")
	case "SOA":
		out = append(out, "[SOA: reachability of the zone]")
	}
	switch c.Action {
	case "DELETE":
		out = append(out, "[DELETE removes]")
	case "UPSERT":
		out = append(out, "[UPSERT replaces]")
	default:
		out = append(out, summaryClip(c.Action, 8))
	}
	return strings.Join(out, " ")
}

func (c r53SummaryChange) line() string {
	r := c.Record
	out := c.flags() + " " + summaryClip(r.Name, r53SummaryNameRunes) + " " +
		summaryClip(r.Type, 8)
	if r.TTL != nil {
		out += " ttl " + strconv.FormatInt(*r.TTL, 10)
	}
	if a := r.Alias; a != nil {
		return out + " alias to zone " + summaryClip(a.HostedZoneID, r53SummaryAliasZone) + " " +
			summaryClip(a.DNSName, r53SummaryAliasRunes) + " (target not checked for account)"
	}
	values := make([]string, 0, r53SummaryValues)
	for _, v := range r.Records[:min(len(r.Records), r53SummaryValues)] {
		values = append(values, summaryClip(v.Value, r53SummaryValueRunes))
	}
	out += " " + strings.Join(values, ", ")
	if more := len(r.Records) - len(values); more > 0 {
		out += " +" + strconv.Itoa(more) + " more values"
	}
	return out
}

func recordSetChangeSummary(raw json.RawMessage) string {
	var a struct {
		Zone  string `json:"hosted_zone_id"`
		Batch struct {
			Changes []r53SummaryChange `json:"changes"`
		} `json:"change_batch"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	changes := slices.Clone(a.Batch.Changes)
	slices.SortStableFunc(changes, func(x, y r53SummaryChange) int { return x.rank() - y.rank() })
	out := "Change records of Route 53 zone " + summaryClip(a.Zone, r53SummaryZoneRunes) + ", one atomic batch of " +
		strconv.Itoa(len(changes)) + " changes"
	used := len([]rune(out))
	listed := 0
	for i, c := range changes {
		line := "; " + c.line()
		if i == 0 {
			line = ": " + c.line()
		}
		n := len([]rune(line))
		// Only the last change may use the room of the remainder note, since no note follows it.
		if limit := r53SummaryBudget - r53SummaryRemainder; used+n > limit && (i < len(changes)-1 || used+n > r53SummaryBudget) {
			break
		}
		out += line
		used += n
		listed++
	}
	if rest := len(changes) - listed; rest > 0 {
		out += "; " + strconv.Itoa(rest) + " more changes not listed, check change_batch"
	}
	return out
}
