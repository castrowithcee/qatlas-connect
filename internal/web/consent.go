package web

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/manage"
)

// consentReturnPath is the fixed path of the redirect address of a consent: this run's own loopback address,
// so no second receiver is opened.
const consentReturnPath = "/consent"

// pendingConsent is the consent this run waits for and the credential it was started for.
type pendingConsent struct {
	consent    *manage.Consent
	credential string
}

// adminGrant is what withAdminProof hands a consent route: the proof for the core and the check whether the
// approval it spent still holds later.
type adminGrant struct {
	proof manage.AdminProof
	holds func() bool
}

// withAdminProof is withAdminGuard for a route that calls a core function demanding an AdminProof: the same
// checks, the approval spent the same way, and the mutation serialized behind writeMu.
func (s *Server) withAdminProof(next func(w http.ResponseWriter, r *http.Request, grant adminGrant)) http.HandlerFunc {
	return s.withSessionGuard(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		grant, ok := s.adminGrantLocked()
		s.mu.Unlock()
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
		next(w, r, grant)
	})
}

// adminGrantLocked spends the active admin approval for one action. Callers must hold s.mu.
func (s *Server) adminGrantLocked() (adminGrant, bool) {
	if !s.adminActiveLocked() {
		return adminGrant{}, false
	}
	session := s.session
	var proof manage.AdminProof
	once := false
	if required, _ := s.adminRequiredLocked(); required {
		once = s.admin.SingleUse()
		var ok bool
		if proof, ok = s.admin.Authorize(s.adminTimeout()); !ok {
			return adminGrant{}, false
		}
	} else {
		// A vault without a passphrase has nothing to spend, as for every other management command.
		unprotected := manage.NewAdminSession(true, nil)
		unprotected.Grant(0)
		proof, _ = unprotected.Authorize(0)
	}
	holds := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.sessionSet || subtle.ConstantTimeCompare([]byte(s.session), []byte(session)) != 1 {
			return false
		}
		// A single use was spent on this very action, so it covers its completion; a session must still be
		// active then.
		return once || s.adminActiveLocked()
	}
	return adminGrant{proof: proof, holds: holds}, true
}

// consentStartData is what the consent start page renders. Link carries the state and the PKCE challenge,
// never a secret.
type consentStartData struct {
	Credential string
	Link       string
	Minutes    int
}

// consentResultData is what the page the provider sends the browser back to renders. Nothing in it comes
// from the request: no code, no token and no text of the provider.
type consentResultData struct {
	Credential string
	Notice     string
	Warnings   []string
	Error      string
}

