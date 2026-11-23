package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The six methods of ReachPlanService of API version v26. All of them read: a forecast describes a plan and
// creates nothing, so no tool of this group has a validate_only or a confirmation path.
const (
	reachPlansListLocationsID           = Provider + ".reach-plans.list-locations"
	reachPlansListProductsID            = Provider + ".reach-plans.list-products"
	reachPlansGenerateForecastID        = Provider + ".reach-plans.generate-forecast"
	reachPlansGenerateConversionRatesID = Provider + ".reach-plans.generate-conversion-rates"
	reachPlansListUserListsID           = Provider + ".reach-plans.list-user-lists"
	reachPlansListUserInterestsID       = Provider + ".reach-plans.list-user-interests"

	// maxReachProducts is the documented maximum of planned products of one forecast.
	maxReachProducts = 15
	// maxReachCampaignDays is the documented longest date range of a forecast, both ends inclusive.
	maxReachCampaignDays = 92
	maxReachQuery        = 200
	maxReachList         = 100
	maxInt32             = 2147483647
)

const (
	reachIDPatternText       = `^[0-9]{1,20}$`
	reachCodePatternText     = `^[A-Za-z0-9_]{1,64}$`
	reachAppIDPatternText    = `^[A-Za-z0-9_.-]{1,128}$`
	reachDatePatternText     = `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`
	reachCurrencyPatternText = `^[A-Z]{3}$`
)

var (
	reachIDPattern    = regexp.MustCompile(reachIDPatternText)
	reachCodePattern  = regexp.MustCompile(reachCodePatternText)
	reachAppIDPattern = regexp.MustCompile(reachAppIDPatternText)
	reachCurrency     = regexp.MustCompile(reachCurrencyPatternText)
)

// Enum values of v26 that a request may name; UNSPECIFIED and UNKNOWN are answer values only.
var (
	reachAgeRanges = []string{"AGE_RANGE_18_24", "AGE_RANGE_18_34", "AGE_RANGE_18_44", "AGE_RANGE_18_49",
		"AGE_RANGE_18_54", "AGE_RANGE_18_64", "AGE_RANGE_18_65_UP", "AGE_RANGE_21_34", "AGE_RANGE_21_44",
		"AGE_RANGE_21_49", "AGE_RANGE_21_54", "AGE_RANGE_21_64", "AGE_RANGE_21_65_UP", "AGE_RANGE_25_34",
		"AGE_RANGE_25_44", "AGE_RANGE_25_49", "AGE_RANGE_25_54", "AGE_RANGE_25_64", "AGE_RANGE_25_65_UP",
		"AGE_RANGE_35_44", "AGE_RANGE_35_49", "AGE_RANGE_35_54", "AGE_RANGE_35_64", "AGE_RANGE_35_65_UP",
		"AGE_RANGE_45_54", "AGE_RANGE_45_64", "AGE_RANGE_45_65_UP", "AGE_RANGE_50_65_UP", "AGE_RANGE_55_64",
		"AGE_RANGE_55_65_UP", "AGE_RANGE_65_UP"}
	reachNetworks       = []string{"YOUTUBE", "GOOGLE_VIDEO_PARTNERS", "YOUTUBE_AND_GOOGLE_VIDEO_PARTNERS"}
	reachDevices        = []string{"CONNECTED_TV", "DESKTOP", "MOBILE", "OTHER", "TABLET"}
	reachGenders        = []string{"FEMALE", "MALE", "UNDETERMINED"}
	reachParentalStatus = []string{"NOT_A_PARENT", "PARENT", "UNDETERMINED"}
	reachSurfaces       = []string{"DISCOVER_FEED", "GMAIL", "GOOGLE_DISPLAY_NETWORK", "IN_FEED", "IN_STREAM_BUMPER",
		"IN_STREAM_NON_SKIPPABLE", "IN_STREAM_NON_SKIPPABLE_THIRTY_SECONDS", "IN_STREAM_SKIPPABLE", "SHORTS"}
	reachCapUnits      = []string{"DAY", "WEEK", "MONTH"}
	reachTargetUnits   = []string{"WEEKLY", "MONTHLY"}
	reachInterestTypes = []string{"AFFINITY", "IN_MARKET"}
	reachAppInstances  = []string{"PRODUCTION", "DEVELOPMENT_AND_TESTING"}
)

// --- arguments ---

