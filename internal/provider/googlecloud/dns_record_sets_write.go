package googlecloud

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	dnsVisibilityProp = `"visibility":{"type":"string","enum":["public","private"]}`
	dnsRecordResult   = `{"type":"object","properties":{"record_set":` + recordSetSchema + `,"previous":` + recordSetSchema + `,` +
		`"deleted":{"type":"boolean"},` + dnsVisibilityProp + `},"required":["visibility"],"additionalProperties":false}`
	dnsChangeResult = `{"type":"object","properties":{"change":` + changeSchema + `,` + dnsVisibilityProp + `},` +
		`"required":["change","visibility"],"additionalProperties":false}`
)

var (
	dnsVisibilityField = capability.Field{Name: "visibility", Description: "public or private: the visibility of " +
		"the zone as Google describes it"}
	dnsTags = []string{"google", "cloud", "dns"}
)

var changesCreate = capability.Descriptor{
	ID: changesCreateID, Version: 1, Title: "Change several records of a Cloud DNS managed zone atomically",
	Description: "Apply one change to a managed zone in Cloud DNS (changes.create of dns v1): the record sets in " +
		"additions are added and those in deletions are removed together, all or none. A record set in deletions " +
		"must match the existing one exactly (name, type, ttl, and data; read it with record-sets.get), so " +
		"replacing a record deletes the old set and adds the new one in one change. At least one record set is " +
		"needed. The result is the change with its id and its status, pending or done." + dnsWriteNote,
	Tags: append(append([]string{}, dnsTags...), "changes", "write"), Provider: Provider,
	Risk: dnsWriteRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + dnsZoneProp + `,` +
		`"additions":{"type":"array","minItems":1,"maxItems":100,"items":` + dnsRecordInput + `},` +
		`"deletions":{"type":"array","minItems":1,"maxItems":100,"items":` + dnsRecordInput + `},` + dnsOperationProp +
		`},"required":["project_id","managed_zone"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(dnsChangeResult),
	Arguments: []capability.Argument{serviceProjectArgument, dnsZoneArgument,
		{Name: "additions", Description: "Record sets to add, up to 100, each {name, type, ttl, rrdatas} or " +
			"{name, type, routingPolicy}; see record-sets.create for the forms"},
		{Name: "deletions", Description: "Record sets to remove, up to 100, each exactly as the zone holds it"},
		dnsOperationArgument},
	Fields: []capability.Field{
		{Name: "change", Description: "The change as one object: " + changeFields},
		dnsVisibilityField},
	Examples: []capability.Example{{
		Description: "Replace the address of www in one change",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","managed_zone":"example-zone",` +
			`"deletions":[{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.9"]}],` +
			`"additions":[{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.10"]}]}`),
	}},
}

var recordSetsCreate = capability.Descriptor{
	ID: recordSetsCreateID, Version: 1, Title: "Create a record set in a Cloud DNS managed zone",
	Description: "Create one record set in a managed zone in Cloud DNS (resourceRecordSets.create of dns v1), " +
		"with either rrdatas and ttl, or a routingPolicy. Google refuses a name and type that already exist; " +
		"change an existing record with record-sets.patch. Types: " + strings.Join(dnsRecordTypes, ", ") +
		", except SOA." + dnsWriteNote,
	Tags: append(append([]string{}, dnsTags...), "record-sets", "write"), Provider: Provider,
	Risk: dnsWriteRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + dnsZoneProp + `,` +
		dnsRecordProps + `,` + dnsOperationProp + `},"required":["project_id","managed_zone","name","type"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(dnsRecordResult),
	Arguments: []capability.Argument{serviceProjectArgument, dnsZoneArgument, recordNameArgument, recordTypeArgument,
		dnsTTLArgument, dnsRrdatasArgument, dnsRoutingPolicyArgument, dnsOperationArgument},
	Fields: []capability.Field{
		{Name: "record_set", Description: "The record set Google created: " + recordSetFields}, dnsVisibilityField},
	Examples: []capability.Example{{
		Description: "Create an A record",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","managed_zone":"example-zone",` +
			`"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["192.0.2.10"]}`),
	}},
}

