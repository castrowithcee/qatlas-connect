package aws

import (
	"context"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/sigv4"
)

// refuseOwnRole asks GetCallerIdentity who signs the call and refuses it when target names the role of that
// session, so a connection never changes or deletes the role it works with. IAM role names are unique without
// regard to case, so the comparison ignores case. A failed or unreadable answer refuses the call: the check
// fails closed.
func (c *Client) refuseOwnRole(ctx context.Context, op, region string, creds sigv4.Credentials, name, target string) error {
	return c.refuseOwn(ctx, op, region, creds, name, target, false)
}

// refuseOwnUser is refuseOwnRole for the IAM user whose access key signs the call.
func (c *Client) refuseOwnUser(ctx context.Context, op, region string, creds sigv4.Credentials, name, target string) error {
	return c.refuseOwn(ctx, op, region, creds, name, target, true)
}

func (c *Client) refuseOwn(ctx context.Context, op, region string, creds sigv4.Credentials, name, target string, user bool) error {
	who, failure := c.identity(ctx, op, region, creds)
	if failure != nil {
		return failure
	}
	role, owner, ok := ownIdentity(who.Arn, c.binding.account)
	if !ok {
		return provider.InvalidResponse(op, "AWS returned a caller identity the own-identity check cannot read; the change "+
			"was refused and nothing was sent with it")
	}
	own, kind := role, "role"
	if user {
		own, kind = owner, "user"
	}
	if own != "" && strings.EqualFold(own, target) {
		return &provider.InvalidRequestError{Message: name + " names the " + kind + " whose credentials this connection " +
			"uses; a connection never changes or deletes its own " + kind}
	}
	return nil
}

// ownIdentity reads the caller ARN of the bound account: the role name of an assumed-role session, or the
// user name (the last segment of the path) of an IAM user. Both are "" for the root user. Any other form is
// not readable.
func ownIdentity(arn, account string) (role, user string, ok bool) {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[3] != "" || parts[4] != account {
		return "", "", false
	}
	resource := parts[5]
	switch parts[2] {
	case "iam":
		if resource == "root" {
			return "", "", true
		}
		// user/<path>/<name>; the name is the last segment and a user name holds no slash.
		if path, found := strings.CutPrefix(resource, "user/"); found {
			if name := path[strings.LastIndex(path, "/")+1:]; name != "" {
				return "", name, true
			}
		}
	case "sts":
		// assumed-role/<role name>/<session name>; a role name holds no slash.
		if rest, found := strings.CutPrefix(resource, "assumed-role/"); found {
			role, session, found := strings.Cut(rest, "/")
			if found && role != "" && session != "" {
				return role, "", true
			}
		}
	}
	return "", "", false
}
