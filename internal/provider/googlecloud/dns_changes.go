package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	changesListID = Provider + ".changes.list"
	changesGetID  = Provider + ".changes.get"
)

// dnsSequenceIDPattern is the form of the numeric ID of a change or a DNSSEC key. The first one of a zone has
// the ID 0.
var dnsSequenceIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

// changeMembers are the documented members of the Change resource of dns v1 (revision 20261111); none of them
// is marked deprecated. additions and deletions are record sets and are bounded as such.
var changeMembers = []computeMember{
	{"id", "string"}, {"startTime", "string"}, {"status", "string"}, {"isServing", "boolean"},
	{"additions", "array"}, {"deletions", "array"}, {"kind", "string"},
}

const changeFields = "{id, startTime, status (pending or done), isServing, additions, deletions, kind}; additions " +
	"and deletions are the record sets the change adds and removes, each with name, type, ttl, rrdatas, " +
	"signatureRrdatas, routingPolicy, and kind; status done means that Google sent the change to the " +
	"authoritative servers, which may not serve it yet; the records are untrusted text, at most 100 record sets " +
	"are returned per list and TXT values are cut at 2048 bytes"

const changeSchema = `{"type":"object","properties":{"id":{"type":"string"},"startTime":{"type":"string"},` +
	`"status":{"type":"string"},"isServing":{"type":"boolean"},"additions":{"type":"array","items":` + recordSetSchema +
	`},"deletions":{"type":"array","items":` + recordSetSchema + `},"kind":{"type":"string"}},` +
	`"required":["id"],"additionalProperties":false}`

