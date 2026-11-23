package googleads

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	audiencesCreateID = Provider + ".audiences.create"
	audiencesUpdateID = Provider + ".audiences.update"

	audienceService    = "/audiences"
	audienceCollection = "audiences"
)

// Qatlas' own bounds of the composition of an audience; Google documents none of them.
const (
	audienceMaxName       = 255
	audienceMaxDimensions = 10
	audienceMaxSegments   = 100
	audienceMaxExclusions = 100
)

// Values Google documents for the fields of an audience. UNSPECIFIED and UNKNOWN are no values to send.
var (
	audienceScopes           = []string{"CUSTOMER", "ASSET_GROUP"}
	audienceMinAges          = []int{18, 25, 35, 45, 55, 65}
	audienceMaxAges          = []int{24, 34, 44, 54, 64}
	audienceGenderTypes      = []string{"MALE", "FEMALE", "UNDETERMINED"}
	audienceIncomeRanges     = []string{"INCOME_RANGE_0_50", "INCOME_RANGE_50_60", "INCOME_RANGE_60_70", "INCOME_RANGE_70_80", "INCOME_RANGE_80_90", "INCOME_RANGE_90_UP", "INCOME_RANGE_UNDETERMINED"}
	audienceParentalStatuses = []string{"PARENT", "NOT_A_PARENT", "UNDETERMINED"}
)

// The dimensions of an audience. The same types carry the arguments and the answer.
type (
	audienceAgeRange struct {
		MinAge *int `json:"min_age,omitempty"`
		MaxAge *int `json:"max_age,omitempty"`
	}
	audienceAgeDimension struct {
		AgeRanges           []audienceAgeRange `json:"age_ranges,omitempty"`
		IncludeUndetermined *bool              `json:"include_undetermined,omitempty"`
	}
	audienceGenderDimension struct {
		Genders             []string `json:"genders,omitempty"`
		IncludeUndetermined *bool    `json:"include_undetermined,omitempty"`
	}
	audienceIncomeDimension struct {
		IncomeRanges        []string `json:"income_ranges,omitempty"`
		IncludeUndetermined *bool    `json:"include_undetermined,omitempty"`
	}
	audienceParentalDimension struct {
		ParentalStatuses    []string `json:"parental_statuses,omitempty"`
		IncludeUndetermined *bool    `json:"include_undetermined,omitempty"`
	}
	// segment names one segment by the resource name of the account; exactly one member is set.
	audienceSegment struct {
		UserList             string `json:"user_list,omitempty"`
		UserInterestCategory string `json:"user_interest_category,omitempty"`
		LifeEvent            string `json:"life_event,omitempty"`
		DetailedDemographic  string `json:"detailed_demographic,omitempty"`
		CustomAudience       string `json:"custom_audience,omitempty"`
	}
	audienceSegmentsDimension struct {
		Segments []audienceSegment `json:"segments,omitempty"`
	}
	// dimension is one positive dimension; exactly one member is set.
	audienceDimension struct {
		Age              *audienceAgeDimension      `json:"age,omitempty"`
		Gender           *audienceGenderDimension   `json:"gender,omitempty"`
		HouseholdIncome  *audienceIncomeDimension   `json:"household_income,omitempty"`
		ParentalStatus   *audienceParentalDimension `json:"parental_status,omitempty"`
		AudienceSegments *audienceSegmentsDimension `json:"audience_segments,omitempty"`
	}
	audienceExclusion struct {
		UserList string `json:"user_list,omitempty"`
	}
	audienceExclusionDimension struct {
		Exclusions []audienceExclusion `json:"exclusions"`
	}
)

type audienceSegmentRef struct{ argument, kind, field, collection, value string }

// refs are the members of the segment that are set, with the names Google uses for them.
func (s audienceSegment) refs() []audienceSegmentRef {
	var out []audienceSegmentRef
	for _, r := range []audienceSegmentRef{
		{"user_list", "userList", "userList", "userLists", s.UserList},
		{"user_interest_category", "userInterest", "userInterestCategory", "userInterests", s.UserInterestCategory},
		{"life_event", "lifeEvent", "lifeEvent", "lifeEvents", s.LifeEvent},
		{"detailed_demographic", "detailedDemographic", "detailedDemographic", "detailedDemographics", s.DetailedDemographic},
		{"custom_audience", "customAudience", "customAudience", "customAudiences", s.CustomAudience}} {
		if r.value != "" {
			out = append(out, r)
		}
	}
	return out
}

func audienceCheckEnums(argument string, values, allowed []string, include *bool) error {
	if len(values) == 0 && include == nil {
		return badRequest(argument + " must name a value or set include_undetermined")
	}
	for _, v := range values {
		if !slices.Contains(allowed, v) {
			return badRequest(argument + " holds a value Google does not define: " + boundedText(v, 40))
		}
	}
	return nil
}

