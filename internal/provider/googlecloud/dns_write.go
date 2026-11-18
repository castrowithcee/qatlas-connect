package googlecloud

import (
	"context"
	"encoding/json"
	"math"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// This file is the base of the Cloud DNS tools that change records: the checks of a record set that Qatlas
// makes before any request, the protection of the zone apex, the read of the zone, and the one request that is
// never repeated. The tools take structured record sets and never a body, a path, or a URL. The project and the
// zone come from the arguments alone and are bound to the targets before a secret is read; nothing Google
// answers decides a target.

const (
	changesCreateID    = Provider + ".changes.create"
	recordSetsCreateID = Provider + ".record-sets.create"
	recordSetsPatchID  = Provider + ".record-sets.patch"
	recordSetsDeleteID = Provider + ".record-sets.delete"
)

// dnsRecordTypes are the record types of Cloud DNS ("Supported DNS record types",
// https://docs.cloud.google.com/dns/docs/records-overview), ALIAS (a preview type) included. SOA is one of
// them, but no tool writes it: Google maintains it.
var dnsRecordTypes = []string{"A", "AAAA", "ALIAS", "CAA", "CNAME", "DNSKEY", "DS", "HTTPS", "IPSECKEY", "MX",
	"NAPTR", "NS", "PTR", "SOA", "SPF", "SRV", "SSHFP", "SVCB", "TLSA", "TXT"}

var (
	// dnsOperationIDPattern is the form Qatlas allows for clientOperationId, which Google documents only as an
	// identifier that is unique among the operations.
	dnsOperationIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	dnsCAAPattern         = regexp.MustCompile(`^[0-9]{1,3} [A-Za-z0-9]{1,15} "[^"]*"$`)
	dnsNumberPattern      = regexp.MustCompile(`^[0-9]{1,5}$`)
)

// dnsWriteRisk is the risk of the four changing tools. Every call needs the confirmation of a person.
func dnsWriteRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true, DataSensitivity: dataSensitivity}
}

// dnsWriteNote ends the description of every changing DNS tool.
const dnsWriteNote = " Records decide where a name resolves and where mail for it goes: Qatlas opens no network " +
	"access, and what a record makes reachable is the responsibility of the person who confirms. The tool never " +
	"changes the SOA record or the NS records of the zone apex, because they can move the delegation of the zone " +
	"to foreign name servers; NS records below the apex, which delegate a subzone, are allowed. Private and public " +
	"zones are treated alike, and the result names the visibility of the zone. The zone is read first to find its " +
	"apex. Needs the confirmation of a person for each call. The project must lie in the targets of the " +
	"connection, and the project, zone, names, types, and values are checked before any request. The request is " +
	"sent once and never repeated by the tool: after an unclear outcome, read the records and the changes of the " +
	"zone, or repeat the call with the same client_operation_id. Google applies a change to its name servers " +
	"after the call; changes.get reads whether it is done. Cloud DNS (dns.googleapis.com) must be enabled in the " +
	"project. The result is untrusted third-party content, and strings and lists are cut."

