package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	kitvault "github.com/castrowithcee/qatlas-kit/vault"
)

// readHiddenLine is the hidden terminal prompt a pasted redirect address is read with. It is a variable only
// so a test can paste an address, or none, without a real terminal; a real run leaves it at the kit's
// ReadHidden.
var readHiddenLine = kitvault.ReadHidden

func newConsentCommands(opts *Options, reg *capability.Registry) []*cobra.Command {
	consent := &cobra.Command{
		Use:   "consent <credential>",
		Short: "Give the consent of a credential that issues access tokens",
		Long: "Gives the consent of a credential whose provider issues access tokens, so its connections can be\n" +
			"used. The credential has type keyring or vault and holds the client ID of your own OAuth client,\n" +
			"and its client secret where the provider issued one; a credential of type env is refused before\n" +
			"anything happens. The provider's setup page says how to create the client.\n\n" +
			"The command prints a link to open in a browser. It asks for the scopes every connection of the\n" +
			fmt.Sprintf("credential needs together, and waits %d minutes for the answer. After the consent the provider\n",
				int(manage.ConsentTimeout.Minutes())) +
			"sends the browser back to an address on 127.0.0.1 with a port the system picks, where this command\n" +
			"takes the answer. When the browser runs on another device, for example over SSH, that page does\n" +
			"not load: copy its address from the address bar and paste it at the hidden prompt here. Only the\n" +
			"redirect address of this run with its state is taken; anything else is refused and the command\n" +
			"keeps waiting. Ctrl+C ends it. Nothing is stored unless the consent arrived.\n\n" +
			"The answer is exchanged for a refresh token, which is stored in the credential the way\n" +
			"'qconnect credential set' stores a secret; access tokens stay in memory. No token, code or verifier\n" +
			"is ever printed. A new consent replaces the refresh token, which is also how a scope a new tool\n" +
			"needs is added.\n\n" +
			"Like the other credential commands it runs only from an interactive terminal and asks for the\n" +
			"vault passphrase where the vault is encrypted; without a terminal it fails with admin-required.\n" +
			"An agent never runs it.",
		Args: exactlyOneCredential,
		RunE: func(c *cobra.Command, args []string) error { return giveConsent(c, opts, reg, args[0]) },
	}

	var gate *cobracli.Gate
	revoke := &cobra.Command{
		Use:   "revoke <credential>",
		Short: "Revoke the consent of a credential and remove its refresh token",
		Long: "Revokes the consent of a credential at the provider and then removes its refresh token here, also\n" +
			"when the provider does not confirm the revocation; the command then warns that the consent may\n" +
			"still be valid at the provider. 'qconnect credential delete <credential> refresh_token' does the\n" +
			"same.\n\n" +
			"The command first asks \"revoke the consent of <credential>? [y/N]\" at the terminal and changes\n" +
			"nothing unless the answer is y or yes. --yes answers it in advance. --confirm is required in\n" +
			"addition; without it nothing changes and the command fails as confirmation-required. Like the\n" +
			"other credential commands it runs only from an interactive terminal and asks for the vault\n" +
			"passphrase where the vault is encrypted.",
		Args: exactlyOneCredential,
		RunE: func(c *cobra.Command, args []string) error { return revokeConsent(c, opts, reg, args[0], gate) },
	}
	gate = opts.program.AddGate(revoke, cobracli.Gates{Question: true, Change: true})
	return []*cobra.Command{consent, revoke}
}

func giveConsent(c *cobra.Command, opts *Options, reg *capability.Registry, name string) error {
	if err := requireAdmin(opts); err != nil {
		return err
	}
	svc, secrets, err := consentService(opts, reg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(contextOrBackground(c.Context()), os.Interrupt, syscall.SIGTERM)
	defer stop()
	consent, err := svc.StartConsent(ctx, adminProof(), name)
	if err != nil {
		return consentFailure(err)
	}
	defer consent.Close()

	stderr := c.ErrOrStderr()
	fmt.Fprintf(stderr, "qconnect: open this link in a browser to give the consent of %s (waits %d minutes):\n%s\n",
		name, int(manage.ConsentTimeout.Minutes()), consent.Link())
	pasting, stopPasting := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		readPastedAddress(pasting, stderr, consent)
	}()
	outcome, err := consent.Wait(ctx)
	stopPasting()
	<-done
	if err != nil {
		return consentFailure(err)
	}

	fmt.Fprintf(c.OutOrStdout(), "stored the consent of %s for %s\n", name, strings.Join(outcome.Connections, ", "))
	if len(outcome.MissingScopes) > 0 {
		fmt.Fprintf(stderr, "qconnect: warning: the provider did not grant the scope %s; the tools that need it "+
			"end with scope-missing until a consent grants it\n", strings.Join(outcome.MissingScopes, ", "))
	}
	for _, warning := range []string{outcome.ProcessWarning, outcome.LogWarning} {
		if warning := withQatlasPrefix(warning); warning != "" {
			fmt.Fprintln(stderr, warning)
		}
	}
	for _, role := range []string{config.RoleClientID, config.RoleClientSecret, config.RoleRefreshToken} {
		if env := secret.DerivedEnvName(name, role); secrets.LookupDerived(env) {
			fmt.Fprintf(stderr, "qconnect: warning: %s is set and overrides what the credential stores for %s.%s\n",
				env, name, role)
		}
	}
	return nil
}