type (
	reachAppInfo struct {
		ApplicationID       string `json:"application_id,omitempty"`
		ApplicationInstance string `json:"application_instance,omitempty"`
	}
	reachTypeValue struct {
		Type string `json:"type"`
	}
	reachDateRange struct {
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	reachDuration struct {
		DateRange      *reachDateRange `json:"date_range,omitempty"`
		DurationInDays *int            `json:"duration_in_days,omitempty"`
	}
	reachFrequencyCap struct {
		Impressions int    `json:"impressions"`
		TimeUnit    string `json:"time_unit"`
	}
	reachTargetFrequency struct {
		TargetFrequency int    `json:"target_frequency"`
		TimeUnit        string `json:"time_unit"`
	}
	reachAdvancedTargeting struct {
		YouTubeSelectSettings *struct {
			LineupID *int64 `json:"lineup_id,omitempty"`
		} `json:"youtube_select_settings,omitempty"`
		SurfaceTargetingSettings *struct {
			Surfaces []string `json:"surfaces,omitempty"`
		} `json:"surface_targeting_settings,omitempty"`
		TargetFrequencySettings *reachTargetFrequency `json:"target_frequency_settings,omitempty"`
	}
	reachProduct struct {
		PlannableProductCode    string                  `json:"plannable_product_code"`
		BudgetMicros            json.Number             `json:"budget_micros"`
		ConversionRate          *float64                `json:"conversion_rate,omitempty"`
		AdvancedProductTargeted *reachAdvancedTargeting `json:"advanced_product_targeting,omitempty"`
	}
	reachTargeting struct {
		AgeRange          string `json:"age_range,omitempty"`
		AudienceTargeting *struct {
			UserInterest []struct {
				UserInterestCategory string `json:"user_interest_category"`
			} `json:"user_interest,omitempty"`
			UserLists []struct {
				UserList string `json:"user_list"`
			} `json:"user_lists,omitempty"`
		} `json:"audience_targeting,omitempty"`
		Devices              []reachTypeValue `json:"devices,omitempty"`
		Genders              []reachTypeValue `json:"genders,omitempty"`
		Network              string           `json:"network,omitempty"`
		ParentalStatuses     []reachTypeValue `json:"parental_statuses,omitempty"`
		PlannableLocationIDs []string         `json:"plannable_location_ids,omitempty"`
	}
	reachForecastArguments struct {
		CustomerID                string             `json:"customer_id"`
		CampaignDuration          reachDuration      `json:"campaign_duration"`
		CookieFrequencyCapSetting *reachFrequencyCap `json:"cookie_frequency_cap_setting,omitempty"`
		CurrencyCode              string             `json:"currency_code,omitempty"`
		CustomerReachGroup        string             `json:"customer_reach_group,omitempty"`
		EffectiveFrequencyLimit   *struct {
			Limit int `json:"effective_frequency_breakdown_limit"`
		} `json:"effective_frequency_limit,omitempty"`
		ForecastMetricOptions *struct {
			IncludeCoview bool `json:"include_coview"`
		} `json:"forecast_metric_options,omitempty"`
		MinEffectiveFrequency *int            `json:"min_effective_frequency,omitempty"`
		PlannedProducts       []reachProduct  `json:"planned_products"`
		ReachApplicationInfo  *reachAppInfo   `json:"reach_application_info,omitempty"`
		Targeting             *reachTargeting `json:"targeting,omitempty"`
	}
	reachLocationsArguments struct {
		ReachApplicationInfo *reachAppInfo `json:"reach_application_info,omitempty"`
	}
	reachProductsArguments struct {
		PlannableLocationID  string        `json:"plannable_location_id"`
		ReachApplicationInfo *reachAppInfo `json:"reach_application_info,omitempty"`
	}
	// reachCustomerArguments serve conversion-rates and user-lists, which take the account and a group name.
	reachCustomerArguments struct {
		CustomerID           string        `json:"customer_id"`
		CustomerReachGroup   string        `json:"customer_reach_group,omitempty"`
		ReachApplicationInfo *reachAppInfo `json:"reach_application_info,omitempty"`
	}
	reachInterestsArguments struct {
		CustomerID                string        `json:"customer_id"`
		NameQuery                 string        `json:"name_query,omitempty"`
		PathQuery                 string        `json:"path_query,omitempty"`
		ReachApplicationInfo      *reachAppInfo `json:"reach_application_info,omitempty"`
		UserInterestTaxonomyTypes []string      `json:"user_interest_taxonomy_types,omitempty"`
	}
)

// --- checks, before any secret is read ---

func (a *reachAppInfo) check() error {
	if a == nil {
		return nil
	}
	if a.ApplicationID != "" && !reachAppIDPattern.MatchString(a.ApplicationID) {
		return badRequest("reach_application_info.application_id must be 1 to 128 letters, digits, dots, hyphens or underscores")
	}
	if a.ApplicationInstance != "" && !slices.Contains(reachAppInstances, a.ApplicationInstance) {
		return badRequest("reach_application_info.application_instance must be PRODUCTION or DEVELOPMENT_AND_TESTING")
	}
	return nil
}

func reachCheckGroup(group string) error {
	if group == "" {
		return nil
	}
	return recCheckText("customer_reach_group", group, maxReachQuery)
}

// reachArgs is what the shared handler needs of the arguments of a tool: the account it binds, or none, and the
// checks that need no request.
type reachArgs interface {
	customer() string
	check() error
	// keepCustomer says whether customerId is a member of the body; the forecast carries it in the path.
	keepCustomer() bool
}

func (a *reachLocationsArguments) customer() string   { return "" }
func (a *reachLocationsArguments) check() error       { return a.ReachApplicationInfo.check() }
func (a *reachLocationsArguments) keepCustomer() bool { return false }
func (a *reachProductsArguments) customer() string    { return "" }
func (a *reachProductsArguments) keepCustomer() bool  { return false }
func (a *reachCustomerArguments) customer() string    { return a.CustomerID }
func (a *reachCustomerArguments) keepCustomer() bool  { return true }
func (a *reachInterestsArguments) customer() string   { return a.CustomerID }
func (a *reachInterestsArguments) keepCustomer() bool { return true }
func (a *reachForecastArguments) customer() string    { return a.CustomerID }
func (a *reachForecastArguments) keepCustomer() bool  { return false }

func (a *reachProductsArguments) check() error {
	if !reachIDPattern.MatchString(a.PlannableLocationID) {
		return badRequest("plannable_location_id must be the numeric ID of a plannable location")
	}
	return a.ReachApplicationInfo.check()
}

func (a *reachCustomerArguments) check() error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := reachCheckGroup(a.CustomerReachGroup); err != nil {
		return err
	}
	return a.ReachApplicationInfo.check()
}

