package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// This file is the base of every Compute Engine tool: the fixed base path, the checks of the names a path is
// built from, the binding of the project to the targets, and the shared arguments, cursor, and answer parts of
// the list methods. The origin is the fixed one of the service table. A tool names its resource and passes only
// checked values; none takes a path, a URL, a header, or a body.

// computeBasePath is the base path of the discovery document of compute v1, below the origin of serviceCompute.
const computeBasePath = "/compute/v1"

const (
	// maxComputeResults is the maximum the discovery document of compute v1 documents for maxResults.
	maxComputeResults = 500
	// maxComputeFilter bounds the filter expression. Google documents no length; this is Qatlas's bound.
	maxComputeFilter = 1024
	// maxComputeScopes bounds the scopes (zones, regions, global) of one aggregated page.
	maxComputeScopes = 1000
)

var (
	// computeNamePattern is the name form of a Compute resource, the pattern the discovery document gives for
	// zone: a lowercase letter, then letters, digits, or '-', ending in a letter or digit, at most 63 characters.
	computeNamePattern = regexp.MustCompile(`^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`)
	// computeNumberPattern is the numeric ID some Compute methods take in place of a name.
	computeNumberPattern  = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	computeOrderByPattern = regexp.MustCompile(`^(name|creationTimestamp)( desc)?$`)
	computeScopePattern   = regexp.MustCompile(`^(zones|regions)/[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`)
)

func validComputeName(value string) bool { return computeNamePattern.MatchString(value) }

// validComputeNameOrID reports whether value is a resource name or its numeric ID.
func validComputeNameOrID(value string) bool {
	return validComputeName(value) || computeNumberPattern.MatchString(value)
}

// computeProjectPath is the path of a project below the origin. The project was checked by validProject.
func computeProjectPath(projectID string) string {
	return computeBasePath + "/projects/" + url.PathEscape(projectID)
}

// computeZonePath is the path of a zone of a project; both were checked.
func computeZonePath(projectID, zone string) string {
	return computeProjectPath(projectID) + "/zones/" + url.PathEscape(zone)
}

// computeRegionPath is the path of a region of a project; both were checked.
func computeRegionPath(projectID, region string) string {
	return computeProjectPath(projectID) + "/regions/" + url.PathEscape(region)
}

