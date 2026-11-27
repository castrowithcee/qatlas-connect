package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
)

// newRequestsCommand lists the open tool requests of agents for a person and decides them. Listing reads
// only; the three decisions are management commands that run at an interactive terminal.
func newRequestsCommand(opts *Options, reg *capability.Registry) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "requests",
		Short: "List the tool requests of agents and decide them",
		Long: "Requests lists the open requests of agents for tools a connection does not offer: the request ID,\n" +
			"the connection, the project it came from, when it expires, the reason the agent gave, and the\n" +
			"requested tools grouped like the tool picker of 'qconnect tui'. A request to remove tools (made with\n" +
			"'qconnect request --remove') shows kind remove-tools, any other kind add-tools. A request to create a\n" +
			"connection (made with 'qconnect request --create-connection') shows kind create-connection with the\n" +
			"provider, the wished name as connection, the targets and the tools; only 'decline' decides it here, and\n" +
			"'approve' and 'block' refuse with invalid-request: the user accepts it in 'qconnect tui' by setting the\n" +
			"connection up there. The reason is text of the agent: it\n" +
			"is shortened and cleaned of known secrets and control characters, and it is never logged. Listing\n" +
			"needs no terminal and reads no secret. A request expires after 14 days.\n\n" +
			"'requests approve', 'requests decline', and 'requests block' decide the tools of one request. Each\n" +
			"takes the request ID and, repeated, --tool with a tool ID or prefix entry exactly as the list shows\n" +
			"it; without --tool the decision covers every tool of the request. The tools you leave out stay\n" +
			"open. They run only at an interactive terminal and, where the vault is encrypted, ask for its\n" +
			"passphrase there, like 'qconnect vault approve'; an agent cannot decide a request.\n\n" +
			"The output is " + toonContract + " with LF line endings. --output json returns the same data as\n" +
			"JSON.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			format, err := discoveryFormat(c, opts)
			if err != nil {
				return err
			}
			path, err := config.Path(opts.Config)
			if err != nil {
				return err
			}
			src, err := manage.NewRequestSource(path, reg)
			if err != nil {
				return classifyUserError(err)
			}
			core, err := applicationCoreForInvoke(opts, reg, "cli", nil)
			if err != nil {
				return err
			}
			core.SetRequestQueue(src.Queue)
			open, err := core.OpenToolRequests()
			if err != nil {
				return classifyUserError(err)
			}
			return emitDocument(c, format, newRequestListDocument(open))
		},
	}
	cmd.AddCommand(
		newRequestsDecisionCommand(opts, reg, manage.RequestAccept),
		newRequestsDecisionCommand(opts, reg, manage.RequestDecline),
		newRequestsDecisionCommand(opts, reg, manage.RequestBlock),
	)
	return cmd
}

type requestListDocument struct {
	Requests []requestDocument `json:"requests"`
}

type requestDocument struct {
	ID         string                 `json:"id"`
	Kind       string                 `json:"kind"`
	Connection string                 `json:"connection"`
	Provider   string                 `json:"provider"`
	Targets    []string               `json:"targets,omitempty"`
	Project    string                 `json:"project"`
	ExpiresAt  string                 `json:"expires_at"`
	Reason     string                 `json:"reason"`
	Groups     []requestGroupDocument `json:"groups"`
}

// requestGroupDocument is the tools of a request under one group. Group and Title are empty for a provider
// that sorts its tools into no groups.
type requestGroupDocument struct {
	Group string   `json:"group"`
	Title string   `json:"title"`
	Tools []string `json:"tools"`
}

func newRequestListDocument(open []application.OpenToolRequest) requestListDocument {
	doc := requestListDocument{Requests: []requestDocument{}}
	for _, r := range open {
		item := requestDocument{
			ID: r.ID, Kind: string(r.Kind), Connection: r.Connection, Provider: r.Provider, Targets: r.Targets, Project: r.Project,
			ExpiresAt: r.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"), Reason: r.Reason,
			Groups: []requestGroupDocument{},
		}
		at := map[string]int{}
		for _, entry := range r.Entries {
			i, ok := at[entry.Group]
			if !ok {
				i = len(item.Groups)
				at[entry.Group] = i
				item.Groups = append(item.Groups, requestGroupDocument{Group: entry.Group, Title: entry.GroupTitle})
			}
			item.Groups[i].Tools = append(item.Groups[i].Tools, entry.Tool)
		}
		doc.Requests = append(doc.Requests, item)
	}
	return doc
}