const (
	dnsTTLProp       = `"ttl":{"type":"integer","minimum":0,"maximum":2147483647}`
	dnsRrdatasProp   = `"rrdatas":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"string","minLength":1,"maxLength":2048}}`
	dnsOperationProp = `"client_operation_id":{"type":"string","minLength":1,"maxLength":64,"pattern":"^[A-Za-z0-9._-]{1,64}$"}`
	dnsShortText     = `{"type":"string","minLength":1,"maxLength":1024}`
	dnsHealthSchema  = `{"type":"object","properties":{"internalLoadBalancers":{"type":"array","maxItems":100,"items":` +
		`{"type":"object","properties":{"ipAddress":` + dnsShortText + `,"ipProtocol":{"type":"string","enum":["undefined","tcp","udp"]},` +
		`"loadBalancerType":{"type":"string","enum":["none","globalL7ilb","regionalL4ilb","regionalL7ilb"]},` +
		`"networkUrl":` + dnsShortText + `,"port":` + dnsShortText + `,"project":` + dnsShortText + `,"region":` + dnsShortText +
		`},"additionalProperties":false}},"externalEndpoints":{"type":"array","maxItems":100,"items":` + dnsShortText +
		`}},"additionalProperties":false}`
	dnsGeoPolicySchema = `{"type":"object","properties":{"items":{"type":"array","minItems":1,"maxItems":100,"items":` +
		`{"type":"object","properties":{"location":{"type":"string","minLength":1,"maxLength":64},` + dnsRrdatasProp +
		`,"healthCheckedTargets":` + dnsHealthSchema + `},"required":["location"],"additionalProperties":false}},` +
		`"enableFencing":{"type":"boolean"}},"required":["items"],"additionalProperties":false}`
	dnsRoutingPolicyProp = `"routingPolicy":{"type":"object","minProperties":1,"properties":{` +
		`"wrr":{"type":"object","properties":{"items":{"type":"array","minItems":1,"maxItems":100,"items":` +
		`{"type":"object","properties":{"weight":{"type":"number","minimum":0},` + dnsRrdatasProp +
		`,"healthCheckedTargets":` + dnsHealthSchema + `},"required":["weight"],"additionalProperties":false}}},` +
		`"required":["items"],"additionalProperties":false},` +
		`"geo":` + dnsGeoPolicySchema + `,` +
		`"primaryBackup":{"type":"object","properties":{"primaryTargets":` + dnsHealthSchema + `,` +
		`"backupGeoTargets":` + dnsGeoPolicySchema + `,"trickleTraffic":{"type":"number","minimum":0,"maximum":1}},` +
		`"required":["primaryTargets","backupGeoTargets"],"additionalProperties":false},` +
		`"healthCheck":` + dnsShortText + `},"additionalProperties":false}`
	// dnsRecordProps are the members of a record set Qatlas lets a caller write.
	dnsRecordProps   = dnsNameProp + `,` + dnsTypeProp + `,` + dnsTTLProp + `,` + dnsRrdatasProp + `,` + dnsRoutingPolicyProp
	dnsExpectedProps = `"expected_ttl":{"type":"integer","minimum":0,"maximum":2147483647},` +
		`"expected_rrdatas":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"string","minLength":1,"maxLength":2048}}`
	dnsRecordInput = `{"type":"object","properties":{` + dnsRecordProps + `},"required":["name","type"],` +
		`"additionalProperties":false}`
)

var (
	dnsTTLArgument = capability.Argument{Name: "ttl", Description: "Seconds resolvers may cache the record, 0 to " +
		"2147483647; needed together with rrdatas"}
	dnsRrdatasArgument = capability.Argument{Name: "rrdatas", Description: "Data of the record, 1 to 100 values in " +
		"the form of RFC 1035 for the type: an IPv4 address for A, an IPv6 address for AAAA, a fully qualified name " +
		"with its trailing dot for ALIAS, CNAME, NS, and PTR, \"priority name.\" for MX, quoted strings of at most " +
		"255 characters for TXT and SPF (or one string of at most 255 characters without spaces and quotes), \"priority weight port name.\" for SRV, and \"flags tag \\\"value\\\"\" for CAA; a " +
		"CNAME has exactly one value. Not together with routingPolicy"}
	dnsRoutingPolicyArgument = capability.Argument{Name: "routingPolicy", Description: "Dynamic answers instead " +
		"of rrdatas: exactly one of wrr (items with weight, rrdatas, healthCheckedTargets), geo (items with " +
		"location, rrdatas, healthCheckedTargets, and enableFencing), or primaryBackup (primaryTargets, " +
		"backupGeoTargets, trickleTraffic 0 to 1), and optionally healthCheck, the URL of a Compute health check; " +
		"the rrdatas of its items follow the type"}
	dnsExpectedTTLArgument = capability.Argument{Name: "expected_ttl", Description: "TTL the record set has now. " +
		"Named in the confirmation; the tool reads the record set after the confirmation and changes nothing if " +
		"the TTL differs"}
	dnsExpectedRrdatasArgument = capability.Argument{Name: "expected_rrdatas", Description: "Values the record set " +
		"has now, compared as a set (order and repeats do not matter). Named in the confirmation; the tool reads " +
		"the record set after the confirmation and changes nothing if the values differ. For an exact binding to " +
		"the deleted record set use changes.create with deletions, where Google requires an exact match"}
	dnsOperationArgument = capability.Argument{Name: "client_operation_id", Description: "Identifier of this " +
		"change, 1 to 64 letters, digits, dot, underscore, or dash, unique among the operations of the zone; " +
		"repeating the call with the same value after an unclear outcome lets Google recognise it"}
)

