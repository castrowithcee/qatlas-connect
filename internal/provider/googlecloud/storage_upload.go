package googlecloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// This file is the one request that sends the content of an object: the multipart upload of objects.insert to
// the upload endpoint of storage v1. The origin is the fixed one of the service table and the path is built
// from checked values, never from an argument. client.send always sends JSON, so the upload has its own
// request, which follows the rules of every mutation: one request, no repetition, an unknown outcome is said.

// uploadBasePath is the base path of the upload endpoint of the discovery document of storage v1.
const uploadBasePath = "/upload/storage/v1"

// uploadPath is the path of the multipart upload into a bucket. The name was checked by validBucketName and is
// escaped anyway.
func uploadPath(bucket string) string { return uploadBasePath + "/b/" + url.PathEscape(bucket) + "/o" }

// multipartBody builds the body of a multipart upload: the JSON metadata of the object, then the content with
// the content type of the object. It returns the body and the Content-Type header value that names its boundary.
func multipartBody(metadata map[string]any, contentType string, content []byte) ([]byte, string, error) {
	meta, err := json.Marshal(metadata)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	if err != nil {
		return nil, "", err
	}
	if _, err = part.Write(meta); err != nil {
		return nil, "", err
	}
	if part, err = w.CreatePart(textproto.MIMEHeader{"Content-Type": {contentType}}); err != nil {
		return nil, "", err
	}
	if _, err = part.Write(content); err != nil {
		return nil, "", err
	}
	if err = w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "multipart/related; boundary=" + w.Boundary(), nil
}

// upload sends one multipart POST to the storage upload endpoint and decodes the JSON answer into out. It is
// never repeated: a 401 only drops the cached token, and a failure after which the request may have reached
// Google says that the outcome is unknown.
func (c *client) upload(ctx context.Context, op, tool, path string, query url.Values, contentType string,
	payload []byte, out any) error {
	origin, ok := serviceOrigins[serviceStorage]
	if !ok {
		return failure(op, "the service is not one of this provider")
	}
	endpoint := origin + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	build := func(access string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		return req, nil
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return provider.Waited(op, subject, err)
	}
	unsure := func(failed *provider.Error) *provider.Error {
		failed.Message += unsureNote
		return failed
	}
	response, err := c.source.Mutate(ctx, c.http, tool, build)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return err
		}
		failed = provider.Transport(op, subject, err)
		if failed.MayHaveArrived() {
			return unsure(failed)
		}
		return failed
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		mapped := c.statusFailure(op, serviceStorage, response)
		if response.StatusCode >= 500 {
			return unsure(mapped)
		}
		return mapped
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return unsure(provider.InvalidResponse(op, subject+" returned an answer that is not JSON"))
	}
	if failed := provider.ReadJSON(op, subject, response.Body, maxResponseBytes, out); failed != nil {
		return unsure(failed)
	}
	return nil
}

// upload binds the bucket to the targets with one reading, before the request that sends the content, and then
// sends the one multipart request without a repetition.
func (s *storageSession) upload(ctx context.Context, tool, bucket, userProject string, query url.Values,
	contentType string, payload []byte, out any) error {
	if err := s.bindBucket(ctx, tool, bucket, userProject); err != nil {
		return err
	}
	all := storageQuery(userProject)
	for name, values := range query {
		all[name] = values
	}
	return s.c.upload(ctx, s.op, tool, uploadPath(bucket), all, contentType, payload, out)
}
