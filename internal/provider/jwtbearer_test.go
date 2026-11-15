package provider_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// bearerServer is a token endpoint that records the form it was sent and answers with status and body.
type bearerServer struct {
	status int
	body   string
	forms  []url.Values
	hits   int
}

func (s *bearerServer) RoundTrip(req *http.Request) (*http.Response, error) {
	s.hits++
	raw, _ := io.ReadAll(req.Body)
	form, _ := url.ParseQuery(string(raw))
	s.forms = append(s.forms, form)
	if req.URL.String() != sampleToken {
		return answer(req, http.StatusNotFound, `{}`), nil
	}
	return answer(req, s.status, s.body), nil
}

const bearerOK = `{"access_token":"synthetic-bearer-access","token_type":"Bearer","expires_in":3599,` +
	`"refresh_token":"synthetic-unexpected-refresh","scope":"items.read items.write"}`

// keyFile generates a key at run time and returns the key file and the key.
func keyFile(t *testing.T, mutate func(map[string]any)) (string, *rsa.PrivateKey) {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{
		"type": "service_account", "client_email": "robot-sample-account", "private_key_id": "synthetic-kid",
		"token_uri":   sampleToken,
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	}
	if mutate != nil {
		mutate(fields)
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw), private
}

func bearerOptions(server *bearerServer, now time.Time) provider.TokenOptions {
	grant := provider.ServiceAccountGrant(provider.ServiceAccountOptions{
		Endpoint: sampleToken, Client: provider.NoRedirectClient(5*time.Second, server),
		Now: func() time.Time { return now },
	})
	return provider.TokenOptions{
		Token: *sampleMetadata.Token, Cache: provider.NewTokenCache(func() time.Time { return now }),
		Grants: map[config.TokenProcedure]provider.TokenGrant{config.TokenServiceAccount: grant},
	}
}

// openBearer opens the token source of a credential that holds only the given key file.
func openBearer(t *testing.T, key string, server *bearerServer, red *redact.Redactor, extra map[string]string) (*provider.TokenSource, error) {
	t.Helper()
	h := newSampleHarness()
	h.values = map[string]string{config.RoleServiceAccountKey: key}
	for role, value := range extra {
		h.values[role] = value
	}
	return provider.OpenTokenSource(context.Background(), h.resolved(t), h.secrets(red), red,
		bearerOptions(server, h.clock.Now()))
}

