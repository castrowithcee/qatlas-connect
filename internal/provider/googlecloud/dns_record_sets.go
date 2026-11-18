package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	recordSetsListID = Provider + ".record-sets.list"
	recordSetsGetID  = Provider + ".record-sets.get"
)

// The routing policy types follow ResourceRecordSet and RRSetRoutingPolicy of dns v1 (revision 20261111). A
// member of another type than the documented one makes the answer invalid; undocumented members are dropped.
// The policy nests deeper than the generic bound of shape.go allows, so it is bounded member by member.

type dnsLoadBalancer struct {
	Project          string `json:"project,omitempty"`
	IPProtocol       string `json:"ipProtocol,omitempty"`
	IPAddress        string `json:"ipAddress,omitempty"`
	Port             string `json:"port,omitempty"`
	Kind             string `json:"kind,omitempty"`
	LoadBalancerType string `json:"loadBalancerType,omitempty"`
	NetworkURL       string `json:"networkUrl,omitempty"`
	Region           string `json:"region,omitempty"`
}

type dnsHealthTargets struct {
	InternalLoadBalancers []dnsLoadBalancer `json:"internalLoadBalancers,omitempty"`
	ExternalEndpoints     []string          `json:"externalEndpoints,omitempty"`
}

type dnsWeightedItem struct {
	Weight               *float64          `json:"weight,omitempty"`
	Kind                 string            `json:"kind,omitempty"`
	Rrdatas              []string          `json:"rrdatas,omitempty"`
	SignatureRrdatas     []string          `json:"signatureRrdatas,omitempty"`
	HealthCheckedTargets *dnsHealthTargets `json:"healthCheckedTargets,omitempty"`
}

type dnsGeoItem struct {
	Location             string            `json:"location,omitempty"`
	Kind                 string            `json:"kind,omitempty"`
	Rrdatas              []string          `json:"rrdatas,omitempty"`
	SignatureRrdatas     []string          `json:"signatureRrdatas,omitempty"`
	HealthCheckedTargets *dnsHealthTargets `json:"healthCheckedTargets,omitempty"`
}

type dnsGeoPolicy struct {
	Items         []dnsGeoItem `json:"items,omitempty"`
	EnableFencing *bool        `json:"enableFencing,omitempty"`
	Kind          string       `json:"kind,omitempty"`
}

type dnsWeightedPolicy struct {
	Items []dnsWeightedItem `json:"items,omitempty"`
	Kind  string            `json:"kind,omitempty"`
}

type dnsPrimaryBackup struct {
	PrimaryTargets   *dnsHealthTargets `json:"primaryTargets,omitempty"`
	BackupGeoTargets *dnsGeoPolicy     `json:"backupGeoTargets,omitempty"`
	TrickleTraffic   *float64          `json:"trickleTraffic,omitempty"`
	Kind             string            `json:"kind,omitempty"`
}

type dnsRoutingPolicy struct {
	Wrr           *dnsWeightedPolicy `json:"wrr,omitempty"`
	Geo           *dnsGeoPolicy      `json:"geo,omitempty"`
	PrimaryBackup *dnsPrimaryBackup  `json:"primaryBackup,omitempty"`
	HealthCheck   string             `json:"healthCheck,omitempty"`
	Kind          string             `json:"kind,omitempty"`
}

// recordSet is the ResourceRecordSet of dns v1. It is both the decoded answer and, after bounded, the result.
type recordSet struct {
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	TTL              *int32            `json:"ttl,omitempty"`
	Rrdatas          []string          `json:"rrdatas,omitempty"`
	SignatureRrdatas []string          `json:"signatureRrdatas,omitempty"`
	RoutingPolicy    *dnsRoutingPolicy `json:"routingPolicy,omitempty"`
	Kind             string            `json:"kind,omitempty"`
}

// cutList keeps the first maxListItems items, each bounded by f.
func cutList[T any](in []T, f func(T) T) []T {
	if len(in) == 0 {
		return nil
	}
	out := make([]T, 0, min(len(in), maxListItems))
	for _, item := range in[:min(len(in), maxListItems)] {
		out = append(out, f(item))
	}
	return out
}

func name256(value string) string { return boundedText(value, maxNameText) }

func (t *dnsHealthTargets) bounded() *dnsHealthTargets {
	if t == nil {
		return nil
	}
	return &dnsHealthTargets{
		InternalLoadBalancers: cutList(t.InternalLoadBalancers, func(l dnsLoadBalancer) dnsLoadBalancer {
			return dnsLoadBalancer{Project: name256(l.Project), IPProtocol: name256(l.IPProtocol),
				IPAddress: name256(l.IPAddress), Port: name256(l.Port), Kind: name256(l.Kind),
				LoadBalancerType: name256(l.LoadBalancerType), NetworkURL: boundedText(l.NetworkURL, maxText),
				Region: name256(l.Region)}
		}),
		ExternalEndpoints: cutList(t.ExternalEndpoints, name256),
	}
}

