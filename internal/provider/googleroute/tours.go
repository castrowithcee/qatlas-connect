package googleroute

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const toursOptimizeID = Provider + ".tours.optimize"

// optimizeRisk is the risk of an optimization: it runs a billed computation at Google, a repeated call is
// billed again, and the model and the answer hold locations and vehicles.
var optimizeRisk = capability.Risk{
	Effect:          capability.EffectExecute,
	Idempotency:     capability.IdempotencyNonIdempotent,
	Confirmation:    capability.ConfirmationRequired,
	OpenWorld:       true,
	DataSensitivity: dataSensitivity,
}

// SKU names of the billing of optimizeTours: Google bills each shipment of the model, by the number of
// vehicles of the request.
const (
	skuSingleVehicle = "Single Vehicle Routing"
	skuFleet         = "Fleet Routing"
)

var optimizeArgumentText = map[string]string{
	"model": "The ShipmentModel as Google's JSON: shipments, vehicles, and everything else the reference of " +
		"optimizeTours lists, under Google's camelCase names. Locations, times, and loads are personal data",
	"timeout": "Solving time as a duration such as 30s, longer than 0 and at most 45s; default 30s. A longer " +
		"optimization needs tours.optimize-long-running",
	"solving_mode":                              "DEFAULT_SOLVE, VALIDATE_ONLY, DETECT_SOME_INFEASIBLE_SHIPMENTS, or TRANSFORM_AND_RETURN_REQUEST",
	"search_mode":                               "RETURN_FAST or CONSUME_ALL_AVAILABLE_TIME",
	"label":                                     "Label that identifies the request; Google returns it as requestLabel",
	"populate_polylines":                        "True to return the encoded polyline of each route; large",
	"populate_transition_polylines":             "True to return the encoded polyline of each transition; larger still",
	"injected_first_solution_routes":            "Routes of an earlier solution that the search starts from, as ShipmentRoute",
	"injected_solution_constraint":              "Constraint on the routes of an injected solution, as InjectedSolutionConstraint",
	"refresh_details_routes":                    "Routes whose details Google recomputes without changing their visits, as ShipmentRoute",
	"interpret_injected_solutions_using_labels": "True to match injected routes to the model by label instead of index",
	"consider_road_traffic":                     "True to take road traffic into account for the times of the routes",
	"use_geodesic_distances":                    "True to use geodesic distances; needs geodesic_meters_per_second",
	"geodesic_meters_per_second":                "Speed in meters per second for geodesic distances, at least 1.0",
	"max_validation_errors":                     "Most validation errors Google returns, 0 to 10000; Google's default is 100",
}

func optimizeArguments() []capability.Argument {
	out := []capability.Argument{{Name: "project", Required: true, Description: "Project ID such as " +
		"my-project-123, or the project number; never a projects/ name. It must be one of the targets of the " +
		"connection. The optimization is billed to it"}}
	for _, a := range optimizeTours.args() {
		out = append(out, capability.Argument{Name: a.arg, Required: a.required, Description: optimizeArgumentText[a.arg]})
	}
	return out
}