func (a *reachInterestsArguments) check() error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	for name, value := range map[string]string{"name_query": a.NameQuery, "path_query": a.PathQuery} {
		if value != "" {
			if err := recCheckText(name, value, maxReachQuery); err != nil {
				return err
			}
		}
	}
	seen := map[string]bool{}
	for _, kind := range a.UserInterestTaxonomyTypes {
		if !slices.Contains(reachInterestTypes, kind) || seen[kind] {
			return badRequest("user_interest_taxonomy_types may name AFFINITY and IN_MARKET, each once")
		}
		seen[kind] = true
	}
	return a.ReachApplicationInfo.check()
}

// reachCheckResource refuses a name that is not customers/ID/<collection>/NUMBER of exactly the checked account.
func reachCheckResource(argument, customerID, collection, name string) error {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+collection+"/")
	if !ok || !reachIDPattern.MatchString(rest) {
		return badRequest(argument + " must be a " + collection + " name of the account of customer_id, " +
			"customers/CUSTOMER_ID/" + collection + "/ID")
	}
	return nil
}

func (a *reachForecastArguments) check() error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := reachCheckGroup(a.CustomerReachGroup); err != nil {
		return err
	}
	if err := a.ReachApplicationInfo.check(); err != nil {
		return err
	}
	if err := a.checkDuration(); err != nil {
		return err
	}
	if a.CurrencyCode != "" && !reachCurrency.MatchString(a.CurrencyCode) {
		return badRequest("currency_code must be a three-letter ISO 4217 code in capitals")
	}
	if a.MinEffectiveFrequency != nil && a.EffectiveFrequencyLimit != nil {
		return badRequest("min_effective_frequency cannot be combined with effective_frequency_limit")
	}
	if a.MinEffectiveFrequency != nil && (*a.MinEffectiveFrequency < 1 || *a.MinEffectiveFrequency > 10) {
		return badRequest("min_effective_frequency must be 1 to 10")
	}
	if l := a.EffectiveFrequencyLimit; l != nil && (l.Limit < 1 || l.Limit > 10) {
		return badRequest("effective_frequency_limit.effective_frequency_breakdown_limit must be 1 to 10")
	}
	if c := a.CookieFrequencyCapSetting; c != nil && (c.Impressions < 1 || !slices.Contains(reachCapUnits, c.TimeUnit)) {
		return badRequest("cookie_frequency_cap_setting needs impressions of at least 1 and a time_unit of DAY, WEEK or MONTH")
	}
	if len(a.PlannedProducts) < 1 || len(a.PlannedProducts) > maxReachProducts {
		return badRequest("planned_products must name 1 to 15 products")
	}
	for _, p := range a.PlannedProducts {
		if err := p.check(); err != nil {
			return err
		}
	}
	return a.checkTargeting()
}

func (a *reachForecastArguments) checkDuration() error {
	d := a.CampaignDuration
	if (d.DateRange == nil) == (d.DurationInDays == nil) {
		return badRequest("campaign_duration needs exactly one of date_range and duration_in_days")
	}
	if d.DurationInDays != nil {
		if *d.DurationInDays < 1 || *d.DurationInDays > maxInt32 {
			return badRequest("campaign_duration.duration_in_days must be at least 1")
		}
		return nil
	}
	start, errStart := time.Parse(time.DateOnly, d.DateRange.StartDate)
	end, errEnd := time.Parse(time.DateOnly, d.DateRange.EndDate)
	if errStart != nil || errEnd != nil {
		return badRequest("campaign_duration.date_range needs start_date and end_date as yyyy-mm-dd")
	}
	if days := int(end.Sub(start).Hours()/24) + 1; days < 1 || days > maxReachCampaignDays {
		return badRequest("campaign_duration.date_range must span 1 to 92 days, both ends inclusive")
	}
	return nil
}

