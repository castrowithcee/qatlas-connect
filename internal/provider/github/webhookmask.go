package github

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Masking of the values a webhook carries. The target address of a webhook and the headers and payloads of
// its deliveries are untrusted data and often hold credentials: a token in the path or the query of a chat
// webhook address, an authorization header, a signature derived from the secret. Nothing here fetches an
// address.

const (
	webhookURLLimit      = 200
	webhookHeaderLimit   = 50
	webhookHeaderValue   = 200
	webhookPayloadLimit  = 2000
	maskedWebhookSegment = "***"
)

// webhookPathWords are the only path segments shown as sent: common route words of webhook receivers.
// Every other segment is replaced, since a token in the path has no fixed shape and a short lowercase token
// would pass any shape test.
var webhookPathWords = map[string]bool{"api": true, "hook": true, "hooks": true, "webhook": true, "webhooks": true,
	"github": true, "github-webhook": true, "services": true, "incoming": true, "integration": true,
	"integrations": true, "events": true, "event": true, "callback": true, "notify": true, "trigger": true,
	"triggers": true, "v1": true, "v2": true, "v3": true}

var webhookQueryKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,39}$`)

// maskWebhookURL returns the target address of a webhook with userinfo removed, every path segment but a
// common route word and every query value replaced, and the fragment dropped, cut to a bound. masked
// reports whether anything was replaced or the address could not be read; an unreadable address is never
// passed on.
func maskWebhookURL(raw string) (text string, masked, cut bool) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "[unreadable address]", true, false
	}
	var b strings.Builder
	b.WriteString(parsed.Scheme + "://")
	if parsed.User != nil {
		b.WriteString(maskedWebhookSegment + "@")
		masked = true
	}
	b.WriteString(cleanHost(parsed.Host))
	if parsed.Fragment != "" {
		masked = true
	}
	path := parsed.EscapedPath()
	if path != "" {
		segments := strings.Split(path, "/")
		for i, segment := range segments {
			if segment != "" && !webhookPathWords[segment] {
				segments[i] = maskedWebhookSegment
				masked = true
			}
		}
		b.WriteString(strings.Join(segments, "/"))
	}
	if parsed.RawQuery != "" {
		masked = true
		pairs := strings.Split(parsed.RawQuery, "&")
		for i, pair := range pairs {
			key, _, hasValue := strings.Cut(pair, "=")
			if !webhookQueryKey.MatchString(key) {
				key = maskedWebhookSegment
			}
			pairs[i] = key
			if hasValue {
				pairs[i] += "=" + maskedWebhookSegment
			}
		}
		b.WriteString("?" + strings.Join(pairs, "&"))
	}
	text, cut = cutRunes(b.String(), webhookURLLimit)
	return text, masked, cut
}

func cleanHost(host string) string {
	if utf8.RuneCountInString(host) > 100 {
		return maskedWebhookSegment
	}
	return host
}

// webhookSecretHeader reports whether a header name can carry a credential, or a value derived from one.
func webhookSecretHeader(name string) bool {
	name = strings.ToLower(name)
	for _, part := range []string{"authorization", "cookie", "token", "secret", "signature", "api-key", "apikey",
		"password", "credential", "session"} {
		if strings.Contains(name, part) {
			return true
		}
	}
	return false
}

// WebhookHeaders is the bounded header set of one delivery side. Omitted counts the headers left out for
// their name or for the bound.
type WebhookHeaders struct {
	Headers map[string]string `json:"headers,omitempty"`
	Omitted int               `json:"headers_omitted,omitempty"`
}

// WebhookPayload is the cut text of a delivery payload.
type WebhookPayload struct {
	Payload   string `json:"payload,omitempty"`
	Truncated bool   `json:"payload_truncated,omitempty"`
}

// WebhookDeliverySide is the request or the response of one delivery: bounded headers without any that can
// carry a credential, and the payload as cut text. Both are untrusted data.
type WebhookDeliverySide struct {
	WebhookHeaders
	WebhookPayload
}

// webhookSideRaw is the request or the response of a delivery as GitHub sends it. The request payload is an
// object, the response payload a string; both are taken as raw JSON.
type webhookSideRaw struct {
	Headers json.RawMessage `json:"headers"`
	Payload json.RawMessage `json:"payload"`
}

// view bounds one delivery side, or reports false for a member of another shape.
func (s *webhookSideRaw) view() (*WebhookDeliverySide, bool) {
	if s == nil {
		return nil, true
	}
	side := &WebhookDeliverySide{}
	if len(bytes.TrimSpace(s.Headers)) > 0 && !bytes.Equal(bytes.TrimSpace(s.Headers), []byte("null")) {
		var headers map[string]any
		if json.Unmarshal(s.Headers, &headers) != nil {
			return nil, false
		}
		names := make([]string, 0, len(headers))
		for name := range headers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value, isText := headers[name].(string)
			if !isText || webhookSecretHeader(name) || len(side.Headers) >= webhookHeaderLimit ||
				utf8.RuneCountInString(name) > projectionNameLimit {
				side.Omitted++
				continue
			}
			if side.Headers == nil {
				side.Headers = map[string]string{}
			}
			text, _ := cleanText(&value, webhookHeaderValue, false)
			clean, _ := cleanText(&name, projectionNameLimit, false)
			side.Headers[*clean] = *text
		}
	}
	payload := bytes.TrimSpace(s.Payload)
	if len(payload) > 0 && !bytes.Equal(payload, []byte("null")) {
		var text string
		if payload[0] == '"' {
			if json.Unmarshal(payload, &text) != nil {
				return nil, false
			}
		} else {
			var compact bytes.Buffer
			if json.Compact(&compact, payload) != nil {
				return nil, false
			}
			text = compact.String()
		}
		cleaned, cut := cleanText(&text, webhookPayloadLimit, false)
		side.Payload, side.Truncated = *cleaned, cut
	}
	return side, true
}