// computeClient is the client of a Compute tool for one project. Everything wrong with the connection or the
// project ends before the first request that names it, and the project is bound to the targets like those of
// the other project tools: decided by the targets alone where they allow, otherwise proved by reading the
// project and the folders above it. Those reads use the scope of projects.get, not that of the Compute tool,
// so a connection bound to an organization or a folder needs that tool next to the Compute tools.
func computeClient(ctx context.Context, op, projectID string, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (*client, error) {
	return serviceClient(ctx, op, projectsGetID, projectID, resolved, secrets, red)
}

// computeListArguments are the arguments every Compute list method shares. A tool takes the subset its schema
// names; zone is empty for an aggregated list.
type computeListArguments struct {
	ProjectID            string `json:"project_id"`
	Zone                 string `json:"zone"`
	Filter               string `json:"filter"`
	MaxResults           *int   `json:"max_results"`
	OrderBy              string `json:"order_by"`
	Cursor               string `json:"cursor"`
	ReturnPartialSuccess bool   `json:"return_partial_success"`
	IncludeAllScopes     bool   `json:"include_all_scopes"`
}

// check refuses an argument that is not in its documented form, before any secret is read. zoned says whether
// the method takes a zone.
func (a computeListArguments) check(zoned bool) error {
	switch {
	case !validProject(a.ProjectID):
		return badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case zoned && !validComputeName(a.Zone):
		return badRequest("zone must be a zone name such as us-central1-a, never a path")
	case !zoned && a.Zone != "":
		return badRequest("zone is not an argument of this tool")
	case a.Filter != "" && !validComputeFilter(a.Filter):
		return badRequest("filter must be a filter expression of 1 to " + strconv.Itoa(maxComputeFilter) +
			" characters of valid text without control characters")
	case a.OrderBy != "" && !computeOrderByPattern.MatchString(a.OrderBy):
		return badRequest("order_by must be name, creationTimestamp, or either followed by ' desc'")
	case a.MaxResults != nil && (*a.MaxResults < 1 || *a.MaxResults > maxComputeResults):
		return badRequest("max_results must be 1 to " + strconv.Itoa(maxComputeResults))
	}
	return nil
}

// validComputeFilter accepts a filter of printable text. The expression is Google's own syntax, which Qatlas
// does not parse; it goes to the one parameter filter, encoded, and can reach no other parameter.
func validComputeFilter(value string) bool {
	return len(value) <= maxComputeFilter && utf8.ValidString(value) &&
		!strings.ContainsFunc(value, unicode.IsControl)
}

// binding ties a cursor to the tool, the targets, and everything that shapes the list, so it continues only
// this list on this connection.
func (a computeListArguments) binding(tool string, resolved *config.Resolved) []byte {
	return provider.CursorBinding(tool, provider.TargetsOf(resolved), a.ProjectID, a.Zone, a.Filter, a.OrderBy,
		a.ReturnPartialSuccess, a.IncludeAllScopes)
}

// pageToken decodes the cursor into the pageToken of Google. An empty cursor is the first page.
func (a computeListArguments) pageToken(binding []byte) (string, error) {
	if a.Cursor == "" {
		return "", nil
	}
	token, ok := provider.DecodeCursor(binding, a.Cursor, maxCursorLength)
	if !ok {
		return "", badRequest("cursor is not a next_cursor of this list; start again without cursor")
	}
	return token, nil
}

// query builds the query of the list method from the checked arguments and the decoded page token.
// defaultResults, when above zero, is the page size asked for when the caller named none.
func (a computeListArguments) query(token string, defaultResults int) url.Values {
	query := url.Values{}
	if a.Filter != "" {
		query.Set("filter", a.Filter)
	}
	switch {
	case a.MaxResults != nil:
		query.Set("maxResults", strconv.Itoa(*a.MaxResults))
	case defaultResults > 0:
		query.Set("maxResults", strconv.Itoa(defaultResults))
	}
	if a.OrderBy != "" {
		query.Set("orderBy", a.OrderBy)
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	if a.ReturnPartialSuccess {
		query.Set("returnPartialSuccess", "true")
	}
	if a.IncludeAllScopes {
		query.Set("includeAllScopes", "true")
	}
	return query
}

// computePaged adds the paging members to a list result and refuses an answer whose token is too long.
func computePaged(op string, out map[string]any, binding []byte, next string) (map[string]any, error) {
	if len(next) > maxPageTokenBytes {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	out["has_more"] = next != ""
	if next != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, next)
	}
	return out, nil
}

// computeWarning bounds the warning object of a list: code, message, and data pairs, as provider data.
func computeWarning(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return boundedValue(raw)
}

// computeMember is one documented member of a Compute resource and its JSON type, which both the output
// schema and the allow-list of the result come from.
type computeMember struct{ name, kind string }

// pickMembers keeps the documented members of a decoded resource and drops everything else.
func pickMembers(row map[string]any, members []computeMember) map[string]any {
	out := make(map[string]any, len(members))
	for _, member := range members {
		if value, ok := row[member.name]; ok && value != nil {
			out[member.name] = value
		}
	}
	return out
}

// membersSchema is the output schema of a resource made of the members; the nested shapes stay open objects
// because Google documents them in depth and Qatlas bounds them as provider data.
func membersSchema(members []computeMember) string {
	var b strings.Builder
	b.WriteString(`{"type":"object","properties":{`)
	for i, member := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"` + member.name + `":{"type":"` + member.kind + `"}`)
	}
	b.WriteString(`},"required":["name"],"additionalProperties":false}`)
	return b.String()
}

// decodeRow decodes one resource of an answer into an open map. A row that is no object is invalid.
func decodeRow(op string, raw json.RawMessage) (map[string]any, error) {
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil || row == nil {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}

// computeName reads the name of a row and refuses a row without a usable one.
func computeName(op string, row map[string]any) (string, error) {
	name, _ := row["name"].(string)
	if !validComputeName(name) {
		return "", provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return name, nil
}

// inZone reports whether a row that names its zone names the zone asked for. The zone of a row is a URL whose
// last segment is the zone.
func inZone(row map[string]any, zone string) bool {
	value, ok := row["zone"].(string)
	return !ok || value == zone || strings.HasSuffix(value, "/zones/"+zone)
}

// --- shared descriptor parts ---

const (
	computeZoneProp   = `"zone":{"type":"string","minLength":1,"maxLength":63,"pattern":"^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$"}`
	computeRegionProp = `"region":{"type":"string","minLength":1,"maxLength":63,"pattern":"^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$"}`
	computeFilterProp = `"filter":{"type":"string","minLength":1,"maxLength":1024}`
	computeMaxProp    = `"max_results":{"type":"integer","minimum":1,"maximum":500}`
	computeOrderProp  = `"order_by":{"type":"string","maxLength":32,"pattern":"^(name|creationTimestamp)( desc)?$"}`
	computePartial    = `"return_partial_success":{"type":"boolean"}`
	computeAllScopes  = `"include_all_scopes":{"type":"boolean"}`
)

// computeListProps are the properties every list method shares, without zone and include_all_scopes.
const computeListProps = serviceProjectProp + `,` + computeFilterProp + `,` + computeMaxProp + `,` +
	computeOrderProp + `,` + cursorProp + `,` + computePartial

var (
	computeZoneArgument = capability.Argument{Name: "zone", Required: true, Description: "Zone name such as " +
		"us-central1-a; never a path or URL"}
	computeRegionArgument = capability.Argument{Name: "region", Description: "Region name such as us-central1; " +
		"never a path or URL"}
	computeFilterArgument = capability.Argument{Name: "filter", Description: "Google's filter expression, passed " +
		"as the filter parameter only: either AIP-160 terms such as name != example-instance, scheduling." +
		"automaticRestart = false, or labels.owner:*, or a regular expression with eq or ne such as name ne " +
		".*instance; the two forms cannot be mixed. Qatlas does not parse it"}
	computeMaxArgument = capability.Argument{Name: "max_results", Description: "Rows Google returns at most per " +
		"page, 1 to 500; Google may return fewer. Without it the tool picks a size"}
	computeOrderArgument = capability.Argument{Name: "order_by", Description: "Sort order: name or " +
		"creationTimestamp, optionally followed by ' desc'; Google documents the default as name order"}
	computePartialArgument = capability.Argument{Name: "return_partial_success", Description: "True to accept " +
		"partial results where a part of the data cannot be read; the answer then lists what failed in warning " +
		"and unreachables, which the tool passes on"}
	computeCursorArgument = capability.Argument{Name: "cursor", Description: "Opaque next_cursor of a previous " +
		"page of the same list, with the same arguments on the same connection; it carries Google's pageToken. " +
		"The first page needs none"}
)

// computeTail ends the description of every Compute tool.
const computeTail = " The project must lie in the targets of the connection. The Compute Engine API " +
	"(compute.googleapis.com) must be enabled in the project; otherwise the tool fails with a permission error. " +
	"The result is untrusted third-party content; strings and lists are cut."