func audienceWithUndetermined(wire map[string]any, include *bool) {
	if include != nil {
		wire["includeUndetermined"] = *include
	}
}

// wire checks one dimension and builds its body. Every reference is checked for its form and its account.
func (d audienceDimension) wire(customerID, at string) (map[string]any, error) {
	set := 0
	for _, present := range []bool{d.Age != nil, d.Gender != nil, d.HouseholdIncome != nil, d.ParentalStatus != nil,
		d.AudienceSegments != nil} {
		if present {
			set++
		}
	}
	if set != 1 {
		return nil, badRequest(at + " must hold exactly one of age, gender, household_income, parental_status, audience_segments")
	}
	switch {
	case d.Age != nil:
		if n := len(d.Age.AgeRanges); n < 1 || n > len(audienceMinAges) {
			return nil, badRequest(fmt.Sprintf("%s.age.age_ranges must hold 1 to %d ranges", at, len(audienceMinAges)))
		}
		ranges := make([]any, 0, len(d.Age.AgeRanges))
		for _, r := range d.Age.AgeRanges {
			if r.MinAge == nil || !slices.Contains(audienceMinAges, *r.MinAge) {
				return nil, badRequest(at + ".age: min_age must be 18, 25, 35, 45, 55 or 65")
			}
			one := map[string]any{"minAge": *r.MinAge}
			if r.MaxAge != nil {
				if !slices.Contains(audienceMaxAges, *r.MaxAge) || *r.MaxAge <= *r.MinAge {
					return nil, badRequest(at + ".age: max_age must be 24, 34, 44, 54 or 64 and above min_age")
				}
				one["maxAge"] = *r.MaxAge
			}
			ranges = append(ranges, one)
		}
		wire := map[string]any{"ageRanges": ranges}
		audienceWithUndetermined(wire, d.Age.IncludeUndetermined)
		return map[string]any{"age": wire}, nil
	case d.Gender != nil:
		if err := audienceCheckEnums(at+".gender", d.Gender.Genders, audienceGenderTypes, d.Gender.IncludeUndetermined); err != nil {
			return nil, err
		}
		wire := map[string]any{}
		if len(d.Gender.Genders) > 0 {
			wire["genders"] = d.Gender.Genders
		}
		audienceWithUndetermined(wire, d.Gender.IncludeUndetermined)
		return map[string]any{"gender": wire}, nil
	case d.HouseholdIncome != nil:
		if err := audienceCheckEnums(at+".household_income", d.HouseholdIncome.IncomeRanges, audienceIncomeRanges,
			d.HouseholdIncome.IncludeUndetermined); err != nil {
			return nil, err
		}
		wire := map[string]any{}
		if len(d.HouseholdIncome.IncomeRanges) > 0 {
			wire["incomeRanges"] = d.HouseholdIncome.IncomeRanges
		}
		audienceWithUndetermined(wire, d.HouseholdIncome.IncludeUndetermined)
		return map[string]any{"householdIncome": wire}, nil
	case d.ParentalStatus != nil:
		if err := audienceCheckEnums(at+".parental_status", d.ParentalStatus.ParentalStatuses, audienceParentalStatuses,
			d.ParentalStatus.IncludeUndetermined); err != nil {
			return nil, err
		}
		wire := map[string]any{}
		if len(d.ParentalStatus.ParentalStatuses) > 0 {
			wire["parentalStatuses"] = d.ParentalStatus.ParentalStatuses
		}
		audienceWithUndetermined(wire, d.ParentalStatus.IncludeUndetermined)
		return map[string]any{"parentalStatus": wire}, nil
	}
	if n := len(d.AudienceSegments.Segments); n < 1 || n > audienceMaxSegments {
		return nil, badRequest(fmt.Sprintf("%s.audience_segments.segments must hold 1 to %d segments", at, audienceMaxSegments))
	}
	segments := make([]any, 0, len(d.AudienceSegments.Segments))
	for _, s := range d.AudienceSegments.Segments {
		refs := s.refs()
		if len(refs) != 1 {
			return nil, badRequest(at + ".audience_segments: each segment must hold exactly one of user_list, " +
				"user_interest_category, life_event, detailed_demographic, custom_audience")
		}
		r := refs[0]
		if !ownedName(r.value, customerID, r.collection) {
			return nil, badRequest(at + ".audience_segments: " + r.argument + " must be a customers/" +
				queryNumber(customerID) + "/" + r.collection + "/N of customer_id")
		}
		segments = append(segments, map[string]any{r.kind: map[string]any{r.field: r.value}})
	}
	return map[string]any{"audienceSegments": map[string]any{"segments": segments}}, nil
}

