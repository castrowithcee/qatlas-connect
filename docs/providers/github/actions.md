---
description: >
  Describes the GitHub Actions tools: the observer and operator tools, diagnosing a run, log and artifact limits, billable time, the listed-only deletion of run logs, executions, the Actions variables of a repository, an environment, or an organization, and the listed-only workflow maintainer and Actions administrator tools with their paths and settings, including the finer Actions settings of a repository or an organization: selected actions, fork pull request approval, artifact and log retention, access of outside workflows, and the OIDC subject claim, and the self-hosted runners of a repository or an organization with their labels and the listed-only removal of a runner, and the listed-only runner group and hosted runner tools of an organization.
type: knowledge
edit: shared
created: 2026-10-22
updated: 2026-11-11
---

# GitHub Actions

The Actions tools of the [GitHub provider](README.md). Every tool takes `repository` as described under
[the target of a call](README.md#the-target-of-a-call), except the [Actions variable](#actions-variables) tools,
which take exactly one of `repository` or `organization`, and the [finer Actions settings](#finer-actions-settings), which do the same except the access tools, which take `repository` only, as do the [self-hosted runner](#self-hosted-runners) tools, and the [runner group and hosted runner tools](#runner-groups-and-hosted-runners), which take `organization` only.

A connection can observe the GitHub Actions of a repository it allows and, separately, operate them. The two
groups are distinct tools with distinct effects: the observer tools are reads, and the operator tools are
executions with the effect `execute`. Every route lies below the chosen repository; no tool accepts an owner
or a free REST path.

| Tool | Does |
| --- | --- |
| `github.workflows.list` | lists workflows |
| `github.workflows.get` | reads one workflow by identifier or file name such as `ci.yml` |
| `github.workflow-runs.list` | lists runs, newest first, with structured filters |
| `github.workflow-runs.get` | reads one run |
| `github.workflow-jobs.list` | lists the jobs of one run with their steps |
| `github.workflow-jobs.get` | reads one job with its steps |
| `github.workflow-jobs.log` | reads the last lines of one job log within a hard size limit |
| `github.workflow-artifacts.list` | lists the artifact metadata of one run, optionally by name |
| `github.workflow-runs.usage` | reads the billable time of one run for each runner operating system |
| `github.workflows.dispatch` | starts one `workflow_dispatch` run on a branch or tag |
| `github.workflow-runs.rerun` | re-runs every job of a completed run |
| `github.workflow-runs.rerun-failed` | re-runs the failed jobs of a completed run and their dependents |
| `github.workflow-runs.cancel` | asks GitHub to cancel a run; a completed run is reported with its state |
| `github.workflow-run-logs.delete` | deletes all logs of one run; listed-only, see [Deleting run logs](#deleting-run-logs) |

No observer or operator tool changes a workflow file or a setting; that is the listed-only group under
[workflow maintenance and Actions administration](#workflow-maintenance-and-actions-administration).
Environments with their deployment branch policies and protection rules, and deployments, have [their own
page](environments.md), as do [Actions caches and the cleanup of runs and artifacts](actions-maintenance.md);
Actions variables have tools under [Actions variables](#actions-variables). No tool approves or creates a
deployment, administers secrets or an organization, or registers runners; the [self-hosted
runners](#self-hosted-runners) and the [runner groups and hosted runners](#runner-groups-and-hosted-runners)
have their own listed-only tools.

## Observer and operator

An observer is a connection whose `effects` lack `execute`, or whose `tools` list names no
operator tool. It neither discovers nor runs an operator tool: `qconnect tools` and the MCP tools `search` and
`describe` do not offer them for it, and an invocation is refused as an unsupported capability before
a secret is read. Planning effects (`create`, `update`) never allow an execution; only `execute` does.

The terminal editor offers two setup profiles that are never preselected. `actions-observer` selects
the nine observer tools. `actions-operator` selects the observer tools and the four
operator tools. No profile selects `github.workflow-run-logs.delete`. The recommended profile `read` stays without Actions tools. A connection without a
`tools` list offers every read, the observer tools included, but never a listed-only tool; give it a `tools`
list to narrow that.

```yaml
connections:
  ci-observer:
    service: github
    credential: github-reader
    target: repos/octo-org/example
    tools: [github.workflows.list, github.workflows.get, github.workflow-runs.list, github.workflow-runs.get,
      github.workflow-jobs.list, github.workflow-jobs.get, github.workflow-jobs.log, github.workflow-artifacts.list,
      github.workflow-runs.usage]
  ci-operator:
    service: github
    credential: github-operator
    target: repos/octo-org/example
    tools: [github.workflow-runs.list, github.workflow-runs.get, github.workflow-jobs.list,
      github.workflow-jobs.log, github.workflows.dispatch, github.workflow-runs.rerun-failed,
      github.workflow-runs.cancel]
```

## Diagnosing a run

`github.workflow-runs.list` and `github.workflow-runs.get` answer a run with the same members. The called
repository is named once at the result level, since the object a call names is fixed by the call and not
repeated in every entry. The list leaves out pull requests unless `include_pull_requests` is true; the
single read always carries them. The head repository of a fork outside the targets of the connection shows
only its identifiers. An account inside a run carries the fields GitHub's own REST answers give it; the full
profile is read with the account tools. `status` takes a status or a conclusion. Titles, commit messages
(cut with a marker), and other texts are untrusted data. `github.workflow-jobs.list` answers a job with the
same members and steps as `github.workflow-jobs.get`.

## Logs and artifacts

A job log may carry anything a workflow printed and is treated as sensitive, untrusted data. Only
`github.workflow-jobs.log` reads it; no other answer embeds a log. The tool returns the last lines of at most the last
`max_bytes` bytes and `truncated` when the log holds more. Invalid UTF-8, terminal escape sequences, and control characters other than tab and line
feed are removed, so no binary content reaches the answer. The log passes through memory into the answer
only: Qatlas never writes it to disk, and audit events never carry it.

GitHub answers the log route with a redirect to a short-lived signed address, usually on another host.
Qatlas follows exactly that one https redirect without the token and without any other GitHub header, asks
for the last `max_bytes` bytes only, and follows no further redirect. A storage that ignores the range is
read up to 8 MiB, of which only the end is kept; a longer log is then refused.

Artifacts are zip archives. A bounded download would still be binary and would need unpacking or storing,
so Qatlas reads only their metadata. Run logs, which GitHub serves as a zip archive as well, are read per job
instead.

The `archive_download_url` of an artifact and the `logs_url` of a run are never answered: both addresses
answer with a short-lived signed download link that grants access without sign-in, and logs and artifacts
are read only through their own tools.

## Billable time

`github.workflow-runs.usage` reads the billable time of one run in the chosen repository for each GitHub-hosted
runner operating system GitHub reports. Re-runs are included; the macOS and Windows multiplier is not applied
and nothing is rounded to whole minutes. Billable time only
exists for private repositories on GitHub-hosted runners. At most 500 `job_runs` are answered for each
operating system, and `truncated` is true when more were cut. GitHub has announced that this endpoint is being
closed down; a refusal then comes back as GitHub answers it.

## Deleting run logs

`github.workflow-run-logs.delete` deletes all logs of one run in the chosen repository and cannot be undone. It is listed-only: a connection offers it only while its `tools` list names it, and it needs
confirmation (`--confirm`). No profile selects it. Qatlas reads the run first,
so a missing run is answered not found. GitHub
documents only the success answer `204`; when GitHub answers the delete of an existing run with not found,
Qatlas reads the logs as already deleted and answers `deleted: true` with `already_deleted: true`. The delete
is sent once and never repeated.

## Executions

Every execution needs the tool offered by the connection, by `execute` in its `effects` or by name in its
`tools` list, and confirmation in its own invoke request (`--confirm`, or `confirm: true` over MCP). A missing
confirmation ends the request before a secret is read and before GitHub is contacted. An
execution is sent exactly once; an unclear outcome (a timeout, a dropped connection, a server error) says
that the execution may have happened, is never repeated by Qatlas, and must be checked with
`github.workflow-runs.list` or `github.workflow-runs.get` before a repetition. After each execution the next
request of the same token waits one second.

`github.workflows.dispatch` takes `inputs` as a flat map of input names to string values; write a boolean as
`"true"` or `"false"`. Before it dispatches, Qatlas reads the workflow and its file at `ref` and checks the inputs against the
`on.workflow_dispatch.inputs` the file declares: an undeclared input, a missing required input without a
default, a `boolean` other than `true` or `false`, a `number` that is no number, and a `choice` outside its
options are refused without a dispatch. A workflow without the `workflow_dispatch` trigger at `ref`, an
inactive workflow, and a workflow without a file are refused as well. GitHub validates the dispatch again.
GitHub answers a dispatch without the new run; list the runs with `event: workflow_dispatch` and the branch
to follow it.

The re-runs and the cancel read the run first. A re-run needs a completed run, so GitHub's refusal never
reads like a missing permission.

The cancel is idempotent. For a run that has not completed it sends one cancel and answers `accepted: true`;
the run then ends asynchronously. For a run that has already completed, cancelled or not,
it sends nothing and answers `accepted: false` with the `status` and `conclusion` it found. When the run
completes between the read and the cancel, GitHub refuses the cancel; Qatlas reads the run again and answers
the same way.

The token rights are listed under [GitHub Actions](setup.md#github-actions).

## Actions variables

| Tool | Does |
| --- | --- |
| `github.action-variables.list` | lists the variables of a repository, an environment, or an organization, with their values |
| `github.action-variables.get` | reads one variable by name |
| `github.action-variables.create` | creates one variable; a name that already exists is refused |
| `github.action-variables.update` | changes the value or the name of one variable, and of an organization variable its visibility and selected repositories |
| `github.action-variables.delete` | deletes one variable permanently; listed-only |

Every call takes exactly one of `repository` or `organization`, like the
[rulesets](repositories.md#rulesets-repository-and-organization-governance); an organization must be an owner
target of the connection. `environment` together with `repository` addresses the variables of one environment of
that repository and never comes with `organization`. Both, neither, or an environment without a repository is an
invalid request before a credential is resolved. The environment name travels as one path segment, so a slash
stays part of the name; `.`, `..`, and control characters are refused. No tool covers secrets, Dependabot or
Codespaces variables, or the environments themselves.

GitHub stores a value as plain text that the workflows of the repository can read, and a value may still carry
data its owner did not mean to publish. Qatlas treats it as sensitive, untrusted data: an answer cuts it at 1000
characters in a list and at 20000 in a single read and then sets `value_truncated`, a change never echoes it, and
no message or refusal quotes it. A connection that selects by `effects` offers the two reads, values included;
give it a `tools` list to keep them out. Names and the 48 KB bound of a value are checked before any request;
GitHub compares names without case.

An organization variable needs `visibility` when it is created. `selected` needs `selected_repositories` in the same
call, which replaces the current selection: repositories of that organization, as `OWNER/REPO`, each inside the
connection's repository targets and not excluded. An `orgs/LOGIN` target alone admits none of them; add
`repos/LOGIN/*` or the repositories themselves. Qatlas reads each of them for the identifier GitHub expects before
it changes anything, so a repository GitHub does not show stops the call unchanged. `all` and `private` let the
workflows of every repository, or every private repository, of the organization read the variable, including
repositories outside the connection's targets or excluded by it.

The create and the update need `create` or `update` in `effects` or their name in `tools`; the delete is listed
only. Each needs confirmation, is sent exactly once, and an unclear outcome says that the change may have been
applied. GitHub answers a create and an update without the variable, so the answer names what was sent: the name
and, for an organization, the visibility and the selected repositories. The token rights are listed under
[GitHub Actions](setup.md#github-actions).

## Workflow maintenance and Actions administration

Two more groups of repository tools change what runs in a repository and with which rights: the workflow
maintainer writes workflow files, which decide what code runs with the repository's secrets, and the Actions
administrator decides whether Actions run at all, which actions they may use, what the `GITHUB_TOKEN` of
every run may do, and how the repository accepts merges (the [repository settings](repositories.md#repository-settings)). They are high risk,
and Qatlas keeps them behind a boundary of their own:

- **Listed only.** A connection offers such a tool only when its `tools` list names it. A connection that
  selects by `effects` never offers one: a planning connection with `create` and `update`, an operator with
  `execute`, or a connection with every effect neither discovers nor runs them. `qconnect tools`, `qconnect describe`, the MCP tools `search` and
  `describe`, route selection, and invoke apply the same rule, and the complete tool contract
  (`qconnect describe --full`) shows `requires_tool_allow_list: true`. The terminal editor marks these tools
  `(listed only)`.
- **Never preselected.** No profile a new connection starts with selects them, and the recommended profile
  of a provider may not. The profiles `workflow-maintainer` and `actions-admin` exist only to be chosen on
  purpose; they keep the two groups apart.
- **One repository per call.** Each call acts on one repository inside the connection's targets, and every
  route lies below that repository; only the [finer Actions settings](#finer-actions-settings) may address one
  organization instead. Without targets, such a connection reaches every repository its token can
  change, so give it targets that name exactly the repositories it may maintain.
- **Confirmed and sent once.** Every change needs confirmation in its own invoke request, is sent exactly
  once, and is never retried; an unclear outcome says that the change may have been applied. A refused or
  unconfirmed request ends before a secret is read and before GitHub is contacted.

| Tool | Does |
| --- | --- |
| `github.workflow-files.list` | lists the workflow files, without content |
| `github.workflow-files.get` | reads one workflow file with its content and blob SHA |
| `github.workflow-files.create` | commits one new workflow file; fails when the file exists |
| `github.workflow-files.update` | replaces one workflow file while it still has the given blob SHA |
| `github.workflows.enable` | enables one workflow |
| `github.workflows.disable` | disables one workflow; a disabled workflow stays as it is |
| `github.actions-permissions.get` | reads whether Actions run, which actions they may use, and whether they must be pinned to a commit SHA |
| `github.actions-permissions.update` | changes whether Actions run and which actions they may use |
| `github.workflow-permissions.get` | reads the default `GITHUB_TOKEN` permissions and whether Actions may approve pull request reviews |
| `github.workflow-permissions.update` | changes either of them |
| `github.actions-selected.get` / `.set` | reads or changes which actions and reusable workflows may run while `allowed_actions` is `selected`; repository or organization |
| `github.actions-fork-approval.get` / `.set` | reads or changes which contributors' fork pull requests need approval before workflows run; repository or organization |
| `github.actions-retention.get` / `.set` | reads or changes how many days checks, runs, statuses, artifacts, and logs are kept; repository or organization |
| `github.actions-access.get` / `.set` | reads or changes which workflows outside a private repository may use its actions; repository only |
| `github.actions-oidc.get` / `.set` | reads or changes the customization of the OIDC subject claim; repository or organization |

The reads are listed only as well: a workflow file and the Actions settings are part of what this group
maintains, and the observer tools already read what diagnosing a run needs. A create and an update are
idempotent because a repetition writes nothing: GitHub refuses a create of an existing file and an update
whose blob SHA the file no longer has.

### Paths, blob SHAs, and content

A `path` is `.github/workflows/NAME.yml` or `.github/workflows/NAME.yaml`, where `NAME` has 1 to 100
letters, digits, `.`, `_`, or `-`, does not start with `.`, and holds no `..`. Nothing else is accepted: no
subdirectory, no other directory, no absolute or relative prefix, no percent-encoding, no whitespace, and no
character outside ASCII, so a lookalike slash or dot cannot lead elsewhere. The path is checked before a
secret is read, and a path of another form, `ci.yml` as well as a subdirectory, is an invalid request that
names the rule: `$.path does not have the required form a file directly in .github/workflows/ ending in .yml
or .yaml`. No tool deletes or renames a file or writes any other path.

`github.workflow-files.update` needs `sha`, the blob SHA `github.workflow-files.get` or
`github.workflow-files.list` reported. GitHub writes the file only while it still has that blob; otherwise
nothing is written, and the refusal says so: read the file again and apply the change to its current
content. `github.workflow-files.create` takes no `sha` and never replaces an existing file.

Qatlas does not generate, repair, or interpret workflows; GitHub decides whether a file is a valid workflow.
`github.workflow-files.get` refuses a file larger than 512 KiB or one that is not text, and returns the content
as untrusted data only on that explicit read. The `download_url` of a workflow file is never answered: "For a
private repository GitHub answers `download_url` with a temporary token; it grants access without sign-in and is
withheld."

A change answers with metadata only. It never echoes the content or the commit message, and the audit event of
a change names only the request, tool, connection, confirmation, result, and time.

`github.workflows.enable` and `github.workflows.disable` accept only a workflow whose file lies below
`.github/workflows/`. Both are idempotent: enabling an active workflow leaves it `active`. A workflow already in
a disabled state (`disabled_manually`, `disabled_inactivity`, or `disabled_fork`) is not changed by a disable,
which sends nothing and answers that state as `previous_state` and `state`.

### Actions settings

`allowed_actions` set to `selected` keeps the selection of allowed actions maintained in the repository settings,
which Qatlas does not change: that pattern list is a separate, wider surface. At least one value is required, and
a value left out stays as it is: GitHub replaces a setting as a whole, so Qatlas reads it first and sends it with
the requested values in one change. `allowed_actions` applies only while Actions are enabled and is refused for
disabled Actions unless `enabled: true` comes with it. An organization or enterprise policy may fix a value; GitHub's refusal is reported, and nothing is changed or
retried.

### Finer Actions settings

Five more settings have one tool that reads and one that changes them, ten tools in all, and every one is
listed only like the rest of this group. The reads join the profile `actions-admin`; the five changes are in no
profile and need their name in a connection's `tools` list. Four settings exist for a repository and for an
organization and take exactly one of `repository` or `organization`, like the
[Actions variables](#actions-variables): both, neither, an organization outside the connection's owner targets,
a user instead of an organization, and a target the connection excludes are refused before a credential is
resolved. The access setting exists for a repository only and takes `repository` like the other repository
tools. An answer names the repository or the organization it addressed.

| Setting | Values | Notes |
| --- | --- | --- |
| Selected actions | `github_owned_allowed`, `verified_allowed`, `patterns_allowed` | applies only while `allowed_actions` is `selected` |
| Fork pull request approval | `approval_policy`: `first_time_contributors_new_to_github`, `first_time_contributors`, `all_external_contributors` | |
| Retention | `days` from 1 through 400, and the read's `maximum_allowed_days` | GitHub rejects what the plan or a policy does not allow |
| Access | `access_level`: `none`, `user`, `organization` | only for private and internal repositories |
| OIDC subject claim | `use_default` (repository only), `include_claim_keys`, `use_immutable_subject` | the read also answers `sub_claim_prefix` for a repository |

Each read answers every documented member of its route. Patterns, claim keys, and prefixes are untrusted data.

Every change reads the setting first, sends one request that is never repeated, and reads it again, so the answer
holds `before` and `after` for the values the call gave, as GitHub reports them; a value left out stays as it is
and is not named. A read that fails after an applied change does not turn it into an error: `after` is then left
out and `after_unconfirmed` is `true`, which says that GitHub accepted the change but its state could not be read
back. A value that was sent never stands in for GitHub's state. A refusal or an unclear outcome follows the rules above: GitHub's text never reaches a
message, 409 and 422 become messages that say nothing was changed, and a timeout or a server error says that the
change may have been applied. A refused first read ends the call before anything is changed.

`patterns_allowed` takes patterns as text, such as `octo-org/*`, `octo-org/action@v1`, or `!octo-org/denied`, at
most 1000, each of at most 255 letters, digits, and `_ . / @ * ! + -` without whitespace, and replaces the
current patterns; an empty list removes them. The selection is a security boundary: changing from only
GitHub-owned actions to all of them, allowing verified creators, or adding a pattern such as `*` lets workflows
run code Qatlas does not review, so treat each loosening as security relevant and grant the change only to a
connection that needs it. `include_claim_keys` takes only the claim keys GitHub documents for the subject claim
(`actor`, `actor_id`, `base_ref`, `context`, `environment`, `event_name`, `head_ref`, `job_workflow_ref`,
`job_workflow_sha`, `ref`, `ref_type`, `repo`, `repository`, `repository_id`, `repository_owner`,
`repository_owner_id`, `repository_visibility`, `run_attempt`, `run_id`, `run_number`, `runner_environment`, `sha`,
`workflow`, `workflow_ref`, `workflow_sha`, `check_run_id`) and the repository property claims, written `repo_property_` and the
name of the property, with letters, digits, and `_` only, at least one and none twice. A cloud provider that trusts a subject
claim trusts the new form at once, so a change can lock workflows out or widen who may assume a role. For a
repository `use_default` is required: `true` selects the default claim and comes without keys, `false` needs the
keys. An organization takes `include_claim_keys`, `use_immutable_subject`, or both and refuses `use_default`; with
only `use_immutable_subject` it keeps the keys it has, and without customized keys they must be given.

Plan and policy limits are a state, not an error, for the reads. When GitHub refuses a read with a conflict
because the plan, an organization or enterprise policy, or another setting does not offer the setting, for example
the selection while `allowed_actions` is not `selected` or the access of a public repository, the tool answers
with `state: "unavailable"` and a fixed `reason` and gives no value of the setting and no text of GitHub. A
refusal with 403 or 404 stays an error, because GitHub does not tell a missing right or target apart from it. A
change on such a setting stays an error that says nothing was changed. No tool changes the enterprise level of
these settings, runner groups, hosted runners, or secrets.

### Self-hosted runners

Seven tools read the self-hosted runners of a repository or an organization, change their custom labels, and remove
a runner. Each takes exactly one of `repository` or `organization`, with the refusals of the
[finer Actions settings](#finer-actions-settings): both, neither, an organization outside the connection's owner
targets, a user instead of an organization, and a target the connection excludes end before a credential is resolved.
Every tool is listed only like the rest of this group: the three reads join the profile `actions-admin`, the four
changes are in no profile, and all need their name in a connection's `tools` list. An answer names the repository or
the organization it addressed.

| Tool | Effect | Does |
| --- | --- | --- |
| `github.runners.list` | read | lists one batch of runners, optionally by `name`, with `limit` up to 100 and `cursor` |
| `github.runners.get` | read | reads one runner by `runner_id` |
| `github.runners.delete` | delete | removes one runner for good |
| `github.runner-labels.list` | read | lists the labels of one runner |
| `github.runner-labels.add` | update | adds 1 to 100 custom labels, none twice |
| `github.runner-labels.set` | update | replaces all custom labels with 0 to 100 labels; an empty list removes them all |
| `github.runner-labels.remove` | update | removes one custom label by `label` |

A runner answers every documented member: `id`, `runner_group_id`, `name`, `os`, `status`, `busy`, `labels` (each
with `id`, `name`, and `type`, which is `read-only` or `custom`), `ephemeral`, and `version`. The list adds
`total_count`, `next_cursor`, and `has_more`. Names, labels, operating system, status, and version are untrusted data and cut at
200 characters. The `name` filter is applied by GitHub; Qatlas drops an entry whose name differs when compared
without case, so a server that ignores the filter lets nothing else through. A repository lists the organization
runners available to it as well; the organization list shows the runners of the organization.

`runner_id` is bound to the chosen target by evidence: every removal and every label change first reads the runner
below that repository or organization, and ends with GitHub's refusal, without a change, when the target does not
hold it or when the answer names another runner. The runner is the one GitHub shows the target; whether GitHub
lets that target change a runner that belongs to another level is GitHub's decision, and the call reports a
refusal as nothing removed or changed. The time between the evidence read and the change is a known boundary. A
removal and a label change are one request that is never repeated; a timeout or a server error says that the
change may have been applied, and an answer without a readable label list after a change says so as well.

Read-only labels, such as `self-hosted`, the operating system, and the architecture, are applied by GitHub and cannot
be changed. `remove` refuses a label that the evidence read shows as read-only, and `add` and `set` refuse a name
equal to a read-only label, compared without case; the refusal says so and nothing is sent. A label is 1 to 100
characters of one line without control characters, is not padded with spaces, and is not `.` or `..`; it travels
as one escaped path segment. The change answers `runner_id`, `total_count`, and the labels GitHub reports afterwards.

Removing a runner takes its registration from GitHub; GitHub refuses a runner that is running a job, and the machine
must be registered again to return, which no tool does. Labels decide which jobs a runner takes, so a label change
can move jobs to another machine or leave them waiting. No tool registers a runner or issues a registration or
removal token, a JIT configuration, or the download of the runner application; runner groups and GitHub-hosted
runners have no tool.

### Runner groups and hosted runners

Four tools read and change the runner groups of an organization and read its GitHub-hosted runners. A runner
group decides which repositories may send jobs to which machines, so every one is listed only like the rest of
this group: a connection offers it only when its `tools` list names it. The three reads join the profile
`actions-admin`; `github.runner-groups.update` is in no profile. All four take `organization` as `orgs/LOGIN`
and no `repository`: a user, a repository, an organization outside the connection's owner targets, and an
organization the connection excludes are refused before a credential is resolved. An answer names the
organization it addressed. Runners and runner labels are not part of these tools.

| Tool | Does |
| --- | --- |
| `github.runner-groups.list` | lists the self-hosted runner groups of the organization, including those inherited from an enterprise; `visible_to_repository` keeps only the groups one repository may use, `limit` and `cursor` page |
| `github.runner-groups.get` | reads one group by `id` |
| `github.runner-groups.update` | changes the `name` or the `visibility` (`selected`, `all`, `private`) of one group |
| `github.hosted-runners.list` | lists the GitHub-hosted runners of the organization; `limit` and `cursor` page |

Each read answers every member GitHub documents for its route. A group has `id`, `name`, `visibility`,
`default`, `selected_repositories_url`, `runners_url`, `hosted_runners_url`, `network_configuration_id`,
`inherited`, `inherited_allows_public_repositories`, `allows_public_repositories`,
`workflow_restrictions_read_only`, `restricted_to_workflows`, and `selected_workflows`. A hosted runner has `id`,
`name`, `runner_group_id`, `image_details`, `machine_size_details`, `status`, `platform`, `maximum_runners`,
`public_ip_enabled`, `public_ips`, `last_active_on`, and `image_gen`. Names, labels, workflow names, and every
address are untrusted data, cut at fixed lengths, and the addresses are never fetched. The IP and network details
of a hosted runner are cut hard: at most ten `public_ips` entries, each prefix at 64 characters, and
`public_ips_truncated` says when entries were left out. `visible_to_repository` takes `OWNER/REPO`, which must
belong to the organization and lie inside the connection's repository targets; GitHub receives its name. GitHub
documents the filter without saying whether it expects the name alone, so confirm it against a live organization
before relying on it.

`github.runner-groups.update` sends exactly one `PATCH` and offers only `name` and `visibility`, at least one of
them. GitHub's route also takes `allows_public_repositories`, `restricted_to_workflows`, `selected_workflows`,
and `network_configuration_id`, and the selected repositories of a group have a route of their own; none of them
is offered, so `selected` leaves the repositories of the group as GitHub holds them. GitHub requires a name with
every change, so a call that gives only `visibility` sends the group's current name, exactly as GitHub reported
it. The group is first read below the organization, which binds its `id` to the connection's target: a group
GitHub does not find there, or answers with another identifier, ends the call before anything is changed. The
default group and an inherited group are not refused by Qatlas; GitHub decides what it allows. The answer holds
the `id`, the `name` and `visibility` before the change, and the group as GitHub answered the change in `after`;
there is no further read, since the answer to the change is the group. GitHub documents defaults for the settings the request leaves out, so the tool sends the values it just read for them: `allows_public_repositories` as read and, while the group is restricted, `restricted_to_workflows` with the whole `selected_workflows` list as GitHub reported it. It loosens nothing the call did not ask to change. A group restricted to more workflows than the tool can read whole (over 100 entries or an entry over 1000 characters) is refused before the request, with a message that says nothing was changed. For an unrestricted group neither the restriction nor the list is sent. A refusal or an unclear outcome follows the rules above: GitHub's text never reaches a message, 409 and
422 say that nothing was changed, and a timeout, a server error, or an answer that cannot be read says that the
change may have been applied; the change is never repeated. Changing `visibility` decides which repositories may
run jobs on the group's machines, so treat it as security relevant and grant the change only to a connection that
needs it.

No tool creates or deletes a runner group or a hosted runner, registers a runner, or answers a registration token,
a JIT configuration, or a download address of the runner application.

### Credential

The token rights are listed under [GitHub Actions](setup.md#github-actions), which also says why these connections
get a credential of their own.

```yaml
connections:
  ci-maintainer:
    service: github
    credential: github-maintainer
    target: repos/octo-org/example
    tools: [github.workflows.list, github.workflows.get, github.workflow-files.list, github.workflow-files.get,
      github.workflow-files.create, github.workflow-files.update, github.workflows.enable, github.workflows.disable]
  ci-admin:
    service: github
    credential: github-maintainer
    target: repos/octo-org/example
    tools: [github.actions-permissions.get, github.actions-permissions.update, github.workflow-permissions.get,
      github.workflow-permissions.update, github.repository-settings.get, github.repository-settings.update]
```
