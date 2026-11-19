package googleroute

import (
	"math"
	"sort"
)

// The request model of the optimization methods follows the reference of the Route Optimization API v1
// (REST and RPC). It is described once as a tree of nodes. The tree yields the input schema and drives the
// check of an argument, so the two cannot drift apart. Nested objects carry Google's JSON names. The unspecified
// value of an enumeration is not offered, because Google treats it as the default.

type nodeKind int

const (
	kindObject nodeKind = iota
	kindArray
	kindMap
	kindText
	kindLongText
	kindBool
	kindDouble
	kindInt32
	kindInt64
	kindDuration
	kindTimestamp
	kindEnum
)

// node is one type of the model. name is set on objects and enumerations, so that a test can match them
// against the reference.
type node struct {
	kind   nodeKind
	name   string
	props  []prop
	elem   *node
	values []string
}

type prop struct {
	name string
	n    *node
}

// Bounds of Qatlas that the reference does not give. The size of the whole body is bounded in toursbody.go.
const (
	maxTextRunes     = 1024
	maxPolylineRunes = 1 << 20
	maxMapEntries    = 100
)

var (
	tText      = &node{kind: kindText}
	tLongText  = &node{kind: kindLongText}
	tBool      = &node{kind: kindBool}
	tDouble    = &node{kind: kindDouble}
	tInt32     = &node{kind: kindInt32}
	tInt64     = &node{kind: kindInt64}
	tDuration  = &node{kind: kindDuration}
	tTimestamp = &node{kind: kindTimestamp}
)

func object(name string, props ...prop) *node {
	return &node{kind: kindObject, name: name, props: props}
}
func p(name string, n *node) prop { return prop{name, n} }
func array(n *node) *node         { return &node{kind: kindArray, elem: n} }
func dict(n *node) *node          { return &node{kind: kindMap, elem: n} }
func enum(name string, values ...string) *node {
	return &node{kind: kindEnum, name: name, values: values}
}

const (
	int32Min = math.MinInt32
	int32Max = math.MaxInt32

	// maxValidationErrorsLimit is the highest value Google documents for maxValidationErrors.
	maxValidationErrorsLimit = 10000
)

const (
	durationPattern  = `^-?[0-9]{1,13}(\.[0-9]{1,9})?s$`
	timestampPattern = `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-][0-9]{2}:[0-9]{2})$`
	int64Pattern     = `^-?[0-9]{1,19}$`
)

// schema renders the node as a JSON Schema in the subset the invoke core checks.
func (n *node) schema() map[string]any {
	switch n.kind {
	case kindObject:
		properties := make(map[string]any, len(n.props))
		for _, field := range n.props {
			properties[field.name] = field.n.schema()
		}
		return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	case kindArray:
		return map[string]any{"type": "array", "items": n.elem.schema()}
	case kindMap:
		return map[string]any{"type": "object", "additionalProperties": n.elem.schema(), "maxProperties": maxMapEntries}
	case kindText:
		return map[string]any{"type": "string", "maxLength": maxTextRunes}
	case kindLongText:
		return map[string]any{"type": "string", "maxLength": maxPolylineRunes}
	case kindBool:
		return map[string]any{"type": "boolean"}
	case kindDouble:
		return map[string]any{"type": "number"}
	case kindInt32:
		return map[string]any{"type": "integer", "minimum": int32Min, "maximum": int32Max}
	case kindInt64:
		// Google reads an int64 as a JSON number or as a decimal string and writes it as a string.
		return map[string]any{"anyOf": []any{
			map[string]any{"type": "string", "pattern": int64Pattern},
			map[string]any{"type": "integer"},
		}}
	case kindDuration:
		return map[string]any{"type": "string", "pattern": durationPattern, "maxLength": 32,
			"x-form": "a number of seconds with an s, such as 3600s or 0.5s"}
	case kindTimestamp:
		return map[string]any{"type": "string", "pattern": timestampPattern, "maxLength": 40,
			"x-form": "an RFC 3339 timestamp in UTC or with an offset, such as 2026-11-11T08:00:00Z"}
	default:
		values := make([]any, len(n.values))
		for i, value := range n.values {
			values[i] = value
		}
		return map[string]any{"type": "string", "enum": values}
	}
}