func audienceDimensionsWire(customerID, at string, dimensions []audienceDimension) ([]any, error) {
	if len(dimensions) > audienceMaxDimensions {
		return nil, badRequest(fmt.Sprintf("%s must hold at most %d dimensions", at, audienceMaxDimensions))
	}
	out := make([]any, 0, len(dimensions))
	for i, d := range dimensions {
		wire, err := d.wire(customerID, fmt.Sprintf("%s[%d]", at, i))
		if err != nil {
			return nil, err
		}
		out = append(out, wire)
	}
	return out, nil
}

// audienceExclusionWire builds the exclusion dimension. An empty list is the clearing of the field and has no body.
func audienceExclusionWire(customerID, at string, e audienceExclusionDimension) (map[string]any, error) {
	if len(e.Exclusions) > audienceMaxExclusions {
		return nil, badRequest(fmt.Sprintf("%s.exclusions must hold at most %d entries", at, audienceMaxExclusions))
	}
	list := make([]any, 0, len(e.Exclusions))
	for _, x := range e.Exclusions {
		if !ownedName(x.UserList, customerID, "userLists") {
			return nil, badRequest(at + ".exclusions: user_list must be a customers/" + queryNumber(customerID) +
				"/userLists/N of customer_id")
		}
		list = append(list, map[string]any{"userList": map[string]any{"userList": x.UserList}})
	}
	if len(list) == 0 {
		return nil, nil
	}
	return map[string]any{"exclusions": list}, nil
}

func audienceCheckName(at, name string) error {
	if !audienceText(name, 4*audienceMaxName) || utf8.RuneCountInString(name) > audienceMaxName {
		return badRequest(at + ".name must be text of 1 to 255 characters without control characters")
	}
	return nil
}

type audienceCreateArguments struct {
	CustomerID string `json:"customer_id"`
	Audiences  []struct {
		Name               string                      `json:"name"`
		Description        string                      `json:"description"`
		Dimensions         []audienceDimension         `json:"dimensions"`
		ExclusionDimension *audienceExclusionDimension `json:"exclusion_dimension"`
		Scope              string                      `json:"scope"`
		AssetGroup         string                      `json:"asset_group"`
	} `json:"audiences"`
	ValidateOnly        bool   `json:"validate_only"`
	ResponseContentType string `json:"response_content_type"`
}

func (a audienceCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	if err := audienceCheckCount("audiences", len(a.Audiences)); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(a.Audiences))
	for i, x := range a.Audiences {
		at := fmt.Sprintf("audiences[%d]", i)
		create := map[string]any{}
		switch x.Scope {
		case "", "CUSTOMER":
			if err := audienceCheckName(at, x.Name); err != nil {
				return nil, err
			}
			if x.AssetGroup != "" {
				return nil, badRequest(at + ".asset_group is only for the scope ASSET_GROUP")
			}
			create["name"] = x.Name
		case "ASSET_GROUP":
			if x.Name != "" {
				return nil, badRequest(at + ".name cannot be set with the scope ASSET_GROUP")
			}
			if !ownedName(x.AssetGroup, a.CustomerID, "assetGroups") {
				return nil, badRequest(at + ".asset_group must be a customers/" + queryNumber(a.CustomerID) +
					"/assetGroups/N of customer_id")
			}
			create["scope"], create["assetGroup"] = x.Scope, x.AssetGroup
		default:
			return nil, badRequest(at + ".scope must be CUSTOMER or ASSET_GROUP")
		}
		if x.Description != "" {
			if err := audienceCheckDescription(at+".description", x.Description); err != nil {
				return nil, err
			}
			create["description"] = x.Description
		}
		dims, err := audienceDimensionsWire(a.CustomerID, at+".dimensions", x.Dimensions)
		if err != nil {
			return nil, err
		}
		if len(dims) > 0 {
			create["dimensions"] = dims
		}
		if x.ExclusionDimension != nil {
			ex, err := audienceExclusionWire(a.CustomerID, at+".exclusion_dimension", *x.ExclusionDimension)
			if err != nil {
				return nil, err
			}
			if ex != nil {
				create["exclusionDimension"] = ex
			}
		}
		if len(dims) == 0 && create["exclusionDimension"] == nil {
			return nil, badRequest(at + " must hold at least one dimension or exclusion")
		}
		out = append(out, map[string]any{"create": create})
	}
	return out, nil
}

type audienceUpdateArguments struct {
	CustomerID string `json:"customer_id"`
	Audiences  []struct {
		ID                 string                      `json:"id"`
		Name               *string                     `json:"name"`
		Description        *string                     `json:"description"`
		Dimensions         *[]audienceDimension        `json:"dimensions"`
		ExclusionDimension *audienceExclusionDimension `json:"exclusion_dimension"`
		Scope              *string                     `json:"scope"`
	} `json:"audiences"`
	ValidateOnly        bool   `json:"validate_only"`
	ResponseContentType string `json:"response_content_type"`
}