var recordSetsPatch = capability.Descriptor{
	ID: recordSetsPatchID, Version: 1, Title: "Change a record set of a Cloud DNS managed zone",
	Description: "Change one existing record set of a managed zone in Cloud DNS (resourceRecordSets.patch of dns " +
		"v1), by its name and type: ttl, rrdatas, or routingPolicy, at least one. A member given replaces the " +
		"whole member (rrdatas replaces the complete list of values); members left out stay. The current record set " +
		"is read first and returned as previous, so the call fails when no such record set exists. With expected_ttl " +
		"or expected_rrdatas the confirmation names what you expect, and the tool changes nothing if the record set " +
		"no longer has these values. changes.create with deletions binds the change exactly to the old record " +
		"set." + dnsWriteNote,
	Tags: append(append([]string{}, dnsTags...), "record-sets", "write"), Provider: Provider,
	Risk: dnsWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + dnsZoneProp + `,` +
		dnsRecordProps + `,` + dnsExpectedProps + `,` + dnsOperationProp + `},"required":["project_id","managed_zone","name","type"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(dnsRecordResult),
	Arguments: []capability.Argument{serviceProjectArgument, dnsZoneArgument, recordNameArgument, recordTypeArgument,
		dnsTTLArgument, dnsRrdatasArgument, dnsRoutingPolicyArgument, dnsExpectedTTLArgument, dnsExpectedRrdatasArgument, dnsOperationArgument},
	Fields: []capability.Field{
		{Name: "record_set", Description: "The record set after the change: " + recordSetFields},
		{Name: "previous", Description: "The record set as the tool read it before the change"},
		dnsVisibilityField},
	Examples: []capability.Example{{
		Description: "Lower the TTL of an A record",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","managed_zone":"example-zone",` +
			`"name":"www.example.com.","type":"A","ttl":60}`),
	}},
}

var recordSetsDelete = capability.Descriptor{
	ID: recordSetsDeleteID, Version: 1, Title: "Delete a record set of a Cloud DNS managed zone",
	Description: "Delete one record set of a managed zone in Cloud DNS (resourceRecordSets.delete of dns v1), by " +
		"its name and type. The current record set is read first and returned as previous, so the call fails when " +
		"no such record set exists; the data cannot be restored except by creating it again. With expected_ttl or " +
		"expected_rrdatas the confirmation names what you expect, and the tool deletes nothing if the record set " +
		"no longer has these values; changes.create with deletions binds the deletion exactly to the record set, " +
		"as Google requires an exact match there. Zones cannot be created or deleted." + dnsWriteNote,
	Tags: append(append([]string{}, dnsTags...), "record-sets", "write", "delete"), Provider: Provider,
	Risk: dnsWriteRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + dnsZoneProp + `,` +
		dnsNameProp + `,` + dnsTypeProp + `,` + dnsExpectedProps + `,` + dnsOperationProp + `},` +
		`"required":["project_id","managed_zone","name","type"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(dnsRecordResult),
	Arguments: []capability.Argument{serviceProjectArgument, dnsZoneArgument, recordNameArgument, recordTypeArgument,
		dnsExpectedTTLArgument, dnsExpectedRrdatasArgument, dnsOperationArgument},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True when Google deleted the record set"},
		{Name: "previous", Description: "The record set as the tool read it before the deletion"},
		dnsVisibilityField},
	Examples: []capability.Example{{
		Description: "Delete a TXT record",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","managed_zone":"example-zone",` +
			`"name":"_dmarc.example.com.","type":"TXT"}`),
	}},
}

// dnsWriteArguments are the arguments every changing DNS tool shares.
type dnsWriteArguments struct {
	ProjectID         string `json:"project_id"`
	ManagedZone       string `json:"managed_zone"`
	ClientOperationID string `json:"client_operation_id"`
}

func (a dnsWriteArguments) checkBase() error {
	if err := checkDNSProject(a.ProjectID); err != nil {
		return err
	}
	if err := checkDNSZone(a.ManagedZone); err != nil {
		return err
	}
	return checkOperationID(a.ClientOperationID)
}

type recordWriteArguments struct {
	dnsWriteArguments
	recordInput
	ExpectedTTL     *int64   `json:"expected_ttl"`
	ExpectedRrdatas []string `json:"expected_rrdatas"`
}

// checkExpected refuses expectations that are out of form, before any request.
func (a recordWriteArguments) checkExpected() error {
	if a.ExpectedTTL != nil && (*a.ExpectedTTL < 0 || *a.ExpectedTTL > math.MaxInt32) {
		return badRequest("expected_ttl must be 0 to 2147483647")
	}
	if len(a.ExpectedRrdatas) > maxDNSValues {
		return badRequest("expected_rrdatas takes at most " + strconv.Itoa(maxDNSValues) + " values")
	}
	for _, value := range a.ExpectedRrdatas {
		if !plainText(value, maxDNSValueText) {
			return badRequest("a value of expected_rrdatas must be bounded printable text")
		}
	}
	return nil
}

