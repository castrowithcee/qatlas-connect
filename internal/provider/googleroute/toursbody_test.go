package googleroute

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// wantFields is the fixed list of the fields of every object of the model, from the reference of the API
// (REST v1 and RPC google.maps.routeoptimization.v1). It is written out on purpose: the schema is generated
// from the node tree, so only an independent list shows a field that went missing.
var wantFields = map[string]string{
	"ShipmentModel": "shipments vehicles objectives globalStartTime globalEndTime " +
		"globalDurationCostPerHour durationDistanceMatrices durationDistanceMatrixSrcTags " +
		"durationDistanceMatrixDstTags transitionAttributes shipmentTypeIncompatibilities " +
		"shipmentTypeRequirements precedenceRules maxActiveVehicles",
	"Shipment": "displayName pickups deliveries loadDemands allowedVehicleIndices costsPerVehicle " +
		"costsPerVehicleIndices pickupToDeliveryAbsoluteDetourLimit pickupToDeliveryTimeLimit shipmentType " +
		"label ignore penaltyCost pickupToDeliveryRelativeDetourLimit",
	"VisitRequest": "arrivalLocation arrivalWaypoint departureLocation departureWaypoint tags timeWindows " +
		"duration cost loadDemands visitTypes label avoidUTurns",
	"Load":     "amount",
	"LatLng":   "latitude longitude",
	"Waypoint": "sideOfRoad vehicleStopover location placeId",
	"Location": "latLng heading",
	"TimeWindow": "startTime endTime softStartTime softEndTime costPerHourBeforeSoftStartTime " +
		"costPerHourAfterSoftEndTime",
	"Vehicle": "displayName travelMode routeModifiers startLocation startWaypoint endLocation endWaypoint " +
		"startTags endTags startTimeWindows endTimeWindows unloadingPolicy loadLimits costPerHour " +
		"costPerTraveledHour costPerKilometer fixedCost usedIfRouteIsEmpty routeDurationLimit " +
		"travelDurationLimit routeDistanceLimit extraVisitDurationForVisitType breakRule label ignore " +
		"travelDurationMultiple",
	"RouteModifiers": "avoidTolls avoidHighways avoidFerries avoidIndoor",
	"LoadLimit": "softMaxLoad costPerUnitAboveSoftMax startLoadInterval endLoadInterval maxLoad " +
		"costPerKilometer costPerTraveledHour",
	"Interval": "min max",
	"LoadCost": "loadThreshold costPerUnitBelowThreshold costPerUnitAboveThreshold",
	"DurationLimit": "maxDuration softMaxDuration quadraticSoftMaxDuration costPerHourAfterSoftMax " +
		"costPerSquareHourAfterQuadraticSoftMax",
	"DistanceLimit":          "maxMeters softMaxMeters costPerKilometerBelowSoftMax costPerKilometerAboveSoftMax",
	"BreakRule":              "breakRequests frequencyConstraints",
	"BreakRequest":           "earliestStartTime latestStartTime minDuration",
	"FrequencyConstraint":    "minBreakDuration maxInterBreakDuration",
	"Objective":              "type weight",
	"DurationDistanceMatrix": "rows vehicleStartTag",
	"Row":                    "durations meters",
	"TransitionAttributes": "srcTag excludedSrcTag dstTag excludedDstTag cost costPerKilometer distanceLimit " +
		"delay",
	"ShipmentTypeIncompatibility": "types incompatibilityMode",
	"ShipmentTypeRequirement":     "requiredShipmentTypeAlternatives dependentShipmentTypes requirementMode",
	"PrecedenceRule":              "firstIsDelivery secondIsDelivery offsetDuration firstIndex secondIndex",
	"ShipmentRoute": "vehicleIndex vehicleLabel vehicleStartTime vehicleEndTime visits transitions " +
		"hasTrafficInfeasibilities routePolyline breaks metrics vehicleFullness routeCosts routeTotalCost",
	"Visit": "shipmentIndex isPickup visitRequestIndex startTime loadDemands detour shipmentLabel visitLabel " +
		"visitType injectedSolutionLocationToken",
	"Transition": "travelDuration travelDistanceMeters trafficInfoUnavailable delayDuration breakDuration " +
		"waitDuration totalDuration startTime routePolyline routeToken vehicleLoads",
	"EncodedPolyline": "points",
	"Break":           "startTime duration",
	"AggregatedMetrics": "performedShipmentCount travelDuration waitDuration delayDuration breakDuration " +
		"visitDuration totalDuration travelDistanceMeters maxLoads performedMandatoryShipmentCount " +
		"performedShipmentPenaltyCostSum",
	"VehicleLoad":                "amount",
	"VehicleFullness":            "maxFullness distance travelDuration activeDuration maxLoad activeSpan",
	"SkippedShipment":            "index label reasons penaltyCost estimatedIncompatibleVehicleRatio",
	"Reason":                     "code exampleVehicleIndices exampleExceededCapacityType exampleVehicleIndex",
	"InjectedSolutionConstraint": "routes skippedShipments constraintRelaxations",
	"ConstraintRelaxation":       "relaxations vehicleIndices",
	"Relaxation":                 "level thresholdTime thresholdVisitCount",
}