func TestServiceAccountGrantSignsAnAssertionAndExchangesIt(t *testing.T) {
	key, private := keyFile(t, nil)
	server := &bearerServer{status: http.StatusOK, body: bearerOK}
	red := &redact.Redactor{}
	source, err := openBearer(t, key, server, red, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Check(context.Background()); err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if len(server.forms) != 1 || server.forms[0].Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		t.Fatalf("forms = %v", server.forms)
	}
	parts := strings.Split(server.forms[0].Get("assertion"), ".")
	if len(parts) != 3 {
		t.Fatalf("assertion has %d parts", len(parts))
	}
	decode := func(s string) []byte {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&private.PublicKey, crypto.SHA256, digest[:], decode(parts[2])); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	var header map[string]string
	var claims struct {
		Iss, Scope, Aud, Sub string
		Iat, Exp             int64
	}
	if err := json.Unmarshal(decode(parts[0]), &header); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(decode(parts[1]), &claims); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "RS256" || header["kid"] != "synthetic-kid" {
		t.Fatalf("header = %v", header)
	}
	if claims.Iss != "robot-sample-account" || claims.Aud != sampleToken || claims.Sub != "" ||
		claims.Scope != "items.read items.write" {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.Exp <= claims.Iat || claims.Exp-claims.Iat > 3600 {
		t.Fatalf("exp - iat = %d, want within one hour", claims.Exp-claims.Iat)
	}
}

func TestServiceAccountGrantRedactsKeyAssertionAndToken(t *testing.T) {
	key, private := keyFile(t, nil)
	server := &bearerServer{status: http.StatusOK, body: bearerOK}
	red := &redact.Redactor{}
	source, err := openBearer(t, key, server, red, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(private)
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	assertion := server.forms[0].Get("assertion")
	for name, secret := range map[string]string{"key file": key, "private key": pemText, "assertion": assertion,
		"signature": assertion[strings.LastIndexByte(assertion, '.')+1:],
		"access":    "synthetic-bearer-access", "refresh": "synthetic-unexpected-refresh"} {
		if got := red.Apply("seen " + secret + " here"); strings.Contains(got, secret) {
			t.Errorf("redactor does not know the %s", name)
		}
	}
}

func TestServiceAccountGrantKeepsNoRefreshToken(t *testing.T) {
	key, _ := keyFile(t, nil)
	server := &bearerServer{status: http.StatusOK, body: bearerOK}
	opts := bearerOptions(server, time.Now())
	token, err := opts.Grants[config.TokenServiceAccount](context.Background(), provider.TokenRequest{
		Procedure: config.TokenServiceAccount, Secrets: provider.TokenSecrets{ServiceAccountKey: key},
		Scopes: []string{"items.read"}})
	if err != nil || token.Refresh != "" || token.Access != "synthetic-bearer-access" {
		t.Fatalf("token = %+v, err = %v", token, err)
	}
}

func TestServiceAccountKeyIsRejectedWithTheFieldNotTheValue(t *testing.T) {
	cases := map[string]struct {
		mutate func(map[string]any)
		field  string
	}{
		"type":            {func(f map[string]any) { f["type"] = "authorized_user" }, "type"},
		"client_email":    {func(f map[string]any) { delete(f, "client_email") }, "client_email"},
		"private_key_id":  {func(f map[string]any) { f["private_key_id"] = 7 }, "private_key_id"},
		"token_uri":       {func(f map[string]any) { f["token_uri"] = "https://evil.sample.invalid/synthetic-uri" }, "token_uri"},
		"token_uri empty": {func(f map[string]any) { delete(f, "token_uri") }, "token_uri"},
		"private_key":     {func(f map[string]any) { f["private_key"] = "synthetic-not-a-pem" }, "private_key"},
		"private_key der": {func(f map[string]any) {
			f["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("synthetic-garbage")}))
		}, "private_key"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			key, _ := keyFile(t, tc.mutate)
			_, err := provider.ParseServiceAccountKey(key, sampleToken)
			if classOf(err) != provider.ClassCredentialUnusable {
				t.Fatalf("error = %v, want credential-unusable", err)
			}
			message := err.Error()
			if !strings.Contains(message, `"`+tc.field+`"`) {
				t.Errorf("message %q does not name the field %s", message, tc.field)
			}
			for _, value := range []string{"synthetic-uri", "synthetic-not-a-pem", "synthetic-garbage", "BEGIN", "robot-sample-account"} {
				if strings.Contains(message, value) {
					t.Errorf("message %q shows a value", message)
				}
			}
		})
	}
	if _, err := provider.ParseServiceAccountKey("synthetic-not-json", sampleToken); classOf(err) != provider.ClassCredentialUnusable ||
		strings.Contains(err.Error(), "synthetic-not-json") {
		t.Errorf("error = %v", err)
	}
}

func TestServiceAccountForeignTokenURINeverReachesTheNetwork(t *testing.T) {
	key, _ := keyFile(t, func(f map[string]any) { f["token_uri"] = "https://evil.sample.invalid/token" })
	server := &bearerServer{status: http.StatusOK, body: bearerOK}
	source, err := openBearer(t, key, server, &redact.Redactor{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Check(context.Background()); classOf(err) != provider.ClassCredentialUnusable {
		t.Fatalf("Check() = %v, want credential-unusable", err)
	}
	if server.hits != 0 {
		t.Fatalf("hits = %d, want no request", server.hits)
	}
}

func TestServiceAccountKeyWithClientIDIsUnusable(t *testing.T) {
	key, _ := keyFile(t, nil)
	server := &bearerServer{status: http.StatusOK, body: bearerOK}
	_, err := openBearer(t, key, server, &redact.Redactor{}, map[string]string{config.RoleClientID: "sample-client-id"})
	if classOf(err) != provider.ClassCredentialUnusable || server.hits != 0 {
		t.Fatalf("error = %v, hits = %d", err, server.hits)
	}
}

func TestServiceAccountRefusalCarriesNoEndpointText(t *testing.T) {
	key, _ := keyFile(t, nil)
	server := &bearerServer{status: http.StatusBadRequest,
		body: `{"error":"invalid_grant","error_description":"synthetic-echo-of-the-assertion"}`}
	source, err := openBearer(t, key, server, &redact.Redactor{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = source.Check(context.Background())
	if classOf(err) != provider.ClassAuth {
		t.Fatalf("Check() = %v, want auth", err)
	}
	if strings.Contains(err.Error(), "synthetic-echo") {
		t.Fatalf("error %q carries text of the endpoint", err)
	}
}
