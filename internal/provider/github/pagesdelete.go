package github

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Pages are only read and switched off here. Creating or changing a site would publish content to anyone,
// so no tool offers it.

const pagesDeletePermission = "GitHub refused this token the removal of the GitHub Pages site of this " +
	"repository; it needs repo on a classic token with the Admin role on the repository, or Pages: read and " +
	"write on a fine-grained token"

var pagesDelete = capability.Descriptor{
	ID:      Provider + ".pages.delete",
	Version: 1,
	Title:   "Turn off the GitHub Pages site of a repository",
	Description: "Turn off the GitHub Pages site of a repository a connection allows; the published site " +
		"goes offline and its settings are removed, while the repository content stays. Offered only by a " +
		"connection whose tools list names it",
	Tags:                  []string{"github", "repositories", "pages", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(""),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"deleted":{"type":"boolean"}},"required":["deleted"],` +
		`"additionalProperties":false}`),
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub turned the Pages site off"},
	},
	Examples: []capability.Example{{Description: "Turn off the Pages site", Arguments: json.RawMessage(`{}`)}},
}

func pagesDeleteOperations() []capability.Operation {
	return []capability.Operation{{Descriptor: pagesDelete, Handler: capability.Handler(invokePagesDelete)}}
}

// invokePagesDelete checks the repository before a credential is resolved.
func invokePagesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	bound, err := selectTarget(resolved, kindRepository, raw)
	if err != nil {
		return nil, err
	}
	if bound.pattern() {
		return nil, invalidRequest("repository must name one repository")
	}
	client, err := openAt(ctx, resolved, secrets, red, bound)
	if err != nil {
		return nil, err
	}
	return bound.locate(client.deletePages(ctx))
}

// PagesDeleted is the answer to a turned-off Pages site.
type PagesDeleted struct {
	Deleted bool `json:"deleted"`
}

// deletePages turns the Pages site of the bound repository off in one request that is never repeated.
func (c *Client) deletePages(ctx context.Context) (*PagesDeleted, error) {
	const op = "delete GitHub Pages site"
	if err := c.restChange(ctx, op, http.MethodDelete, c.repoPath("pages"), struct{}{}, nil); err != nil {
		return nil, writeFailure(err, "GitHub refused to turn off the Pages site; the repository has none or "+
			"it cannot be removed in its current state", pagesDeletePermission)
	}
	return &PagesDeleted{Deleted: true}, nil
}