// wantEnums are the values offered for each enumeration, without the unspecified value.
var wantEnums = map[string]string{
	"TravelMode":          "DRIVING WALKING TWO_WHEELER",
	"UnloadingPolicy":     "LAST_IN_FIRST_OUT FIRST_IN_FIRST_OUT",
	"ObjectiveType":       "DEFAULT MIN_DISTANCE MIN_WORKING_TIME MIN_TRAVEL_TIME MIN_NUM_VEHICLES",
	"IncompatibilityMode": "NOT_PERFORMED_BY_SAME_VEHICLE NOT_IN_SAME_VEHICLE_SIMULTANEOUSLY",
	"RequirementMode":     "PERFORMED_BY_SAME_VEHICLE IN_SAME_VEHICLE_AT_PICKUP_TIME IN_SAME_VEHICLE_AT_DELIVERY_TIME",
	"VisitType":           "PICKUP_SHIPMENT DELIVER_SHIPMENT",
	"RelaxationLevel":     "RELAX_VISIT_TIMES_AFTER_THRESHOLD RELAX_VISIT_TIMES_AND_SEQUENCE_AFTER_THRESHOLD RELAX_ALL_AFTER_THRESHOLD",
	"SolvingMode":         "DEFAULT_SOLVE VALIDATE_ONLY DETECT_SOME_INFEASIBLE_SHIPMENTS TRANSFORM_AND_RETURN_REQUEST",
	"SearchMode":          "RETURN_FAST CONSUME_ALL_AVAILABLE_TIME",
	"ReasonCode": "NO_VEHICLE DEMAND_EXCEEDS_VEHICLE_CAPACITY CANNOT_BE_PERFORMED_WITHIN_VEHICLE_DISTANCE_LIMIT " +
		"CANNOT_BE_PERFORMED_WITHIN_VEHICLE_DURATION_LIMIT CANNOT_BE_PERFORMED_WITHIN_VEHICLE_TRAVEL_DURATION_LIMIT " +
		"CANNOT_BE_PERFORMED_WITHIN_VEHICLE_TIME_WINDOWS VEHICLE_NOT_ALLOWED VEHICLE_IGNORED SHIPMENT_IGNORED " +
		"SKIPPED_IN_INJECTED_SOLUTION_CONSTRAINT VEHICLE_ROUTE_IS_FULLY_SEQUENCE_CONSTRAINED ZERO_PENALTY_COST",
}