func (a audienceUpdateArguments) operations() ([]any, []string, [][]string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, nil, err
	}
	if err := audienceCheckCount("audiences", len(a.Audiences)); err != nil {
		return nil, nil, nil, err
	}
	var out []any
	var names []string
	var changed [][]string
	for i, x := range a.Audiences {
		at := fmt.Sprintf("audiences[%d]", i)
		name, err := audienceObjectName(at+".id", a.CustomerID, audienceCollection, x.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		if slices.Contains(names, name) {
			return nil, nil, nil, badRequest(at + ".id repeats an ID of this call")
		}
		update := map[string]any{"resourceName": name}
		var fields audienceChangedNames
		if x.Name != nil {
			if err := audienceCheckName(at, *x.Name); err != nil {
				return nil, nil, nil, err
			}
			update["name"] = *x.Name
			fields.add("name", "name")
		}
		if x.Description != nil {
			if err := audienceCheckDescription(at+".description", *x.Description); err != nil {
				return nil, nil, nil, err
			}
			if *x.Description != "" {
				update["description"] = *x.Description
			}
			fields.add("description", "description")
		}
		if x.Dimensions != nil {
			if len(*x.Dimensions) == 0 {
				return nil, nil, nil, badRequest(at + ".dimensions must hold at least one dimension; it replaces all dimensions")
			}
			dims, err := audienceDimensionsWire(a.CustomerID, at+".dimensions", *x.Dimensions)
			if err != nil {
				return nil, nil, nil, err
			}
			update["dimensions"] = dims
			fields.add("dimensions", "dimensions")
		}
		if x.ExclusionDimension != nil {
			ex, err := audienceExclusionWire(a.CustomerID, at+".exclusion_dimension", *x.ExclusionDimension)
			if err != nil {
				return nil, nil, nil, err
			}
			if ex != nil {
				update["exclusionDimension"] = ex
			}
			fields.add("exclusionDimension", "exclusion_dimension")
		}
		if x.Scope != nil {
			if *x.Scope != "CUSTOMER" {
				return nil, nil, nil, badRequest(at + ".scope can only be set to CUSTOMER: an audience of an asset group " +
					"becomes an audience of the account, never the other way round")
			}
			update["scope"] = *x.Scope
			fields.add("scope", "scope")
		}
		if len(fields.mask) == 0 {
			return nil, nil, nil, badRequest(at + " must give at least one field to change")
		}
		out = append(out, map[string]any{"update": update, "updateMask": strings.Join(fields.mask, ",")})
		names = append(names, name)
		changed = append(changed, fields.names)
	}
	return out, names, changed, nil
}

// The audience Google returns after a change with MUTABLE_RESOURCE.
type (
	audienceRefWire struct {
		UserList             string `json:"userList"`
		UserInterestCategory string `json:"userInterestCategory"`
		LifeEvent            string `json:"lifeEvent"`
		DetailedDemographic  string `json:"detailedDemographic"`
		CustomAudience       string `json:"customAudience"`
	}
	audienceSegmentWire struct {
		UserList            *audienceRefWire `json:"userList"`
		UserInterest        *audienceRefWire `json:"userInterest"`
		LifeEvent           *audienceRefWire `json:"lifeEvent"`
		DetailedDemographic *audienceRefWire `json:"detailedDemographic"`
		CustomAudience      *audienceRefWire `json:"customAudience"`
	}
	audienceDimensionWire struct {
		Age *struct {
			AgeRanges []struct {
				MinAge *int `json:"minAge"`
				MaxAge *int `json:"maxAge"`
			} `json:"ageRanges"`
			IncludeUndetermined *bool `json:"includeUndetermined"`
		} `json:"age"`
		Gender *struct {
			Genders             []string `json:"genders"`
			IncludeUndetermined *bool    `json:"includeUndetermined"`
		} `json:"gender"`
		HouseholdIncome *struct {
			IncomeRanges        []string `json:"incomeRanges"`
			IncludeUndetermined *bool    `json:"includeUndetermined"`
		} `json:"householdIncome"`
		ParentalStatus *struct {
			ParentalStatuses    []string `json:"parentalStatuses"`
			IncludeUndetermined *bool    `json:"includeUndetermined"`
		} `json:"parentalStatus"`
		AudienceSegments *struct {
			Segments []audienceSegmentWire `json:"segments"`
		} `json:"audienceSegments"`
	}
	audienceWire struct {
		ResourceName       string                  `json:"resourceName"`
		ID                 *wireInt                `json:"id"`
		Status             string                  `json:"status"`
		Name               string                  `json:"name"`
		Description        string                  `json:"description"`
		Dimensions         []audienceDimensionWire `json:"dimensions"`
		ExclusionDimension *struct {
			Exclusions []struct {
				UserList *audienceRefWire `json:"userList"`
			} `json:"exclusions"`
		} `json:"exclusionDimension"`
		Scope      string `json:"scope"`
		AssetGroup string `json:"assetGroup"`
	}
	audienceOut struct {
		ResourceName       string                      `json:"resource_name"`
		ID                 string                      `json:"id,omitempty"`
		Status             string                      `json:"status,omitempty"`
		Name               string                      `json:"name,omitempty"`
		Description        string                      `json:"description,omitempty"`
		Dimensions         []audienceDimension         `json:"dimensions,omitempty"`
		ExclusionDimension *audienceExclusionDimension `json:"exclusion_dimension,omitempty"`
		Scope              string                      `json:"scope,omitempty"`
		AssetGroup         string                      `json:"asset_group,omitempty"`
	}
)

