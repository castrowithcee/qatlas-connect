package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	dnsKeysListID = Provider + ".dns-keys.list"
	dnsKeysGetID  = Provider + ".dns-keys.get"
)

// dnsKeyMembers are the documented members of the DnsKey resource of dns v1 (revision 20261111); none of them
// is marked deprecated. The public key and the digests are public zone-signing material, not secrets.
var dnsKeyMembers = []computeMember{
	{"id", "string"}, {"keyTag", "number"}, {"algorithm", "string"}, {"type", "string"}, {"isActive", "boolean"},
	{"creationTime", "string"}, {"publicKey", "string"}, {"digests", "array"}, {"keyLength", "number"},
	{"description", "string"}, {"kind", "string"},
}

// dnsKeySchema is membersSchema with the ID as the required member: a key has no name.
func dnsKeySchema() string {
	return strings.Replace(membersSchema(dnsKeyMembers), `"required":["name"]`, `"required":["id"]`, 1)
}

const dnsKeyFields = "{id, keyTag, algorithm, type (keySigning or zoneSigning), isActive, creationTime, publicKey, " +
	"digests (digest and type: sha1, sha256, or sha384), keyLength, description, kind}; the public key and the " +
	"digests are public material of the zone signature, from which the DS record at the parent is built; the " +
	"digests are computed for key signing keys only; descriptions are untrusted text"

// dnsKeyResult keeps the documented members of one key, bounded, and refuses a row of wrong types or without an ID.
func dnsKeyResult(op string, raw json.RawMessage) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	if err := dnsMemberKinds(op, row, dnsKeyMembers); err != nil {
		return nil, err
	}
	if id, _ := row["id"].(string); id == "" {
		return nil, invalidRow(op)
	}
	budget := maxNodes
	bounded, _ := boundNode(pickMembers(row, dnsKeyMembers), 0, &budget).(map[string]any)
	return bounded, nil
}

// validDigestTypes accepts the comma-separated list of digest types of Google, each one of sha1, sha256, sha384.
func validDigestTypes(value string) bool {
	for _, part := range strings.Split(value, ",") {
		if part != "sha1" && part != "sha256" && part != "sha384" {
			return false
		}
	}
	return true
}

const (
	dnsKeyIDProp = `"dns_key_id":{"type":"string","minLength":1,"maxLength":20,"pattern":"^[0-9]{1,20}$"}`
	// digestProp is a comma-separated list of the three digest types.
	digestProp = `"digest_type":{"type":"string","minLength":4,"maxLength":20,` +
		`"pattern":"^(sha1|sha256|sha384)(,(sha1|sha256|sha384))*$"}`
)

var digestArgument = capability.Argument{Name: "digest_type", Description: "Comma-separated list of digest types " +
	"to compute for key signing keys, from sha1, sha256, and sha384, such as sha256 or sha256,sha384. Without it " +
	"Google computes the recommended digest type"}

var dnsKeysList = capability.Descriptor{
	ID: dnsKeysListID, Version: 1, Title: "List the DNSSEC keys of a Cloud DNS managed zone",
	Description: "List the DNSSEC keys of a managed zone from Cloud DNS: each with its ID, key tag, algorithm, " +
		"type (key signing or zone signing), whether it is active, creation time, public key, digests, and key " +
		"length. A zone without DNSSEC has no keys. Keys cannot be created or changed." + dnsTail,
	Tags: []string{"google", "cloud", "dns", "dns-keys"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + dnsZoneProp + `,` +
		digestProp + `,` + dnsMaxProp + `,` + cursorProp +
		`},"required":["project_id","managed_zone"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("dns_keys", dnsKeySchema()),
	Arguments:    []capability.Argument{serviceProjectArgument, dnsZoneArgument, digestArgument, dnsMaxArgument, dnsCursorArgument},
	Fields: append([]capability.Field{
		{Name: "dns_keys", Description: "DNSSEC keys of the page, each " + dnsKeyFields},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the keys of a zone with the SHA-256 digest",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","managed_zone":"example-zone",` +
			`"digest_type":"sha256"}`),
	}},
}