// wantTop maps every argument of the request to Google's member. project is the path of the call, not a member.
var wantTop = map[string]string{
	"model": "model", "timeout": "timeout", "solving_mode": "solvingMode", "search_mode": "searchMode",
	"injected_first_solution_routes":            "injectedFirstSolutionRoutes",
	"injected_solution_constraint":              "injectedSolutionConstraint",
	"refresh_details_routes":                    "refreshDetailsRoutes",
	"interpret_injected_solutions_using_labels": "interpretInjectedSolutionsUsingLabels",
	"consider_road_traffic":                     "considerRoadTraffic",
	"populate_polylines":                        "populatePolylines",
	"populate_transition_polylines":             "populateTransitionPolylines",
	"use_geodesic_distances":                    "useGeodesicDistances",
	"geodesic_meters_per_second":                "geodesicMetersPerSecond",
	"label":                                     "label",
	"max_validation_errors":                     "maxValidationErrors",
}

func fieldsOf(list string) []string {
	names := strings.Fields(list)
	sort.Strings(names)
	return names
}

// roots are the trees the request carries.
func roots() []*node {
	out := []*node{}
	for _, a := range toursArgs {
		out = append(out, a.n)
	}
	return out
}

func TestRequestModelMatchesTheReference(t *testing.T) {
	objects, enums := map[string][]string{}, map[string][]string{}
	for _, root := range roots() {
		root.walk(func(n *node) {
			if n.kind == kindEnum {
				enums[n.name] = append([]string(nil), n.values...)
				return
			}
			objects[n.name] = n.memberNames()
		})
	}
	for name, list := range wantFields {
		got, ok := objects[name]
		if !ok {
			t.Errorf("type %s is missing from the model", name)
			continue
		}
		if strings.Join(got, " ") != strings.Join(fieldsOf(list), " ") {
			t.Errorf("type %s has fields %v, the reference lists %v", name, got, fieldsOf(list))
		}
	}
	for name := range objects {
		if _, ok := wantFields[name]; !ok {
			t.Errorf("type %s is not in the reference list", name)
		}
	}
	for name, list := range wantEnums {
		got, ok := enums[name]
		if !ok {
			t.Errorf("enumeration %s is missing from the model", name)
			continue
		}
		sort.Strings(got)
		if strings.Join(got, " ") != strings.Join(fieldsOf(list), " ") {
			t.Errorf("enumeration %s has values %v, the reference lists %v", name, got, fieldsOf(list))
		}
		for _, value := range got {
			if strings.HasSuffix(value, "UNSPECIFIED") {
				t.Errorf("enumeration %s offers the unspecified value %s", name, value)
			}
		}
	}
	for name := range enums {
		if _, ok := wantEnums[name]; !ok {
			t.Errorf("enumeration %s is not in the reference list", name)
		}
	}
	if len(objects) != len(wantFields) || len(enums) != len(wantEnums) {
		t.Errorf("model has %d types and %d enumerations, want %d and %d", len(objects), len(enums),
			len(wantFields), len(wantEnums))
	}
}

func TestRequestArgumentsMatchTheReference(t *testing.T) {
	got := map[string]string{}
	for _, a := range toursArgs {
		got[a.arg] = a.wire
	}
	if len(got) != len(wantTop) {
		t.Errorf("request has %d arguments, want %d", len(got), len(wantTop))
	}
	for arg, wire := range wantTop {
		if got[arg] != wire {
			t.Errorf("argument %s maps to %q, want %q", arg, got[arg], wire)
		}
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
		Additional bool                       `json:"additionalProperties"`
	}
	if err := json.Unmarshal(optimizeTours.schema(), &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Properties) != len(wantTop)+1 || schema.Properties["project"] == nil || schema.Additional {
		t.Errorf("schema members: %d, project %v, additional %v", len(schema.Properties),
			schema.Properties["project"] != nil, schema.Additional)
	}
	if _, offered := schema.Properties["allow_large_deadline_despite_interruption_risk"]; offered {
		t.Error("the schema offers allow_large_deadline_despite_interruption_risk")
	}
	sort.Strings(schema.Required)
	if strings.Join(schema.Required, " ") != "model project" {
		t.Errorf("required arguments %v, want model and project", schema.Required)
	}
}

