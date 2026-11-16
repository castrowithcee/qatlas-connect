package googleanalytics

import (
	"context"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// maxPropertyBytes bounds the answer of properties.get, a single small resource.
const maxPropertyBytes = 1 << 20

const notFoundProperty = subject + " did not find the property, or the credential may not see it"

// property is the Property resource of analyticsadmin v1beta, every documented member. Timestamps are kept
// as Google sends them.
type property struct {
	Name             resourceName `json:"name,omitempty"`
	PropertyType     code         `json:"propertyType,omitempty"`
	CreateTime       code         `json:"createTime,omitempty"`
	UpdateTime       code         `json:"updateTime,omitempty"`
	Parent           resourceName `json:"parent,omitempty"`
	DisplayName      text         `json:"displayName,omitempty"`
	IndustryCategory code         `json:"industryCategory,omitempty"`
	TimeZone         code         `json:"timeZone,omitempty"`
	CurrencyCode     code         `json:"currencyCode,omitempty"`
	ServiceLevel     code         `json:"serviceLevel,omitempty"`
	DeleteTime       code         `json:"deleteTime,omitempty"`
	ExpireTime       code         `json:"expireTime,omitempty"`
	Account          resourceName `json:"account,omitempty"`
}

// getProperty reads one property (properties.get) with the access token tool needs. name must have passed
// checkPropertyName; the caller has checked the binding.
func (c *client) getProperty(ctx context.Context, op, tool, name string) (property, error) {
	var out property
	if err := c.get(ctx, apiCall{op: op, tool: tool, path: "/v1beta/" + name, limit: maxPropertyBytes,
		notFound: notFoundProperty}, &out); err != nil {
		return property{}, err
	}
	if string(out.Name) != name {
		return property{}, provider.InvalidResponse(op, subject+" answered with another property than the one asked for")
	}
	return out, nil
}

// readProperty reads the property name names within the binding. The binding is checked before any request,
// and a property that is not bound itself is read once as proof that it belongs to a bound account; a
// property of another account is refused like one outside the targets.
func (c *client) readProperty(ctx context.Context, op, tool, name string) (property, error) {
	if _, err := checkPropertyName("property", name); err != nil {
		return property{}, err
	}
	proof, err := c.bind.property(name)
	if err != nil {
		return property{}, err
	}
	out, err := c.getProperty(ctx, op, tool, name)
	if err != nil {
		return property{}, err
	}
	if proof && !c.bind.ownsProperty(out) {
		return property{}, badRequest(outsideTargets)
	}
	return out, nil
}