var dnsKeysGet = capability.Descriptor{
	ID: dnsKeysGetID, Version: 1, Title: "Get one DNSSEC key of a Cloud DNS managed zone",
	Description: "Read one DNSSEC key of a managed zone from Cloud DNS by its ID, with the documented fields of " +
		"the key." + dnsTail,
	Tags: []string{"google", "cloud", "dns", "dns-keys"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + dnsZoneProp + `,` +
		dnsKeyIDProp + `,` + digestProp + `},"required":["project_id","managed_zone","dns_key_id"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(dnsKeySchema()),
	Arguments: []capability.Argument{serviceProjectArgument, dnsZoneArgument,
		{Name: "dns_key_id", Required: true, Description: "Numeric ID of the key, as listed by dns-keys.list"},
		digestArgument},
	Fields: []capability.Field{{Name: "id", Description: "The key as one object: " + dnsKeyFields}},
	Examples: []capability.Example{{
		Description: "Read one key",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","managed_zone":"example-zone","dns_key_id":"0"}`),
	}},
}

func handleDNSKeysList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list DNSSEC keys"
	var arguments struct {
		dnsPaging
		ProjectID   string `json:"project_id"`
		ManagedZone string `json:"managed_zone"`
		DigestType  string `json:"digest_type"`
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
	if arguments.DigestType != "" && !validDigestTypes(arguments.DigestType) {
		return nil, badRequest("digest_type must be a comma-separated list of sha1, sha256, and sha384")
	}
	if err := arguments.check(); err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(dnsKeysListID, provider.TargetsOf(resolved), arguments.ProjectID,
		arguments.ManagedZone, arguments.DigestType)
	token, err := arguments.token(binding)
	if err != nil {
		return nil, err
	}
	c, err := dnsClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := arguments.query(token)
	if arguments.DigestType != "" {
		query.Set("digestType", arguments.DigestType)
	}
	var answer struct {
		DNSKeys       []json.RawMessage `json:"dnsKeys"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if err := c.get(ctx, op, dnsKeysListID, serviceDNS, dnsZonePath(arguments.ProjectID, arguments.ManagedZone)+"/dnsKeys",
		query, &answer); err != nil {
		return nil, err
	}
	if len(answer.DNSKeys) > maxDNSRows {
		return nil, invalidRow(op)
	}
	rows := make([]map[string]any, 0, len(answer.DNSKeys))
	for _, item := range answer.DNSKeys {
		row, err := dnsKeyResult(op, item)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return dnsPaged(op, map[string]any{"dns_keys": rows}, binding, answer.NextPageToken)
}

func handleDNSKeysGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get DNSSEC key"
	var arguments struct {
		ProjectID   string `json:"project_id"`
		ManagedZone string `json:"managed_zone"`
		DNSKeyID    string `json:"dns_key_id"`
		DigestType  string `json:"digest_type"`
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
	if !dnsSequenceIDPattern.MatchString(arguments.DNSKeyID) {
		return nil, badRequest("dns_key_id must be the numeric ID of a key, never a path")
	}
	if arguments.DigestType != "" && !validDigestTypes(arguments.DigestType) {
		return nil, badRequest("digest_type must be a comma-separated list of sha1, sha256, and sha384")
	}
	c, err := dnsClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var query url.Values
	if arguments.DigestType != "" {
		query = url.Values{"digestType": {arguments.DigestType}}
	}
	var wire json.RawMessage
	path := dnsZonePath(arguments.ProjectID, arguments.ManagedZone) + "/dnsKeys/" + url.PathEscape(arguments.DNSKeyID)
	if err := c.get(ctx, op, dnsKeysGetID, serviceDNS, path, query, &wire); err != nil {
		return nil, err
	}
	row, err := dnsKeyResult(op, wire)
	if err != nil {
		return nil, err
	}
	if row["id"] != arguments.DNSKeyID {
		return nil, invalidRow(op)
	}
	return row, nil
}