func (p reachProduct) check() error {
	if !reachCodePattern.MatchString(p.PlannableProductCode) {
		return badRequest("planned_products[].plannable_product_code must be a product code such as VIDEO_VIEW_CAMPAIGN")
	}
	if n, err := p.BudgetMicros.Int64(); err != nil || n < 1 || n > recMaxMicros {
		return badRequest("planned_products[].budget_micros must be a whole number of micros of at least 1")
	}
	if p.ConversionRate != nil {
		if err := recCheckFinite("planned_products[].conversion_rate", *p.ConversionRate, 0, 1, true, true); err != nil {
			return err
		}
	}
	t := p.AdvancedProductTargeted
	if t == nil {
		return nil
	}
	set := 0
	if t.YouTubeSelectSettings != nil {
		set++
		if l := t.YouTubeSelectSettings.LineupID; l == nil || *l < 0 {
			return badRequest("advanced_product_targeting.youtube_select_settings needs a lineup_id")
		}
	}
	if s := t.SurfaceTargetingSettings; s != nil {
		set++
		if len(s.Surfaces) < 1 || len(s.Surfaces) > len(reachSurfaces) {
			return badRequest("advanced_product_targeting.surface_targeting_settings needs surfaces")
		}
		for _, surface := range s.Surfaces {
			if !slices.Contains(reachSurfaces, surface) {
				return badRequest("advanced_product_targeting.surface_targeting_settings names an unknown surface")
			}
		}
	}
	if f := t.TargetFrequencySettings; f != nil {
		set++
		if f.TargetFrequency < 1 || !slices.Contains(reachTargetUnits, f.TimeUnit) {
			return badRequest("advanced_product_targeting.target_frequency_settings needs target_frequency of at " +
				"least 1 and a time_unit of WEEKLY or MONTHLY")
		}
	}
	if set != 1 {
		return badRequest("advanced_product_targeting names exactly one of youtube_select_settings, " +
			"surface_targeting_settings and target_frequency_settings")
	}
	return nil
}

func (a *reachForecastArguments) checkTargeting() error {
	t := a.Targeting
	if t == nil {
		return nil
	}
	if t.AgeRange != "" && !slices.Contains(reachAgeRanges, t.AgeRange) {
		return badRequest("targeting.age_range is not an age range of ReachPlanAgeRange")
	}
	if t.Network != "" && !slices.Contains(reachNetworks, t.Network) {
		return badRequest("targeting.network must be YOUTUBE, GOOGLE_VIDEO_PARTNERS or YOUTUBE_AND_GOOGLE_VIDEO_PARTNERS")
	}
	for argument, list := range map[string]struct {
		items  []reachTypeValue
		values []string
	}{"devices": {t.Devices, reachDevices}, "genders": {t.Genders, reachGenders},
		"parental_statuses": {t.ParentalStatuses, reachParentalStatus}} {
		if len(list.items) > len(list.values) {
			return badRequest("targeting." + argument + " names a value more than once")
		}
		for _, item := range list.items {
			if !slices.Contains(list.values, item.Type) {
				return badRequest("targeting." + argument + "[].type is not a documented value")
			}
		}
	}
	if len(t.PlannableLocationIDs) > maxReachList {
		return badRequest("targeting.plannable_location_ids may name up to 100 locations")
	}
	for _, id := range t.PlannableLocationIDs {
		if !reachIDPattern.MatchString(id) {
			return badRequest("targeting.plannable_location_ids must be numeric location IDs")
		}
	}
	if at := t.AudienceTargeting; at != nil {
		if len(at.UserInterest) > maxReachList || len(at.UserLists) > maxReachList {
			return badRequest("targeting.audience_targeting may name up to 100 user interests and 100 user lists")
		}
		for _, u := range at.UserInterest {
			if err := reachCheckResource("targeting.audience_targeting.user_interest[].user_interest_category",
				a.CustomerID, "userInterests", u.UserInterestCategory); err != nil {
				return err
			}
		}
		for _, u := range at.UserLists {
			if err := reachCheckResource("targeting.audience_targeting.user_lists[].user_list",
				a.CustomerID, "userLists", u.UserList); err != nil {
				return err
			}
		}
	}
	return nil
}

// --- request and answer ---

// reachCamel turns a member name of the reference, such as plannable_location_ids, into the one of the JSON
// mapping, plannableLocationIds.
func reachCamel(name string) string {
	parts := strings.Split(name, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func reachRename(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for name, child := range v {
			out[reachCamel(name)] = reachRename(child)
		}
		return out
	case []any:
		for i, child := range v {
			v[i] = reachRename(child)
		}
	}
	return value
}

// reachBody is the request body: the checked arguments under the member names of the JSON mapping. Numbers keep
// their text, so a budget in micros loses no digit.
func reachBody(args reachArgs) ([]byte, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var members map[string]any
	if err := decoder.Decode(&members); err != nil {
		return nil, err
	}
	if !args.keepCustomer() {
		delete(members, "customer_id")
	}
	return json.Marshal(reachRename(members))
}

// reachAnswer cleans the answer of Google into bounded plain data under the member names of the reference.
func reachAnswer(op, customerID string, raw json.RawMessage) (map[string]any, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, false, provider.InvalidResponse(op, subject+" returned a reach plan answer Qatlas Connect cannot read")
	}
	cleaner := &recommendationCleaner{op: op, customerID: customerID, what: "reach plan answer"}
	cleaned, _, err := cleaner.clean(value, 0)
	if err != nil {
		return nil, false, err
	}
	return cleaned.(map[string]any), cleaner.cut, nil
}