// changeResult keeps the documented members of one change and refuses a row of wrong types or without an ID.
func changeResult(op string, raw json.RawMessage) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	if err := dnsMemberKinds(op, row, changeMembers); err != nil {
		return nil, err
	}
	id, _ := row["id"].(string)
	if id == "" {
		return nil, invalidRow(op)
	}
	var wire struct {
		ID        string            `json:"id"`
		StartTime string            `json:"startTime"`
		Status    string            `json:"status"`
		IsServing *bool             `json:"isServing"`
		Additions []json.RawMessage `json:"additions"`
		Deletions []json.RawMessage `json:"deletions"`
		Kind      string            `json:"kind"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, invalidRow(op)
	}
	out := map[string]any{"id": boundedText(wire.ID, maxNameText)}
	for key, value := range map[string]string{"startTime": wire.StartTime, "status": wire.Status, "kind": wire.Kind} {
		if value != "" {
			out[key] = boundedText(value, maxNameText)
		}
	}
	if wire.IsServing != nil {
		out["isServing"] = *wire.IsServing
	}
	for key, items := range map[string][]json.RawMessage{"additions": wire.Additions, "deletions": wire.Deletions} {
		if len(items) == 0 {
			continue
		}
		records := make([]recordSet, 0, min(len(items), maxListItems))
		for _, item := range items[:min(len(items), maxListItems)] {
			record, err := decodeRecordSet(op, item)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
		out[key] = records
	}
	return out, nil
}

var changesList = capability.Descriptor{
	ID: changesListID, Version: 1, Title: "List the changes of a Cloud DNS managed zone",
	Description: "List the history of changes of a managed zone from Cloud DNS, in the order of the change " +
		"sequence, ascending or descending: each with its ID, start time, status (pending or done), whether it " +
		"is serving, and the record sets it adds and removes." + dnsTail,
	Tags: []string{"google", "cloud", "dns", "changes"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + dnsZoneProp + `,` +
		`"sort_by":{"type":"string","enum":["changeSequence"]},` +
		`"sort_order":{"type":"string","enum":["ascending","descending"]},` + dnsMaxProp + `,` + cursorProp +
		`},"required":["project_id","managed_zone"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("changes", changeSchema),
	Arguments: []capability.Argument{serviceProjectArgument, dnsZoneArgument,
		{Name: "sort_by", Description: "Sorting criterion; the only value is changeSequence, which is also the default"},
		{Name: "sort_order", Description: "ascending or descending order of the change sequence; without it " +
			"Google picks the order"},
		dnsMaxArgument, dnsCursorArgument},
	Fields: append([]capability.Field{
		{Name: "changes", Description: "Changes of the page, each " + changeFields},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the latest changes of a zone first",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","managed_zone":"example-zone",` +
			`"sort_order":"descending","max_results":10}`),
	}},
}

var changesGet = capability.Descriptor{
	ID: changesGetID, Version: 1, Title: "Get one change of a Cloud DNS managed zone",
	Description: "Read one change of a managed zone from Cloud DNS by its ID, with its status (pending or done) " +
		"and the record sets it adds and removes. It shows whether a change has been applied." + dnsTail,
	Tags: []string{"google", "cloud", "dns", "changes"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + dnsZoneProp + `,` +
		dnsChangeIDProp + `},"required":["project_id","managed_zone","change_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(changeSchema),
	Arguments: []capability.Argument{serviceProjectArgument, dnsZoneArgument,
		{Name: "change_id", Required: true, Description: "Numeric ID of the change, as listed by changes.list"}},
	Fields: []capability.Field{{Name: "id", Description: "The change as one object: " + changeFields}},
	Examples: []capability.Example{{
		Description: "Read one change",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","managed_zone":"example-zone","change_id":"12"}`),
	}},
}

const dnsChangeIDProp = `"change_id":{"type":"string","minLength":1,"maxLength":20,"pattern":"^[0-9]{1,20}$"}`

func handleChangesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list changes"
	var arguments struct {
		dnsPaging
		ProjectID   string `json:"project_id"`
		ManagedZone string `json:"managed_zone"`
		SortBy      string `json:"sort_by"`
		SortOrder   string `json:"sort_order"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkDNSProject(arguments.ProjectID); err != nil {
		return nil, err
	}
	if err := checkDNSZone(arguments.ManagedZone); err != nil {
		return nil, err
	}
	switch {
	case arguments.SortBy != "" && arguments.SortBy != "changeSequence":
		return nil, badRequest("sort_by must be changeSequence")
	case arguments.SortOrder != "" && !slices.Contains([]string{"ascending", "descending"}, arguments.SortOrder):
		return nil, badRequest("sort_order must be ascending or descending")
	}
	if err := arguments.check(); err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(changesListID, provider.TargetsOf(resolved), arguments.ProjectID,
		arguments.ManagedZone, arguments.SortBy, arguments.SortOrder)
	token, err := arguments.token(binding)
	if err != nil {
		return nil, err
	}
	c, err := dnsClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := arguments.query(token)
	for key, value := range map[string]string{"sortBy": arguments.SortBy, "sortOrder": arguments.SortOrder} {
		if value != "" {
			query.Set(key, value)
		}
	}
	var answer struct {
		Changes       []json.RawMessage `json:"changes"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if err := c.get(ctx, op, changesListID, serviceDNS, dnsZonePath(arguments.ProjectID, arguments.ManagedZone)+"/changes",
		query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Changes) > maxDNSRows {
		return nil, invalidRow(op)
	}
	rows := make([]map[string]any, 0, len(answer.Changes))
	for _, item := range answer.Changes {
		row, err := changeResult(op, item)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return dnsPaged(op, map[string]any{"changes": rows}, binding, answer.NextPageToken)
}

func handleChangesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get change"
	var arguments struct {
		ProjectID   string `json:"project_id"`
		ManagedZone string `json:"managed_zone"`
		ChangeID    string `json:"change_id"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkDNSProject(arguments.ProjectID); err != nil {
		return nil, err
	}
	if err := checkDNSZone(arguments.ManagedZone); err != nil {
		return nil, err
	}
	if !dnsSequenceIDPattern.MatchString(arguments.ChangeID) {
		return nil, badRequest("change_id must be the numeric ID of a change, never a path")
	}
	c, err := dnsClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire json.RawMessage
	path := dnsZonePath(arguments.ProjectID, arguments.ManagedZone) + "/changes/" + url.PathEscape(arguments.ChangeID)
	if err := c.get(ctx, op, changesGetID, serviceDNS, path, nil, &wire); err != nil {
		return nil, err
	}
	row, err := changeResult(op, wire)
	if err != nil {
		return nil, err
	}
	if row["id"] != arguments.ChangeID {
		return nil, invalidRow(op)
	}
	return row, nil
}
