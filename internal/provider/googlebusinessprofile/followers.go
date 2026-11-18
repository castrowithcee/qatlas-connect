package googlebusinessprofile

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The followers tool uses accounts.locations.followers.getMetadata of the My Business API v4 and its
// FollowersMetadata resource (name and count), as documented at
// https://developers.google.com/my-business/reference/rest/v4/accounts.locations.followers/getMetadata, read
// on 2026-11-18. v4 has no discovery document, so nothing here comes from one. Google documents the method
// only for a location that is verified or connected to Maps.
const followersGetID = Provider + ".followers.get"

// followersMetadata is the FollowersMetadata resource. count is an int64, which Google writes as a string.
type followersMetadata struct {
	Name  line  `json:"name,omitempty"`
	Count short `json:"count,omitempty"`
}

var followersSchema = objectSchema("name", stringSchema, "count", stringSchema)

const notFoundFollowers = "Google did not find the account or the location for this credential; check the path " +
	"and that the consenting account manages the location"

var followersGet = capability.Descriptor{
	ID: followersGetID, Version: 1, Title: "Get the followers of a Business Profile location",
	Description: "Read the followers metadata of one location (Google My Business API v4 " +
		"accounts.locations.followers.getMetadata): the number of its followers as count. Google documents the " +
		"method only for a location that is verified or connected to Maps. On a connection bound to an account " +
		"the location is first proved to belong to it.",
	Tags: []string{"google", "business-profile", "followers"},
	Risk: capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
		Confirmation: capability.ConfirmationNone, OpenWorld: true,
		DataSensitivity: "the number of followers of a business location, which is business data"},
	Provider:     Provider,
	InputSchema:  inputSchema([]string{"location"}, "location", legacyLocationArgSchema),
	OutputSchema: json.RawMessage(followersSchema),
	Arguments: []capability.Argument{{Name: "location", Required: true, Description: "Location as " +
		formLegacyLocation + "; on a connection bound to an account the account must be that account, and on one " +
		"bound to a location the location must be that location"}},
	Fields: []capability.Field{
		{Name: "name", Description: "accounts/ACCOUNT/locations/LOCATION/followers/metadata"},
		{Name: "count", Description: "Total number of followers of the location, as a decimal string"},
	},
	Examples: []capability.Example{{Description: "Followers of one location", Arguments: json.RawMessage(
		`{"location":"accounts/1000001/locations/2000002"}`)}},
}

func handleFollowersGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get followers"
	var args struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	name, err := bound.legacyLocationOf("location", args.Location)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveOne(ctx, op, bound, name); err != nil {
		return nil, err
	}
	var out followersMetadata
	if err := c.get(ctx, op, followersGetID, request{root: legacyRoot, prefix: legacyPrefix,
		path: name + "/followers/metadata", notFound: notFoundFollowers}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