// handleStartConsent starts the consent of a credential with the redirect address of this run and shows the
// link to the provider. A consent started before is discarded.
func (s *Server) handleStartConsent(w http.ResponseWriter, r *http.Request, grant adminGrant) {
	if !s.credentialsReady(w) {
		return
	}
	name := r.PathValue("name")
	consent, err := s.svc.StartConsentAt(r.Context(), grant.proof, name, manage.ConsentReturn{
		Redirect: "http://" + s.addr + consentReturnPath, Holds: grant.holds,
	})
	if err != nil {
		s.credentialFailure(w, name, s.redact(err.Error()))
		return
	}
	s.mu.Lock()
	previous := s.consent
	s.consent = &pendingConsent{consent: consent, credential: name}
	s.mu.Unlock()
	if previous != nil {
		previous.consent.Close()
	}
	data := consentStartData{
		Credential: name, Link: consent.Link(),
		Minutes: int((time.Until(consent.Deadline()) + 30*time.Second) / time.Minute),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.credTmpl.ExecuteTemplate(w, "consent-start", data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// handleConsentReturn takes the answer the provider sends the browser back with. It is a cross-site
// navigation, so it carries neither the session cookie nor the CSRF value: only the exact Host
// (withLocalBoundary) and the state bind it to the start, and the core checks before the code is redeemed
// and before the refresh token is stored that the admin approval of the starting session still holds.
// Anything but the matching answer leaves the consent waiting.
func (s *Server) handleConsentReturn(w http.ResponseWriter, r *http.Request) {
	// The address carries the authorization code; no link of the result page may hand it on.
	w.Header().Set("Referrer-Policy", "no-referrer")
	s.mu.Lock()
	pending := s.consent
	s.mu.Unlock()
	if pending == nil {
		s.renderConsentResult(w, http.StatusGone, consentResultData{
			Error: "qconnect waits for no consent in this run. Start the consent again from the credential page."})
		return
	}
	switch err := pending.consent.Answer(r); {
	case errors.Is(err, manage.ErrConsentEnded):
		s.dropConsent(pending)
		s.renderConsentResult(w, http.StatusGone, consentResultData{Credential: pending.credential,
			Error: "This consent no longer waits for an answer. Start it again from the credential page."})
		return
	case err != nil:
		s.renderConsentResult(w, http.StatusBadRequest, consentResultData{
			Error: "This address does not belong to the consent qconnect waits for."})
		return
	}
	s.dropConsent(pending)

	s.writeMu.Lock()
	out, err := pending.consent.Wait(r.Context())
	s.writeMu.Unlock()
	data := consentResultData{Credential: pending.credential}
	if err != nil {
		data.Error = s.redact(err.Error())
		s.renderConsentResult(w, http.StatusOK, data)
		return
	}
	data.Notice = "Stored the consent of " + pending.credential + " for " + strings.Join(out.Connections, ", ") + "."
	if len(out.MissingScopes) > 0 {
		data.Warnings = append(data.Warnings, "The provider did not grant the scope "+
			strings.Join(out.MissingScopes, ", ")+"; the tools that need it end with scope-missing until a consent grants it.")
	}
	for _, warning := range []string{out.ProcessWarning, out.LogWarning} {
		if warning != "" {
			data.Warnings = append(data.Warnings, warning)
		}
	}
	s.renderConsentResult(w, http.StatusOK, data)
}

// dropConsent forgets pending unless a newer start replaced it already.
func (s *Server) dropConsent(pending *pendingConsent) {
	s.mu.Lock()
	if s.consent == pending {
		s.consent = nil
	}
	s.mu.Unlock()
}

func (s *Server) renderConsentResult(w http.ResponseWriter, status int, data consentResultData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = s.credTmpl.ExecuteTemplate(w, "consent-result", data)
}

// handleRevokeConsent revokes the consent of a credential through the core, which removes the refresh token
// also when the provider does not confirm the revocation.
func (s *Server) handleRevokeConsent(w http.ResponseWriter, r *http.Request, grant adminGrant) {
	if !s.credentialsReady(w) {
		return
	}
	name := r.PathValue("name")
	if r.PostFormValue("confirm") != "1" {
		s.credentialFailure(w, name, "tick the box to confirm the revocation; nothing was changed")
		return
	}
	// The removal must not stop halfway because the browser went away.
	out, err := s.svc.RevokeConsent(context.WithoutCancel(r.Context()), grant.proof, name)
	if err != nil {
		text := s.redact(err.Error())
		if out.Note != "" {
			text += "; " + out.Note
		}
		s.credentialFailure(w, name, withWarning(text, out.ProcessWarning))
		return
	}
	notice := "Removed the refresh token; " + out.Note + "."
	for _, warning := range []string{out.ProcessWarning, out.LogWarning} {
		notice = withWarning(notice, warning)
	}
	s.setNotice(notice)
	http.Redirect(w, r, "/credentials/"+url.PathEscape(name), http.StatusSeeOther)
}

// credentialFailure shows the page of credential name with errText, or 404 for a credential that is gone.
func (s *Server) credentialFailure(w http.ResponseWriter, name, errText string) {
	cfg, err := s.loadConfig()
	if err != nil {
		http.Error(w, s.redact(err.Error()), http.StatusInternalServerError)
		return
	}
	cred, ok := cfg.Credentials[name]
	if !ok {
		http.Error(w, "unknown credential", http.StatusNotFound)
		return
	}
	s.renderCredential(w, cfg, name, cred, "", errText)
}

// consentTemplates defines the consent start page and the page the provider sends the browser back to. Both
// work without a script; the link to the provider is an ordinary anchor.
const consentTemplates = `
{{define "consent-start"}}
{{template "layout-top" (page "Give the consent" "credentials")}}
<h1>Give the consent of {{.Credential}}</h1>
<p>Open the consent page of the provider in this browser and agree there. The provider then sends this browser back to qconnect, which stores the consent in the credential.</p>
<p class="actions"><a href="{{.Link}}" rel="noreferrer">Open the consent page of the provider</a></p>
<p class="hint">The link works once, for {{.Minutes}} minutes, while this run lasts and its admin approval stays active. Starting again replaces it. Nothing is stored unless you agree.</p>
<p class="actions"><a href="/credentials/{{.Credential}}">Back to {{.Credential}}</a></p>
{{template "layout-bottom"}}
{{end}}

{{define "consent-result"}}
{{template "layout-top" (page "Consent" "credentials")}}
<h1>Consent{{if .Credential}} of {{.Credential}}{{end}}</h1>
{{if .Notice}}<p class="notice notice-ok" role="status">{{.Notice}}</p>{{end}}
{{range .Warnings}}<p class="notice" role="status">{{.}}</p>{{end}}
{{if .Error}}<p class="notice notice-error" role="alert">{{.Error}}</p>{{end}}
<p class="actions">{{if .Credential}}<a href="/credentials/{{.Credential}}">Back to {{.Credential}}</a> {{end}}<a href="/">Overview</a></p>
{{template "layout-bottom"}}
{{end}}
`