// sample fills every field of the node with a valid value, so a whole model passes through the checks.
func sample(n *node) any {
	switch n.kind {
	case kindObject:
		out := map[string]any{}
		for _, field := range n.props {
			out[field.name] = sample(field.n)
		}
		return out
	case kindArray:
		return []any{sample(n.elem)}
	case kindMap:
		return map[string]any{"slot-key": sample(n.elem)}
	case kindText, kindLongText:
		return "sample-text"
	case kindBool:
		return true
	case kindDouble:
		return 1.5
	case kindInt32:
		return 3
	case kindInt64:
		return "7"
	case kindDuration:
		return "90s"
	case kindTimestamp:
		return "2026-11-11T08:00:00Z"
	}
	return n.values[0]
}

func fullArguments() map[string]any {
	args := map[string]any{"project": "route-project-123"}
	for _, a := range toursArgs {
		args[a.arg] = sample(a.n)
	}
	args["timeout"] = "20s"
	args["max_validation_errors"] = 50
	return args
}

func marshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func keysOf(v any, into map[string]bool) {
	switch v := v.(type) {
	case map[string]any:
		for key, value := range v {
			if key != "slot-key" {
				into[key] = true
			}
			keysOf(value, into)
		}
	case []any:
		for _, item := range v {
			keysOf(item, into)
		}
	}
}

