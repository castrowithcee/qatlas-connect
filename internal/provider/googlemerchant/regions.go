package googlemerchant

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
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

const (
	regionsListID   = Provider + ".regions.list"
	regionsGetID    = Provider + ".regions.get"
	regionsCreateID = Provider + ".regions.create"
	regionsUpdateID = Provider + ".regions.update"
)

// Bounds of the region arguments. Google documents none of them except the page size, so the caps are Qatlas'.
const (
	maxRegionIDLen     = 64
	maxRegionNameRunes = 200
	maxPostalRanges    = 250
	maxGeotargets      = 100
	maxRadius          = 100000
	minRadius          = 0.01
	maxRegionRows      = 1000 // the documented maximum page size of regions.list
)

// A region ID is a query parameter of create and a path segment everywhere else. Google documents no form for
// it, so Qatlas accepts the narrow one that can never alter a path: ASCII letters, digits, "-" and "_", with
// no "/" and no ".". A region that already has another ID cannot be addressed through these tools.
const regionIDPattern = `[A-Za-z0-9][A-Za-z0-9_-]{0,63}`

var (
	regionIDForm   = regexp.MustCompile(`^` + regionIDPattern + `$`)
	territoryForm  = regexp.MustCompile(`^[A-Z]{2}$`)
	postalCodeForm = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 -]{0,19}\*?$`)
)

const postalCodePattern = `^[A-Za-z0-9][A-Za-z0-9 -]{0,19}\\*?$`

func validRegionID(raw string) bool { return regionIDForm.MatchString(raw) }

// The update mask paths: the only members of a Region that can be written. Each one replaces the whole member.
// This is the only place a path of the mask is spelled; the mask is never taken from the caller as text.
const (
	maskRegionDisplayName    = "displayName"
	maskRegionPostalCodeArea = "postalCodeArea"
	maskRegionGeotargetArea  = "geotargetArea"
	maskRegionRadiusArea     = "radiusArea"
)

var (
	regionMaskPaths = []string{maskRegionDisplayName, maskRegionPostalCodeArea, maskRegionGeotargetArea,
		maskRegionRadiusArea}
	regionAreaPaths = []string{maskRegionPostalCodeArea, maskRegionGeotargetArea, maskRegionRadiusArea}
)

// What Google returns for a region is untrusted: the display name comes from whoever runs the account.
type (
	regionNumber float64

	postalRange struct {
		Begin idText `json:"begin,omitempty"`
		End   idText `json:"end,omitempty"`
	}
	postalRanges []postalRange

	postalCodeArea struct {
		RegionCode  idText       `json:"regionCode,omitempty"`
		PostalCodes postalRanges `json:"postalCodes,omitempty"`
	}
	geotargetArea struct {
		GeotargetCriteriaIDs idList `json:"geotargetCriteriaIds,omitempty"`
	}
	latLng struct {
		Latitude  regionNumber `json:"latitude"`
		Longitude regionNumber `json:"longitude"`
	}
	radiusArea struct {
		Radius      regionNumber `json:"radius"`
		LatLng      *latLng      `json:"latLng,omitempty"`
		RegionCode  idText       `json:"regionCode,omitempty"`
		RadiusUnits idText       `json:"radiusUnits,omitempty"`
	}

	// region is the Region resource of the accounts sub-API; no member is added or derived.
	region struct {
		Name                      idText          `json:"name,omitempty"`
		DisplayName               nameText        `json:"displayName,omitempty"`
		PostalCodeArea            *postalCodeArea `json:"postalCodeArea,omitempty"`
		GeotargetArea             *geotargetArea  `json:"geotargetArea,omitempty"`
		RadiusArea                *radiusArea     `json:"radiusArea,omitempty"`
		ShippingEligible          bool            `json:"shippingEligible,omitempty"`
		RegionalInventoryEligible bool            `json:"regionalInventoryEligible,omitempty"`
	}
)

// UnmarshalJSON keeps a finite number and drops anything else.
func (n *regionNumber) UnmarshalJSON(b []byte) error {
	var v float64
	if json.Unmarshal(b, &v) != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		*n = 0
		return nil
	}
	*n = regionNumber(v)
	return nil
}

// UnmarshalJSON keeps at most maxPostalRanges ranges of a list; an entry that is not an object is dropped.
func (l *postalRanges) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		*l = nil
		return nil
	}
	var out postalRanges
	for _, item := range raw {
		if len(out) == maxPostalRanges {
			break
		}
		var r postalRange
		if json.Unmarshal(item, &r) == nil && (r.Begin != "" || r.End != "") {
			out = append(out, r)
		}
	}
	*l = out
	return nil
}

type regionsAnswer struct {
	Regions       []region `json:"regions"`
	NextPageToken string   `json:"nextPageToken"`
}

type regionsResult struct {
	Regions    []region `json:"regions"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// regionChangeResult says only that Google accepted the request.
