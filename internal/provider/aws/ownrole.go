package aws

import (
	"context"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/sigv4"
)

// refuseOwnRole asks GetCallerIdentity who signs the call and refuses it when target names the role of that
// session, so a connection never changes or deletes the role it works with. IAM role names are unique without
// regard to case, so the comparison ignores case. An IAM user or the root user has no own role. A failed or
// unreadable answer refuses the call: the check fails closed.
func (c *Client) refuseOwnRole(ctx context.Context, op, region string, creds sigv4.Credentials, name, target string) error {
	who, failure := c.identity(ctx, op, region, creds)
	if failure != nil {
		return failure
	}
	own, ok := ownRoleName(who.Arn, c.binding.account)
	if !ok {
		return provider.InvalidResponse(op, "AWS returned a caller identity the own-role check cannot read; the change "+
			"was refused and nothing was sent with it")
	}
	if own != "" && strings.EqualFold(own, target) {
		return &provider.InvalidRequestError{Message: name + " names the role whose credentials this connection " +
			"uses; a connection never changes or deletes its own role"}
	}
	return nil
}

// ownRoleName reads the role name from the caller ARN of the bound account: the name of an assumed-role
// session, or "" for an IAM user or the root user. Any other form is not readable.
func ownRoleName(arn, account string) (string, bool) {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[3] != "" || parts[4] != account {
		return "", false
	}
	resource := parts[5]
	switch parts[2] {
	case "iam":
		if resource == "root" || (strings.HasPrefix(resource, "user/") && len(resource) > len("user/")) {
			return "", true
		}
	case "sts":
		// assumed-role/<role name>/<session name>; a role name holds no slash.
		if rest, found := strings.CutPrefix(resource, "assumed-role/"); found {
			role, session, found := strings.Cut(rest, "/")
			if found && role != "" && session != "" {
				return role, true
			}
		}
	}
	return "", false
}