func TestFullModelPassesSchemaAndBuildsBody(t *testing.T) {
	raw := marshal(t, fullArguments())
	if err := application.ValidateJSON(optimizeTours.schema(), raw); err != nil {
		t.Fatalf("the schema refuses a full request: %v", err)
	}
	built, err := optimizeTours.body(raw)
	if err != nil {
		t.Fatal(err)
	}
	if built.Project != "route-project-123" || built.Shipments != 1 || built.Vehicles != 1 || built.Timeout != 20*time.Second {
		t.Errorf("checked request: %+v", built)
	}
	var body map[string]any
	if err := json.Unmarshal(built.Payload, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["project"]; ok {
		t.Error("project is part of the body")
	}
	for _, wire := range wantTop {
		if _, ok := body[wire]; !ok {
			t.Errorf("body lacks %s", wire)
		}
	}
	if len(body) != len(wantTop) {
		t.Errorf("body has %d members, want %d", len(body), len(wantTop))
	}
	// Every field of the reference travels in the body under Google's name.
	seen := map[string]bool{}
	keysOf(body["model"], seen)
	keysOf(body["injectedSolutionConstraint"], seen)
	for name, list := range wantFields {
		if name == "ShipmentModel" {
			continue
		}
		for _, field := range fieldsOf(list) {
			if !seen[field] {
				t.Errorf("body lacks field %s of %s", field, name)
			}
		}
	}
	if body["timeout"] != "20s" || body["maxValidationErrors"].(float64) != 50 {
		t.Errorf("timeout or maxValidationErrors changed: %v %v", body["timeout"], body["maxValidationErrors"])
	}
}

func TestBodyMapsSnakeCaseAndDefaultsTheTimeout(t *testing.T) {
	raw := json.RawMessage(`{"project":"123456789","model":{"shipments":[{"label":"s1"},{}],` +
		`"vehicles":[{"startLocation":{"latitude":52.5,"longitude":13.4},"loadLimits":{"weight":{"maxLoad":"10"}}}]},` +
		`"solving_mode":"VALIDATE_ONLY","populate_polylines":true,"geodesic_meters_per_second":8.5,"label":"run-a"}`)
	built, err := optimizeTours.body(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"geodesicMetersPerSecond":8.5,"label":"run-a","model":{"shipments":[{"label":"s1"},{}],` +
		`"vehicles":[{"loadLimits":{"weight":{"maxLoad":"10"}},"startLocation":{"latitude":52.5,"longitude":13.4}}]},` +
		`"populatePolylines":true,"solvingMode":"VALIDATE_ONLY","timeout":"30s"}`
	if string(built.Payload) != want {
		t.Errorf("body\n got %s\nwant %s", built.Payload, want)
	}
	if built.Project != "123456789" || built.Shipments != 2 || built.Vehicles != 1 || built.Timeout != 30*time.Second {
		t.Errorf("checked request: %+v", built)
	}
	// A number given as number and as string both stay as written; Google reads either.
	if !strings.Contains(string(built.Payload), `"maxLoad":"10"`) {
		t.Error("an int64 string was changed")
	}
}

func TestBodyRefusesMalformedRequests(t *testing.T) {
	model := `"model":{"shipments":[{}],"vehicles":[{}]}`
	cases := map[string]string{
		"model missing":           `{"project":"route-project-123"}`,
		"model is a list":         `{"project":"route-project-123","model":[]}`,
		"project missing":         `{` + model + `}`,
		"project malformed":       `{"project":"Route Project",` + model + `}`,
		"project is a number":     `{"project":123456,` + model + `}`,
		"unknown argument":        `{"project":"route-project-123",` + model + `,"parent":"projects/x"}`,
		"large deadline":          `{"project":"route-project-123",` + model + `,"allow_large_deadline_despite_interruption_risk":true}`,
		"camelCase top argument":  `{"project":"route-project-123",` + model + `,"solvingMode":"VALIDATE_ONLY"}`,
		"unknown model field":     `{"project":"route-project-123","model":{"shipmentz":[]}}`,
		"snake_case model field":  `{"project":"route-project-123","model":{"global_start_time":"2026-11-11T08:00:00Z"}}`,
		"unknown nested field":    `{"project":"route-project-123","model":{"shipments":[{"pickups":[{"arrivalLocation":{"lat":1}}]}]}}`,
		"list expected":           `{"project":"route-project-123","model":{"shipments":{}}}`,
		"object expected":         `{"project":"route-project-123","model":{"vehicles":[{"breakRule":"none"}]}}`,
		"string expected":         `{"project":"route-project-123","model":{"shipments":[{"label":7}]}}`,
		"bool expected":           `{"project":"route-project-123","model":{"shipments":[{"ignore":"yes"}]}}`,
		"number expected":         `{"project":"route-project-123","model":{"vehicles":[{"costPerHour":"2"}]}}`,
		"int32 is fractional":     `{"project":"route-project-123","model":{"maxActiveVehicles":1.5}}`,
		"int32 overflows":         `{"project":"route-project-123","model":{"maxActiveVehicles":2147483648}}`,
		"int64 is text":           `{"project":"route-project-123","model":{"shipments":[{"loadDemands":{"w":{"amount":"many"}}}]}}`,
		"int64 is fractional":     `{"project":"route-project-123","model":{"shipments":[{"loadDemands":{"w":{"amount":1.5}}}]}}`,
		"duration without s":      `{"project":"route-project-123","model":{"shipments":[{"pickupToDeliveryTimeLimit":"60"}]}}`,
		"duration is a number":    `{"project":"route-project-123","model":{"shipments":[{"pickupToDeliveryTimeLimit":60}]}}`,
		"timestamp malformed":     `{"project":"route-project-123","model":{"globalStartTime":"2026-11-11 08:00"}}`,
		"timestamp impossible":    `{"project":"route-project-123","model":{"globalStartTime":"2026-13-45T08:00:00Z"}}`,
		"travel mode enum":        `{"project":"route-project-123","model":{"vehicles":[{"travelMode":"FLYING"}]}}`,
		"unspecified enum":        `{"project":"route-project-123","model":{"vehicles":[{"travelMode":"TRAVEL_MODE_UNSPECIFIED"}]}}`,
		"enum is lower case":      `{"project":"route-project-123","model":{"vehicles":[{"unloadingPolicy":"last_in_first_out"}]}}`,
		"requirement mode enum":   `{"project":"route-project-123","model":{"shipmentTypeRequirements":[{"requirementMode":"ALWAYS"}]}}`,
		"solving mode enum":       `{"project":"route-project-123",` + model + `,"solving_mode":"SOLVE_FAST"}`,
		"search mode enum":        `{"project":"route-project-123",` + model + `,"search_mode":"SEARCH_MODE_UNSPECIFIED"}`,
		"relaxation level enum":   `{"project":"route-project-123",` + model + `,"injected_solution_constraint":{"constraintRelaxations":[{"relaxations":[{"level":"LEVEL_UNSPECIFIED"}]}]}}`,
		"route visit type enum":   `{"project":"route-project-123",` + model + `,"refresh_details_routes":[{"visits":[{"visitType":"PICKUP"}]}]}`,
		"routes not a list":       `{"project":"route-project-123",` + model + `,"injected_first_solution_routes":{}}`,
		"validation errors high":  `{"project":"route-project-123",` + model + `,"max_validation_errors":10001}`,
		"validation errors minus": `{"project":"route-project-123",` + model + `,"max_validation_errors":-1}`,
		"validation errors text":  `{"project":"route-project-123",` + model + `,"max_validation_errors":"5"}`,
		"flag is text":            `{"project":"route-project-123",` + model + `,"consider_road_traffic":"true"}`,
		"label too long":          `{"project":"route-project-123",` + model + `,"label":"` + strings.Repeat("l", maxTextRunes+1) + `"}`,
		"too many load entries":   `{"project":"route-project-123","model":{"shipments":[{"loadDemands":` + manyEntries(maxMapEntries+1) + `}]}}`,
		"two json values":         `{"project":"route-project-123",` + model + `} {}`,
		"not an object":           `[]`,
	}
	for name, raw := range cases {
		_, err := optimizeTours.body(json.RawMessage(raw))
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: want an invalid request, got %v", name, err)
			continue
		}
		if strings.Contains(err.Error(), "many") || strings.Contains(err.Error(), "FLYING") {
			t.Errorf("%s: the message quotes the value: %v", name, err)
		}
	}
}

func manyEntries(n int) string {
	var b strings.Builder
	b.WriteString("{")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"k` + strconv.Itoa(i) + `":{"amount":"1"}`)
	}
	b.WriteString("}")
	return b.String()
}