type regionChangeResult struct {
	Accepted bool   `json:"accepted"`
	Region   region `json:"region"`
}

// Arguments of the area blocks. The members are those of Google's areas; nothing else can be passed.
type (
	postalRangeInput struct {
		Begin string `json:"begin"`
		End   string `json:"end"`
	}
	postalAreaInput struct {
		RegionCode  string             `json:"regionCode"`
		PostalCodes []postalRangeInput `json:"postalCodes"`
	}
	geotargetAreaInput struct {
		GeotargetCriteriaIDs []string `json:"geotargetCriteriaIds"`
	}
	latLngInput struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	radiusAreaInput struct {
		Radius      *float64     `json:"radius"`
		LatLng      *latLngInput `json:"latLng"`
		RegionCode  string       `json:"regionCode"`
		RadiusUnits string       `json:"radiusUnits"`
	}
)

type regionArguments struct {
	Account        string              `json:"account"`
	RegionID       string              `json:"region_id"`
	PageSize       int                 `json:"page_size"`
	Cursor         string              `json:"cursor"`
	DisplayName    *string             `json:"display_name"`
	PostalCodeArea *postalAreaInput    `json:"postal_code_area"`
	GeotargetArea  *geotargetAreaInput `json:"geotarget_area"`
	RadiusArea     *radiusAreaInput    `json:"radius_area"`
	UpdateMask     []string            `json:"update_mask"`
}

// areas names the area blocks that are set, in the order of regionAreaPaths.
func (a regionArguments) areas() []string {
	var set []string
	for _, p := range []struct {
		on   bool
		path string
	}{{a.PostalCodeArea != nil, maskRegionPostalCodeArea}, {a.GeotargetArea != nil, maskRegionGeotargetArea},
		{a.RadiusArea != nil, maskRegionRadiusArea}} {
		if p.on {
			set = append(set, p.path)
		}
	}
	return set
}

