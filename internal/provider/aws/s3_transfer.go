package aws

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/localfile"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/sigv4"
)

// The S3 upload and download move the content of one object through a local file the connection releases,
// at most httpbind.MaxStreamBytes. The tool is generated like any other (table LocalFile); this file holds
// what is hand-written: opening the file before any secret is read, the checksum pass of an upload, and the
// result members that describe the file.

// transfer is the local file of one call.
type transfer struct {
	resolved *config.Resolved
	path     string
	// upload is open for a LocalFilesRead tool, download for a LocalFilesWrite tool.
	upload   *localfile.Upload
	download *localfile.Download
	// size and sha256 describe the content that was sent or written once the call succeeded.
	size   int64
	sha256 string
}

func (x *transfer) close() {
	if x.upload != nil {
		_ = x.upload.Close()
	}
	if x.download != nil {
		_ = x.download.Abort() // nothing after a Commit
	}
}

// openTransfer takes local_path out of args and opens the file for the direction of the tool: the path is
// checked against the released directories, an upload against the size limit, and a download against an
// existing file, which needs the confirmation of the request. It runs before the credential is resolved.
func (s toolSpec) openTransfer(ctx context.Context, resolved *config.Resolved, args map[string]any) (*transfer, error) {
	path, _ := args[localfile.LocalPathArgument].(string)
	delete(args, localfile.LocalPathArgument)
	if path == "" {
		return nil, &provider.InvalidRequestError{Message: localfile.LocalPathArgument + " is required"}
	}
	x := &transfer{resolved: resolved, path: path}
	var err error
	switch s.descriptor.LocalFiles {
	case config.LocalFilesRead:
		if x.upload, err = localfile.OpenForUpload(ctx, resolved, path); err != nil {
			return nil, err
		}
		if x.upload.Size > httpbind.MaxStreamBytes {
			x.close()
			return nil, &provider.InvalidRequestError{Message: "the file is larger than the limit of 100 MiB; nothing was sent"}
		}
	case config.LocalFilesWrite:
		if x.download, err = localfile.CreateForDownload(ctx, resolved, path); err != nil {
			return nil, err
		}
	default:
		return nil, provider.Fail(s.op, "the tool description is invalid")
	}
	return x, nil
}

// encodeTransfer is encode for a tool with a local file: the request is built before any secret is read, and
// the file is attached when it is sent.
func (s toolSpec) encodeTransfer(args map[string]any, owner string, x *transfer) (sender, error) {
	if s.rest == nil {
		return nil, provider.Fail(s.op, "the tool description is invalid")
	}
	codec := s.restCodec()
	req, err := s.rest.EncodeWith(codec, args, httpbind.EncodeOptions{Owner: owner})
	if err != nil {
		return nil, encodeRefusal(s.op, err, "httpbind: ")
	}
	if x.download != nil {
		if err := req.SetSink(s.rest, x.download); err != nil {
			return nil, provider.Fail(s.op, "the tool description is invalid")
		}
	}
	return func(ctx context.Context, c *Client, creds sigv4.Credentials, region string, out *map[string]any) (bool, *provider.Error) {
		if x.upload != nil {
			// The checksum header is signed and sent before the body, so the file is read once to hash it and then
			// opened again to be streamed. The hash is the integrity S3 verifies; the payload itself is signed
			// UNSIGNED-PAYLOAD because the stream is read once, over https only. A file that changed in between
			// fails S3's check or the size check of the client.
			sum, failure := s.hashUpload(ctx, x)
			if failure != nil {
				return false, failure
			}
			req.Header.Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(sum))
			up, err := localfile.OpenForUpload(ctx, x.resolved, x.path)
			if err != nil {
				return false, provider.Fail(s.op, "the local file could not be opened again: "+err.Error())
			}
			defer up.Close()
			if up.Size != x.upload.Size {
				return false, provider.Fail(s.op, "the local file changed while it was read; nothing was sent")
			}
			if err := req.SetStream(s.rest, httpbind.StreamBody{Reader: up, Size: up.Size}); err != nil {
				return false, provider.Fail(s.op, "the tool description is invalid")
			}
			meta, failure := c.sendRest(ctx, s.op, s.service, region, creds, s.descriptor.Risk.Idempotency, codec, s.rest, req, out)
			if failure != nil {
				return false, failure
			}
			if meta.SHA256 != hex.EncodeToString(sum) {
				return false, provider.InvalidResponse(s.op, "the content sent is not the content that was hashed")
			}
			x.size, x.sha256 = meta.Size, meta.SHA256
			return meta.Truncated, nil
		}
		meta, failure := c.sendRest(ctx, s.op, s.service, region, creds, s.descriptor.Risk.Idempotency, codec, s.rest, req, out)
		if failure != nil {
			return false, failure
		}
		if err := x.download.Commit(); err != nil {
			return false, transferFailure(s.op, err)
		}
		x.size, x.sha256 = meta.Size, meta.SHA256
		return meta.Truncated, nil
	}, nil
}

// hashUpload reads the opened file to its end and returns its SHA-256.
func (s toolSpec) hashUpload(ctx context.Context, x *transfer) ([]byte, *provider.Error) {
	if _, err := io.Copy(io.Discard, x.upload); err != nil {
		return nil, provider.Fail(s.op, "the local file could not be read: "+err.Error())
	}
	hexSum, done := x.upload.SHA256()
	sum, err := hex.DecodeString(hexSum)
	if !done || err != nil {
		return nil, provider.Fail(s.op, "the local file could not be read completely")
	}
	_ = x.upload.Close()
	return sum, nil
}

// transferFailure turns the error of finishing a download into a provider error; the file errors name no path.
func transferFailure(op string, err error) *provider.Error {
	return provider.Fail(op, "the downloaded content could not be stored: "+err.Error())
}

// addTransfer adds the members that describe the file to the result.
func (x *transfer) addTransfer(result map[string]any) {
	result[localfile.LocalPathArgument] = x.path
	result["size"] = x.size
	result["sha256"] = x.sha256
}

// checkUploadKMSKey requires the KMS key of an upload to be named by the ARN of a key or alias in the bound
// account and an allowed region, and bounds the user metadata that travels in headers. A bare key ID or alias
// name carries no account and is refused.
func checkUploadKMSKey(b binding, _ string, args map[string]any) error {
	if err := checkObjectKMSKey(b, "", args); err != nil {
		return err
	}
	metadata, _ := args["metadata"].(map[string]any)
	total := 0
	for name, value := range metadata {
		text, _ := value.(string)
		total += len(name) + len(text)
	}
	if len(metadata) > 100 || total > 8192 {
		return &provider.InvalidRequestError{Message: "metadata must hold at most 100 entries and 8192 bytes in all"}
	}
	for name := range metadata {
		if name == "" || strings.ContainsAny(name, "\r\n") {
			return &provider.InvalidRequestError{Message: "metadata names must not be empty or hold line breaks"}
		}
	}
	return nil
}
