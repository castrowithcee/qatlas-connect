package googleroute

import "github.com/castrowithcee/qatlas-connect/internal/config"

// scopeCloudPlatform is the only scope the reference of the Route Optimization API lists for its methods,
// operations.get included. Google lists no narrower or read-only scope, so a token with it may reach more
// than the methods this provider wires. Optimizing tours needs it too.
const scopeCloudPlatform = "https://www.googleapis.com/auth/cloud-platform"

// tokenMetadata maps every tool to the smallest scopes it needs. The core derives the scopes of a connection
// from it, so a tool that is not offered adds none.
var tokenMetadata = config.TokenMetadata{Scopes: map[string][]string{
	operationsGetID: {scopeCloudPlatform},
	toursOptimizeID: {scopeCloudPlatform},
}}
