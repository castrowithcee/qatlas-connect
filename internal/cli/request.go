package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// newRequestCommand asks for tools a connection does not offer. A request changes nothing, so the command
// has no confirmation gate, needs no terminal, no passphrase, and no agent token.
func newRequestCommand(opts *Options, registry *capability.Registry) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "request <tool-id or prefix>...",
		Short: "Ask the user for tools a connection does not offer",
		Long: "Request asks for tools that a connection of this project does not offer, so that the user can offer\n" +
			"them. Name each tool by its ID, or a whole group by a prefix of the form <provider>.<segment>.*;\n" +
			"'qconnect tools <provider> --all' lists the tools no connection offers and names, as requestable_on,\n" +
			"the connection to ask, always an active one. Ask only for what the task needs. One call names the\n" +
			"tools of one provider, at most 50, and is accepted or refused as a whole.\n\n" +
			"--connection selects the connection like it does for invoke: its name, its id, or name|id; left out,\n" +
			"the default of the tool, then that of its provider, then the one connection of the provider in this\n" +
			"project is used, and several candidates are refused with connection-ambiguous.\n\n" +
			"--reason gives the user a short reason. It is shown to the user as untrusted text, shortened, with\n" +
			"known secrets removed and control characters escaped, and it is never logged.\n\n" +
			"A request changes nothing by itself: it stays open for 14 days, until the user decides, and a\n" +
			"further request for the same connection joins it. The answer names the request id, the connection,\n" +
			"every tool the request now holds, the status open, and expires_at. Stop then, tell the user what you\n" +
			"asked for, and look at 'qconnect tools' later to see whether the tool is offered. Requesting the\n" +
			"same tools again does not hurry the user.\n\n" +
			"A tool the user blocked on the connection ends with request-blocked: do not request it again. A\n" +
			"tool the connection already offers, a tool no decision of the user could offer (see 'qconnect tools\n" +
			"--all'), and a malformed name end with invalid-request. A request needs no passphrase and no\n" +
			"terminal; it is written beside the configuration and logged without its reason.\n\n" +
			"The output is " + toonContract + " with LF line endings. --output json returns the same data as\n" +
			"JSON.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return newSyntaxError(fmt.Errorf("expected at least one tool ID or prefix"))
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			format, err := discoveryFormat(c, opts)
			if err != nil {
				return err
			}
			core, err := applicationCoreForRequest(opts, registry, "cli")
			if err != nil {
				return err
			}
			response, err := core.RequestTools(application.ToolRequest{
				Connection: opts.Connection, Tools: args, Reason: reason,
			})
			if err != nil {
				return classifyUserError(err)
			}
			return emitDocument(c, format, response)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "a short reason for the user, shown as untrusted text and never logged")
	return cmd
}
