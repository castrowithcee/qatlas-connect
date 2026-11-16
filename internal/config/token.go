package config

import (
	"slices"
	"strings"
)

// The secret roles of a provider whose credential issues access tokens (see ProviderMetadata.Token). All of
// them are optional: which of them hold a value decides the procedure (see TokenProcedureOf).
const (
	RoleClientID          = "client_id"
	RoleClientSecret      = "client_secret"
	RoleRefreshToken      = "refresh_token"
	RoleServiceAccountKey = "service_account_key"
)

// TokenSecretRoles returns the secret roles of a provider whose credential issues access tokens.
func TokenSecretRoles() []SecretRole {
	return []SecretRole{
		{Name: RoleClientID, Description: "ID of the OAuth client from your own cloud project", Optional: true},
		{Name: RoleClientSecret, Description: "secret of that OAuth client, if it has one", Optional: true},
		{Name: RoleRefreshToken, Description: "refresh token of the consent; empty until consent is given", Optional: true},
		{Name: RoleServiceAccountKey, Description: "complete key file of a service account", Optional: true},
	}
}

// TokenMetadata describes a provider whose credential issues access tokens instead of holding a static
// secret. Scopes maps the ID of every tool of the provider to the smallest scopes it needs.
type TokenMetadata struct {
	Scopes map[string][]string
}

// TokenProcedure is the way a token credential obtains its access tokens.
type TokenProcedure string

const (
	// TokenConsent redeems the refresh token of a person's consent.
	TokenConsent TokenProcedure = "consent"
	// TokenServiceAccount signs an assertion with a service account key.
	TokenServiceAccount TokenProcedure = "service-account"
	// TokenImpersonation trades the token of a person's consent for one of the service account the
	// connection names.
	TokenImpersonation TokenProcedure = "impersonation"
)

// TokenProcedureOf picks the procedure from the roles that hold a value and the service account a connection
// names, empty for none: a service account key means a service account, a client ID a consent, and a client
// ID with a named service account an impersonation. It reports false for a key next to a client ID, for
// neither of them, and for a key next to a named service account; such a credential is unusable.
func TokenProcedureOf(clientID, key bool, serviceAccount string) (TokenProcedure, bool) {
	switch {
	case clientID == key, key && serviceAccount != "":
		return "", false
	case key:
		return TokenServiceAccount, true
	case serviceAccount != "":
		return TokenImpersonation, true
	}
	return TokenConsent, true
}

// TokenScopes returns, sorted and each once, the scopes needed by the tools that the effects, tools list
// and file release of a connection offer; its targets do not narrow them. It is nil for a connection whose
// provider does not issue access tokens. A tool without scopes in the provider's map adds none.
func (c *Config) TokenScopes(name string) []string {
	conn, ok := c.Connections[name]
	if !ok {
		return nil
	}
	provider := c.Services[conn.Service].Provider
	metadata, _ := c.ProviderMetadata(provider)
	if metadata.Token == nil {
		return nil
	}
	var effects []Effect
	if conn.Tools == nil {
		effects = c.ConnectionEffects(name)
	}
	scopes := []string{}
	for _, tool := range metadata.Tools {
		if listRefusalOf(provider, conn.Tools, effects, conn.Files, tool) != "" {
			continue
		}
		for _, scope := range metadata.Token.Scopes[tool.ID] {
			if !slices.Contains(scopes, scope) {
				scopes = append(scopes, scope)
			}
		}
	}
	slices.Sort(scopes)
	return scopes
}

// maxServiceAccount bounds the service account a connection names.
const maxServiceAccount = 254

// ValidServiceAccount reports whether account can stand as the service account a connection names: letters,
// digits and the characters . _ - + @, starting with a letter or digit, of a bounded length. A provider puts
// it into a fixed address as one escaped path segment, so it can neither leave that segment nor be a dot
// segment.
func ValidServiceAccount(account string) bool {
	if account == "" || len(account) > maxServiceAccount {
		return false
	}
	for i := 0; i < len(account); i++ {
		c := account[i]
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum && (i == 0 || !strings.ContainsRune("._-+@", rune(c))) {
			return false
		}
	}
	return true
}

// validateServiceAccount checks the service account a connection names. The message never repeats the
// value: a value that fails the rule may be anything pasted into the file.
func validateServiceAccount(name, account string, issuesTokens bool, report func(Problem)) {
	switch {
	case !issuesTokens:
		report(problem(SectionConnections, name, "service_account", CodeServiceAccountUnsupported,
			"the provider of the connection issues no access tokens, so it cannot impersonate a service account"))
	case !ValidServiceAccount(account):
		report(problem(SectionConnections, name, "service_account", CodeInvalidServiceAccount,
			"the service account must be its address or ID: letters, digits and . _ - + @, starting with a "+
				"letter or digit, at most %d characters", maxServiceAccount))
	}
}

// validateTokenCredential checks the credential of a connection whose provider issues access tokens. Its
// refresh token and scopes serve exactly one provider, so it must name it. Only an environment credential
// shows in the file which roles it supplies; it must supply exactly one complete procedure, because no
// consent can ever store a refresh token into a variable. A connection that names a service account
// impersonates it with a consent, which a service account key cannot do.
func validateTokenCredential(name, credential, provider, serviceAccount string, cred Credential, report func(Problem)) {
	if cred.Provider == "" {
		report(problem(SectionConnections, name, "credential", CodeTokenProviderMissing,
			"credential %q issues access tokens and must name its provider %q", credential, provider).
			with("credential", credential).with("provider", provider))
	}
	if cred.Type != CredentialTypeEnv {
		return
	}
	named := func(role string) bool { return strings.TrimSpace(cred.Values[role]) != "" }
	key, clientID := named(RoleServiceAccountKey), named(RoleClientID)
	switch {
	case serviceAccount != "" && (key || !clientID || !named(RoleRefreshToken)):
		report(problem(SectionConnections, name, "", CodeTokenProcedure,
			"credential %q must name the roles %s and %s and no %s, since the connection impersonates a service account",
			credential, RoleClientID, RoleRefreshToken, RoleServiceAccountKey).with("credential", credential))
	case key == clientID || clientID && !named(RoleRefreshToken):
		report(problem(SectionConnections, name, "", CodeTokenProcedure,
			"credential %q must name exactly one complete procedure: the role %s, or the roles %s and %s",
			credential, RoleServiceAccountKey, RoleClientID, RoleRefreshToken).with("credential", credential))
	}
}