// The schema refuses what its keywords can express, before a handler runs; the check of the body is the second
// line and must agree.
func TestSchemaRefusesTheSameForm(t *testing.T) {
	schema := optimizeTours.schema()
	for name, raw := range map[string]string{
		"unknown model field": `{"project":"route-project-123","model":{"shipmentz":[]}}`,
		"enum":                `{"project":"route-project-123","model":{"vehicles":[{"travelMode":"FLYING"}]}}`,
		"type":                `{"project":"route-project-123","model":{"shipments":[{"ignore":"yes"}]}}`,
		"duration form":       `{"project":"route-project-123","model":{"shipments":[{"pickupToDeliveryTimeLimit":"60"}]}}`,
		"timestamp form":      `{"project":"route-project-123","model":{"globalStartTime":"tomorrow"}}`,
		"int64":               `{"project":"route-project-123","model":{"shipments":[{"loadDemands":{"w":{"amount":"many"}}}]}}`,
		"int32":               `{"project":"route-project-123","model":{"maxActiveVehicles":2147483648}}`,
		"validation errors":   `{"project":"route-project-123","model":{},"max_validation_errors":10001}`,
		"model missing":       `{"project":"route-project-123"}`,
		"project":             `{"project":"x","model":{}}`,
	} {
		if err := application.ValidateJSON(schema, json.RawMessage(raw)); err == nil {
			t.Errorf("%s: the schema accepts the request", name)
		}
	}
	// What Google decides stays with Google: an inconsistent but well-formed model is passed on.
	consistent := `{"project":"route-project-123","use_geodesic_distances":true,` +
		`"refresh_details_routes":[{}],"injected_first_solution_routes":[{}],` +
		`"model":{"globalStartTime":"2026-11-11T08:00:00Z","globalEndTime":"2030-11-11T08:00:00Z",` +
		`"shipments":[{"pickups":[{"timeWindows":[{"startTime":"2027-01-01T00:00:00Z","endTime":"2026-01-01T00:00:00Z"}]}]}]}}`
	if err := application.ValidateJSON(schema, json.RawMessage(consistent)); err != nil {
		t.Errorf("the schema applies a consistency rule of Google: %v", err)
	}
	if _, err := optimizeTours.body(json.RawMessage(consistent)); err != nil {
		t.Errorf("the body applies a consistency rule of Google: %v", err)
	}
}