// reachCall is what the six tools differ in.
type reachCall struct {
	op, tool string
	// path is the path below the version; the account is the customer of the arguments, empty when none.
	path func(customerID string) string
}

// reachHandler is the handler of a tool: arguments, checks, target binding of the account when there is one,
// one POST, and the bounded answer. An account of the call is bound to the targets of the connection before a
// secret is read; a tool without an account touches no target, like fields.get.
func reachHandler[T any, P interface {
	*T
	reachArgs
}](rc reachCall) func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor, json.RawMessage) (any, error) {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, failure(rc.op, "the validated arguments could not be read")
		}
		args := P(&value)
		env, err := handleEnv(ctx, resolved, secrets, red, rc.op, rc.tool)
		if err != nil {
			return nil, err
		}
		if err := args.check(); err != nil {
			return nil, err
		}
		body, err := reachBody(args)
		if err != nil {
			return nil, failure(rc.op, "the request body could not be encoded")
		}
		id := args.customer()
		var c *client
		if id != "" {
			c, err = env.settle(id, false)
		} else {
			c, err = env.open()
		}
		if err != nil {
			return nil, err
		}
		var answer json.RawMessage
		if err := c.read(ctx, request{op: rc.op, tool: rc.tool, method: http.MethodPost, path: rc.path(id),
			body: body, onAccount: id != "", judgesInput: true}, &answer); err != nil {
			return nil, err
		}
		out, cut, err := reachAnswer(rc.op, id, answer)
		if err != nil {
			return nil, err
		}
		if id != "" {
			out["customer_id"] = id
		}
		if cut {
			out["truncated"] = true
		}
		return out, nil
	}
}

var (
	handleReachListLocations = reachHandler[reachLocationsArguments](reachCall{op: "list plannable locations",
		tool: reachPlansListLocationsID, path: func(string) string { return ":listPlannableLocations" }})
	handleReachListProducts = reachHandler[reachProductsArguments](reachCall{op: "list plannable products",
		tool: reachPlansListProductsID, path: func(string) string { return ":listPlannableProducts" }})
	handleReachGenerateForecast = reachHandler[reachForecastArguments](reachCall{op: "generate reach forecast",
		tool: reachPlansGenerateForecastID, path: func(id string) string { return "/" + customerName(id) + ":generateReachForecast" }})
	handleReachGenerateConversionRates = reachHandler[reachCustomerArguments](reachCall{op: "generate conversion rates",
		tool: reachPlansGenerateConversionRatesID, path: func(string) string { return ":generateConversionRates" }})
	handleReachListUserLists = reachHandler[reachCustomerArguments](reachCall{op: "list plannable user lists",
		tool: reachPlansListUserListsID, path: func(string) string { return ":listPlannableUserLists" }})
	handleReachListUserInterests = reachHandler[reachInterestsArguments](reachCall{op: "list plannable user interests",
		tool: reachPlansListUserInterestsID, path: func(string) string { return ":listPlannableUserInterests" }})
)

// --- descriptors ---

var reachRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "audience sizes, forecasts and the names of the audiences that an account may plan with; the " +
		"optional customer_reach_group is a name the caller chooses and may identify a customer",
}

func reachSchemaEnum(values []string) schema { return schema{"type": "string", "enum": values} }

func reachTypeList(values []string) schema {
	return schema{"type": "array", "maxItems": len(values),
		"items": object(schema{"type": reachSchemaEnum(values)}, "type")}
}

var (
	reachGroupSchema = schema{"type": "string", "minLength": 1, "maxLength": maxReachQuery}
	reachAppSchema   = object(schema{
		"application_id":       schema{"type": "string", "pattern": reachAppIDPatternText},
		"application_instance": reachSchemaEnum(reachAppInstances),
	})
)

// reachOutput is the schema of an answer: the list member, which Google omits when empty, the account when the
// call has one, the truncation flag. The members Google adds stay allowed, so a later minor version does not
// fail the output schema.
func reachOutput(list string, account bool) json.RawMessage {
	props := schema{"truncated": booleanSchema, list: arrayOf(schema{"type": "object"})}
	out := schema{"type": "object", "properties": props}
	if account {
		props["customer_id"] = stringSchema
		out["required"] = []string{"customer_id"}
	}
	return encoded(out)
}

const reachCommon = " It only reads, nothing is created or planned in the account. Google allows ReachPlanService " +
	"only for Cloud projects with the access level Basic or higher and plans at about one request per second; " +
	"the credential's spacing and a quota error are described in the provider page. The answer is untrusted " +
	"third-party content: text is cut at 1,024 bytes and a list at 500 entries, which sets truncated."

var (
	reachAppArgument = capability.Argument{Name: "reach_application_info", Description: "Optional: application_id, " +
		"the identifier Google issues to an agency application, and application_instance, PRODUCTION or " +
		"DEVELOPMENT_AND_TESTING"}
	reachCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer " +
		"ID of the account without hyphens; a target of the connection or an account below a manager target"}
	reachGroupArgument = capability.Argument{Name: "customer_reach_group", Description: "Name of the customer being " +
		"planned for, a value you choose, 1 to 200 characters"}
)