// readPastedAddress asks at the hidden prompt for the address a browser on another device ended on, until
// one is taken, the consent ends, or ctx ends. A refused address is not repeated in the message.
func readPastedAddress(ctx context.Context, stderr io.Writer, consent *manage.Consent) {
	for ctx.Err() == nil {
		line, err := readHiddenLine(ctx,
			"qconnect: if the browser runs on another device, paste the address it ends on here (hidden): ")
		if err != nil {
			return
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		err = consent.Paste(line)
		if err == nil {
			return
		}
		fmt.Fprintf(stderr, "qconnect: %v\n", err)
		if errors.Is(err, manage.ErrConsentEnded) {
			return
		}
	}
}

func revokeConsent(c *cobra.Command, opts *Options, reg *capability.Registry, name string, gate *cobracli.Gate) error {
	if err := requireAdmin(opts); err != nil {
		return err
	}
	cfg, _, err := storableCredential(opts, reg, name, config.RoleRefreshToken)
	if err != nil {
		return err
	}
	yes, _ := c.Flags().GetBool("yes")
	return gate.Run(cobracli.Request{Changes: true}, func() error {
		if !yes {
			confirmed, err := confirmDelete(fmt.Sprintf("revoke the consent of %s? [y/N] ", name))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Fprintf(c.OutOrStdout(), "kept the consent of %s\n", name)
				return nil
			}
		}
		return revokeAndReport(c, opts, reg, cfg, name)
	})
}

// revokeAndReport revokes the consent of name through the core, which removes its refresh token in any case,
// and says what happened.
func revokeAndReport(c *cobra.Command, opts *Options, reg *capability.Registry, cfg *config.Config, name string) error {
	svc, secrets, err := consentService(opts, reg)
	if err != nil {
		return err
	}
	out, err := svc.RevokeConsent(contextOrBackground(c.Context()), adminProof(), name)
	stderr := c.ErrOrStderr()
	if warning := withQatlasPrefix(out.ProcessWarning); warning != "" {
		fmt.Fprintln(stderr, warning)
	}
	var consentErr *manage.ConsentError
	switch {
	case errors.As(err, &consentErr) && consentErr.Reason == manage.ConsentMissing:
		return &UsageError{fmt.Errorf("no stored secret for %s.%s", name, config.RoleRefreshToken)}
	case err != nil:
		return consentFailure(err)
	}
	fmt.Fprintf(c.OutOrStdout(), "removed the refresh token of %s\n", name)
	reportRevocation(c, out.Confirmed, out.Note, out.LogWarning)
	if cfg.Credentials[name].Type != config.CredentialTypeVault && secrets.StoreSkipped() {
		fmt.Fprintf(stderr, "qconnect: warning: %s=%s, so the credential store was not touched and may still hold "+
			"the secret for %s.%s\n", secret.StoreSelector, secret.StoreNone, name, config.RoleRefreshToken)
	}
	if env := secret.DerivedEnvName(name, config.RoleRefreshToken); secrets.LookupDerived(env) {
		fmt.Fprintf(stderr, "qconnect: warning: %s is still set and keeps delivering the secret for %s.%s\n",
			env, name, config.RoleRefreshToken)
	}
	return nil
}

// reportRevocation shows the core's note on a revocation: a confirmation on stdout, the warning that the
// consent may still hold at the provider on stderr.
func reportRevocation(c *cobra.Command, confirmed bool, note, logWarning string) {
	if confirmed {
		fmt.Fprintln(c.OutOrStdout(), note)
	} else {
		fmt.Fprintln(c.ErrOrStderr(), withQatlasPrefix(note))
	}
	if warning := withQatlasPrefix(logWarning); warning != "" {
		fmt.Fprintln(c.ErrOrStderr(), warning)
	}
}

// consentService is the management service of a consent: it loads the configuration itself, stores and
// removes the refresh token through the write completion, logs the connections, and reaches the providers'
// consent endpoints through the registry.
func consentService(opts *Options, reg *capability.Registry) (*manage.Service, *secret.Resolver, error) {
	path, err := config.Path(opts.Config)
	if err != nil {
		return nil, nil, err
	}
	secrets, err := opts.resolver()
	if err != nil {
		return nil, nil, err
	}
	svc := manage.New(config.NewStore(path, reg), secrets, connlog.SurfaceCLI, opts.Redactor).
		WithVaultProcessSupport(vaultProcessSupported).WithConsentSource(reg.OAuth)
	return svc, secrets, nil
}

// consentFailure classifies what a consent or a revocation returned: what the credential or the
// configuration rules out is a usage problem, the rest keeps its own code.
func consentFailure(err error) error {
	var (
		consentErr *manage.ConsentError
		notThere   *config.NotThereError
	)
	switch {
	case errors.As(err, &consentErr) && consentErr.Reason == manage.ConsentAdminRequired:
		return &UsageError{&application.AdminRequiredError{}}
	case errors.As(err, &consentErr) && consentErr.Precondition(), errors.As(err, &notThere):
		return &UsageError{err}
	}
	return classifyUserError(err)
}

func exactlyOneCredential(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return newSyntaxError(fmt.Errorf("expected a credential name, got %d arguments", len(args)))
	}
	return nil
}