func (w audienceSegmentWire) audienceSegment(customerID string) (audienceSegment, bool) {
	var s audienceSegment
	n := 0
	take := func(ref *audienceRefWire, value func(audienceRefWire) string, collection string, dst *string) bool {
		if ref == nil {
			return true
		}
		n++
		v := value(*ref)
		if !ownedName(v, customerID, collection) {
			return false
		}
		*dst = v
		return true
	}
	ok := take(w.UserList, func(r audienceRefWire) string { return r.UserList }, "userLists", &s.UserList) &&
		take(w.UserInterest, func(r audienceRefWire) string { return r.UserInterestCategory }, "userInterests", &s.UserInterestCategory) &&
		take(w.LifeEvent, func(r audienceRefWire) string { return r.LifeEvent }, "lifeEvents", &s.LifeEvent) &&
		take(w.DetailedDemographic, func(r audienceRefWire) string { return r.DetailedDemographic }, "detailedDemographics",
			&s.DetailedDemographic) &&
		take(w.CustomAudience, func(r audienceRefWire) string { return r.CustomAudience }, "customAudiences", &s.CustomAudience)
	return s, ok && n == 1
}

func audienceCodes(values []string) []string { return boundedList(values, maxCodeText) }

func (w audienceDimensionWire) audienceDimension(customerID string) (audienceDimension, bool) {
	switch {
	case w.Age != nil:
		out := &audienceAgeDimension{IncludeUndetermined: w.Age.IncludeUndetermined}
		for _, r := range w.Age.AgeRanges[:min(len(w.Age.AgeRanges), maxListItems)] {
			out.AgeRanges = append(out.AgeRanges, audienceAgeRange{MinAge: r.MinAge, MaxAge: r.MaxAge})
		}
		return audienceDimension{Age: out}, true
	case w.Gender != nil:
		return audienceDimension{Gender: &audienceGenderDimension{Genders: audienceCodes(w.Gender.Genders),
			IncludeUndetermined: w.Gender.IncludeUndetermined}}, true
	case w.HouseholdIncome != nil:
		return audienceDimension{HouseholdIncome: &audienceIncomeDimension{IncomeRanges: audienceCodes(w.HouseholdIncome.IncomeRanges),
			IncludeUndetermined: w.HouseholdIncome.IncludeUndetermined}}, true
	case w.ParentalStatus != nil:
		return audienceDimension{ParentalStatus: &audienceParentalDimension{ParentalStatuses: audienceCodes(w.ParentalStatus.ParentalStatuses),
			IncludeUndetermined: w.ParentalStatus.IncludeUndetermined}}, true
	case w.AudienceSegments != nil:
		out := &audienceSegmentsDimension{}
		for _, s := range w.AudienceSegments.Segments[:min(len(w.AudienceSegments.Segments), maxListItems)] {
			seg, ok := s.audienceSegment(customerID)
			if !ok {
				return audienceDimension{}, false
			}
			out.Segments = append(out.Segments, seg)
		}
		return audienceDimension{AudienceSegments: out}, true
	}
	return audienceDimension{}, true
}

// audienceOf maps the audience of a mutate answer. It must be the object of the result, every reference in it
// must belong to the account of the call, and the asset group as well.
func audienceOf(raw json.RawMessage, customerID, resourceName string) (*audienceOut, bool) {
	var w audienceWire
	if json.Unmarshal(raw, &w) != nil || w.ResourceName != resourceName {
		return nil, false
	}
	out := &audienceOut{ResourceName: w.ResourceName, ID: intText(w.ID), Status: code(w.Status),
		Name: boundedText(w.Name, maxText), Description: boundedText(w.Description, maxLongText), Scope: code(w.Scope)}
	if w.AssetGroup != "" {
		if !ownedName(w.AssetGroup, customerID, "assetGroups") {
			return nil, false
		}
		out.AssetGroup = w.AssetGroup
	}
	for _, d := range w.Dimensions[:min(len(w.Dimensions), maxListItems)] {
		dim, ok := d.audienceDimension(customerID)
		if !ok {
			return nil, false
		}
		out.Dimensions = append(out.Dimensions, dim)
	}
	if w.ExclusionDimension != nil {
		ex := &audienceExclusionDimension{Exclusions: []audienceExclusion{}}
		for _, x := range w.ExclusionDimension.Exclusions[:min(len(w.ExclusionDimension.Exclusions), maxListItems)] {
			if x.UserList == nil || !ownedName(x.UserList.UserList, customerID, "userLists") {
				return nil, false
			}
			ex.Exclusions = append(ex.Exclusions, audienceExclusion{UserList: x.UserList.UserList})
		}
		out.ExclusionDimension = ex
	}
	return out, true
}

