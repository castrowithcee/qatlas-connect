package googleroute

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const toursLongRunningID = Provider + ".tours.optimize-long-running"

// Solving times of the long-running method. Google allows 30 minutes, and 60 with the switch, which it says
// raises the risk that the optimization is interrupted.
const (
	toursLongDefaultTimeout = 30 * time.Minute
	toursLongMaxTimeout     = 30 * time.Minute
	toursLongLargeTimeout   = 60 * time.Minute
	largeDeadlineArgument   = "allow_large_deadline_despite_interruption_risk"
)

// toursStartTimeout is the time the start request gets besides optimizeSlack. It has nothing to do with the
// solving time: Google answers with an operation at once, so only the network and the reading of that small
// answer count. Together with the slack the request ends after 30 seconds and leaves half of the 60 an invoke
// may take for the limiter wait and the token.
const toursStartTimeout = 25 * time.Second

// optimizeToursLongRunning is the request of the long-running method. The switch is part of the request, so
// Google sees what the person confirmed.
var optimizeToursLongRunning = toursRequest{
	extra:          []topArg{top(largeDeadlineArgument, "allowLargeDeadlineDespiteInterruptionRisk", tBool)},
	defaultTimeout: toursLongDefaultTimeout, maxTimeout: toursLongMaxTimeout,
	largeSwitch: largeDeadlineArgument, largeTimeout: toursLongLargeTimeout,
}

var longRunningArgumentText = map[string]string{
	"timeout": "Solving time as a duration such as 1200s, longer than 0 and at most 1800s (30 minutes); default " +
		"1800s. With allow_large_deadline_despite_interruption_risk the most is 3600s (60 minutes)",
	largeDeadlineArgument: "True to allow a timeout of up to 60 minutes; Google says requests with a deadline " +
		"above 30 minutes have a higher risk of being interrupted",
	"refresh_details_routes": "Routes whose details Google recomputes without changing their visits, as " +
		"ShipmentRoute; Google needs populate_polylines then and refuses the combination with the injected fields",
}

// toursLongRunning starts an optimization and returns the operation without waiting for it.
var toursLongRunning = capability.Descriptor{
	ID: toursLongRunningID, Version: 1, Title: "Start a long-running tour optimization",
	Description: "Start an optimization of vehicle tours that may run longer than one invoke " +
		"(optimizeToursLongRunning of the Route Optimization API v1) and return the operation Google answers " +
		"with. The tool never waits: it sends one request and returns name, done, and metadata, the optimization " +
		"goes on at Google, and operations.get with that name reads its state and, once it is done, the " +
		"OptimizeToursResponse. COSTS MONEY: every call is billed to the project per shipment, as Single Vehicle " +
		"Routing for one vehicle and as Fleet Routing for two or more, and Google lists no free tier. Qatlas " +
		"Connect checks neither price nor quota. A person confirms each call. A started run cannot be cancelled " +
		"and not listed, because Google offers neither for these operations, so it runs and is billed until it " +
		"ends. The tool sends exactly one request and never repeats it: after a timeout, an abort, a 5xx answer, " +
		"or an unreadable answer an optimization may have been started and billed, the operation name is then " +
		"unknown and cannot be asked for, and a repeated call would start and bill a second one. timeout is at " +
		"most 30 minutes, and 60 minutes with allow_large_deadline_despite_interruption_risk, which Google says " +
		"carries a higher risk of interruption. The model and the other arguments are those of tours.optimize; " +
		"Qatlas checks only form, types, enumeration values, and a request size of 2 MiB, and Google checks the " +
		"rest. The method has no regional locations and no Cloud Storage path; the request and the result are " +
		"JSON. The name of the operation is checked for its form and for the project of the request before it is " +
		"returned. Locations and vehicles are personal data, and metadata is untrusted third-party data. The " +
		"token holds the broad cloud-platform scope, the only one Google lists for this method. The API must be " +
		"enabled in the project, billing must be on, and the account needs routeoptimization.operations.create.",
	Tags: []string{"google", "route-optimization", "tours", "optimize", "long-running"}, Risk: optimizeRisk,
	Provider:     Provider,
	InputSchema:  optimizeToursLongRunning.schema(),
	OutputSchema: operationOutput,
	Arguments:    optimizeArguments(optimizeToursLongRunning, longRunningArgumentText),
	Fields:       operationFields,
	Examples: []capability.Example{{
		Description: "Start an optimization of one vehicle with two shipments that may take up to 20 minutes",
		Arguments: json.RawMessage(`{"project":"my-project-123","timeout":"1200s","model":{"shipments":[` +
			`{"deliveries":[{"arrivalLocation":{"latitude":48.1,"longitude":11.5}}]},` +
			`{"deliveries":[{"arrivalLocation":{"latitude":48.2,"longitude":11.6}}]}],` +
			`"vehicles":[{"startLocation":{"latitude":48.0,"longitude":11.4}}]}}`),
	}},
}

func toursLongRunningSummary(raw json.RawMessage) string {
	return summarize(raw, "Start a long-running tour optimization", "the run cannot be cancelled or listed and "+
		"is billed until it ends. If the answer is lost the operation name is unknown and cannot be looked up, "+
		"and a repeated call starts and bills a new operation. The price is not checked")
}

func handleToursLongRunning(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "start tour optimization"
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	body, err := optimizeToursLongRunning.body(raw)
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
	if err := c.post(ctx, op, toursLongRunningID, "/v1/"+prefixProject+body.Project+":optimizeToursLongRunning",
		body.Payload, toursStartTimeout, &answer); err != nil {
		return nil, err
	}
	result, err := startedOperation(op, body.Project, answer)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			failed.Message += unsureNote
		}
		return nil, err
	}
	return result, nil
}

// startedOperation turns the answer of the start into the operation it names. The name must have the form of an
// operation and hold the project of the request, spelled the same way: a name for another project, or for the
// same one by its number, is not handed out, because the person could not tell which project is billed.
func startedOperation(op, project string, answer json.RawMessage) (operationResult, error) {
	var wire operationWire
	if err := json.Unmarshal(answer, &wire); err != nil {
		return operationResult{}, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	if ref, ok := parseOperationName(wire.Name); !ok || ref.project != project {
		return operationResult{}, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return operationOf(op, wire)
}