// recordInput is a record set as a caller writes it.
type recordInput struct {
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	TTL           *int64            `json:"ttl"`
	Rrdatas       []string          `json:"rrdatas"`
	RoutingPolicy *dnsRoutingPolicy `json:"routingPolicy"`
}

// wire is the record set sent to Google: only what the caller wrote.
func (r recordInput) wire() recordSet {
	out := recordSet{Name: r.Name, Type: r.Type, Rrdatas: r.Rrdatas, RoutingPolicy: r.RoutingPolicy}
	if r.TTL != nil {
		ttl := int32(*r.TTL)
		out.TTL = &ttl
	}
	return out
}

func plainText(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

func validHostOrRoot(value string) bool { return value == "." || validDNSName(value, false) }

func numberUpTo(value string, limit int) bool {
	n, err := strconv.Atoi(value)
	return dnsNumberPattern.MatchString(value) && err == nil && n <= limit
}

// validQuotedStrings accepts one or more double-quoted strings of at most 255 characters, separated by single
// spaces, the form of a TXT or SPF value.
func validQuotedStrings(value string) bool {
	rest := value
	for {
		if !strings.HasPrefix(rest, `"`) {
			return false
		}
		i, n := 1, 0
		for ; i < len(rest) && rest[i] != '"'; i++ {
			if rest[i] == '\\' {
				if i++; i >= len(rest) {
					return false
				}
			}
			n++
		}
		if i >= len(rest) || n > 255 {
			return false
		}
		if rest = rest[i+1:]; rest == "" {
			return true
		}
		if rest[0] != ' ' {
			return false
		}
		rest = rest[1:]
	}
}

// validBareString accepts one unquoted string of at most 255 characters without white space and quotes, which
// Google takes as one string. An unquoted value with spaces is refused because Google would split it.
func validBareString(value string) bool {
	return len(value) <= 255 && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r == '"' })
}

// validRecordData reports whether one value of rrdatas has the form of its record type and a bounded length.
// Types without a check here are held to bounded printable text; Google checks their syntax.
func validRecordData(typ, value string) bool {
	if !plainText(value, maxDNSValueText) {
		return false
	}
	switch typ {
	case "A":
		a, err := netip.ParseAddr(value)
		return err == nil && a.Is4()
	case "AAAA":
		a, err := netip.ParseAddr(value)
		return err == nil && a.Is6() && a.Zone() == ""
	case "ALIAS", "CNAME", "NS", "PTR":
		return validDNSName(value, false)
	case "MX":
		parts := strings.Split(value, " ")
		return len(parts) == 2 && numberUpTo(parts[0], 65535) && validHostOrRoot(parts[1])
	case "SRV":
		parts := strings.Split(value, " ")
		return len(parts) == 4 && numberUpTo(parts[0], 65535) && numberUpTo(parts[1], 65535) &&
			numberUpTo(parts[2], 65535) && validHostOrRoot(parts[3])
	case "CAA":
		return dnsCAAPattern.MatchString(value)
	case "TXT", "SPF":
		return validQuotedStrings(value) || validBareString(value)
	}
	return true
}

