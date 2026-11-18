package googlebusinessprofile

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// The My Business API v4 at legacyRoot is the base of every tool group that Google did not move to the v1
// APIs (reviews, local posts, media, and others). Its resources are named below an account, so a path is
// accounts/ACCOUNT/locations/LOCATION/..., whereas a target of this provider may be a location alone. The
// helpers here check such a path against its form and the target of the connection before any secret is
// read; a tool group of v4 builds its requests with legacyPrefix on top of them.
//
// Binding: an account target accepts only paths below that account, and a location below it is proved
// against the account's location list (see proveLocation). A location target compares only the location
// ID; the account part of the path is an argument, since a location names no account.
const (
	formLegacyLocation = "accounts/ACCOUNT/locations/LOCATION"

	legacyLocationPattern = `^accounts/` + idPattern + `/locations/` + idPattern + `$`

	maxLegacyLocationLen = 80
)

var legacyLocationForm = regexp.MustCompile(legacyLocationPattern)

// legacyLocationArgSchema is the schema of a v4 location path argument.
var legacyLocationArgSchema = `{"type":"string","maxLength":` + strconv.Itoa(maxLegacyLocationLen) +
	`,"pattern":"` + legacyLocationPattern + `","x-form":"` + formLegacyLocation + `"}`

// legacyLocationOf checks a v4 location path (accounts/ACCOUNT/locations/LOCATION) against its form and the
// target. No message names the other object.
func (s scope) legacyLocationOf(name, given string) (string, error) {
	if len(given) > maxLegacyLocationLen || !legacyLocationForm.MatchString(given) {
		return "", invalidArgument(name + " must be " + formLegacyLocation + " with decimal IDs")
	}
	return given, s.legacyInTarget(name, given)
}

// legacyInTarget refuses a v4 location path that lies outside the target.
func (s scope) legacyInTarget(name, path string) error {
	account, location := splitLegacyLocation(path)
	if (s.account != "" && account != s.account) || (s.location != "" && location != s.location) {
		return invalidArgument(name + " " + outsideTarget)
	}
	return nil
}

// splitLegacyLocation cuts accounts/A/locations/L (optionally followed by more segments) into accounts/A and
// locations/L.
func splitLegacyLocation(path string) (account, location string) {
	account, rest, _ := strings.Cut(path, "/locations/")
	location, _, _ = strings.Cut(rest, "/")
	return account, "locations/" + location
}

// legacyAccountOf settles the account of a v4 call that is not about one location path: the argument, or
// the account of the target when it is left out. A location target names no account, so there the argument
// is required.
func (s scope) legacyAccountOf(name, given string) (string, error) {
	switch {
	case given == "" && s.account == "":
		return "", invalidArgument(name + " is required unless the connection is bound to an account")
	case given == "":
		return s.account, nil
	case !accountForm.MatchString(given):
		return "", invalidArgument(name + " must be accounts/ACCOUNT with a decimal ID")
	case s.account != "" && given != s.account:
		return "", invalidArgument(name + " " + outsideTarget)
	}
	return given, nil
}

// proveLegacyLocations proves, for an account target, that every v4 location path lies in the location list
// of the account, reading that list once, names only, and stopping when all are found. A path the list does
// not hold is refused like one outside the target, without naming it.
func (c *client) proveLegacyLocations(ctx context.Context, op, account string, paths []string) error {
	missing := map[string]bool{}
	for _, path := range paths {
		_, location := splitLegacyLocation(path)
		missing[location] = true
	}
	query := readMaskQuery([]string{"name"})
	query = pageSizeQuery(query, maxLocationsPage)
	for range maxProofPages {
		var answer locationsAnswer
		if err := c.get(ctx, op, locationsGetID, request{root: informationRoot, path: locationsOfPath(account),
			query: query, notFound: notFoundAccount}, &answer); err != nil {
			return err
		}
		for _, entry := range answer.Locations {
			delete(missing, string(entry.Name))
		}
		switch {
		case len(missing) == 0:
			return nil
		case answer.NextPageToken == "":
			return invalidArgument("location " + outsideTarget)
		case len(answer.NextPageToken) > maxPageToken:
			return provider.InvalidResponse(op, subject+" returned a continuation token that is too long")
		}
		query.Set("pageToken", answer.NextPageToken)
	}
	return invalidArgument("location could not be proved to belong to the account of this connection within " +
		"the first 2000 locations of the account")
}