func (g *dnsGeoPolicy) bounded() *dnsGeoPolicy {
	if g == nil {
		return nil
	}
	return &dnsGeoPolicy{EnableFencing: g.EnableFencing, Kind: name256(g.Kind),
		Items: cutList(g.Items, func(i dnsGeoItem) dnsGeoItem {
			return dnsGeoItem{Location: name256(i.Location), Kind: name256(i.Kind), Rrdatas: dnsTexts(i.Rrdatas),
				SignatureRrdatas: dnsTexts(i.SignatureRrdatas), HealthCheckedTargets: i.HealthCheckedTargets.bounded()}
		})}
}

func (p *dnsRoutingPolicy) bounded() *dnsRoutingPolicy {
	if p == nil {
		return nil
	}
	out := &dnsRoutingPolicy{HealthCheck: boundedText(p.HealthCheck, maxText), Kind: name256(p.Kind),
		Geo: p.Geo.bounded()}
	if p.Wrr != nil {
		out.Wrr = &dnsWeightedPolicy{Kind: name256(p.Wrr.Kind),
			Items: cutList(p.Wrr.Items, func(i dnsWeightedItem) dnsWeightedItem {
				return dnsWeightedItem{Weight: i.Weight, Kind: name256(i.Kind), Rrdatas: dnsTexts(i.Rrdatas),
					SignatureRrdatas: dnsTexts(i.SignatureRrdatas), HealthCheckedTargets: i.HealthCheckedTargets.bounded()}
			})}
	}
	if b := p.PrimaryBackup; b != nil {
		out.PrimaryBackup = &dnsPrimaryBackup{PrimaryTargets: b.PrimaryTargets.bounded(),
			BackupGeoTargets: b.BackupGeoTargets.bounded(), TrickleTraffic: b.TrickleTraffic, Kind: name256(b.Kind)}
	}
	return out
}

func (r recordSet) bounded() recordSet {
	return recordSet{Name: boundedText(r.Name, maxDNSName*2), Type: name256(r.Type), TTL: r.TTL,
		Rrdatas: dnsTexts(r.Rrdatas), SignatureRrdatas: dnsTexts(r.SignatureRrdatas),
		RoutingPolicy: r.RoutingPolicy.bounded(), Kind: name256(r.Kind)}
}

// decodeRecordSet reads one record set of an answer; a row without name and type is invalid.
func decodeRecordSet(op string, raw json.RawMessage) (recordSet, error) {
	var wire recordSet
	if err := json.Unmarshal(raw, &wire); err != nil || wire.Name == "" || wire.Type == "" {
		return recordSet{}, invalidRow(op)
	}
	return wire.bounded(), nil
}

const recordSetFields = "{name, type, ttl, rrdatas, signatureRrdatas, routingPolicy, kind}; rrdatas holds the " +
	"data of the record (an address, a CNAME target, a TXT value) and routingPolicy a weighted (wrr), geographic " +
	"(geo), or failover (primaryBackup) policy with its own rrdatas and health-checked targets; a record has " +
	"either rrdatas or a routingPolicy; every value is untrusted text, TXT values are cut at 2048 bytes and at " +
	"most 100 values are returned per list"

const recordSetSchema = `{"type":"object","properties":{"name":{"type":"string"},"type":{"type":"string"},` +
	`"ttl":{"type":"integer"},"rrdatas":{"type":"array","items":{"type":"string"}},` +
	`"signatureRrdatas":{"type":"array","items":{"type":"string"}},"routingPolicy":{"type":"object"},` +
	`"kind":{"type":"string"}},"required":["name","type"],"additionalProperties":false}`

var (
	recordNameArgument = capability.Argument{Name: "name", Required: true, Description: "Fully qualified record " +
		"name with its trailing dot, such as www.example.com.; a first label * is allowed. Never a path or URL"}
	recordTypeArgument = capability.Argument{Name: "type", Required: true, Description: "Record type in capital " +
		"letters such as A, AAAA, CNAME, MX, or TXT"}
)

var recordSetsList = capability.Descriptor{
	ID: recordSetsListID, Version: 1, Title: "List the records of a Cloud DNS managed zone",
	Description: "List the record sets of a managed zone from Cloud DNS, optionally only those of one name, of one " +
		"name and type, or matching Google's filter expression: each with its name, type, TTL, data, signature " +
		"data, and routing policy (weighted, geographic, or failover). Records cannot be created, changed, or " +
		"deleted." + dnsTail,
	Tags: []string{"google", "cloud", "dns", "record-sets"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + dnsZoneProp + `,` +
		dnsNameProp + `,` + dnsTypeProp + `,"filter":{"type":"string","minLength":1,"maxLength":1024},` + dnsMaxProp +
		`,` + cursorProp + `},"required":["project_id","managed_zone"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("record_sets", recordSetSchema),
	Arguments: []capability.Argument{serviceProjectArgument, dnsZoneArgument,
		{Name: "name", Description: "Return only the records of this fully qualified name, with its trailing dot, " +
			"such as www.example.com.; not together with filter"},
		{Name: "type", Description: "Return only the records of this type such as A or TXT; needs name and is not " +
			"allowed together with filter"},
		{Name: "filter", Description: "Google's filter expression of AIP-160, passed as the filter parameter only: " +
			"a name written as name=\"example.com.\", optionally with AND type=\"A\", or name=has_suffix(\"example.com.\") " +
			"for a domain and its subdomains; not together with name or type. Qatlas does not parse it"},
		dnsMaxArgument, dnsCursorArgument},
	Fields: append([]capability.Field{
		{Name: "record_sets", Description: "Record sets of the page, each " + recordSetFields},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the TXT records of one name",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","managed_zone":"example-zone",` +
			`"name":"example.com.","type":"TXT"}`),
	}},
}

