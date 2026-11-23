package googleads

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// apiRoot is the one origin of the API and the only base URL a service configuration may name.
const apiRoot = "https://googleads.googleapis.com"

// apiVersion is the fixed version of the API. Raising it is a change of this provider, never a setting.
const apiVersion = "/v26"

// optionLoginCustomerID names the service option that holds the manager account Google evaluates the
// rights of the credential against. It is no secret and never an argument.
const optionLoginCustomerID = "login_customer_id"

const headerLoginCustomerID = "login-customer-id"

// defaultTimeout bounds one request. It stays below the 60 seconds an invoke may take, which also holds the
// membership reads that may precede the request of a tool.
const defaultTimeout = 20 * time.Second

// maxResponseBytes bounds the answer of one request. Every query of this provider has a LIMIT far below it.
const maxResponseBytes = 8 << 20

// minInterval is the spacing of two requests of one credential. It is a cautious start of this provider, not a
// documented limit.
const minInterval = 250 * time.Millisecond

// maxHold bounds how long a wait Google asks for may delay the next request of this process.
const maxHold = time.Minute

const userAgent = "qatlas-connect"

// limiters holds the spacing of every credential this process has used.
var limiters = ratelimit.NewRegistry(minInterval)

// transport carries every request, the token requests included. A nil value is Go's default transport; the
// package's own tests replace it with a fake.
var transport http.RoundTripper

// connection is what a request of one connection starts from: the manager account of the login-customer-id
// header, empty for none, and the targets.
type connection struct {
	login   string
	binding binding
}

// connectionOf checks everything of a connection that no request may start without: the official origin,
// the service options, no exclusions, and the targets. It reads no secret, and the messages never quote a
// value.
func connectionOf(resolved *config.Resolved) (connection, error) {
	switch {
	case resolved == nil:
		return connection{}, failure("open", "no connection was selected")
	case !isAPIRoot(resolved.BaseURL):
		return connection{}, failure("open", "a Google Ads service must use the official API origin "+apiRoot)
	case len(provider.ExcludesOf(resolved)) > 0:
		return connection{}, failure("open", "a Google Ads connection takes no exclusions")
	}
	var conn connection
	for name, value := range resolved.Options {
		if name != optionLoginCustomerID {
			return connection{}, failure("open", "a Google Ads service takes no option but "+optionLoginCustomerID)
		}
		value = strings.TrimSpace(value)
		if value != "" && !customerIDPattern.MatchString(value) {
			return connection{}, failure("open", "the service option "+optionLoginCustomerID+" must be the ten-digit "+
				"customer ID of a manager account without hyphens")
		}
		conn.login = value
	}
	b, err := parseBinding(provider.TargetsOf(resolved))
	if err != nil {
		return connection{}, failure("open", "the targets of the connection are not usable")
	}
	conn.binding = b
	return conn, nil
}

// isAPIRoot reports whether a configured base URL names the official origin. An empty value and a trailing
// slash are the only variations a configuration may carry.
func isAPIRoot(raw string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	return trimmed == "" || trimmed == apiRoot
}

func tokenOptions() provider.TokenOptions {
	return provider.TokenOptions{Token: tokenMetadata, Grants: googleapi.TokenGrants(transport)}
}

// client sends the requests of one connection with the access tokens of its credential.
type client struct {
	http    *http.Client
	source  *provider.TokenSource
	limiter *ratelimit.Limiter
	login   string
}

// open resolves the token roles of the credential. Everything wrong with the connection, its targets or the
// arguments ends before this call, so no secret is read for a request that would be refused.
func open(ctx context.Context, resolved *config.Resolved, conn connection, secrets *secret.Resolver,
	red *redact.Redactor) (*client, error) {
	if secrets == nil {
		return nil, failure("open", "no credential resolver was configured")
	}
	source, err := provider.OpenTokenSource(ctx, resolved, secrets, red, tokenOptions())
	if err != nil {
		return nil, err
	}
	return withSource(resolved, conn, source), nil
}

func withSource(resolved *config.Resolved, conn connection, source *provider.TokenSource) *client {
	return &client{http: provider.NoRedirectClient(defaultTimeout, transport), source: source,
		limiter: limiters.For(resolved.Credential), login: conn.login}
}

// request is one call of the API: the method, the path below the version, a JSON body for a POST, and
// whether the login-customer-id header goes along, which only requests on an account carry. change marks a
// mutation: it is sent once, never repeated, and a failure that may have reached Google says that the outcome
// is unknown.
type request struct {
	op, tool, method, path string
	body                   []byte
	onAccount              bool
	change                 bool
	// judgesInput marks a read whose arguments Google validates, such as GenerateRecommendations: a 400 with
	// an error code is then Google's refusal of the input and is mapped with the code.
	judgesInput bool
}

// read sends one reading request to the fixed origin and decodes the JSON answer into out. The token
// travels in the Authorization header alone. A 401 renews the token once, and nothing else repeats the
// request: a search is a read, also as a POST.
func (c *client) read(ctx context.Context, r request, out any) error { return c.send(ctx, r, out) }

// send makes one request. A mutation goes through the token source without a repeat: after a 401 the cached
// token is dropped, so only the next call renews it.
func (c *client) send(ctx context.Context, r request, out any) error {
	build := func(access string) (*http.Request, error) {
		var body io.Reader
		if r.body != nil {
			body = bytes.NewReader(r.body)
		}
		req, err := http.NewRequestWithContext(ctx, r.method, apiRoot+apiVersion+r.path, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		if r.body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if r.onAccount && c.login != "" {
			req.Header.Set(headerLoginCustomerID, queryNumber(c.login))
		}
		return req, nil
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return provider.Waited(r.op, subject, err)
	}
	dispatch := c.source.Read
	if r.change {
		dispatch = c.source.Mutate
	}
	response, err := dispatch(ctx, c.http, r.tool, build)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return err
		}
		// A failure of the transport itself may have come after Google took the request.
		return unsure(provider.Transport(r.op, subject, err), r.change)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		failed, hold := adsErrorFor(r.op, response.StatusCode, provider.ReadErrorBody(response.Body), r.change, r.judgesInput)
		if response.StatusCode == http.StatusTooManyRequests || failed.Class == provider.ClassRateLimited {
			// A missing or unusable wait still means the credential sent too much: the next call waits.
			hold = max(hold, provider.RetryAfter(response.Header), time.Second)
			c.limiter.HoldFor(min(hold, maxHold))
		}
		// A 4xx answer and a GoogleAdsFailure reject the whole atomic request; a server error does not say.
		return unsure(failed, r.change && response.StatusCode >= 500)
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return unsure(provider.InvalidResponse(r.op, subject+" returned an answer that is not JSON"), r.change)
	}
	if failed := provider.ReadJSON(r.op, subject, response.Body, maxResponseBytes, out); failed != nil {
		return unsure(failed, r.change)
	}
	return nil
}

// unsure says that a mutation whose request may have reached Google has an unknown outcome: it is neither
// claimed nor denied, and never repeated here.
func unsure(failed *provider.Error, change bool) *provider.Error {
	if change {
		failed.Message += "; the outcome of the change is unknown, so read the object before changing it again"
	}
	return failed
}