func checkRrdatas(typ string, values []string) error {
	if len(values) < 1 || len(values) > maxDNSValues {
		return badRequest("rrdatas takes 1 to " + strconv.Itoa(maxDNSValues) + " values")
	}
	if typ == "CNAME" && len(values) != 1 {
		return badRequest("a CNAME record has exactly one value in rrdatas")
	}
	for _, value := range values {
		if !validRecordData(typ, value) {
			return badRequest("a value of rrdatas is not valid for the record type " + typ + "; see the rrdatas " +
				"argument for the form of each type")
		}
	}
	return nil
}

func checkHealthTargets(typ string, t *dnsHealthTargets) error {
	if t == nil {
		return nil
	}
	if len(t.InternalLoadBalancers) > maxListItems || len(t.ExternalEndpoints) > maxListItems {
		return badRequest("healthCheckedTargets holds at most " + strconv.Itoa(maxListItems) + " entries per list")
	}
	for _, lb := range t.InternalLoadBalancers {
		for _, text := range []string{lb.Project, lb.IPProtocol, lb.IPAddress, lb.Port, lb.LoadBalancerType,
			lb.NetworkURL, lb.Region} {
			if text != "" && !plainText(text, maxText) {
				return badRequest("a load balancer target of healthCheckedTargets holds invalid text")
			}
		}
		if lb.Kind != "" {
			return badRequest("kind is set by Google and cannot be written")
		}
	}
	for _, endpoint := range t.ExternalEndpoints {
		if !validRecordData(typ, endpoint) {
			return badRequest("an external endpoint of healthCheckedTargets is not valid for the record type " + typ)
		}
	}
	return nil
}

func checkGeoPolicy(typ string, g *dnsGeoPolicy) error {
	if g == nil || len(g.Items) < 1 || len(g.Items) > maxListItems {
		return badRequest("a geo policy needs 1 to " + strconv.Itoa(maxListItems) + " items")
	}
	if g.Kind != "" {
		return badRequest("kind is set by Google and cannot be written")
	}
	for _, item := range g.Items {
		if !plainText(item.Location, 64) || item.Kind != "" || len(item.SignatureRrdatas) > 0 {
			return badRequest("a geo item needs a location, and kind and signatureRrdatas are set by Google")
		}
		if len(item.Rrdatas) == 0 && item.HealthCheckedTargets == nil {
			return badRequest("a geo item needs rrdatas or healthCheckedTargets")
		}
		if len(item.Rrdatas) > 0 {
			if err := checkRrdatas(typ, item.Rrdatas); err != nil {
				return err
			}
		}
		if err := checkHealthTargets(typ, item.HealthCheckedTargets); err != nil {
			return err
		}
	}
	return nil
}

// check refuses a routing policy that is not exactly one of the three documented kinds with values of the type.
func (p *dnsRoutingPolicy) check(typ string) error {
	if p.Kind != "" {
		return badRequest("kind is set by Google and cannot be written")
	}
	kinds := 0
	for _, set := range []bool{p.Wrr != nil, p.Geo != nil, p.PrimaryBackup != nil} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		return badRequest("routingPolicy needs exactly one of wrr, geo, or primaryBackup")
	}
	if p.HealthCheck != "" && !plainText(p.HealthCheck, maxText) {
		return badRequest("healthCheck must be bounded text")
	}
	switch {
	case p.Wrr != nil:
		if len(p.Wrr.Items) < 1 || len(p.Wrr.Items) > maxListItems || p.Wrr.Kind != "" {
			return badRequest("a wrr policy needs 1 to " + strconv.Itoa(maxListItems) + " items")
		}
		for _, item := range p.Wrr.Items {
			if item.Weight == nil || *item.Weight < 0 || math.IsNaN(*item.Weight) || math.IsInf(*item.Weight, 0) ||
				item.Kind != "" || len(item.SignatureRrdatas) > 0 {
				return badRequest("a wrr item needs a weight of 0 or more, and kind and signatureRrdatas are set by Google")
			}
			if len(item.Rrdatas) == 0 && item.HealthCheckedTargets == nil {
				return badRequest("a wrr item needs rrdatas or healthCheckedTargets")
			}
			if len(item.Rrdatas) > 0 {
				if err := checkRrdatas(typ, item.Rrdatas); err != nil {
					return err
				}
			}
			if err := checkHealthTargets(typ, item.HealthCheckedTargets); err != nil {
				return err
			}
		}
	case p.Geo != nil:
		return checkGeoPolicy(typ, p.Geo)
	default:
		b := p.PrimaryBackup
		if b.PrimaryTargets == nil || b.Kind != "" || (b.TrickleTraffic != nil && (*b.TrickleTraffic < 0 || *b.TrickleTraffic > 1)) {
			return badRequest("a primaryBackup policy needs primaryTargets and a trickleTraffic from 0 to 1")
		}
		if err := checkHealthTargets(typ, b.PrimaryTargets); err != nil {
			return err
		}
		return checkGeoPolicy(typ, b.BackupGeoTargets)
	}
	return nil
}