type audienceResult struct {
	ResourceName string       `json:"resource_name"`
	Updated      []string     `json:"updated,omitempty"`
	Audience     *audienceOut `json:"audience,omitempty"`
}

type audiencesChanged struct {
	CustomerID   string           `json:"customer_id"`
	ValidateOnly bool             `json:"validate_only"`
	Results      []audienceResult `json:"results"`
}

func audiencesOf(customerID string, validateOnly bool, results []audienceMutateResult, changed [][]string) (audiencesChanged, error) {
	out := audiencesChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []audienceResult{}}
	for i, r := range results {
		item := audienceResult{ResourceName: r.ResourceName}
		if changed != nil {
			item.Updated = changed[i]
		}
		if len(r.Audience) > 0 && string(r.Audience) != "null" {
			audience, ok := audienceOf(r.Audience, customerID, r.ResourceName)
			if !ok {
				return out, unsureResult("change audiences", "answered with an audience that is not the one asked for")
			}
			item.Audience = audience
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// audienceDimensionSchema is the schema of a dimension. For an argument it holds Google's documented values; for an
// answer values are plain strings, so a value a later minor version adds does not fail the output schema.
func audienceDimensionSchema(strict bool) schema {
	values := func(allowed []string) schema {
		if strict {
			return audienceEnumSchema(allowed)
		}
		return stringSchema
	}
	age := func(allowed []int) schema {
		if strict {
			return schema{"type": "integer", "enum": allowed}
		}
		return integerSchema
	}
	one := func(properties schema) schema {
		s := object(properties)
		if strict {
			s["minProperties"], s["maxProperties"] = 1, 1
		}
		return s
	}
	reference := func(collection string) schema {
		if strict {
			return schema{"type": "string", "maxLength": 100,
				"pattern": "^customers/[0-9]{1,10}/" + collection + "/[0-9]{1,20}$"}
		}
		return stringSchema
	}
	list := func(items schema, max int) schema {
		if strict {
			return schema{"type": "array", "minItems": 1, "maxItems": max, "items": items}
		}
		return arrayOf(items)
	}
	return one(schema{
		"age": object(schema{"age_ranges": list(object(schema{"min_age": age(audienceMinAges), "max_age": age(audienceMaxAges)}, "min_age"),
			len(audienceMinAges)), "include_undetermined": booleanSchema}, "age_ranges"),
		"gender": object(schema{"genders": list(values(audienceGenderTypes), len(audienceGenderTypes)), "include_undetermined": booleanSchema}),
		"household_income": object(schema{"income_ranges": list(values(audienceIncomeRanges), len(audienceIncomeRanges)),
			"include_undetermined": booleanSchema}),
		"parental_status": object(schema{"parental_statuses": list(values(audienceParentalStatuses), len(audienceParentalStatuses)),
			"include_undetermined": booleanSchema}),
		"audience_segments": object(schema{"segments": list(one(schema{
			"user_list": reference("userLists"), "user_interest_category": reference("userInterests"),
			"life_event": reference("lifeEvents"), "detailed_demographic": reference("detailedDemographics"),
			"custom_audience": reference("customAudiences")}), audienceMaxSegments)}, "segments"),
	})
}

var (
	audienceExclusionSchema = object(schema{"exclusions": schema{"type": "array", "maxItems": audienceMaxExclusions, "items": object(schema{
		"user_list": schema{"type": "string", "maxLength": 100, "pattern": "^customers/[0-9]{1,10}/userLists/[0-9]{1,20}$"}},
		"user_list")}}, "exclusions")
	audienceNameSchema       = schema{"type": "string", "minLength": 1, "maxLength": audienceMaxName}
	audienceDimensionsSchema = schema{"type": "array", "maxItems": audienceMaxDimensions, "items": audienceDimensionSchema(true)}

	audienceOutSchema = object(schema{"resource_name": stringSchema, "id": stringSchema, "status": stringSchema,
		"name": stringSchema, "description": stringSchema, "dimensions": arrayOf(audienceDimensionSchema(false)),
		"exclusion_dimension": object(schema{"exclusions": arrayOf(object(schema{"user_list": stringSchema}))}, "exclusions"),
		"scope":               stringSchema, "asset_group": stringSchema}, "resource_name")
	audiencesOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema, "updated": arrayOf(stringSchema),
			"audience": audienceOutSchema}, "resource_name"))}, "customer_id", "validate_only", "results")
)