func validRegionText(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > maxRegionNameRunes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func postalAreaBody(in *postalAreaInput) (map[string]any, error) {
	if !territoryForm.MatchString(in.RegionCode) {
		return nil, invalidArgument("postal_code_area.regionCode must be a two-letter upper case territory code")
	}
	if len(in.PostalCodes) == 0 || len(in.PostalCodes) > maxPostalRanges {
		return nil, invalidArgument("postal_code_area.postalCodes must hold between 1 and " +
			strconv.Itoa(maxPostalRanges) + " ranges")
	}
	ranges := make([]map[string]any, 0, len(in.PostalCodes))
	for _, r := range in.PostalCodes {
		if !postalCodeForm.MatchString(r.Begin) || (r.End != "" && !postalCodeForm.MatchString(r.End)) {
			return nil, invalidArgument("a postal code is up to 20 letters, digits, spaces, and dashes, " +
				"optionally followed by * as a prefix pattern")
		}
		// Google requires end to have the shape of begin: a pattern with a pattern, a code with a code.
		if r.End != "" && strings.HasSuffix(r.Begin, "*") != strings.HasSuffix(r.End, "*") {
			return nil, invalidArgument("end of a postal code range must be a pattern exactly when begin is one")
		}
		entry := map[string]any{"begin": r.Begin}
		if r.End != "" {
			entry["end"] = r.End
		}
		ranges = append(ranges, entry)
	}
	return map[string]any{"regionCode": in.RegionCode, "postalCodes": ranges}, nil
}

func geotargetAreaBody(in *geotargetAreaInput) (map[string]any, error) {
	if len(in.GeotargetCriteriaIDs) == 0 || len(in.GeotargetCriteriaIDs) > maxGeotargets {
		return nil, invalidArgument("geotarget_area.geotargetCriteriaIds must hold between 1 and " +
			strconv.Itoa(maxGeotargets) + " location IDs")
	}
	seen := map[string]bool{}
	for _, id := range in.GeotargetCriteriaIDs {
		if !validID(id) || seen[id] {
			return nil, invalidArgument("geotargetCriteriaIds must be distinct decimal location IDs")
		}
		seen[id] = true
	}
	return map[string]any{"geotargetCriteriaIds": in.GeotargetCriteriaIDs}, nil
}

func radiusAreaBody(in *radiusAreaInput) (map[string]any, error) {
	if in.Radius == nil || math.IsNaN(*in.Radius) || *in.Radius < minRadius || *in.Radius > maxRadius {
		return nil, invalidArgument("radius_area.radius must be a number from 0.01 to 100000")
	}
	if in.LatLng == nil || in.LatLng.Latitude == nil || in.LatLng.Longitude == nil ||
		*in.LatLng.Latitude < -90 || *in.LatLng.Latitude > 90 ||
		*in.LatLng.Longitude < -180 || *in.LatLng.Longitude > 180 {
		return nil, invalidArgument("radius_area.latLng needs latitude from -90 to 90 and longitude from -180 to 180")
	}
	if !territoryForm.MatchString(in.RegionCode) {
		return nil, invalidArgument("radius_area.regionCode must be a two-letter upper case territory code")
	}
	body := map[string]any{"radius": *in.Radius, "regionCode": in.RegionCode,
		"latLng": map[string]any{"latitude": *in.LatLng.Latitude, "longitude": *in.LatLng.Longitude}}
	if in.RadiusUnits != "" {
		if in.RadiusUnits != "MILES" && in.RadiusUnits != "KILOMETERS" {
			return nil, invalidArgument("radius_area.radiusUnits must be MILES or KILOMETERS")
		}
		body["radiusUnits"] = in.RadiusUnits
	}
	return body, nil
}

// areaBody adds the one area block that is set, checked, to the body.
func areaBody(a regionArguments, body map[string]any) error {
	var err error
	var block map[string]any
	switch {
	case a.PostalCodeArea != nil:
		block, err = postalAreaBody(a.PostalCodeArea)
		body[maskRegionPostalCodeArea] = block
	case a.GeotargetArea != nil:
		block, err = geotargetAreaBody(a.GeotargetArea)
		body[maskRegionGeotargetArea] = block
	case a.RadiusArea != nil:
		block, err = radiusAreaBody(a.RadiusArea)
		body[maskRegionRadiusArea] = block
	}
	return err
}

// createRegionBody builds the body of a create call from checked arguments only. A region has exactly one area.
func createRegionBody(a regionArguments) (map[string]any, error) {
	if !validRegionID(a.RegionID) {
		return nil, invalidArgument("region_id must be 1 to 64 ASCII letters, digits, dashes, and underscores")
	}
	if len(a.areas()) != 1 {
		return nil, invalidArgument("exactly one of postal_code_area, geotarget_area, or radius_area is needed")
	}
	body := map[string]any{}
	if a.DisplayName != nil {
		if !validRegionText(*a.DisplayName) {
			return nil, invalidArgument("display_name must be 1 to " + strconv.Itoa(maxRegionNameRunes) +
				" characters without control characters")
		}
		body["displayName"] = *a.DisplayName
	}
	if err := areaBody(a, body); err != nil {
		return nil, err
	}
	return body, nil
}

// updateRegionRequest builds the update mask and the body. The mask is a list of paths of the allowlist, in
// the fixed order, and each path needs its member: display_name for displayName, and for the area paths
// exactly one area block that is itself in the mask, because a region has exactly one area. An area path
// without its block is cleared, which is how a region changes the kind of its area.
func updateRegionRequest(a regionArguments) (mask []string, body map[string]any, err error) {
	if !validRegionID(a.RegionID) {
		return nil, nil, invalidArgument("region_id must be 1 to 64 ASCII letters, digits, dashes, and underscores")
	}
	if len(a.UpdateMask) == 0 || len(a.UpdateMask) > len(regionMaskPaths) {
		return nil, nil, invalidArgument("update_mask must hold between 1 and 4 of displayName, postalCodeArea, " +
			"geotargetArea, and radiusArea")
	}
	for _, p := range a.UpdateMask {
		if !slices.Contains(regionMaskPaths, p) {
			return nil, nil, invalidArgument("update_mask holds only displayName, postalCodeArea, geotargetArea, " +
				"and radiusArea")
		}
	}
	for _, p := range regionMaskPaths {
		switch countOf(a.UpdateMask, p) {
		case 0:
		case 1:
			mask = append(mask, p)
		default:
			return nil, nil, invalidArgument("update_mask must not repeat a path")
		}
	}
	body = map[string]any{}
	if inMask := slices.Contains(mask, maskRegionDisplayName); inMask != (a.DisplayName != nil) {
		return nil, nil, invalidArgument("display_name and displayName in update_mask go together")
	} else if inMask {
		if !validRegionText(*a.DisplayName) {
			return nil, nil, invalidArgument("display_name must be 1 to " + strconv.Itoa(maxRegionNameRunes) +
				" characters without control characters")
		}
		body["displayName"] = *a.DisplayName
	}
	set := a.areas()
	maskedAreas := 0
	for _, p := range regionAreaPaths {
		if slices.Contains(mask, p) {
			maskedAreas++
		}
	}
	switch {
	case len(set) > 1:
		return nil, nil, invalidArgument("at most one of postal_code_area, geotarget_area, or radius_area can be set")
	case len(set) == 1 && !slices.Contains(mask, set[0]):
		return nil, nil, invalidArgument("an area that is set must be named in update_mask")
	case maskedAreas > 0 && len(set) != 1:
		return nil, nil, invalidArgument("an area in update_mask needs one area block, because a region has exactly one area")
	}
	if err := areaBody(a, body); err != nil {
		return nil, nil, err
	}
	return mask, body, nil
}

func regionsPath(account string) string { return accountPath(account) + "/regions" }

func regionPath(account, id string) string { return regionsPath(account) + "/" + id }

// regionCall is one handler run: the arguments are read and the account is settled against the binding before
// any secret is resolved.
type regionCall struct {
	op, tool   string
	args       regionArguments
	account    string
	needsProof bool
	scope      scope
}

func prepareRegion(op, tool string, resolved *config.Resolved, raw json.RawMessage) (regionCall, error) {
	c := regionCall{op: op, tool: tool}
	if err := json.Unmarshal(raw, &c.args); err != nil {
		return c, provider.Fail(op, "the validated arguments could not be read")
	}
	var err error
	if c.scope, err = checkConnection(resolved); err != nil {
		return c, err
	}
	c.account, c.needsProof, err = c.scope.accountOf(c.args.Account)
	return c, err
}

// regionClient opens the connection and, for an account that no target names, proves that it belongs to a
// bound advanced account before any detail is read or anything is changed.
func (c regionCall) client(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (*client, error) {
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if c.needsProof {
		if err := cl.prove(ctx, c.op, c.tool, c.account); err != nil {
			return nil, err
		}
	}
	return cl, nil
}

func handleRegionsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepareRegion("list regions", regionsListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(c.tool, c.account, c.args.PageSize, c.scope.key())
	query := url.Values{}
	if c.args.PageSize > 0 {
		query.Set("pageSize", strconv.Itoa(c.args.PageSize))
	}
	if c.args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, c.args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer regionsAnswer
	if err := cl.get(ctx, c.op, c.tool, regionsPath(c.account), query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Regions) > maxRegionRows {
		return nil, tooMany(c.op)
	}
	out := regionsResult{Regions: answer.Regions}
	if out.Regions == nil {
		out.Regions = []region{}
	}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleRegionsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepareRegion("get region", regionsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if !validRegionID(c.args.RegionID) {
		return nil, invalidArgument("region_id must be 1 to 64 ASCII letters, digits, dashes, and underscores")
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out region
	if err := cl.get(ctx, c.op, c.tool, regionPath(c.account, c.args.RegionID), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// changeRegion sends one change, once, and reports only that Google accepted it.
func (cl *client) changeRegion(ctx context.Context, op, tool, method, path string, query url.Values,
	body map[string]any) (regionChangeResult, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return regionChangeResult{}, invalidArgument("the arguments could not be encoded")
	}
	var out region
	if err := cl.send(ctx, op, tool, request{method: method, path: path, query: query, body: encoded, change: true},
		&out); err != nil {
		return regionChangeResult{}, err
	}
	return regionChangeResult{Accepted: true, Region: out}, nil
}

func handleRegionsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareRegion("create region", regionsCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	body, err := createRegionBody(c.args)
	if err != nil {
		return nil, err
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return cl.changeRegion(ctx, c.op, c.tool, http.MethodPost, regionsPath(c.account),
		url.Values{"regionId": {c.args.RegionID}}, body)
}

func handleRegionsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareRegion("update region", regionsUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	mask, body, err := updateRegionRequest(c.args)
	if err != nil {
		return nil, err
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return cl.changeRegion(ctx, c.op, c.tool, http.MethodPatch, regionPath(c.account, c.args.RegionID),
		url.Values{"updateMask": {strings.Join(mask, ",")}}, body)
}

// The summaries phrase the confirmation from the validated arguments alone.
func regionCreateSummary(raw json.RawMessage) string {
	var a regionArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Create region " + strconv.Quote(a.RegionID)
	if a.DisplayName != nil {
		out += " " + strconv.Quote(*a.DisplayName)
	}
	if a.Account != "" {
		out += " in account " + a.Account
	}
	if set := a.areas(); len(set) == 1 {
		out += " with a " + set[0]
	}
	return out + "; Google makes it usable afterwards"
}

func regionUpdateSummary(raw json.RawMessage) string {
	var a regionArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Update region " + strconv.Quote(a.RegionID)
	if a.Account != "" {
		out += " of account " + a.Account
	}
	out += ": replace " + strings.Join(a.UpdateMask, ", ")
	if a.DisplayName != nil {
		out += " (new name " + strconv.Quote(*a.DisplayName) + ")"
	}
	return out + "; Google applies it afterwards, and shipping or inventory that uses the region follows it"
}

// regionChangeRisk is the risk of every changing tool: confirmed, and open to the account.
func regionChangeRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true, DataSensitivity: "merchant-account-data"}
}

func countOf(list []string, want string) (n int) {
	for _, item := range list {
		if item == want {
			n++
		}
	}
	return n
}