func checkWriteType(typ string) error {
	switch {
	case typ == "SOA":
		return badRequest("the SOA record is maintained by Google and is never changed")
	case !slices.Contains(dnsRecordTypes, typ):
		return badRequest("type must be a record type of Cloud DNS: " + strings.Join(dnsRecordTypes, ", "))
	}
	return nil
}

// check refuses a record set that is not in its documented form. whole is set when the record set must be
// complete, as for a creation and for both sides of a change; a patch names only the members to change.
func (r recordInput) check(whole bool) error {
	switch {
	case !validDNSName(r.Name, true):
		return badRequest("name must be a fully qualified domain name with its trailing dot such as www.example.com.")
	case !validDNSType(r.Type):
		return badRequest("type must be a record type in capital letters such as A or TXT")
	}
	if err := checkWriteType(r.Type); err != nil {
		return err
	}
	if r.TTL != nil && (*r.TTL < 0 || *r.TTL > math.MaxInt32) {
		return badRequest("ttl must be 0 to 2147483647")
	}
	hasData := len(r.Rrdatas) > 0
	switch {
	case hasData && r.RoutingPolicy != nil:
		return badRequest("a record has either rrdatas or a routingPolicy, not both")
	case whole && !hasData && r.RoutingPolicy == nil:
		return badRequest("a record needs rrdatas with ttl, or a routingPolicy")
	case whole && hasData && r.TTL == nil:
		return badRequest("ttl is required with rrdatas")
	}
	if hasData {
		if err := checkRrdatas(r.Type, r.Rrdatas); err != nil {
			return err
		}
	}
	if r.RoutingPolicy != nil {
		return r.RoutingPolicy.check(r.Type)
	}
	return nil
}

func checkOperationID(value string) error {
	if value != "" && !dnsOperationIDPattern.MatchString(value) {
		return badRequest("client_operation_id must be 1 to 64 letters, digits, dots, underscores, or dashes")
	}
	return nil
}

func operationQuery(id string) url.Values {
	if id == "" {
		return nil
	}
	return url.Values{"clientOperationId": {id}}
}

// zoneInfo is what the tools take from the zone Google describes: the apex and the visibility. Neither
// chooses a target; the apex only narrows what a tool may write.
type zoneInfo struct{ dnsName, visibility string }

// readZone reads the zone of the arguments and checks that Google describes the zone that was asked for.
func (c *client) readZone(ctx context.Context, op, tool, projectID, zone string) (zoneInfo, error) {
	var wire json.RawMessage
	if err := c.get(ctx, op, tool, serviceDNS, dnsZonePath(projectID, zone), nil, &wire); err != nil {
		return zoneInfo{}, err
	}
	row, err := managedZoneResult(op, wire)
	if err != nil {
		return zoneInfo{}, err
	}
	name, _ := row["name"].(string)
	id, _ := row["id"].(string)
	dnsName, _ := row["dnsName"].(string)
	visibility, _ := row["visibility"].(string)
	if (dnsZonePattern.MatchString(zone) && name != zone) || (!dnsZonePattern.MatchString(zone) && id != zone) ||
		!validDNSName(dnsName, false) || (visibility != "public" && visibility != "private") {
		return zoneInfo{}, invalidRow(op)
	}
	return zoneInfo{dnsName: dnsName, visibility: visibility}, nil
}