type requestDecisionDocument struct {
	Action     string                  `json:"action"`
	Request    string                  `json:"request"`
	Connection string                  `json:"connection"`
	Mode       string                  `json:"mode"`
	Tools      []requestToolResult     `json:"tools"`
	Remaining  []string                `json:"remaining"`
	Approval   requestApprovalDocument `json:"approval"`
	Warnings   []string                `json:"warnings"`
}

type requestToolResult struct {
	Tool   string `json:"tool"`
	Result string `json:"result"`
}

type requestApprovalDocument struct {
	State    string `json:"state"`
	Detail   string `json:"detail"`
	NextStep string `json:"next_step"`
}

var requestResultNames = map[manage.RequestAction]string{
	manage.RequestAccept:  "accepted",
	manage.RequestDecline: "declined",
	manage.RequestBlock:   "blocked",
}

func newRequestDecisionDocument(res manage.RequestDecisionResult) requestDecisionDocument {
	doc := requestDecisionDocument{
		Action: string(res.Action), Request: res.Request, Connection: res.Connection, Mode: string(res.Mode),
		Tools: []requestToolResult{}, Remaining: nonNil(res.Remaining), Warnings: nonNil(res.Warnings),
		Approval: requestApprovalDocument{State: string(res.Approval.State), Detail: res.Approval.Detail},
	}
	for _, tool := range res.Decided {
		doc.Tools = append(doc.Tools, requestToolResult{Tool: tool, Result: requestResultNames[res.Action]})
	}
	for _, tool := range res.Dropped {
		doc.Tools = append(doc.Tools, requestToolResult{Tool: tool, Result: "dropped"})
	}
	if res.Approval.State == manage.ApprovalStaysOpen {
		doc.Approval.NextStep = "run 'qconnect vault approve' to approve the connection"
	}
	return doc
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// newRequestsDecisionCommand is approve, decline, or block of 'qconnect requests'. All three run the same
// steps: the terminal check first, then everything that can be refused without a secret, then the passphrase,
// then the decision of the management core.
func newRequestsDecisionCommand(opts *Options, reg *capability.Registry, action manage.RequestAction) *cobra.Command {
	var (
		tools []string
		as    string
	)
	verb := map[manage.RequestAction]string{
		manage.RequestAccept: "approve", manage.RequestDecline: "decline", manage.RequestBlock: "block",
	}[action]
	cmd := &cobra.Command{
		Use:   verb + " <request-id>",
		Short: requestShort[action],
		Long:  requestLong[action] + "\n\n" + requestCommon,
		Args:  exactlyOneArg("request ID"),
		RunE: func(c *cobra.Command, args []string) error {
			if err := requireAdminTerminal(); err != nil {
				return err
			}
			format, err := discoveryFormat(c, opts)
			if err != nil {
				return err
			}
			var mode manage.AcceptMode
			if action == manage.RequestAccept {
				if mode, err = parseAcceptMode(as); err != nil {
					return err
				}
			}
			path, err := config.Path(opts.Config)
			if err != nil {
				return err
			}
			secrets, err := opts.resolver()
			if err != nil {
				return err
			}
			src, err := manage.NewRequestSource(path, reg)
			if err != nil {
				return classifyUserError(err)
			}
			svc := manage.New(config.NewStore(path, reg), secrets, connlog.SurfaceCLI, opts.Redactor).
				WithVaultProcessSupport(vaultProcessSupported).WithRequests(src)
			selection := manage.RequestSelection{Request: args[0], Tools: tools}
			if preview, err := svc.PreviewRequestDecision(selection); err != nil {
				return requestFailure(err)
			} else if preview.Kind == accessrequest.KindCreateConnection && action != manage.RequestDecline {
				return &UsageError{&application.InvalidRequestError{Message: "a request to create a connection " +
					"is accepted by setting the connection up in 'qconnect tui'; here it can only be declined"}}
			}
			if action == manage.RequestAccept {
				if err := checkAcceptMode(svc, selection, mode); err != nil {
					return err
				}
			} else if preview, err := svc.PreviewRequestDecision(selection); err != nil {
				return requestFailure(err)
			} else if action == manage.RequestBlock && preview.Kind == accessrequest.KindRemoveTools {
				return &UsageError{&application.InvalidRequestError{Message: "a request to remove tools is approved or " +
					"declined, not blocked; decline it, or leave it open"}}
			}
			if err := requireAdmin(opts); err != nil {
				return err
			}
			res, err := svc.DecideRequest(contextOrBackground(c.Context()), adminProof(),
				manage.RequestDecision{RequestSelection: selection, Action: action, Mode: mode})
			if err != nil {
				return requestFailure(err)
			}
			doc := newRequestDecisionDocument(res)
			if err := emitDocument(c, format, doc); err != nil {
				return err
			}
			stderr := c.ErrOrStderr()
			if res.Approval.State == manage.ApprovalStaysOpen {
				fmt.Fprintf(stderr, "qconnect: the change to connection %s is saved, but the connection is still open "+
					"for approval (%s); %s\n", res.Connection, res.Approval.Detail, doc.Approval.NextStep)
			}
			for _, warning := range res.Warnings {
				fmt.Fprintln(stderr, "qconnect: "+strings.TrimPrefix(warning, "warning: "))
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&tools, "tool", nil, "tool ID or prefix entry of the request to decide, repeatable; "+
		"without it every tool of the request")
	if action == manage.RequestAccept {
		cmd.Flags().StringVar(&as, "as", "", "how to accept on a connection that offers its tools by effect: "+
			"effect adds the effects, tools switches the connection to a tools list")
	}
	return cmd
}

var requestShort = map[manage.RequestAction]string{
	manage.RequestAccept:  "Offer the requested tools on the connection",
	manage.RequestDecline: "Close requested tools without changing the connection",
	manage.RequestBlock:   "Close requested tools and block them on the connection",
}

var requestLong = map[manage.RequestAction]string{
	manage.RequestAccept: "Approve offers the requested tools on the request's connection and, where the connection\n" +
		"reads a credential of an encrypted vault, approves the connection in the same step. If approving fails,\n" +
		"the change stays saved and the output says that the connection is open under 'qconnect vault approve'.\n\n" +
		"A connection with a tools list gets the tools added to it. A connection that offers its tools by\n" +
		"effect needs --as: 'effect' adds the effects of the tools, so every tool of those effects is offered,\n" +
		"also tools added later; 'tools' switches the connection to a tools list of every tool it offers today\n" +
		"plus the requested ones, and tools added later no longer join by themselves. Without --as the command\n" +
		"stops and names both ways with the number of tools each one offers. A tool that only an exact entry in a\n" +
		"tools list can release goes through 'tools' only. --as is refused for a connection with a tools list.\n\n" +
		"For a request to remove tools, approve takes the named tools out of the connection and leaves every\n" +
		"other tool it offers. A prefix entry of a tools list that covers a removed tool is replaced by the exact\n" +
		"IDs of its other tools, so tools added later under it no longer join by themselves, and a list may end\n" +
		"up empty. A connection by effect needs --as tools, which switches it to a tools list; --as effect is\n" +
		"refused.",
	manage.RequestDecline: "Decline closes the tools of the request and changes nothing else. The agent may request\n" +
		"them again later.",
	manage.RequestBlock: "Block closes the tools of the request and records them as blocked on the connection, so a\n" +
		"new request for them ends at once with request-blocked. A block only stops requests: it never\n" +
		"removes a tool from what the connection offers. A request to remove tools cannot be blocked.",
}

const requestCommon = "The request ID and the tools are those of 'qconnect requests'. A tool that was offered or blocked\n" +
	"meanwhile, or no longer exists, is closed without a change and shown as dropped. The output names the\n" +
	"result for each tool and the tools that stay open. Runs only at an interactive terminal and asks for the\n" +
	"vault's passphrase where the vault is encrypted; without a terminal it fails with admin-required before it\n" +
	"changes anything."

func parseAcceptMode(as string) (manage.AcceptMode, error) {
	switch as {
	case "":
		return "", nil
	case "effect":
		return manage.AcceptAddEffects, nil
	case "tools":
		return manage.AcceptSwitchToList, nil
	}
	return "", &UsageError{&application.InvalidRequestError{Message: fmt.Sprintf("--as takes effect or tools, not %q", as)}}
}

// checkAcceptMode refuses what --as makes impossible before the passphrase is asked: the flag on a
// connection with a tools list, and no flag on a connection by effect, which names both ways with their
// consequence from the preview.
func checkAcceptMode(svc *manage.Service, sel manage.RequestSelection, mode manage.AcceptMode) error {
	preview, err := svc.PreviewRequestDecision(sel)
	if err != nil {
		return requestFailure(err)
	}
	if len(preview.Entries) == 0 {
		return nil
	}
	if preview.Kind == accessrequest.KindRemoveTools {
		if preview.ByEffects && mode != manage.AcceptSwitchToList {
			return &UsageError{&application.InvalidRequestError{Message: removeChoiceMessage(preview)}}
		}
		if !preview.ByEffects && mode != "" {
			return &UsageError{&application.InvalidRequestError{Message: fmt.Sprintf("connection %s offers a tools list, "+
				"so --as does not apply; leave it out", preview.Connection)}}
		}
		return nil
	}
	switch {
	case !preview.ByEffects && mode != "":
		return &UsageError{&application.InvalidRequestError{Message: fmt.Sprintf("connection %s offers a tools list, "+
			"so --as does not apply; leave it out", preview.Connection)}}
	case preview.ByEffects && mode == "":
		return &UsageError{&application.InvalidRequestError{Message: acceptChoiceMessage(preview)}}
	}
	return nil
}

func removeChoiceMessage(p manage.RequestPreview) string {
	return fmt.Sprintf("connection %s offers its tools by effect, so removing tools needs --as tools: it switches the "+
		"connection to a tools list of %d %s, and tools added later no longer join by themselves",
		p.Connection, p.Remaining, plural(p.Remaining, "tool", "tools"))
}

func acceptChoiceMessage(p manage.RequestPreview) string {
	var w strings.Builder
	fmt.Fprintf(&w, "connection %s offers its tools by effect; choose with --as how to accept: ", p.Connection)
	if len(p.AddEffectsBlockedBy) > 0 {
		fmt.Fprintf(&w, "--as effect is not possible, %s needs its exact ID in a tools list; ", p.AddEffectsBlockedBy[0])
	} else {
		var gains []string
		for _, gain := range p.AddEffects {
			gains = append(gains, fmt.Sprintf("%s (%d %s)", gain.Effect, gain.Tools, plural(gain.Tools, "tool", "tools")))
		}
		what := "adds no effect"
		if len(gains) > 0 {
			what = "adds " + strings.Join(gains, ", ")
		}
		fmt.Fprintf(&w, "--as effect %s, and tools of these effects added later are offered too; ", what)
	}
	fmt.Fprintf(&w, "--as tools switches the connection to a tools list of %d %s, and tools added later no longer join by themselves",
		p.SwitchTools, plural(p.SwitchTools, "tool", "tools"))
	return w.String()
}

// requestFailure maps a refusal of the decision core to the codes and exit codes of the command line:
// admin-required stays admin-required, a request or choice that does not apply is invalid-request, and an
// unknown or closed request is invalid-request with the way to the list.
func requestFailure(err error) error {
	var refused *manage.RequestDecisionError
	if !errors.As(err, &refused) {
		return classifyUserError(err)
	}
	switch refused.Code {
	case manage.RequestCodeAdminRequired:
		return &UsageError{&application.AdminRequiredError{}}
	case manage.RequestCodeNotFound:
		return &UsageError{&application.InvalidRequestError{
			Message: refused.Message + "; 'qconnect requests' lists the open requests"}}
	case manage.RequestCodeNeedsList:
		return &UsageError{&application.InvalidRequestError{Message: refused.Message + " (--as tools)"}}
	case manage.RequestCodeInvalid, manage.RequestCodeSetupInTUI:
		return &UsageError{&application.InvalidRequestError{Message: refused.Message}}
	}
	return errors.New(refused.Message)
}