var reachPlansListLocations = capability.Descriptor{
	ID: reachPlansListLocationsID, Version: 1, Title: "List the locations Google Ads can plan reach for",
	Description: "List the locations that reach planning supports (ReachPlanService.ListPlannableLocations of API " +
		"version v26): ID, name, location type, country code and, for a location below a country, the parent " +
		"country ID. The call belongs to no account: it takes no customer_id, sends no login-customer-id and is " +
		"therefore not bound to the targets of the connection, like fields.get." + reachCommon,
	Tags: []string{"google", "ads", "reach", "planning"}, Risk: reachRisk, Provider: Provider,
	InputSchema:  encoded(object(schema{"reach_application_info": reachAppSchema})),
	OutputSchema: reachOutput("plannable_locations", false),
	Arguments:    []capability.Argument{reachAppArgument},
	Fields: []capability.Field{{Name: "plannable_locations", Description: "Per location: id, name, location_type, " +
		"country_code and parent_country_id when present"},
		{Name: "truncated", Description: "true when a list was cut"}},
	Examples: []capability.Example{{Description: "List the plannable locations", Arguments: json.RawMessage(`{}`)}},
}

var reachPlansListProducts = capability.Descriptor{
	ID: reachPlansListProductsID, Version: 1, Title: "List the products Google Ads can plan reach for in a location",
	Description: "List the ad products that reach planning offers in one plannable location " +
		"(ReachPlanService.ListPlannableProducts of API version v26): the product code, name and description, " +
		"its core attributes (buying method, cost model, marketing objective) and the targeting it allows " +
		"(age ranges, devices, genders, networks, parental statuses, surface targeting, YouTube Select lineups). " +
		"The call belongs to no account: it takes no customer_id, sends no login-customer-id and is therefore not " +
		"bound to the targets of the connection, like fields.get." + reachCommon,
	Tags: []string{"google", "ads", "reach", "planning"}, Risk: reachRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"plannable_location_id":  schema{"type": "string", "pattern": reachIDPatternText},
		"reach_application_info": reachAppSchema,
	}, "plannable_location_id")),
	OutputSchema: encoded(schema{"type": "object", "properties": schema{
		"product_metadata": arrayOf(schema{"type": "object"}), "truncated": booleanSchema}}),
	Arguments: []capability.Argument{
		{Name: "plannable_location_id", Required: true, Description: "ID of a location from list-locations"},
		reachAppArgument,
	},
	Fields: []capability.Field{{Name: "product_metadata", Description: "Per product: plannable_product_code, " +
		"plannable_product_name, plannable_product_description, plannable_product_core_attributes and " +
		"plannable_targeting"}, {Name: "truncated", Description: "true when a list was cut"}},
	Examples: []capability.Example{{Description: "List the products of a location",
		Arguments: json.RawMessage(`{"plannable_location_id":"2276"}`)}},
}

var reachPlansGenerateConversionRates = capability.Descriptor{
	ID: reachPlansGenerateConversionRatesID, Version: 1, Title: "Suggest conversion rates for Google Ads reach planning",
	Description: "Suggest conversion rates for planned products, from the history of the account and from the " +
		"inventory (ReachPlanService.GenerateConversionRates of API version v26): per suggestion the product " +
		"code, the rate between 0 and 1, the model (CUSTOMER_HISTORY, INVENTORY_AGGRESSIVE, INVENTORY_CONSERVATIVE or " +
		"INVENTORY_MEDIAN) and the surfaces. Several suggestions may share a product code." + reachCommon,
	Tags: []string{"google", "ads", "reach", "planning"}, Risk: reachRisk, Provider: Provider,
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "customer_reach_group": reachGroupSchema,
		"reach_application_info": reachAppSchema}, "customer_id")),
	OutputSchema: reachOutput("conversion_rate_suggestions", true),
	Arguments:    []capability.Argument{reachCustomerArgument, reachGroupArgument, reachAppArgument},
	Fields: []capability.Field{{Name: "customer_id", Description: "The account of the call"},
		{Name: "conversion_rate_suggestions", Description: "Per suggestion: plannable_product_code, " +
			"conversion_rate, conversion_rate_model and surface_targeting when present"},
		{Name: "truncated", Description: "true when a list was cut"}},
	Examples: []capability.Example{{Description: "Suggest conversion rates for an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890"}`)}},
}

var reachPlansListUserLists = capability.Descriptor{
	ID: reachPlansListUserListsID, Version: 1, Title: "List the user lists Google Ads can plan reach with",
	Description: "List the user lists of an account that reach planning can use as audience " +
		"(ReachPlanService.ListPlannableUserLists of API version v26): per list its display name, the resource " +
		"name in user_list_info, the list type, whether it is plannable and, for a CRM list, the kind of its data " +
		"source." + reachCommon,
	Tags: []string{"google", "ads", "reach", "planning", "audiences"}, Risk: reachRisk, Provider: Provider,
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "customer_reach_group": reachGroupSchema,
		"reach_application_info": reachAppSchema}, "customer_id")),
	OutputSchema: reachOutput("plannable_user_lists", true),
	Arguments:    []capability.Argument{reachCustomerArgument, reachGroupArgument, reachAppArgument},
	Fields: []capability.Field{{Name: "customer_id", Description: "The account of the call"},
		{Name: "plannable_user_lists", Description: "Per list: display_name, plannable_status, " +
			"plannable_user_list_metadata, user_list_info and user_list_type"},
		{Name: "truncated", Description: "true when a list was cut"}},
	Examples: []capability.Example{{Description: "List the plannable user lists of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890"}`)}},
}