// refuseApex refuses the NS records of the zone apex. SOA records were refused by checkWriteType.
func (z zoneInfo) refuseApex(name, typ string) error {
	if typ == "NS" && strings.EqualFold(name, z.dnsName) {
		return badRequest("the NS records of the zone apex are never changed, because they can move the " +
			"delegation of the zone to foreign name servers; NS records below the apex are allowed")
	}
	return nil
}

// unsureResponse is the failure of an answer to a change that cannot be used: the change may have been made.
func unsureResponse(op string) error {
	failed := provider.InvalidResponse(op, subject+" returned an invalid response")
	failed.Message += unsureNote
	return failed
}

// sameRecord reports whether Google answered the record set that was asked for; names compare without case.
func sameRecord(row recordSet, name, typ string) bool {
	return strings.EqualFold(row.Name, name) && row.Type == typ
}

func recordPath(projectID, zone, name, typ string) string {
	return dnsZonePath(projectID, zone) + "/rrsets/" + url.PathEscape(name) + "/" + url.PathEscape(typ)
}

// readRecord reads the current record set for a patch or a deletion.
func (c *client) readRecord(ctx context.Context, op, tool, projectID, zone, name, typ string) (recordSet, error) {
	var wire json.RawMessage
	if err := c.get(ctx, op, tool, serviceDNS, recordPath(projectID, zone, name, typ), nil, &wire); err != nil {
		return recordSet{}, err
	}
	row, err := decodeRecordSet(op, wire)
	if err != nil {
		return recordSet{}, err
	}
	if !sameRecord(row, name, typ) {
		return recordSet{}, invalidRow(op)
	}
	return row, nil
}

// changeRecords sends the one request of a changing tool and returns the raw answer.
func (c *client) changeRecords(ctx context.Context, op, tool, method, path, operationID string, body any) (json.RawMessage, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, failure(op, "the request could not be built")
		}
	}
	var wire json.RawMessage
	if err := c.send(ctx, op, tool, serviceDNS, method, path, operationQuery(operationID), payload, &wire, true); err != nil {
		return nil, err
	}
	return wire, nil
}

// --- confirmation texts, from the arguments alone ---

const dnsSummaryValues = 5

// describeRecord names a record set for a confirmation: name, type, TTL, and the first values.
func describeRecord(r recordInput) string {
	out := boundedText(r.Name, maxNameText) + " " + boundedText(r.Type, maxNameText)
	if r.TTL != nil {
		out += " ttl " + strconv.FormatInt(*r.TTL, 10)
	}
	if len(r.Rrdatas) > 0 {
		shown := make([]string, 0, dnsSummaryValues)
		for _, value := range r.Rrdatas[:min(len(r.Rrdatas), dnsSummaryValues)] {
			shown = append(shown, strconv.Quote(boundedText(value, maxNameText)))
		}
		out += " [" + strings.Join(shown, ", ")
		if len(r.Rrdatas) > dnsSummaryValues {
			out += ", and " + strconv.Itoa(len(r.Rrdatas)-dnsSummaryValues) + " more"
		}
		out += "]"
	}
	if r.RoutingPolicy != nil {
		out += " with a routing policy"
	}
	return out
}

func where(projectID, zone string) string {
	return "in managed zone " + boundedText(zone, maxNameText) + " of project " + boundedText(projectID, maxNameText)
}

const dnsSummaryTail = "; the record changes where the name resolves, in a public zone for the whole internet"