func TestTimeoutIsBoundedBelowTheInvokeLimit(t *testing.T) {
	if toursMaxTimeout >= 60*time.Second || toursDefaultTimeout > toursMaxTimeout {
		t.Fatalf("timeout bounds %v and %v do not fit an invoke of 60 seconds", toursDefaultTimeout, toursMaxTimeout)
	}
	build := func(timeout string) (toursBody, error) {
		return optimizeTours.body(json.RawMessage(`{"project":"route-project-123","model":{},"timeout":"` + timeout + `"}`))
	}
	for timeout, want := range map[string]time.Duration{"1s": time.Second, "1.5s": 1500 * time.Millisecond,
		"45s": 45 * time.Second, "44.999999999s": 45*time.Second - 1} {
		built, err := build(timeout)
		if err != nil || built.Timeout != want {
			t.Errorf("timeout %s: %v, %v", timeout, built.Timeout, err)
		}
	}
	for _, timeout := range []string{"0s", "-5s", "45.000000001s", "46s", "600s", "1800s", "9999999999999s"} {
		_, err := build(timeout)
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "long-running") {
			t.Errorf("timeout %s: want a refusal that names the long-running tool, got %v", timeout, err)
		}
	}
	built, err := optimizeTours.body(json.RawMessage(`{"project":"route-project-123","model":{}}`))
	if err != nil || built.Timeout != toursDefaultTimeout || !strings.Contains(string(built.Payload), `"timeout":"30s"`) {
		t.Errorf("default timeout: %+v, %v", built, err)
	}
}

func TestOversizedModelIsRefused(t *testing.T) {
	label := strings.Repeat("a", maxTextRunes)
	shipments := make([]map[string]string, 0, 2200)
	for len(shipments)*(maxTextRunes+12) <= maxToursBodyBytes {
		shipments = append(shipments, map[string]string{"label": label})
	}
	raw := marshal(t, map[string]any{"project": "route-project-123", "model": map[string]any{"shipments": shipments}})
	_, err := optimizeTours.body(raw)
	var invalid *provider.InvalidRequestError
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "2 MiB") {
		t.Fatalf("want a refusal that names the bound, got %v", err)
	}
	// A model just below the bound passes.
	small := marshal(t, map[string]any{"project": "route-project-123", "model": map[string]any{"shipments": shipments[:len(shipments)-100]}})
	if _, err := optimizeTours.body(small); err != nil {
		t.Errorf("a model below the bound is refused: %v", err)
	}
}

// A later method that takes the same body with one more argument reuses the request.
func TestRequestIsReusableWithAnExtraArgument(t *testing.T) {
	long := toursRequest{
		extra:          []topArg{top("operation_label", "operationLabel", tText)},
		defaultTimeout: 30 * time.Minute, maxTimeout: 3 * time.Hour,
	}
	raw := json.RawMessage(`{"project":"route-project-123","model":{},"operation_label":"night","timeout":"7200s"}`)
	if err := application.ValidateJSON(long.schema(), raw); err != nil {
		t.Fatal(err)
	}
	built, err := long.body(raw)
	if err != nil {
		t.Fatal(err)
	}
	if built.Timeout != 2*time.Hour || built.Extra["operation_label"] != "night" ||
		!strings.Contains(string(built.Payload), `"operationLabel":"night"`) {
		t.Errorf("checked request: %+v %s", built, built.Payload)
	}
	if err := application.ValidateJSON(optimizeTours.schema(), raw); err == nil {
		t.Error("the synchronous schema accepts the extra argument")
	}
	if _, err := optimizeTours.body(raw); err == nil {
		t.Error("the synchronous body accepts the extra argument")
	}
}