const audiencesSensitivity = "audiences of a Google Ads account: demographic dimensions and references to user " +
	"lists, custom audiences and Google segments that decide whom ads reach; the confirmation names the audiences"

const audiencesNote = " At most 10 audiences per call, sent as one atomic request without partial failure. A " +
	"dimension is one of age, gender, household income, parental status or audience segments; a segment and an " +
	"exclusion refer to an object of the same account by its resource name, never to another account. An " +
	"audience is never removed by this tool."

var audiencesCreate = capability.Descriptor{
	ID: audiencesCreateID, Version: 1, Title: "Create audiences in a Google Ads account",
	Description: "Create audiences from dimensions and exclusions (AudienceService.MutateAudiences, create). With " +
		"the scope CUSTOMER, the default, an audience needs a name, unique in the account. With the scope " +
		"ASSET_GROUP it belongs to one asset group of the account, named by asset_group, and has no name." +
		audiencesNote + " On a connection with targets, a manager account needs a manager target that names it." +
		mutateListNote,
	Tags: []string{"google", "ads", "audiences", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, audiencesSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"audiences": audienceOperationListSchema(object(schema{"name": audienceNameSchema, "description": audienceTextSchema,
			"dimensions": audienceDimensionsSchema, "exclusion_dimension": audienceExclusionSchema,
			"scope": audienceEnumSchema(audienceScopes), "asset_group": schema{"type": "string", "maxLength": 100,
				"pattern": "^customers/[0-9]{1,10}/assetGroups/[0-9]{1,20}$"}})),
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "audiences")),
	OutputSchema: encoded(audiencesOutputSchema),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "audiences", Required: true, Description: "1 to 10 audiences to create. Each has name (1 to 255 " +
			"characters, required with the scope CUSTOMER), description (up to 4096 bytes), dimensions (up to 10, " +
			"each exactly one of age with age_ranges of min_age 18, 25, 35, 45, 55 or 65 and max_age 24, 34, 44, 54 " +
			"or 64; gender with MALE, FEMALE, UNDETERMINED; household_income with INCOME_RANGE_0_50, _50_60, " +
			"_60_70, _70_80, _80_90, _90_UP, _UNDETERMINED; parental_status with PARENT, NOT_A_PARENT, " +
			"UNDETERMINED; each of these with include_undetermined; audience_segments with up to 100 segments, " +
			"each exactly one of user_list, user_interest_category, life_event, detailed_demographic, " +
			"custom_audience as customers/ID/userLists/N, userInterests, lifeEvents, detailedDemographics, " +
			"customAudiences of this account), exclusion_dimension (exclusions, each with a user_list of this " +
			"account), scope (CUSTOMER or ASSET_GROUP) and asset_group (customers/ID/assetGroups/N of this " +
			"account, required with ASSET_GROUP and otherwise not allowed). Each needs a dimension or an exclusion"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: audienceFields,
	Examples: []capability.Example{{Description: "Validate an audience of adults aged 25 to 44 without the people on a user list",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","audiences":[{"name":"Adults 25-44","dimensions":[` +
			`{"age":{"age_ranges":[{"min_age":25,"max_age":44}]}}],"exclusion_dimension":{"exclusions":[` +
			`{"user_list":"customers/1234567890/userLists/55"}]}}],"validate_only":true}`)}},
}

var audienceFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per audience in the order sent: resource_name " +
		"(customers/ID/audiences/N), updated (after an update: the fields the change named by argument name) " +
		"and, with MUTABLE_RESOURCE, audience with id, status, name, description, dimensions, " +
		"exclusion_dimension, scope and asset_group as Google returns them; empty after validate_only"}}

