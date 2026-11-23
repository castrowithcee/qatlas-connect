package googlemerchant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	regionsDeleteID      = Provider + ".regions.delete"
	regionsBatchCreateID = Provider + ".regions.batch-create"
	regionsBatchUpdateID = Provider + ".regions.batch-update"
	regionsBatchDeleteID = Provider + ".regions.batch-delete"
)

// maxRegionBatch is the documented maximum of regions in one batch call (discovery and region batching guide).
const maxRegionBatch = 100

// regionEntry is one entry of a batch: the arguments of a single region without the account.
type regionEntry struct {
	RegionID       string              `json:"region_id"`
	DisplayName    *string             `json:"display_name"`
	PostalCodeArea *postalAreaInput    `json:"postal_code_area"`
	GeotargetArea  *geotargetAreaInput `json:"geotarget_area"`
	RadiusArea     *radiusAreaInput    `json:"radius_area"`
	UpdateMask     []string            `json:"update_mask"`
}

func (e regionEntry) arguments() regionArguments {
	return regionArguments{RegionID: e.RegionID, DisplayName: e.DisplayName, PostalCodeArea: e.PostalCodeArea,
		GeotargetArea: e.GeotargetArea, RadiusArea: e.RadiusArea, UpdateMask: e.UpdateMask}
}

type regionBatchArguments struct {
	Regions   []regionEntry `json:"regions"`
	RegionIDs []string      `json:"region_ids"`
}

// regionBatchList keeps at most maxRegionBatch regions of an answer; an entry that is not an object is dropped.
type regionBatchList []region

func (l *regionBatchList) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		*l = nil
		return nil
	}
	var out regionBatchList
	for _, item := range raw {
		if len(out) == maxRegionBatch {
			break
		}
		var r region
		if json.Unmarshal(item, &r) == nil {
			out = append(out, r)
		}
	}
	*l = out
	return nil
}

type regionBatchAnswer struct {
	Regions regionBatchList `json:"regions"`
}

// regionBatchResult says that Google accepted the one request that carried all entries.
type regionBatchResult struct {
	Accepted  bool     `json:"accepted"`
	Requested int      `json:"requested"`
	Regions   []region `json:"regions"`
}

type regionDeleteResult struct {
	Accepted bool   `json:"accepted"`
	Name     string `json:"name"`
}

type regionBatchDeleteResult struct {
	Accepted  bool     `json:"accepted"`
	Requested int      `json:"requested"`
	Names     []string `json:"names"`
}

// entryRefused names the entry of a batch that was refused, without quoting its content.
func entryRefused(i int, err error) error {
	var bad *provider.InvalidRequestError
	if errors.As(err, &bad) {
		return invalidArgument("regions[" + strconv.Itoa(i) + "]: " + bad.Message)
	}
	return err
}

func checkBatchSize(n int, what string) error {
	if n < 1 || n > maxRegionBatch {
		return invalidArgument(what + " must hold between 1 and " + strconv.Itoa(maxRegionBatch) + " entries")
	}
	return nil
}

// distinctRegionIDs refuses a repeated ID. IDs are compared without case, because Google documents no rule for
// it and a batch must never address one region twice.
func distinctRegionIDs(ids []string) error {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		key := strings.ToLower(id)
		if seen[key] {
			return invalidArgument("a region ID must not appear twice in one batch")
		}
		seen[key] = true
	}
	return nil
}

func readBatch(c regionCall, raw json.RawMessage) (regionBatchArguments, error) {
	var in regionBatchArguments
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, provider.Fail(c.op, "the validated arguments could not be read")
	}
	return in, nil
}

// changeBatch sends the one request of a batch, once. The body holds only the checked requests.
func (cl *client) changeBatch(ctx context.Context, c regionCall, method string, requests []map[string]any,
	out any) error {
	encoded, err := json.Marshal(map[string]any{"requests": requests})
	if err != nil {
		return invalidArgument("the arguments could not be encoded")
	}
	return cl.send(ctx, c.op, c.tool, request{method: http.MethodPost, path: regionsPath(c.account) + ":" + method,
		body: encoded, change: true}, out)
}

func batchResult(requested int, answer regionBatchAnswer) regionBatchResult {
	out := regionBatchResult{Accepted: true, Requested: requested, Regions: answer.Regions}
	if out.Regions == nil {
		out.Regions = []region{}
	}
	return out
}

func handleRegionsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareRegion("delete region", regionsDeleteID, resolved, raw)
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
	path := regionPath(c.account, c.args.RegionID)
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodDelete, path: path, change: true}, nil); err != nil {
		return nil, err
	}
	return regionDeleteResult{Accepted: true, Name: path[len(accountsAPI):]}, nil
}

func handleRegionsBatchCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareRegion("batch create regions", regionsBatchCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	in, err := readBatch(c, raw)
	if err != nil {
		return nil, err
	}
	if err := checkBatchSize(len(in.Regions), "regions"); err != nil {
		return nil, err
	}
	ids := make([]string, len(in.Regions))
	requests := make([]map[string]any, len(in.Regions))
	for i, e := range in.Regions {
		if len(e.UpdateMask) > 0 {
			return nil, entryRefused(i, invalidArgument("update_mask belongs to batch-update"))
		}
		body, err := createRegionBody(e.arguments())
		if err != nil {
			return nil, entryRefused(i, err)
		}
		ids[i] = e.RegionID
		// parent comes from the checked account only.
		requests[i] = map[string]any{"parent": accountPath(c.account)[len(accountsAPI):], "regionId": e.RegionID,
			"region": body}
	}
	if err := distinctRegionIDs(ids); err != nil {
		return nil, err
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer regionBatchAnswer
	if err := cl.changeBatch(ctx, c, "batchCreate", requests, &answer); err != nil {
		return nil, err
	}
	return batchResult(len(requests), answer), nil
}

func handleRegionsBatchUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareRegion("batch update regions", regionsBatchUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	in, err := readBatch(c, raw)
	if err != nil {
		return nil, err
	}
	if err := checkBatchSize(len(in.Regions), "regions"); err != nil {
		return nil, err
	}
	ids := make([]string, len(in.Regions))
	requests := make([]map[string]any, len(in.Regions))
	for i, e := range in.Regions {
		mask, body, err := updateRegionRequest(e.arguments())
		if err != nil {
			return nil, entryRefused(i, err)
		}
		ids[i] = e.RegionID
		body["name"] = regionPath(c.account, e.RegionID)[len(accountsAPI):]
		requests[i] = map[string]any{"region": body, "updateMask": strings.Join(mask, ",")}
	}
	if err := distinctRegionIDs(ids); err != nil {
		return nil, err
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer regionBatchAnswer
	if err := cl.changeBatch(ctx, c, "batchUpdate", requests, &answer); err != nil {
		return nil, err
	}
	return batchResult(len(requests), answer), nil
}

func handleRegionsBatchDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepareRegion("batch delete regions", regionsBatchDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	in, err := readBatch(c, raw)
	if err != nil {
		return nil, err
	}
	if err := checkBatchSize(len(in.RegionIDs), "region_ids"); err != nil {
		return nil, err
	}
	requests := make([]map[string]any, len(in.RegionIDs))
	names := make([]string, len(in.RegionIDs))
	for i, id := range in.RegionIDs {
		if !validRegionID(id) {
			return nil, invalidArgument("region_ids[" + strconv.Itoa(i) +
				"] must be 1 to 64 ASCII letters, digits, dashes, and underscores")
		}
		names[i] = regionPath(c.account, id)[len(accountsAPI):]
		requests[i] = map[string]any{"name": names[i]}
	}
	if err := distinctRegionIDs(in.RegionIDs); err != nil {
		return nil, err
	}
	cl, err := c.client(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.changeBatch(ctx, c, "batchDelete", requests, nil); err != nil {
		return nil, err
	}
	return regionBatchDeleteResult{Accepted: true, Requested: len(requests), Names: names}, nil
}

func regionDeleteSummary(raw json.RawMessage) string {
	var a regionArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	out := "Delete region " + strconv.Quote(a.RegionID)
	if a.Account != "" {
		out += " of account " + a.Account
	}
	return out + "; shipping settings and regional inventory that use it can be affected, and Qatlas offers no way " +
		"to undo it"
}

func batchSummary(verb string, count int, ids []string, account string) string {
	out := verb + " " + strconv.Itoa(count) + " regions"
	if account != "" {
		out += " of account " + account
	}
	if len(ids) > 0 {
		shown := ids
		if len(shown) > 10 {
			shown = shown[:10]
		}
		quoted := make([]string, len(shown))
		for i, id := range shown {
			quoted[i] = strconv.Quote(id)
		}
		out += " (" + strings.Join(quoted, ", ")
		if len(ids) > len(shown) {
			out += ", and " + strconv.Itoa(len(ids)-len(shown)) + " more"
		}
		out += ")"
	}
	return out + " in one request"
}

func regionBatchSummary(verb, tail string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a struct {
			Account string `json:"account"`
			regionBatchArguments
		}
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		ids := a.RegionIDs
		count := len(a.RegionIDs)
		if len(a.Regions) > 0 {
			count, ids = len(a.Regions), make([]string, len(a.Regions))
			for i, e := range a.Regions {
				ids[i] = e.RegionID
			}
		}
		return batchSummary(verb, count, ids, a.Account) + tail
	}
}

func regionBatchSummaries() map[string]capability.SummaryFunc {
	return map[string]capability.SummaryFunc{
		regionsBatchCreateID: regionBatchSummary("Create", "; Google applies all or none, and makes them usable afterwards"),
		regionsBatchUpdateID: regionBatchSummary("Replace members of", "; Google applies all or none, and shipping or "+
			"inventory that uses the regions follows"),
		regionsBatchDeleteID: regionBatchSummary("Delete", "; shipping settings and regional inventory that use the "+
			"regions can be affected, and Qatlas offers no way to undo it"),
	}
}