// matchExpected compares the record set read after the confirmation with what the person confirmed. The values
// compare as a set because the order of rrdatas carries no meaning in an answer. A difference is a failed
// precondition, the class Google's FAILED_PRECONDITION maps to; nothing has been written.
func (a recordWriteArguments) matchExpected(op string, current recordSet) error {
	if a.ExpectedTTL != nil && (current.TTL == nil || int64(*current.TTL) != *a.ExpectedTTL) {
		return failure(op, "the record set no longer has the expected_ttl, so nothing was changed; read it again "+
			"with record-sets.get and confirm what you want to change")
	}
	if len(a.ExpectedRrdatas) > 0 {
		want, have := slices.Clone(a.ExpectedRrdatas), slices.Clone(current.Rrdatas)
		for _, list := range []*[]string{&want, &have} {
			slices.Sort(*list)
			*list = slices.Compact(*list)
		}
		if !slices.Equal(want, have) {
			return failure(op, "the record set no longer has the expected_rrdatas, so nothing was changed; read it "+
				"again with record-sets.get and confirm what you want to change")
		}
	}
	return nil
}

func handleRecordSetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create record set"
	var a recordWriteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.checkBase(); err != nil {
		return nil, err
	}
	if err := a.recordInput.check(true); err != nil {
		return nil, err
	}
	c, err := dnsClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	zone, err := c.readZone(ctx, op, recordSetsCreateID, a.ProjectID, a.ManagedZone)
	if err != nil {
		return nil, err
	}
	if err := zone.refuseApex(a.Name, a.Type); err != nil {
		return nil, err
	}
	wire, err := c.changeRecords(ctx, op, recordSetsCreateID, http.MethodPost,
		dnsZonePath(a.ProjectID, a.ManagedZone)+"/rrsets", a.ClientOperationID, a.recordInput.wire())
	if err != nil {
		return nil, err
	}
	row, err := decodeRecordSet(op, wire)
	if err != nil || !sameRecord(row, a.Name, a.Type) {
		return nil, unsureResponse(op)
	}
	return map[string]any{"record_set": row, "visibility": zone.visibility}, nil
}

func handleRecordSetsPatch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "change record set"
	var a recordWriteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.checkBase(); err != nil {
		return nil, err
	}
	if err := a.recordInput.check(false); err != nil {
		return nil, err
	}
	if err := a.checkExpected(); err != nil {
		return nil, err
	}
	if a.TTL == nil && len(a.Rrdatas) == 0 && a.RoutingPolicy == nil {
		return nil, badRequest("a change needs at least one of ttl, rrdatas, or routingPolicy")
	}
	c, err := dnsClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	zone, err := c.readZone(ctx, op, recordSetsPatchID, a.ProjectID, a.ManagedZone)
	if err != nil {
		return nil, err
	}
	if err := zone.refuseApex(a.Name, a.Type); err != nil {
		return nil, err
	}
	previous, err := c.readRecord(ctx, op, recordSetsPatchID, a.ProjectID, a.ManagedZone, a.Name, a.Type)
	if err != nil {
		return nil, err
	}
	if err := a.matchExpected(op, previous); err != nil {
		return nil, err
	}
	wire, err := c.changeRecords(ctx, op, recordSetsPatchID, http.MethodPatch,
		recordPath(a.ProjectID, a.ManagedZone, a.Name, a.Type), a.ClientOperationID, a.recordInput.wire())
	if err != nil {
		return nil, err
	}
	row, err := decodeRecordSet(op, wire)
	if err != nil || !sameRecord(row, a.Name, a.Type) {
		return nil, unsureResponse(op)
	}
	return map[string]any{"record_set": row, "previous": previous, "visibility": zone.visibility}, nil
}

func handleRecordSetsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete record set"
	var a recordWriteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.checkBase(); err != nil {
		return nil, err
	}
	if err := a.recordInput.check(false); err != nil {
		return nil, err
	}
	if err := a.checkExpected(); err != nil {
		return nil, err
	}
	c, err := dnsClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	zone, err := c.readZone(ctx, op, recordSetsDeleteID, a.ProjectID, a.ManagedZone)
	if err != nil {
		return nil, err
	}
	if err := zone.refuseApex(a.Name, a.Type); err != nil {
		return nil, err
	}
	previous, err := c.readRecord(ctx, op, recordSetsDeleteID, a.ProjectID, a.ManagedZone, a.Name, a.Type)
	if err != nil {
		return nil, err
	}
	if err := a.matchExpected(op, previous); err != nil {
		return nil, err
	}
	if _, err := c.changeRecords(ctx, op, recordSetsDeleteID, http.MethodDelete,
		recordPath(a.ProjectID, a.ManagedZone, a.Name, a.Type), a.ClientOperationID, nil); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": true, "previous": previous, "visibility": zone.visibility}, nil
}