var reachPlansListUserInterests = capability.Descriptor{
	ID: reachPlansListUserInterestsID, Version: 1, Title: "List the user interests Google Ads can plan reach with",
	Description: "List the user interests that reach planning can use as audience " +
		"(ReachPlanService.ListPlannableUserInterests of API version v26): per interest its resource name, " +
		"display name, path and type. name_query and path_query keep the interests whose name or path contains the " +
		"text, user_interest_taxonomy_types narrows to AFFINITY or IN_MARKET. The full taxonomy is large: narrow " +
		"it, since a list is cut at 500 entries." + reachCommon,
	Tags: []string{"google", "ads", "reach", "planning", "audiences"}, Risk: reachRisk, Provider: Provider,
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"name_query":                   schema{"type": "string", "minLength": 1, "maxLength": maxReachQuery},
		"path_query":                   schema{"type": "string", "minLength": 1, "maxLength": maxReachQuery},
		"user_interest_taxonomy_types": schema{"type": "array", "maxItems": 2, "uniqueItems": true, "items": reachSchemaEnum(reachInterestTypes)},
		"reach_application_info":       reachAppSchema}, "customer_id")),
	OutputSchema: reachOutput("plannable_user_interests", true),
	Arguments: []capability.Argument{reachCustomerArgument,
		{Name: "name_query", Description: "Keep interests whose name contains this text, case-insensitive, up to 200 characters"},
		{Name: "path_query", Description: "Keep interests whose path contains this text, case-insensitive, up to 200 characters"},
		{Name: "user_interest_taxonomy_types", Description: "AFFINITY and IN_MARKET, each at most once; all supported types when omitted"},
		reachAppArgument},
	Fields: []capability.Field{{Name: "customer_id", Description: "The account of the call"},
		{Name: "plannable_user_interests", Description: "Per interest: user_interest (user_interest_category), " +
			"user_interest_display_name, user_interest_path and user_interest_type"},
		{Name: "truncated", Description: "true when a list was cut"}},
	Examples: []capability.Example{{Description: "Find in-market interests about cars",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","name_query":"auto","user_interest_taxonomy_types":["IN_MARKET"]}`)}},
}

var reachProductSchema = object(schema{
	"plannable_product_code": schema{"type": "string", "pattern": reachCodePatternText},
	"budget_micros":          schema{"type": "integer", "minimum": 1, "maximum": recMaxMicros},
	"conversion_rate":        schema{"type": "number", "exclusiveMinimum": 0, "exclusiveMaximum": 1},
	"advanced_product_targeting": object(schema{
		"youtube_select_settings": object(schema{"lineup_id": schema{"type": "integer", "minimum": 0}}, "lineup_id"),
		"surface_targeting_settings": object(schema{"surfaces": schema{"type": "array", "minItems": 1,
			"maxItems": len(reachSurfaces), "items": reachSchemaEnum(reachSurfaces)}}, "surfaces"),
		"target_frequency_settings": object(schema{"target_frequency": schema{"type": "integer", "minimum": 1,
			"maximum": maxInt32}, "time_unit": reachSchemaEnum(reachTargetUnits)}, "target_frequency", "time_unit"),
	}),
}, "plannable_product_code", "budget_micros")

var reachTargetingSchema = object(schema{
	"age_range": reachSchemaEnum(reachAgeRanges),
	"audience_targeting": object(schema{
		"user_interest": schema{"type": "array", "maxItems": maxReachList, "items": object(schema{
			"user_interest_category": schema{"type": "string", "maxLength": 128}}, "user_interest_category")},
		"user_lists": schema{"type": "array", "maxItems": maxReachList, "items": object(schema{
			"user_list": schema{"type": "string", "maxLength": 128}}, "user_list")},
	}),
	"devices":           reachTypeList(reachDevices),
	"genders":           reachTypeList(reachGenders),
	"network":           reachSchemaEnum(reachNetworks),
	"parental_statuses": reachTypeList(reachParentalStatus),
	"plannable_location_ids": schema{"type": "array", "maxItems": maxReachList,
		"items": schema{"type": "string", "pattern": reachIDPatternText}},
})

var reachPlansGenerateForecast = capability.Descriptor{
	ID: reachPlansGenerateForecastID, Version: 1, Title: "Generate a Google Ads reach forecast",
	Description: "Forecast the reach of a product mix for a campaign duration, a budget per product and a " +
		"targeting (ReachPlanService.GenerateReachForecast of API version v26, POST " +
		"customers/{customer_id}:generateReachForecast): the reach curve with, per point, the cost, the forecast " +
		"(impressions, reach, views, clicks, conversions, viewable impressions, with co-viewers when " +
		"forecast_metric_options.include_coview is set, and the effective frequency breakdown when " +
		"effective_frequency_limit is set) and the forecast of each planned product, plus the size of the " +
		"reference audiences. The plan is not saved and nothing is bought or created. Product codes and the " +
		"targeting a product allows come from list-products; Google checks the rest of the plan, such as one " +
		"parent country for the locations, a date range that ends less than a year ahead and the conversion rate " +
		"of DEMAND_GEN products, and a refusal is returned as the provider's rejection of the input." + reachCommon,
	Tags: []string{"google", "ads", "reach", "planning", "forecast"}, Risk: reachRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_duration": object(schema{
			"date_range": object(schema{"start_date": schema{"type": "string", "pattern": reachDatePatternText},
				"end_date": schema{"type": "string", "pattern": reachDatePatternText}}, "start_date", "end_date"),
			"duration_in_days": schema{"type": "integer", "minimum": 1, "maximum": maxInt32},
		}),
		"cookie_frequency_cap_setting": object(schema{"impressions": schema{"type": "integer", "minimum": 1,
			"maximum": maxInt32}, "time_unit": reachSchemaEnum(reachCapUnits)}, "impressions", "time_unit"),
		"currency_code":        schema{"type": "string", "pattern": reachCurrencyPatternText},
		"customer_reach_group": reachGroupSchema,
		"effective_frequency_limit": object(schema{"effective_frequency_breakdown_limit": schema{"type": "integer",
			"minimum": 1, "maximum": 10}}, "effective_frequency_breakdown_limit"),
		"forecast_metric_options": object(schema{"include_coview": booleanSchema}),
		"min_effective_frequency": schema{"type": "integer", "minimum": 1, "maximum": 10},
		"planned_products": schema{"type": "array", "minItems": 1, "maxItems": maxReachProducts,
			"items": reachProductSchema},
		"reach_application_info": reachAppSchema,
		"targeting":              reachTargetingSchema,
	}, "customer_id", "campaign_duration", "planned_products")),
	OutputSchema: encoded(schema{"type": "object", "properties": schema{"customer_id": stringSchema,
		"on_target_audience_metrics": schema{"type": "object"}, "reach_curve": schema{"type": "object"},
		"truncated": booleanSchema}, "required": []string{"customer_id"}}),
	Arguments: []capability.Argument{reachCustomerArgument,
		{Name: "campaign_duration", Required: true, Description: "Exactly one of date_range (start_date and " +
			"end_date as yyyy-mm-dd, both inclusive, at most 92 days, ending less than a year ahead) and " +
			"duration_in_days"},
		{Name: "planned_products", Required: true, Description: "1 to 15 products: plannable_product_code, " +
			"budget_micros in the currency_code, conversion_rate between 0 and 1 exclusive (necessary for " +
			"DEMAND_GEN, not supported for other products) and advanced_product_targeting with one of " +
			"youtube_select_settings (lineup_id), surface_targeting_settings (surfaces) and " +
			"target_frequency_settings (target_frequency, time_unit WEEKLY or MONTHLY; necessary for TARGET_FREQUENCY)"},
		{Name: "targeting", Description: "Applied to all products: age_range, audience_targeting (user_interest and " +
			"user_lists as resource names of the account), devices, genders, network, parental_statuses (each as " +
			"objects with type) and plannable_location_ids (one parent country)"},
		{Name: "cookie_frequency_cap_setting", Description: "impressions and time_unit DAY, WEEK or MONTH; no cap when omitted"},
		{Name: "currency_code", Description: "Three-letter ISO 4217 code of the budgets, in capitals"},
		{Name: "effective_frequency_limit", Description: "effective_frequency_breakdown_limit 1 to 10; cannot be " +
			"combined with min_effective_frequency"},
		{Name: "min_effective_frequency", Description: "1 to 10, only for the reported reach; default 1"},
		{Name: "forecast_metric_options", Description: "include_coview adds the co-view metrics"},
		reachGroupArgument, reachAppArgument},
	Fields: []capability.Field{{Name: "customer_id", Description: "The account of the call"},
		{Name: "reach_curve", Description: "reach_forecasts: per point cost_micros, forecast (on_target_reach, " +
			"total_reach, on_target_impressions, total_impressions, viewable_impressions, trueview_views, clicks, " +
			"conversions, coview metrics, effective_frequency_breakdowns) and planned_product_reach_forecasts " +
			"(plannable_product_code, cost_micros, planned_product_forecast)"},
		{Name: "on_target_audience_metrics", Description: "census_audience_size and youtube_audience_size"},
		{Name: "truncated", Description: "true when a list was cut"}},
	Examples: []capability.Example{{Description: "Forecast one video product for four weeks in Germany",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_duration":{"duration_in_days":28},` +
			`"currency_code":"EUR","planned_products":[{"plannable_product_code":"VIDEO_VIEW_CAMPAIGN",` +
			`"budget_micros":5000000000}],"targeting":{"plannable_location_ids":["2276"]}}`)}},
}