// toursOptimize is the synchronous optimization.
var toursOptimize = capability.Descriptor{
	ID: toursOptimizeID, Version: 1, Title: "Optimize tours",
	Description: "Optimize vehicle tours for a ShipmentModel and return the solution (optimizeTours of the Route " +
		"Optimization API v1). COSTS MONEY: every call is billed to the project per shipment, as Single Vehicle " +
		"Routing for one vehicle and as Fleet Routing for two or more, and Google lists no free tier. Qatlas Connect " +
		"checks neither price nor quota. A person confirms each call; the confirmation names the project, the " +
		"number of shipments and vehicles, and the SKU. The tool sends exactly one request and never repeats it: " +
		"after a timeout, an abort, a 5xx answer, or an unreadable answer the call may have been billed, and the " +
		"error says so. One invoke may take 60 seconds in all, so timeout is at most 45s; for longer runs use the " +
		"tool tours.optimize-long-running. Problems of the model that Google finds come back as validationErrors " +
		"in the result, not as an error; skippedShipments lists what the solution leaves out. Google checks the " +
		"consistency of the model; Qatlas checks only form, types, enumeration values, and a request size of " +
		"2 MiB. The result is cut to bounded sizes: complete is false and truncation counts and names what was " +
		"cut, and a polyline is kept whole or left out. Labels, names, and error messages in the result are " +
		"untrusted third-party text, and locations and vehicles are personal data. The token holds the broad " +
		"cloud-platform scope, the only one Google lists for this method. The API must be enabled in the " +
		"project, billing must be on, and the account needs routeoptimization.locations.use.",
	Tags: []string{"google", "route-optimization", "tours", "optimize"}, Risk: optimizeRisk, Provider: Provider,
	InputSchema:  optimizeTours.schema(),
	OutputSchema: toursOutput,
	Arguments:    optimizeArguments(),
	Fields: []capability.Field{
		{Name: "complete", Description: "False when anything was cut or dropped; then truncation says what"},
		{Name: "truncation", Description: "Counts of cut texts, cut lists, omitted items and polylines, dropped " +
			"values, unknown fields, and the lists that were cut with kept and total"},
		{Name: "routes", Description: "One ShipmentRoute per used vehicle: visits, transitions, metrics, costs, polylines"},
		{Name: "skippedShipments", Description: "Shipments the solution does not serve, with reasons"},
		{Name: "validationErrors", Description: "Problems Google found in the model; not an error of the tool"},
		{Name: "metrics", Description: "Aggregated metrics and costs of the solution"},
		{Name: "requestLabel", Description: "The label of the request, if one was given"},
		{Name: "processedRequest", Description: "The transformed request, for solving_mode TRANSFORM_AND_RETURN_REQUEST"},
	},
	Examples: []capability.Example{{
		Description: "Optimize one vehicle with two shipments",
		Arguments: json.RawMessage(`{"project":"my-project-123","timeout":"20s","model":{"shipments":[` +
			`{"deliveries":[{"arrivalLocation":{"latitude":48.1,"longitude":11.5}}]},` +
			`{"deliveries":[{"arrivalLocation":{"latitude":48.2,"longitude":11.6}}]}],` +
			`"vehicles":[{"startLocation":{"latitude":48.0,"longitude":11.4}}]}}`),
	}},
}

// toursSummary phrases the call from its arguments alone: project, shipments, vehicles, and the SKU that the
// number of vehicles selects.
func toursSummary(raw json.RawMessage) string {
	var a struct {
		Project string `json:"project"`
		Model   struct {
			Shipments []json.RawMessage `json:"shipments"`
			Vehicles  []json.RawMessage `json:"vehicles"`
		} `json:"model"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	sku := "no vehicle, so Google is expected to refuse the model"
	switch n := len(a.Model.Vehicles); {
	case n == 1:
		sku = skuSingleVehicle
	case n >= 2:
		sku = skuFleet
	}
	return "Optimize tours in project " + boundedText(a.Project, maxNameText) + " for " +
		strconv.Itoa(len(a.Model.Shipments)) + " shipments and " + strconv.Itoa(len(a.Model.Vehicles)) +
		" vehicles. Billed to the project per shipment (SKU " + sku + "); the call may be billed again if " +
		"repeated and its price is not checked"
}

func handleToursOptimize(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "optimize tours"
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	body, err := optimizeTours.body(raw)
	if err != nil {
		return nil, err
	}
	if !list.allows(body.Project) {
		return nil, badRequest("project is outside the targets of this connection")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer json.RawMessage
	if err := c.post(ctx, op, toursOptimizeID, "/v1/"+prefixProject+body.Project+":optimizeTours", body.Payload,
		body.Timeout, &answer); err != nil {
		return nil, err
	}
	result, err := toursResultOf(op, answer, false)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			failed.Message += unsureNote
		}
		return nil, err
	}
	return result, nil
}