type changeArguments struct {
	dnsWriteArguments
	Additions []recordInput `json:"additions"`
	Deletions []recordInput `json:"deletions"`
}

func handleChangesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create change"
	var a changeArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.checkBase(); err != nil {
		return nil, err
	}
	if len(a.Additions)+len(a.Deletions) == 0 {
		return nil, badRequest("a change needs at least one record set in additions or deletions")
	}
	if len(a.Additions) > maxListItems || len(a.Deletions) > maxListItems {
		return nil, badRequest("additions and deletions take at most " + strconv.Itoa(maxListItems) + " record sets each")
	}
	for _, r := range append(append([]recordInput{}, a.Additions...), a.Deletions...) {
		if err := r.check(true); err != nil {
			return nil, err
		}
	}
	c, err := dnsClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	zone, err := c.readZone(ctx, op, changesCreateID, a.ProjectID, a.ManagedZone)
	if err != nil {
		return nil, err
	}
	body := struct {
		Additions []recordSet `json:"additions,omitempty"`
		Deletions []recordSet `json:"deletions,omitempty"`
	}{}
	for _, r := range a.Additions {
		if err := zone.refuseApex(r.Name, r.Type); err != nil {
			return nil, err
		}
		body.Additions = append(body.Additions, r.wire())
	}
	for _, r := range a.Deletions {
		if err := zone.refuseApex(r.Name, r.Type); err != nil {
			return nil, err
		}
		body.Deletions = append(body.Deletions, r.wire())
	}
	wire, err := c.changeRecords(ctx, op, changesCreateID, http.MethodPost,
		dnsZonePath(a.ProjectID, a.ManagedZone)+"/changes", a.ClientOperationID, body)
	if err != nil {
		return nil, err
	}
	change, err := changeResult(op, wire)
	if status, _ := change["status"].(string); err != nil || (status != "pending" && status != "done") {
		return nil, unsureResponse(op)
	}
	return map[string]any{"change": change, "visibility": zone.visibility}, nil
}

// --- confirmation texts ---

func recordSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a recordWriteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		at := where(a.ProjectID, a.ManagedZone)
		expected := ""
		if a.ExpectedTTL != nil || len(a.ExpectedRrdatas) > 0 {
			expected = "; only if the record set now has " + strings.TrimLeft(describeRecord(recordInput{
				TTL: a.ExpectedTTL, Rrdatas: a.ExpectedRrdatas}), " ")
		}
		switch verb {
		case "create":
			return "Create record set " + describeRecord(a.recordInput) + " " + at + dnsSummaryTail
		case "patch":
			return "Change record set " + boundedText(a.Name, maxNameText) + " " + boundedText(a.Type, maxNameText) +
				" " + at + " to the new members " + describeRecord(recordInput{TTL: a.TTL, Rrdatas: a.Rrdatas,
				RoutingPolicy: a.RoutingPolicy}) + "; the current record set is read first and returned as previous" +
				expected + dnsSummaryTail
		}
		return "Delete record set " + boundedText(a.Name, maxNameText) + " " + boundedText(a.Type, maxNameText) +
			" " + at + ": the name stops resolving for this type; the current record set is read first and " +
			"returned as previous" + expected
	}
}

const dnsSummaryChangeItems = 10

func describeRecords(list []recordInput) string {
	shown := make([]string, 0, dnsSummaryChangeItems)
	for _, r := range list[:min(len(list), dnsSummaryChangeItems)] {
		shown = append(shown, describeRecord(r))
	}
	out := strings.Join(shown, "; ")
	if len(list) > dnsSummaryChangeItems {
		out += "; and " + strconv.Itoa(len(list)-dnsSummaryChangeItems) + " more"
	}
	return out
}

func changeSummary(raw json.RawMessage) string {
	var a changeArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Apply one atomic change " + where(a.ProjectID, a.ManagedZone)
	if len(a.Deletions) > 0 {
		out += ": delete " + strconv.Itoa(len(a.Deletions)) + " record sets (" + describeRecords(a.Deletions) + ")"
	}
	if len(a.Additions) > 0 {
		out += "; add " + strconv.Itoa(len(a.Additions)) + " record sets (" + describeRecords(a.Additions) + ")"
	}
	return out + dnsSummaryTail
}
