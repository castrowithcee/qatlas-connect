package googlecloud

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// This file is the base of every Cloud Storage tool that changes something: the risk of such a tool and the one
// confirmed request, which first binds the bucket to the targets. A tool checks its arguments before it opens
// the session, builds the fixed path with bucketPath or objectPath, and passes only checked values.

// storageChangeRisk is the risk of a Cloud Storage tool that changes or deletes something. The outcome of a
// repeated call is not the same as that of the first, so the tools are not idempotent.
func storageChangeRisk(effect capability.Effect) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: capability.IdempotencyUnknown,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: dataSensitivity}
}

// storageChange is one request that changes something below a bucket. path was built from checked values, and
// query holds only checked values; user_project is added from userProject.
type storageChange struct {
	op, tool, method, path string
	bucket, userProject    string
	// project, when set, is a project ID or number the bucket must belong to: the bucket is then always read, even
	// on an unbound connection, and the change is refused unless its projectNumber is that of project.
	project string
	query   url.Values
	payload []byte
}

// change binds the bucket to the targets with one reading, before any request that changes something, and then
// sends the one request without a repetition. A bucket outside the targets ends the call without a change.
// out is nil for a method that answers 204 without a body.
func (s *storageSession) change(ctx context.Context, c storageChange, out any) error {
	switch c.method {
	case http.MethodDelete, http.MethodPost, http.MethodPatch, http.MethodPut:
	default:
		return failure(c.op, "the request could not be built")
	}
	if c.project != "" {
		if err := s.bindBucketToProject(ctx, c); err != nil {
			return err
		}
	} else if err := s.bindBucket(ctx, c.tool, c.bucket, c.userProject); err != nil {
		return err
	}
	query := storageQuery(c.userProject)
	for name, values := range c.query {
		query[name] = values
	}
	return s.c.send(ctx, c.op, c.tool, serviceStorage, c.method, c.path, query, c.payload, out, true)
}

// bindBucketToProject proves with one reading of the bucket that it lies in the targets and belongs to the
// project the caller named. A project ID is resolved by reading the project. A mismatch ends the call without a
// change and names neither project.
func (s *storageSession) bindBucketToProject(ctx context.Context, c storageChange) error {
	proof, err := s.readBucketProof(ctx, c.tool, c.bucket, c.userProject)
	if err != nil {
		return err
	}
	if s.list.bound() {
		admitted, err := s.admitNumber(ctx, proof.ProjectNumber)
		if err != nil {
			return err
		}
		if !admitted {
			return refuseOutside("the bucket")
		}
	}
	number := c.project
	if !storageNumberPattern.MatchString(number) {
		project, err := s.c.project(ctx, c.op, projectsGetID, number)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(project.Name, prefixProject) {
			return provider.InvalidResponse(c.op, subject+" returned an invalid response")
		}
		number = strings.TrimPrefix(project.Name, prefixProject)
	}
	if !storageNumberPattern.MatchString(proof.ProjectNumber) || proof.ProjectNumber != number {
		return badRequest("project is not the project of the bucket")
	}
	return nil
}

// setQueryValues sets every value that is not empty.
func setQueryValues(query url.Values, values map[string]string) {
	for name, value := range values {
		if value != "" {
			query.Set(name, value)
		}
	}
}

const (
	storageIfGenerationNotMatchProp     = `"if_generation_not_match":` + storageGenProp
	storageIfMetagenerationMatchProp    = `"if_metageneration_match":` + storageGenProp
	storageIfMetagenerationNotMatchProp = `"if_metageneration_not_match":` + storageGenProp
)

// storageChangeTail ends the description of every Cloud Storage tool that changes something.
const storageChangeTail = " Needs the confirmation of a person for each call. The request is sent once and " +
	"never repeated; after an unclear outcome, read the resource before changing it again."