var audiencesUpdate = capability.Descriptor{
	ID: audiencesUpdateID, Version: 1, Title: "Change audiences of a Google Ads account",
	Description: "Change audiences by ID (AudienceService.MutateAudiences, update with its field mask); only the " +
		"fields that are given change. Giving dimensions replaces all dimensions, and giving exclusion_dimension " +
		"replaces the exclusions; an empty list of exclusions clears them. The scope can only be set to CUSTOMER " +
		"to make the audience of an asset group an audience of the account; the asset group cannot be changed." +
		audiencesNote + " On a connection with targets, a manager account needs a manager target that names it." +
		mutateListNote,
	Tags: []string{"google", "ads", "audiences", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, audiencesSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"audiences": audienceOperationListSchema(object(schema{"id": audienceNumberIDSchema, "name": audienceNameSchema,
			"description":         audienceTextSchema,
			"dimensions":          schema{"type": "array", "minItems": 1, "maxItems": audienceMaxDimensions, "items": audienceDimensionSchema(true)},
			"exclusion_dimension": audienceExclusionSchema, "scope": audienceEnumSchema([]string{"CUSTOMER"})}, "id")),
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "audiences")),
	OutputSchema: encoded(audiencesOutputSchema),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "audiences", Required: true, Description: "1 to 10 updates, each for a different audience of the " +
			"account. Each has id (the number of customers/ID/audiences/N) and at least one of name (1 to 255 " +
			"characters), description (empty clears it), dimensions (1 to 10, replaces all; same form as in " +
			"audiences.create), exclusion_dimension (replaces the exclusions, an empty exclusions list clears " +
			"them) and scope (only CUSTOMER)"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: audienceFields,
	Examples: []capability.Example{{Description: "Rename an audience",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","audiences":[{"id":"123","name":"Adults 25-44"}]}`)}},
}

func handleAudiencesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create audiences"
	var args audienceCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	operations, err := args.operations()
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: audiencesCreateID}
	results, err := env.audienceMutateList(args.CustomerID, audienceService, audienceCollection, operations, nil,
		args.ValidateOnly, args.ResponseContentType)
	if err != nil {
		return nil, err
	}
	out, err := audiencesOf(args.CustomerID, args.ValidateOnly, results, nil)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func handleAudiencesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update audiences"
	var args audienceUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	operations, names, changed, err := args.operations()
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: audiencesUpdateID}
	results, err := env.audienceMutateList(args.CustomerID, audienceService, audienceCollection, operations, names,
		args.ValidateOnly, args.ResponseContentType)
	if err != nil {
		return nil, err
	}
	out, err := audiencesOf(args.CustomerID, args.ValidateOnly, results, changed)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// audienceDimensionKinds names the dimensions of an audience for the confirmation.
func audienceDimensionKinds(dims []audienceDimension) string {
	var kinds []string
	for _, d := range dims {
		switch {
		case d.Age != nil:
			kinds = append(kinds, "age")
		case d.Gender != nil:
			kinds = append(kinds, "gender")
		case d.HouseholdIncome != nil:
			kinds = append(kinds, "household income")
		case d.ParentalStatus != nil:
			kinds = append(kinds, "parental status")
		case d.AudienceSegments != nil:
			kinds = append(kinds, audiencePlural(len(d.AudienceSegments.Segments), "audience segment"))
		}
	}
	if len(kinds) == 0 {
		return "no dimension"
	}
	return strings.Join(kinds, ", ")
}

func audienceExclusionText(e *audienceExclusionDimension) string {
	if e == nil {
		return ""
	}
	if len(e.Exclusions) == 0 {
		return "no exclusions"
	}
	return "excludes " + audiencePlural(len(e.Exclusions), "user list")
}

func audiencesCreateSummary(raw json.RawMessage) string {
	var a audienceCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, x := range a.Audiences {
		scope := x.Scope
		if scope == "" {
			scope = "CUSTOMER"
		}
		label := audienceClipped(x.Name)
		if scope == "ASSET_GROUP" {
			label = "audience of " + boundedText(x.AssetGroup, 100)
		}
		item := label + " (scope " + scope + "; " + audienceDimensionKinds(x.Dimensions)
		if ex := audienceExclusionText(x.ExclusionDimension); ex != "" {
			item += "; " + ex
		}
		parts = append(parts, item+")")
	}
	return audienceValidatePrefix(a.ValidateOnly, "Create "+audiencePlural(len(a.Audiences), "audience")+" in Google Ads account "+
		a.CustomerID+": "+strings.Join(parts, "; ")+". The audiences decide whom ads reach once they are used.")
}

func audiencesUpdateSummary(raw json.RawMessage) string {
	var a audienceUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, x := range a.Audiences {
		var what []string
		if x.Name != nil {
			what = append(what, "set the name to "+audienceClipped(*x.Name))
		}
		if x.Description != nil {
			what = append(what, "set the description")
		}
		if x.Dimensions != nil {
			what = append(what, "replace all dimensions by "+audienceDimensionKinds(*x.Dimensions))
		}
		if x.ExclusionDimension != nil {
			what = append(what, "replace the exclusions: "+audienceExclusionText(x.ExclusionDimension))
		}
		if x.Scope != nil {
			what = append(what, "set the scope to "+boundedText(*x.Scope, 40))
		}
		parts = append(parts, "audience "+boundedText(x.ID, 24)+": "+strings.Join(what, ", "))
	}
	return audienceValidatePrefix(a.ValidateOnly, "Change "+audiencePlural(len(a.Audiences), "audience")+" in Google Ads account "+
		a.CustomerID+": "+strings.Join(parts, "; ")+". Campaigns that use these audiences reach other people afterwards.")
}