// walk calls visit for every named object and enumeration below n, each name once.
func (n *node) walk(visit func(*node)) {
	seen := map[string]bool{}
	var step func(*node)
	step = func(n *node) {
		if n.name != "" {
			if seen[n.name] {
				return
			}
			seen[n.name] = true
			visit(n)
		}
		for _, field := range n.props {
			step(field.n)
		}
		if n.elem != nil {
			step(n.elem)
		}
	}
	step(n)
}

// memberNames returns the sorted JSON names of an object.
func (n *node) memberNames() []string {
	names := make([]string, len(n.props))
	for i, field := range n.props {
		names[i] = field.name
	}
	sort.Strings(names)
	return names
}

func (n *node) member(name string) *node {
	for _, field := range n.props {
		if field.name == name {
			return field.n
		}
	}
	return nil
}

var (
	tLatLng = object("LatLng", p("latitude", tDouble), p("longitude", tDouble))

	tLocation = object("Location", p("latLng", tLatLng), p("heading", tInt32))

	tWaypoint = object("Waypoint", p("sideOfRoad", tBool), p("vehicleStopover", tBool),
		p("location", tLocation), p("placeId", tText))

	tTimeWindow = object("TimeWindow", p("startTime", tTimestamp), p("endTime", tTimestamp),
		p("softStartTime", tTimestamp), p("softEndTime", tTimestamp),
		p("costPerHourBeforeSoftStartTime", tDouble), p("costPerHourAfterSoftEndTime", tDouble))

	tLoad = object("Load", p("amount", tInt64))

	tVisitRequest = object("VisitRequest",
		p("arrivalLocation", tLatLng), p("arrivalWaypoint", tWaypoint),
		p("departureLocation", tLatLng), p("departureWaypoint", tWaypoint),
		p("tags", array(tText)), p("timeWindows", array(tTimeWindow)), p("duration", tDuration),
		p("cost", tDouble), p("loadDemands", dict(tLoad)), p("visitTypes", array(tText)),
		p("label", tText), p("avoidUTurns", tBool))

	tShipment = object("Shipment",
		p("displayName", tText), p("pickups", array(tVisitRequest)), p("deliveries", array(tVisitRequest)),
		p("loadDemands", dict(tLoad)), p("allowedVehicleIndices", array(tInt32)),
		p("costsPerVehicle", array(tDouble)), p("costsPerVehicleIndices", array(tInt32)),
		p("pickupToDeliveryAbsoluteDetourLimit", tDuration), p("pickupToDeliveryTimeLimit", tDuration),
		p("shipmentType", tText), p("label", tText), p("ignore", tBool), p("penaltyCost", tDouble),
		p("pickupToDeliveryRelativeDetourLimit", tDouble))

	tRouteModifiers = object("RouteModifiers", p("avoidTolls", tBool), p("avoidHighways", tBool),
		p("avoidFerries", tBool), p("avoidIndoor", tBool))

	tInterval = object("Interval", p("min", tInt64), p("max", tInt64))

	tLoadCost = object("LoadCost", p("loadThreshold", tInt64), p("costPerUnitBelowThreshold", tDouble),
		p("costPerUnitAboveThreshold", tDouble))

	tLoadLimit = object("LoadLimit", p("softMaxLoad", tInt64), p("costPerUnitAboveSoftMax", tDouble),
		p("startLoadInterval", tInterval), p("endLoadInterval", tInterval), p("maxLoad", tInt64),
		p("costPerKilometer", tLoadCost), p("costPerTraveledHour", tLoadCost))

	tDurationLimit = object("DurationLimit", p("maxDuration", tDuration), p("softMaxDuration", tDuration),
		p("quadraticSoftMaxDuration", tDuration), p("costPerHourAfterSoftMax", tDouble),
		p("costPerSquareHourAfterQuadraticSoftMax", tDouble))

	tDistanceLimit = object("DistanceLimit", p("maxMeters", tInt64), p("softMaxMeters", tInt64),
		p("costPerKilometerBelowSoftMax", tDouble), p("costPerKilometerAboveSoftMax", tDouble))

	tBreakRequest = object("BreakRequest", p("earliestStartTime", tTimestamp), p("latestStartTime", tTimestamp),
		p("minDuration", tDuration))

	tFrequencyConstraint = object("FrequencyConstraint", p("minBreakDuration", tDuration),
		p("maxInterBreakDuration", tDuration))

	tBreakRule = object("BreakRule", p("breakRequests", array(tBreakRequest)),
		p("frequencyConstraints", array(tFrequencyConstraint)))

	tVehicle = object("Vehicle",
		p("displayName", tText),
		p("travelMode", enum("TravelMode", "DRIVING", "WALKING", "TWO_WHEELER")),
		p("routeModifiers", tRouteModifiers),
		p("startLocation", tLatLng), p("startWaypoint", tWaypoint),
		p("endLocation", tLatLng), p("endWaypoint", tWaypoint),
		p("startTags", array(tText)), p("endTags", array(tText)),
		p("startTimeWindows", array(tTimeWindow)), p("endTimeWindows", array(tTimeWindow)),
		p("unloadingPolicy", enum("UnloadingPolicy", "LAST_IN_FIRST_OUT", "FIRST_IN_FIRST_OUT")),
		p("loadLimits", dict(tLoadLimit)),
		p("costPerHour", tDouble), p("costPerTraveledHour", tDouble), p("costPerKilometer", tDouble),
		p("fixedCost", tDouble), p("usedIfRouteIsEmpty", tBool),
		p("routeDurationLimit", tDurationLimit), p("travelDurationLimit", tDurationLimit),
		p("routeDistanceLimit", tDistanceLimit),
		p("extraVisitDurationForVisitType", dict(tDuration)),
		p("breakRule", tBreakRule), p("label", tText), p("ignore", tBool), p("travelDurationMultiple", tDouble))

	tObjective = object("Objective",
		p("type", enum("ObjectiveType", "DEFAULT", "MIN_DISTANCE", "MIN_WORKING_TIME", "MIN_TRAVEL_TIME",
			"MIN_NUM_VEHICLES")),
		p("weight", tDouble))

	tRow = object("Row", p("durations", array(tDuration)), p("meters", array(tDouble)))

	tDurationDistanceMatrix = object("DurationDistanceMatrix", p("rows", array(tRow)), p("vehicleStartTag", tText))

	tTransitionAttributes = object("TransitionAttributes",
		p("srcTag", tText), p("excludedSrcTag", tText), p("dstTag", tText), p("excludedDstTag", tText),
		p("cost", tDouble), p("costPerKilometer", tDouble), p("distanceLimit", tDistanceLimit), p("delay", tDuration))

	tShipmentTypeIncompatibility = object("ShipmentTypeIncompatibility", p("types", array(tText)),
		p("incompatibilityMode", enum("IncompatibilityMode", "NOT_PERFORMED_BY_SAME_VEHICLE",
			"NOT_IN_SAME_VEHICLE_SIMULTANEOUSLY")))

	tShipmentTypeRequirement = object("ShipmentTypeRequirement",
		p("requiredShipmentTypeAlternatives", array(tText)), p("dependentShipmentTypes", array(tText)),
		p("requirementMode", enum("RequirementMode", "PERFORMED_BY_SAME_VEHICLE",
			"IN_SAME_VEHICLE_AT_PICKUP_TIME", "IN_SAME_VEHICLE_AT_DELIVERY_TIME")))

	tPrecedenceRule = object("PrecedenceRule", p("firstIsDelivery", tBool), p("secondIsDelivery", tBool),
		p("offsetDuration", tDuration), p("firstIndex", tInt32), p("secondIndex", tInt32))

	// tShipmentModel is the model of the request, in full.
	tShipmentModel = object("ShipmentModel",
		p("shipments", array(tShipment)), p("vehicles", array(tVehicle)), p("objectives", array(tObjective)),
		p("globalStartTime", tTimestamp), p("globalEndTime", tTimestamp), p("globalDurationCostPerHour", tDouble),
		p("durationDistanceMatrices", array(tDurationDistanceMatrix)),
		p("durationDistanceMatrixSrcTags", array(tText)), p("durationDistanceMatrixDstTags", array(tText)),
		p("transitionAttributes", array(tTransitionAttributes)),
		p("shipmentTypeIncompatibilities", array(tShipmentTypeIncompatibility)),
		p("shipmentTypeRequirements", array(tShipmentTypeRequirement)),
		p("precedenceRules", array(tPrecedenceRule)), p("maxActiveVehicles", tInt32))

	// The route types are the shape of a route in a response. A request carries them to inject or refresh
	// routes, so the same types serve the answer.
	tVehicleLoad = object("VehicleLoad", p("amount", tInt64))

	tEncodedPolyline = object("EncodedPolyline", p("points", tLongText))

	tVisit = object("Visit",
		p("shipmentIndex", tInt32), p("isPickup", tBool), p("visitRequestIndex", tInt32), p("startTime", tTimestamp),
		p("loadDemands", dict(tLoad)), p("detour", tDuration), p("shipmentLabel", tText), p("visitLabel", tText),
		p("visitType", enum("VisitType", "PICKUP_SHIPMENT", "DELIVER_SHIPMENT")),
		p("injectedSolutionLocationToken", tInt32))

	tTransition = object("Transition",
		p("travelDuration", tDuration), p("travelDistanceMeters", tDouble), p("trafficInfoUnavailable", tBool),
		p("delayDuration", tDuration), p("breakDuration", tDuration), p("waitDuration", tDuration),
		p("totalDuration", tDuration), p("startTime", tTimestamp), p("routePolyline", tEncodedPolyline),
		p("routeToken", tText), p("vehicleLoads", dict(tVehicleLoad)))

	tBreak = object("Break", p("startTime", tTimestamp), p("duration", tDuration))

	tAggregatedMetrics = object("AggregatedMetrics",
		p("performedShipmentCount", tInt32), p("travelDuration", tDuration), p("waitDuration", tDuration),
		p("delayDuration", tDuration), p("breakDuration", tDuration), p("visitDuration", tDuration),
		p("totalDuration", tDuration), p("travelDistanceMeters", tDouble), p("maxLoads", dict(tVehicleLoad)),
		p("performedMandatoryShipmentCount", tInt32), p("performedShipmentPenaltyCostSum", tDouble))

	tVehicleFullness = object("VehicleFullness", p("maxFullness", tDouble), p("distance", tDouble),
		p("travelDuration", tDouble), p("activeDuration", tDouble), p("maxLoad", tDouble), p("activeSpan", tDouble))

	tShipmentRoute = object("ShipmentRoute",
		p("vehicleIndex", tInt32), p("vehicleLabel", tText), p("vehicleStartTime", tTimestamp),
		p("vehicleEndTime", tTimestamp), p("visits", array(tVisit)), p("transitions", array(tTransition)),
		p("hasTrafficInfeasibilities", tBool), p("routePolyline", tEncodedPolyline), p("breaks", array(tBreak)),
		p("metrics", tAggregatedMetrics), p("vehicleFullness", tVehicleFullness),
		p("routeCosts", dict(tDouble)), p("routeTotalCost", tDouble))

	tSkippedShipmentReason = object("Reason",
		p("code", enum("ReasonCode", "NO_VEHICLE", "DEMAND_EXCEEDS_VEHICLE_CAPACITY",
			"CANNOT_BE_PERFORMED_WITHIN_VEHICLE_DISTANCE_LIMIT", "CANNOT_BE_PERFORMED_WITHIN_VEHICLE_DURATION_LIMIT",
			"CANNOT_BE_PERFORMED_WITHIN_VEHICLE_TRAVEL_DURATION_LIMIT",
			"CANNOT_BE_PERFORMED_WITHIN_VEHICLE_TIME_WINDOWS", "VEHICLE_NOT_ALLOWED", "VEHICLE_IGNORED",
			"SHIPMENT_IGNORED", "SKIPPED_IN_INJECTED_SOLUTION_CONSTRAINT",
			"VEHICLE_ROUTE_IS_FULLY_SEQUENCE_CONSTRAINED", "ZERO_PENALTY_COST")),
		p("exampleVehicleIndices", array(tInt32)), p("exampleExceededCapacityType", tText),
		p("exampleVehicleIndex", tInt32))

	tSkippedShipment = object("SkippedShipment", p("index", tInt32), p("label", tText),
		p("reasons", array(tSkippedShipmentReason)), p("penaltyCost", tDouble),
		p("estimatedIncompatibleVehicleRatio", tDouble))

	tRelaxation = object("Relaxation",
		p("level", enum("RelaxationLevel", "RELAX_VISIT_TIMES_AFTER_THRESHOLD",
			"RELAX_VISIT_TIMES_AND_SEQUENCE_AFTER_THRESHOLD", "RELAX_ALL_AFTER_THRESHOLD")),
		p("thresholdTime", tTimestamp), p("thresholdVisitCount", tInt32))

	tConstraintRelaxation = object("ConstraintRelaxation", p("relaxations", array(tRelaxation)),
		p("vehicleIndices", array(tInt32)))

	tInjectedSolutionConstraint = object("InjectedSolutionConstraint", p("routes", array(tShipmentRoute)),
		p("skippedShipments", array(tSkippedShipment)), p("constraintRelaxations", array(tConstraintRelaxation)))
)