var recordSetsGet = capability.Descriptor{
	ID: recordSetsGetID, Version: 1, Title: "Get one record set of a Cloud DNS managed zone",
	Description: "Read one record set of a managed zone from Cloud DNS, by its fully qualified name and its type, " +
		"with the documented fields of the record." + dnsTail,
	Tags: []string{"google", "cloud", "dns", "record-sets"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + dnsZoneProp + `,` +
		dnsNameProp + `,` + dnsTypeProp + `},"required":["project_id","managed_zone","name","type"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(recordSetSchema),
	Arguments:    []capability.Argument{serviceProjectArgument, dnsZoneArgument, recordNameArgument, recordTypeArgument},
	Fields:       []capability.Field{{Name: "name", Description: "The record set as one object: " + recordSetFields}},
	Examples: []capability.Example{{
		Description: "Read the A record of www",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","managed_zone":"example-zone",` +
			`"name":"www.example.com.","type":"A"}`),
	}},
}

func handleRecordSetsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list record sets"
	var arguments struct {
		dnsPaging
		ProjectID   string `json:"project_id"`
		ManagedZone string `json:"managed_zone"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		Filter      string `json:"filter"`
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
	case arguments.Name != "" && !validDNSName(arguments.Name, true):
		return nil, badRequest("name must be a fully qualified domain name with its trailing dot such as www.example.com.")
	case arguments.Type != "" && !validDNSType(arguments.Type):
		return nil, badRequest("type must be a record type in capital letters such as A or TXT")
	case arguments.Type != "" && arguments.Name == "":
		return nil, badRequest("type needs name")
	case arguments.Filter != "" && (arguments.Name != "" || arguments.Type != ""):
		return nil, badRequest("filter cannot be combined with name or type")
	case arguments.Filter != "" && !validDNSFilter(arguments.Filter):
		return nil, badRequest("filter must be a filter expression of 1 to 1024 characters of valid text without control characters")
	}
	if err := arguments.check(); err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(recordSetsListID, provider.TargetsOf(resolved), arguments.ProjectID,
		arguments.ManagedZone, arguments.Name, arguments.Type, arguments.Filter)
	token, err := arguments.token(binding)
	if err != nil {
		return nil, err
	}
	c, err := dnsClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := arguments.query(token)
	for key, value := range map[string]string{"name": arguments.Name, "type": arguments.Type, "filter": arguments.Filter} {
		if value != "" {
			query.Set(key, value)
		}
	}
	var answer struct {
		Rrsets        []json.RawMessage `json:"rrsets"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if err := c.get(ctx, op, recordSetsListID, serviceDNS, dnsZonePath(arguments.ProjectID, arguments.ManagedZone)+"/rrsets",
		query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Rrsets) > maxDNSRows {
		return nil, invalidRow(op)
	}
	rows := make([]recordSet, 0, len(answer.Rrsets))
	for _, item := range answer.Rrsets {
		row, err := decodeRecordSet(op, item)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return dnsPaged(op, map[string]any{"record_sets": rows}, binding, answer.NextPageToken)
}

func handleRecordSetsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get record set"
	var arguments struct {
		ProjectID   string `json:"project_id"`
		ManagedZone string `json:"managed_zone"`
		Name        string `json:"name"`
		Type        string `json:"type"`
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
	case !validDNSName(arguments.Name, true):
		return nil, badRequest("name must be a fully qualified domain name with its trailing dot such as www.example.com.")
	case !validDNSType(arguments.Type):
		return nil, badRequest("type must be a record type in capital letters such as A or TXT")
	}
	c, err := dnsClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire json.RawMessage
	path := dnsZonePath(arguments.ProjectID, arguments.ManagedZone) + "/rrsets/" + url.PathEscape(arguments.Name) + "/" +
		url.PathEscape(arguments.Type)
	if err := c.get(ctx, op, recordSetsGetID, serviceDNS, path, nil, &wire); err != nil {
		return nil, err
	}
	row, err := decodeRecordSet(op, wire)
	if err != nil {
		return nil, err
	}
	if row.Type != arguments.Type {
		return nil, invalidRow(op)
	}
	return row, nil
}
