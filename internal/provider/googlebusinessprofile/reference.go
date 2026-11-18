package googlebusinessprofile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// The reference tools read what Google holds for everyone: categories, chains, and the Google locations that
// exist. They name no account or location of the connection, so every connection offers them, whatever its
// target.

// Forms of the path and code arguments. A category is a stable Google concept ID in lower case; a chain ID is
// a short token. Neither can hold a slash or a dot, so no argument reaches another path.
const (
	formCategory = "categories/GCID"
	formChain    = "chains/CHAIN"

	categoryPattern = `^categories/gcid:[a-z0-9_]{1,100}$`
	chainPattern    = `^chains/[A-Za-z0-9_-]{1,64}$`
	regionPattern   = `^[A-Z]{2}$`
	languagePattern = `^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8}){0,3}$`

	// maxSearchRunes bounds every free search text.
	maxSearchRunes = 500
)

var (
	categoryForm = regexp.MustCompile(categoryPattern)
	chainForm    = regexp.MustCompile(chainPattern)
	regionForm   = regexp.MustCompile(regionPattern)
	languageForm = regexp.MustCompile(languagePattern)
)

const referenceNote = "This tool reads reference data of Google, not data of the user: it is tied to no account or " +
	"location, so every connection offers it, whatever its target."

var referenceRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "public reference data of Google; the search text is sent to Google and is not logged " +
		"by Qatlas Connect",
}

const (
	regionArgSchema   = `{"type":"string","pattern":"` + regionPattern + `","maxLength":2}`
	languageArgSchema = `{"type":"string","pattern":"` + languagePattern + `","maxLength":35}`
	viewArgSchema     = `{"type":"string","enum":["BASIC","FULL"]}`
	searchArgSchema   = `{"type":"string","minLength":1,"maxLength":500}`
)

const notFoundReference = "Google did not find what the request names; check the argument"

// checkText refuses a search text that is empty, too long, or holds control characters. No message quotes it.
func checkText(name, text string, limit int) error {
	switch {
	case text == "":
		return invalidArgument(name + " must not be empty")
	case utf8.RuneCountInString(text) > limit:
		return invalidArgument(name + " is too long")
	case !utf8.ValidString(text):
		return invalidArgument(name + " must be valid text")
	}
	for _, r := range text {
		if r < ' ' || r == 0x7f {
			return invalidArgument(name + " must not contain control characters")
		}
	}
	return nil
}

func checkRegion(given string, required bool) error {
	if given == "" && !required {
		return nil
	}
	if !regionForm.MatchString(given) {
		return invalidArgument("region_code must be an ISO 3166-1 alpha-2 code in capitals, such as DE")
	}
	return nil
}

func checkLanguage(name, given string) error {
	if !languageForm.MatchString(given) || len(given) > 35 {
		return invalidArgument(name + " must be a BCP 47 language code, such as de or en-GB")
	}
	return nil
}

func checkView(given string) error {
	if given != "BASIC" && given != "FULL" {
		return invalidArgument("view must be BASIC or FULL")
	}
	return nil
}

// postJSON sends one POST with a JSON body to a fixed path and decodes the JSON object answer into out. It is
// used for a search whose Google method takes a body; like get it sends once, and the token source renews a
// rejected token once.
func (c *client) postJSON(ctx context.Context, op, tool string, r request, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return provider.Fail(op, "the request could not be built")
	}
	address := r.root + apiPrefix + r.path
	build := func(access string) (*http.Request, error) {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, provider.Waited(op, subject, err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(payload))
		if err != nil {
			return nil, provider.Fail(op, "the request could not be built")
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", userAgent)
		return req, nil
	}
	resp, err := c.source.Read(ctx, c.http, tool, build)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return err
		}
		return provider.Transport(op, subject, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody := provider.ReadErrorBody(resp.Body)
		if resp.StatusCode == http.StatusTooManyRequests {
			c.limiter.HoldFor(min(max(provider.RetryAfter(resp.Header), time.Second), maxHold))
		}
		return googleError(op, resp.StatusCode, errBody, r.notFound, false)
	}
	if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		return provider.InvalidResponse(op, subject+" returned an answer that is not JSON")
	}
	if failed := provider.ReadJSON(op, subject, resp.Body, maxResponse, out); failed != nil {
		return failed
	}
	return nil
}
