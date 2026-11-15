---
description: >
  Describes the GitHub security tools: the code scanning, Dependabot, and secret scanning alerts of a repository, the global, repository, and organization security advisories with the change of a draft repository advisory, code quality findings, the security switches of a repository, its code scanning default setup, and the dependency changes between two refs with dependency snapshots, the code security configurations of an organization, and the private registries Dependabot uses in an organization, with their targets and cuts.
type: knowledge
edit: shared
created: 2026-10-22
updated: 2026-11-15
---

# GitHub security

The security tools of the [GitHub provider](README.md). The advisory and finding tools only read, except the one change named below, and so do the
alert reads. Three tools change an alert's state, and [the security switches](#security-switches) and
[the code scanning default setup](#code-scanning-default-setup) read and, listed-only, change; each change is
offered only where a connection's `tools` list names it, as are [the dependency snapshot](#dependency-graph) and the
six changes of [code security configurations](#code-security-configurations), the creation, change, and deletion
of a [private registry](#private-registries), and the [change of a draft advisory](#changing-a-draft-advisory). The
not-recommended setup profile `security` selects the six alert tools, the four reads of instances, analyses, and
locations, the six advisory and finding tools, the two reads of the switches and the default setup, the
dependency review, the four reads of [code security configurations](#code-security-configurations), and the two
reads of [private registries](#private-registries), twenty-five in all and all reading; it holds none of the
sixteen changes. The recommended profile `read` is unchanged.

## Security alerts

Six tools read the alerts of one repository, and three more read what lies behind them: the instances of a code
scanning alert, the analyses of code scanning, and the locations of a secret scanning alert. They change nothing.
Three further tools change an alert's state, described under [Changing alerts](#changing-alerts).

| Tool | Does |
| --- | --- |
| `github.code-scanning-alerts.list` | lists code scanning alerts |
| `github.code-scanning-alerts.get` | reads one code scanning alert with the rule help |
| `github.dependabot-alerts.list` | lists Dependabot alerts |
| `github.dependabot-alerts.get` | reads one Dependabot alert with the advisory description |
| `github.secret-scanning-alerts.list` | lists secret scanning alerts without the secret |
| `github.secret-scanning-alerts.get` | reads one secret scanning alert without the secret |
| `github.code-scanning-alert-instances.list` | lists the instances of one code scanning alert |
| `github.code-scanning-analyses.list` | lists the code scanning analyses of a repository |
| `github.code-scanning-analyses.get` | reads one code scanning analysis |
| `github.secret-scanning-locations.list` | lists where one secret scanning alert found its secret |

Each list answers one bounded batch, newest first, with `limit`, `next_cursor`, and `has_more`; a cursor
belongs to the repository and the filters of its first batch. Each `.get` takes an `alert_number` as the list
reports it. Every tool takes the optional `repository` argument and is checked against the connection's
targets before a credential is resolved; an organization-wide alert list is not offered.

Alert texts (rule and advisory descriptions, messages, dismissal comments, paths) come from scanners and other
accounts and are untrusted data. Each is cut at 500 characters (4000 for the rule help and the advisory
description of a `.get`), and `truncated` says so.

The members an alert of any kind carries beyond these are cleaned the same way, and each of their cuts says so
in its own `*_truncated` member, never in `truncated`.

A secret scanning alert never shows the found secret, and its comments are withheld because they may repeat it.
The request asks GitHub to hide the secret (`hide_secret=true`), and the answer is decoded into a shape that
has no field for the secret, a snippet, or a comment, not even inside the first location, so nothing else
GitHub sends can reach a result, an error, or a log. An answer that cannot be read or is too large fails with
a fixed message that quotes nothing of it.

The instance list takes the `alert_number` and the optional filters `ref` and `pr`; each instance carries `ref`,
`analysis_key`, `environment`, `category`, `state`, `commit_sha`, `message`, `location`, `url`, and
`classifications`, cut and cleaned like an alert's most recent instance. The analysis list takes the filters
`tool_name` or `tool_guid` (not both), `ref`, `pr`, `sarif_id`, `sort` (`created`), and `direction`; an analysis
carries `id`, `ref`, `commit_sha`, `analysis_key`, `environment`, `category`, `error`, `created_at`,
`results_count`, `rules_count`, `sarif_id`, `deletable`, `warning`, and `tool`, and GitHub names no web address
for it, so its REST address sits in `links.url` as an untrusted address that is never fetched. `error` and
`warning` are cut at 500 characters and `truncated` says so. The tools read; uploading and deleting analyses
is not offered. The location list names, for each place a secret was found, the `type` (a commit, a wiki
commit, or the title, body, or a comment of an issue, a pull request, or a discussion), the `path`, lines,
columns, `blob_sha`, and `commit_sha` where GitHub gives them, and the addresses of the location under `links`.
It never shows the found text: a location is decoded into a shape without a member for a secret, a snippet,
or a comment.

An `alert_number` and an `analysis_id` are read below the chosen repository, so a number that belongs to another
repository answers `not-found`, and an answer that names another number is refused.

The token rights are listed under [Security](setup.md#security).
Sources: https://docs.github.com/en/rest/code-scanning/code-scanning,
https://docs.github.com/en/rest/dependabot/alerts,
https://docs.github.com/en/rest/secret-scanning/secret-scanning, and
https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens.

### Changing alerts

Three tools change the state of one alert of one repository. Each is offered only where a connection's `tools`
list names it, needs the confirmation described under [confirmed changes](README.md#confirmed-changes), and is in
no setup profile.

| Tool | Does |
| --- | --- |
| `github.code-scanning-alerts.update` | dismisses a code scanning alert or reopens it |
| `github.dependabot-alerts.update` | dismisses a Dependabot alert or reopens it |
| `github.secret-scanning-alerts.update` | resolves a secret scanning alert or reopens it |

Each takes the `alert_number` and a required `state`: `open` or `dismissed` for the two dismissal tools, `open` or
`resolved` for the secret scanning tool. A dismissal needs `dismissed_reason`, a resolution needs `resolution`,
and neither is allowed with `open`; a refused combination never reaches GitHub. The documented values are

| Tool | Values |
| --- | --- |
| code scanning `dismissed_reason` | `false positive`, `won't fix`, `used in tests`, `mitigated` |
| Dependabot `dismissed_reason` | `fix_started`, `inaccurate`, `no_bandwidth`, `not_used`, `tolerable_risk` |
| secret scanning `resolution` | `false_positive`, `wont_fix`, `revoked`, `used_in_tests` |

`dismissed_comment` is optional and at most 280 characters, GitHub's limit; `resolution_comment` is optional and
at most 65536 characters, since GitHub documents no limit for it; a longer one is refused, never cut. `create_request` exists on the code scanning tool only, with `state` `dismissed`: it asks
GitHub for the dismissal to be approved where the repository requires that, and repeating it asks again, so that
tool is classified non-idempotent while the others are idempotent. `pattern_edited` and `pattern_deleted` are
resolutions GitHub sets itself and a list filters by; a change cannot set them. Assigning an alert is not offered.

Every call first reads the alert below the chosen repository, which binds the `alert_number` to it, and then sends
exactly one `PATCH` with only the fields its state needs. The change is never repeated: after a timeout, an
abort, a server error, or an unreadable answer the tool says the change may have been applied, and the alert is
read before the change is repeated. The window between the read and the change is a limit, not a guarantee.
GitHub's refusals become messages of their own without GitHub's text. The answer is the alert as the matching
`.get` tool reads it; the secret scanning answer is decoded into the shape that has no member for the secret or a
comment even though GitHub's answer to a change carries them.

Changing alerts needs write rights, listed under [Security](setup.md#security).

### Advisories and code quality findings

Six more tools read advisories and code quality findings and change nothing. One more tool changes the content
of a draft advisory, described under [Changing a draft advisory](#changing-a-draft-advisory); an advisory is
never created, published, or closed.

| Tool | Does |
| --- | --- |
| `github.global-advisories.list` | lists global security advisories |
| `github.global-advisories.get` | reads one global security advisory |
| `github.repository-advisories.list` | lists the security advisories of a repository |
| `github.repository-advisories.get` | reads one security advisory of a repository by its `ghsa_id` |
| `github.organization-advisories.list` | lists the security advisories of an organization's repositories |
| `github.code-quality-findings.get` | reads one code quality finding |

`github.global-advisories.list` reads the GitHub Advisory Database, GitHub-wide, and gives each entry every
member `.get` does; GitHub answers the type `reviewed` when `type` is omitted, and `is_withdrawn` false
adds no filter. `github.code-quality-findings.get` reads one finding by `finding_number`; GitHub also lists
findings, and that list is not offered. `github.repository-advisories.get` takes a `ghsa_id` of the form
`GHSA-xxxx-xxxx-xxxx`, reads a draft or triage advisory too if the token's account may see it, and shows the
same members as the list; an answer for another `ghsa_id` is refused as `invalid-response`. Its description is
cut at 4000 characters, not 2000.

The global tools name no repository: a connection without targets, or one whose targets name only owners, may
use them, and a connection whose targets name a repository or a project is refused as `invalid-request`
before a credential is resolved. The organization list takes the optional `owner` argument as `orgs/LOGIN`,
which must be an organization the connection's targets name as an owner (or any organization on a connection
without targets); a user owner and a repository or project target alone are refused the same way. The
repository list and read and the finding read take the optional `repository` argument like the alert tools.

The lists page by the `after` cursor GitHub announces in its `Link` header; `next_cursor` carries it together
with the batch size of the first batch and belongs to the tool, the target, and the filters of that batch.

Advisory summaries, descriptions, references, package names, and finding messages come from other accounts and
are untrusted data. Summaries, rule descriptions, and messages are cut at 500 characters, a global advisory
description and a rule help at 4000, and the description of a repository advisory in a list at
2000, and the Markdown of a finding message at 2000; vulnerabilities are capped at 20 per advisory, functions
at 10 per vulnerability, references at 20, and `truncated` says so. The lists an advisory carries beyond these
(identifiers, credits, collaborators) are capped at 100 and say so in their own `*_truncated` member, never in
`truncated`. Draft and triage advisories hold non-public vulnerability details; they reach only a token whose
account GitHub allows to see them.

An account inside an advisory carries the fields GitHub's own REST answers give it; the full profile is read
with the account tools. The temporary private fork of a repository advisory is a repository: one outside the
connection's targets keeps only its identifiers (`id`, `node_id`, `full_name`), and the clone token GitHub
sends with it is never shown.

### Changing a draft advisory

| Tool | Does |
| --- | --- |
| `github.repository-advisories.update` | changes the content of one repository advisory that is still a draft |

The tool is listed-only, in no profile, and needs the confirmation of a change. It reads the advisory first and
refuses one in any other state (triage, published, closed, withdrawn) as an invalid request without a write
request. It changes only `summary` (up to 1024 characters), `description` (up to 65535), `severity` (`critical`,
`high`, `medium`, `low`) or `cvss_vector_string`, never both, `cwe_ids` (such as `CWE-79`, up to 20, replacing
the list), and `vulnerabilities` (up to 20 packages, each with `package` holding `ecosystem` and `name`,
`vulnerable_version_range`, `patched_versions`, and `vulnerable_functions`, replacing the list). At least one of
them is required, and only the ones given are sent. The tool offers no `state`, so it cannot publish or close
an advisory, and no `credits`, `collaborating_users`, `collaborating_teams`, or `cve_id`, which would notify
people or open the draft to others; requesting a CVE is not offered either. An argument outside the input schema is
refused before any request.

All values are checked before a secret is read. The change is one `PATCH` that is never retried; after a
timeout, a server error, or an unreadable answer the tool says it may have been applied. `403` names the needed
rights, `404` is `not-found`, and `422` is an invalid request, none with GitHub's text. The answer has the
members of `github.repository-advisories.get`; the texts are untrusted data, and the description is cut at 4000
characters in the answer.

The token rights are listed under [Security](setup.md#security). Sources:
https://docs.github.com/en/rest/security-advisories/global-advisories,
https://docs.github.com/en/rest/security-advisories/repository-advisories, and
https://docs.github.com/en/rest/code-quality/code-quality.

## Dependency graph

Two tools read and write the dependency graph of one repository.

| Tool | Does | Offered |
| --- | --- | --- |
| `github.dependency-review.compare` | lists the dependencies added or removed between two refs | with the `read` permission |
| `github.dependency-snapshots.create` | submits the dependencies of one build as a snapshot | only where the connection's `tools` list names it |

The software bill of materials of a repository is not offered: GitHub closed down its synchronous export route
after 2026-11-13 in favour of an asynchronous flow that no tool covers.

`github.dependency-review.compare` takes the required `base` and `head`, each a branch, a tag, or a commit SHA, and
the optional `name`, the path of one dependency manifest to filter by. A fork's `OWNER:ref` is not accepted. It
answers `changes`, each with `change_type` (`added` or `removed`), `manifest`, `ecosystem`, `name`, `version`,
`package_url`, `license`, `source_repository_url`, `vulnerabilities` (`severity`, `advisory_ghsa_id`,
`advisory_summary`, `advisory_url`), and `scope` (`unknown`, `runtime`, or `development`); a member GitHub reports
as `null` is left out. GitHub answers the whole list at once, so the tool keeps at most 500 changes and 20
vulnerabilities per change; `total` counts what GitHub answered, `truncated` says that changes were left out, and
`vulnerabilities_truncated` that vulnerabilities were. Package names, versions, licenses, summaries, and every
address come from other accounts and are untrusted data: texts are cut at 500 characters and addresses at 2048, a
cut sets `truncated` on the change, and no address is fetched. A private repository without GitHub Advanced
Security and a fork are refused by GitHub with a permission error, which the message names without quoting GitHub.
Every documented member of the answer is passed on.

`github.dependency-snapshots.create` takes a structured snapshot: `version` (0 through 1000000), `sha`, `ref`
(`refs/...`), `job` (`id`, `correlator`, optional `html_url`), `detector` (`name`, `version`, `url`), `scanned` (an
RFC 3339 time), optional `metadata` (at most 8 keys with scalar values), and optional `manifests` by name, each
with `name`, optional `file.source_location` and `metadata`, and `resolved` packages by name with `package_url`
(starting with `pkg`), `relationship` (`direct` or `indirect`), `scope` (`runtime` or `development`),
`dependencies` (package URLs), and `metadata`. The limits are 50 manifests, 2000 resolved packages in all, 200
dependencies per package, and 2 MiB of request body; the addresses of the job and the detector must be `http` or
`https` without credentials and are only stored by GitHub. Only the latest snapshot per `job.correlator` and detector
name counts for the repository's current dependencies. Everything is checked before a credential is resolved, so a
refused snapshot never reaches GitHub.

The `sha` must be a commit of the chosen repository: the tool first reads it through the Git commits route and
refuses with `invalid-request`, sending nothing, when GitHub does not find it; the window between that read and the
snapshot is a limit, not a guarantee, and GitHub may resolve a commit that reached the repository through a fork of
the same network. Then exactly one `POST` follows, never repeated, and the call needs the confirmation described
under [confirmed changes](README.md#confirmed-changes). A repeated call submits a second snapshot. After a timeout,
an abort, a server error, or an unreadable answer the tool says the snapshot may have been submitted. The answer is
`id`, `created_at`, `result` (`SUCCESS`, `ACCEPTED`, or `INVALID`), and `message`, GitHub's untrusted detail text cut
at 500 characters with `message_truncated`; an `INVALID` result is an answer, not an error.

The token rights are listed under [Security](setup.md#security). Sources:
https://docs.github.com/en/rest/dependency-graph/dependency-review and
https://docs.github.com/en/rest/dependency-graph/dependency-submission.

## Security switches

Two tools read and change the security switches of one repository.

| Tool | Does | Offered |
| --- | --- | --- |
| `github.security-settings.get` | reads the state of every switch | with the `read` permission |
| `github.security-settings.update` | switches the given switches on or off | only where the connection's `tools` list names it |

`.get` reads the repository, which carries the `security_and_analysis` object, and four routes: Dependabot
alerts, Dependabot security updates, private vulnerability reporting, and immutable releases. It answers one
entry per switch with a `state` of `enabled`, `disabled`, or `unavailable`: `dependabot_alerts`,
`automated_security_fixes` (with `paused` while enabled), `private_vulnerability_reporting`,
`immutable_releases` (with `enforced_by_owner`), and the `security_and_analysis` members `advanced_security`,
`code_security`, `dependabot_security_updates`, `secret_scanning`, `secret_scanning_push_protection`,
`secret_scanning_non_provider_patterns`, `secret_scanning_ai_detection`,
`secret_scanning_delegated_alert_dismissal`, and `secret_scanning_delegated_bypass`, plus the reviewers of
delegated bypass as `secret_scanning_delegated_bypass_reviewers` (`reviewer_id`, `reviewer_type`, `mode`). A
read that GitHub refuses with 403 or 404 on the repository, or with 403 on a route, fails with the permission
the read needs and quotes nothing of GitHub.

`unavailable` is a state of its own, not an error, and no switch GitHub does not offer is reported as on or off:

| Switch | Reported as `unavailable` when |
| --- | --- |
| a `security_and_analysis` member | GitHub leaves it out of the repository, as it does for a plan without the feature and for a token without administrator rights; the two cannot be told apart |
| `private_vulnerability_reporting` | the route answers 422 |

The routes for Dependabot alerts, Dependabot security updates, and immutable releases answer 404 when the switch is
not enabled, which GitHub documents, and also to a token without administrator rights. A 404 of one of them is
therefore read as `disabled` only when the repository read showed `permissions.admin` as true. Without that proof
the read fails with the permission hint instead of reporting a state; no state is guessed. The alerts route answers
204 for on. An answer with a status value other than `enabled` or `disabled` fails as an invalid response.

`.update` takes any of `dependabot_alerts`, `automated_security_fixes`, `private_vulnerability_reporting`,
`immutable_releases`, `secret_scanning`, `secret_scanning_push_protection`,
`secret_scanning_non_provider_patterns`, `secret_scanning_ai_detection`,
`secret_scanning_delegated_alert_dismissal`, and `secret_scanning_delegated_bypass` as `true` or `false`, and at
least one. Each switch is exactly one request and is never repeated: a route switch is a `PUT` or `DELETE` of its
route, an analysis switch is a `PATCH` of the repository that names this one member. The switches go in the order
above. The result lists every given switch as `set` (GitHub accepted the request), `rejected` with a `reason`
(`permission`, `not-found`, `conflict`, `invalid`, `auth`, `rate-limited`, or `refused`), `unknown` (the outcome is
unclear: a timeout, a 5xx, an unreadable answer, or an answer whose state differs from the request; read the
settings before repeating), or `not-sent`. A refusal that proves nothing was applied and concerns only that
switch (`permission`, `not-found`, `conflict`, `invalid`) lets the next switch follow. An `unknown` outcome, an
`auth` or `rate-limited` refusal, or any other refusal ends the call, and the rest are `not-sent`. `reported` names
the state GitHub's answer gives for an analysis switch where it gives one. When no switch was set, the call
fails with the class of the first failure and a message naming each switch and its outcome; an unclear outcome adds
that the change may have been applied.

Switching off secret scanning, push protection, or Dependabot alerts lowers the protection of the repository,
and so does turning on delegated bypass, which lets named reviewers approve bypassing push protection. A
`rejected` with `invalid` is GitHub's 422 or 400, which also covers a feature the plan does not include.

Limits of the change:

- `advanced_security` and `code_security` are never written: they change what an organization is billed for.
- `secret_scanning_delegated_bypass_options` is never written: it names reviewers by team or role identifier
  that Qatlas cannot check, so the reviewers of delegated bypass are read only. Enabling
  `secret_scanning_delegated_bypass` sends only its state and leaves the reviewers as they are.
- Dependabot security updates are switched only through `automated_security_fixes`, not through the
  `security_and_analysis` member of the same name.
- The organization-wide security configurations are not offered.

The token rights are listed under [Security](setup.md#security). Sources:
https://docs.github.com/en/rest/repos/repos#update-a-repository,
https://docs.github.com/en/rest/repos/repos#enable-vulnerability-alerts,
https://docs.github.com/en/rest/repos/repos#enable-dependabot-security-updates,
https://docs.github.com/en/rest/repos/repos#enable-private-vulnerability-reporting-for-a-repository, and
https://docs.github.com/en/rest/repos/repos#enable-immutable-releases.

## Code scanning default setup

Two tools read and change the code scanning default setup of one repository.

| Tool | Does | Offered |
| --- | --- | --- |
| `github.code-scanning-setup.get` | reads the default setup | with the `read` permission |
| `github.code-scanning-setup.update` | changes the default setup | only where the connection's `tools` list names it |

`.get` answers `state` (`configured` or `not-configured`), `languages`, `runner_type`, `runner_label`,
`query_suite`, `threat_model`, `updated_at`, and `schedule`; a member GitHub reports as `null` is left out. A
repository for which GitHub answers 403 or 503, which GitHub documents for a repository without the license for
code scanning and for an unavailable service, is answered with the state `unavailable` alone, not as an error. A
token without the right shows the same 403, so the state names no cause. A 404 stays an error naming the
permission. The runner label is untrusted data of the repository owner.

GitHub has no route that lists the languages a repository could be analyzed with, so there is no
`github.code-scanning-languages.list`: `.get` already gives the languages of the configuration in full, and the
languages that could be added are not listed anywhere by the API. A language GitHub reports that the tool does not
know yet is passed through when it is a short lower-case name.

`.update` takes `state` (`configured` or `not-configured`), `runner_type` (`standard` or `labeled`),
`runner_label` (only together with `runner_type` `labeled`, which in turn needs it), `query_suite` (`default` or
`extended`), `threat_model` (`remote` or `remote_and_local`), and `languages` (one to nine of `actions`, `c-cpp`,
`csharp`, `go`, `java-kotlin`, `javascript-typescript`, `python`, `ruby`, `swift`, each once), at least one, and
sends only the given values in one `PATCH` that is never repeated. The result is `started`, which is true when
GitHub answered 202 by starting a configuration run, with that run's `run_id` and `run_url` (the address is untrusted
text and never fetched); a 200 answer, with or without a body, is `started` false. Qatlas never waits for the run.
A 403 or 404 names the permission, a 409 says a validation run with a different configuration is in progress,
and a 422 says the repository is not in the state the change needs; a 5xx or an unreadable answer says the change
may have been applied. Setting `state` to `not-configured` turns code scanning off and lowers the protection of the
repository.

The token rights are listed under [Security](setup.md#security). Sources:
https://docs.github.com/en/rest/code-scanning/code-scanning#get-a-code-scanning-default-setup-configuration and
https://docs.github.com/en/rest/code-scanning/code-scanning#update-a-code-scanning-default-setup-configuration.

## Code security configurations

Ten tools read, maintain, and assign the code security configurations of one organization: a named set of settings for
code scanning, Dependabot, secret protection, and the dependency graph that GitHub applies to repositories. Four only
read. Creating, updating, and deleting a configuration and assigning one are listed-only changes, offered only where a
connection's `tools` list names them. The enterprise configurations are not offered.

| Tool | Does |
| --- | --- |
| `github.code-security-configurations.list` | lists the configurations of an organization |
| `github.code-security-configurations.get` | reads one configuration by `configuration_id` |
| `github.code-security-defaults.get` | reads the configurations applied to new repositories |
| `github.code-security-configuration-repositories.list` | lists the repositories a configuration is attached to |
| `github.code-security-configurations.create` | creates a configuration (listed-only) |
| `github.code-security-configurations.update` | changes the given settings of a configuration (listed-only) |
| `github.code-security-configurations.delete` | deletes a configuration (listed-only) |
| `github.code-security-configurations.attach` | attaches a configuration to repositories (listed-only) |
| `github.code-security-configurations.detach` | detaches repositories from their configuration (listed-only) |
| `github.code-security-configurations.set-default` | sets a configuration as the default for new repositories (listed-only) |

The tools take the `owner` argument as `orgs/LOGIN`. The organization must be one the connection's targets name as an
owner (any organization on a connection without targets), and an exclusion of the owner refuses it; a user owner and a
connection whose targets name only repositories or projects are refused as `invalid-request`, before a credential is
resolved.

`.list` takes `target_type` (`global` for GitHub's own configurations, otherwise `all`). `.get` and
`github.code-security-configuration-repositories.list` need the `configuration_id` that `.list` reports. The
repository list takes `status`, a list of `attached`, `attaching`, `detached`, `removed`, `enforced`, `failed`,
`updating`, `removed_by_enterprise`, and `enterprise_enforced`; `removed` and `detached` cannot be requested together,
which GitHub refuses, and without `status` every status is listed. Both lists page by the `after` cursor GitHub
announces in its `Link` header; `next_cursor` carries it with the batch size of the first batch and belongs to the
tool, the organization, and the filters of that batch. The defaults are not paged.

A configuration carries every member GitHub documents: `id`, `name`, `target_type`, `description`, the state of each
setting (`enabled`, `disabled`, or `not_set`; `advanced_security` is `enabled`, `disabled`, `code_security`, or
`secret_protection`), the options of automatic dependency submission, code scanning, the default setup, and delegated
bypass, `enforcement`, the addresses, and the two times. A member GitHub leaves out or sends as `null` is absent from
the answer. The reviewers of delegated bypass are capped at 100 and `reviewers_truncated` says so. Names,
descriptions, and runner labels come from the organization and are untrusted data; the addresses are never fetched.

`github.code-security-defaults.get` answers a list of `default_for_new_repos` (`public`, `private_and_internal`, or
`all`) with the `configuration` applied to that kind of new repository; it is empty when no default is set.

`github.code-security-configuration-repositories.list` gives each repository with `status` and the members of
GitHub's minimal repository object. A repository outside the connection's targets, in an excluded repository, or
whose name cannot be checked is neither shown nor named: the batch counts it in `withheld`, which is absent when none
was withheld. A configuration is an object of the organization, so its settings are shown even when it is attached
only to repositories outside the targets.

GitHub answers 403 for a token without the right or for an account that is neither an organization owner nor a
security manager, and 404 for a missing configuration or one the token may not see; both are results with a reason
and never carry GitHub's own text. A gateway timeout is a timeout. Nothing is retried.

### Changing configurations

`.create`, `.update`, and `.delete` take the same `owner` as the reads and need a confirmation. Each sends exactly one
request, never repeated; an unclear outcome is reported as uncertain and is not retried. A name and a description are
untrusted data.

`.create` needs `name` and takes every setting GitHub documents for a new configuration: `description` (at most 255
characters), `advanced_security` (`enabled`, `disabled`, `code_security`, or `secret_protection`), `enforcement`
(`enforced` or `unenforced`), the state of each setting (`enabled`, `disabled`, or `not_set`: `code_security`,
`dependency_graph`, `dependency_graph_autosubmit_action`, `dependabot_alerts`, `dependabot_reachability`,
`dependabot_security_updates`, `dependabot_delegated_alert_dismissal`, `code_scanning_default_setup`,
`code_scanning_delegated_alert_dismissal`, `secret_protection`, `secret_scanning`, `secret_scanning_push_protection`,
`secret_scanning_delegated_bypass`, `secret_scanning_validity_checks`, `secret_scanning_non_provider_patterns`,
`secret_scanning_generic_secrets`, `secret_scanning_generic_secrets_push_protection`,
`secret_scanning_delegated_alert_dismissal`, `secret_scanning_extended_metadata`, and
`private_vulnerability_reporting`), and the four option objects: `dependency_graph_autosubmit_action_options`
(`labeled_runners`), `code_scanning_options` (`allow_advanced`), `code_scanning_default_setup_options`
(`runner_type` as `standard`, `labeled`, or `not_set`, and `runner_label`), and
`secret_scanning_delegated_bypass_options` (`reviewers`, at most 100, each with `reviewer_id`, `reviewer_type` as
`TEAM` or `ROLE`, and `mode` as `ALWAYS` or `EXEMPT`). A setting left out takes GitHub's default. Every enumeration is
checked against these values before a request is made, and a refused value never reaches GitHub. The new
configuration is attached to no repository and is no default. It answers like `.get`.

`.update` takes the `configuration_id` and the same settings, all optional but at least one required; only the given
settings change, and an empty `description` clears it. It answers `updated` and the `configuration` after the change;
when GitHub reports that nothing new was applied (204), `updated` is false and no configuration is returned.

`.delete` takes the `configuration_id` and answers `deleted`.

An update and a delete act on the configuration of the organization, not on a repository, and so on every repository
the configuration is attached to: an update changes the settings of all of them, a delete detaches it from all of them
(they keep their settings). They are allowed even when the configuration is attached to repositories outside the
connection's targets, because they change the organization's object, not a repository. The `configuration_id` is
addressed only below the organization in the path, so an id of another organization is not found. A configuration
owned by GitHub or by an enterprise cannot be changed, and an enterprise-enforced setting cannot be overridden: GitHub
answers 400, 403, or 409 (security configuration work already in progress) and 422 for an invalid or unavailable
setting, each a result with a reason and without GitHub's own text.

### Assigning configurations

`.attach`, `.detach`, and `.set-default` take the same `owner` as the reads and need a confirmation. Each sends exactly
one request, never repeated; an unclear outcome is reported as uncertain and is not retried.

`.attach` takes the `configuration_id` and a `scope`. With `selected`, `selected_repositories` is required: the
repositories to attach it to as `OWNER/REPO`, at most 50, of the organization and inside the connection's repository
targets. `.detach` takes only `selected_repositories` (at least one, at most 50) and detaches those repositories from
the configuration each is attached to. Qatlas never takes identifiers from an argument: it reads each repository,
sends the identifier GitHub reports, and refuses a repository GitHub reports under another name, all before the
write. A repository of another organization, outside the targets, or excluded is refused as `invalid-request` before a
credential is resolved.

Every `scope` but `selected` (`all`, `all_without_configurations`, `public`, `private_or_internal`) reaches repositories
the call does not name. It is therefore refused as `invalid-request`, without a request, whenever the connection's
targets name a single repository of the organization or exclude one; a connection that names the whole organization
or has no targets allows it, and `selected_repositories` must not be given with it.

`.set-default` takes the `configuration_id` and `default_for_new_repos` (`all`, `none`, `private_and_internal`, or
`public`) and answers the value GitHub reports and the `configuration`. A default applies only to repositories created
afterwards and attaches nothing to existing ones, so narrowed targets do not stop it.

GitHub processes an attachment or a detachment asynchronously: `accepted` says only that GitHub took the request, and
the status of each repository is read with `github.code-security-configuration-repositories.list`. Without GitHub
Advanced Security licenses an attachment enables only the free features. GitHub answers 409 while security
configuration work is already in progress for the organization or changes are blocked, 422 for a body it does not
accept, 403 for a token without the right, and 404 for a missing configuration; each is a result with a reason and
without GitHub's own text.

The token rights are listed under [Security](setup.md#security). Sources:
https://docs.github.com/en/rest/code-security/configurations.

## Private registries

Five tools read, create, change, and delete the private registries an organization hands to Dependabot, so it can
resolve packages from a private feed. The two reads only read; creating, changing, and deleting are listed-only
changes, offered only where a connection's `tools` list names them. The public key of the organization is not a tool
of its own: the two writes read it themselves.

| Tool | Does |
| --- | --- |
| `github.private-registries.list` | lists the private registries of an organization |
| `github.private-registries.get` | reads one private registry by `secret_name` |
| `github.private-registries.create` | creates a private registry from a local credential file (listed-only) |
| `github.private-registries.update` | changes a private registry by `secret_name` (listed-only) |
| `github.private-registries.delete` | deletes a private registry (listed-only) |

The tools take the `owner` argument as `orgs/LOGIN`. The organization must be one the connection's targets name as an
owner (any organization on a connection without targets), and an exclusion of the owner refuses it; a user owner and a
connection whose targets name only repositories or projects are refused as `invalid-request`, before a credential is
resolved.

`.get` and `.delete` take `secret_name`, the `name` that `.list` reports: letters, digits, and `_`, at most 255
characters, sent as one URL-encoded path segment. Any other name is refused as `invalid-request` without a request.
`.list` pages by number: `next_cursor` carries the page and the batch size of the first batch and belongs to the tool
and the organization.

A registry carries every member GitHub documents: `name`, `registry_type`, `auth_type` (`token`, `username_password`,
`anonymous`, or an `oidc_` kind), `url`, `username`, `replaces_base`, `visibility` (`all`, `private`, or `selected`),
`selected_repository_ids`, the settings of the OIDC kinds (`tenant_id`, `client_id`, `aws_region`, `account_id`,
`role_name`, `domain`, `domain_owner`, `jfrog_oidc_provider_name`, `audience`, `identity_mapping_name`, `namespace`,
`service_slug`, `api_host`, `workload_identity_provider`, and `service_account`), `created_at`, and `updated_at`; the
list adds `total_count`. GitHub documents `selected_repository_ids` for the detail read only; the list shows it when
GitHub sends it. A member GitHub leaves out or sends as `null` is absent. No tool answers a value or a key: the answer
has no member for the encrypted value or the key id, so one a server adds is dropped. Every text is untrusted data and
is cleaned and shortened. The `url` is answered as text and is never fetched or used as an address by Qatlas.

`selected_repository_ids` names the repositories that may use a registry whose visibility is `selected`. When an
exclusion of the connection names a repository of the organization, Qatlas reads each id with one repository read,
at most 100 per call, and lists only the repositories inside the targets. A repository that is excluded, that cannot be
read, or that lies beyond that bound is not listed but counted in `withheld`, which is absent when none was withheld.
Without such an exclusion nothing is read beyond the registries.

`.delete` takes `secret_name`, needs a confirmation, sends exactly one `DELETE`, never repeated, and answers `deleted`
and the `name`; an unclear outcome is reported as uncertain and is not retried. A registry belongs to the organization,
so a delete is allowed even when the registry is selected for repositories outside the connection's targets, and those
repositories lose it too.

GitHub answers 403 for a token without the right or an account that is not an organization owner, and 404 for a
missing registry; both are results with a reason and never carry GitHub's own text. A gateway timeout is a timeout.

### Creating and changing a registry

`.create` takes `registry_type` (one of the fifteen types GitHub documents, such as `npm_registry` or
`maven_repository`), `url`, `visibility`, and `local_path`, and optionally `auth_type` (`token`, the default, or
`username_password`), `username`, `replaces_base`, and, with `visibility` `selected`, `selected_repositories`.
`.update` takes `secret_name` and at least one of `registry_type`, `url`, `username`, `replaces_base`, `visibility`
with `selected_repositories`, and `local_path`; the authentication type cannot be changed. The other
authentication types (`anonymous` and the `oidc_` kinds) are not offered: they carry no local value and name further
addresses.

The credential is never an argument. `local_path` names a file the connection releases for reading under
`files.read`, absolute or starting with `~/`, of at most 48 KB. Qatlas reads its whole content unchanged, so a line
break at the end of the file is part of the value, seals it locally with the public key GitHub serves for the
organization (a sealed box), and sends only the sealed text with the key id. The value is never part of an answer, a
log, or a message, however a server answers; a tool without `files.read` on its connection is not offered. A
`.update` without `local_path` keeps the current credential and sends no key request.

The address is approved by the connection, not by the caller. A `url` is accepted only as an `https` address without
userinfo whose host, with its port when one is given, equals a `registry-hosts/HOST` entry of the connection's targets
without regard to case; there is no pattern, no subdomain, and no default port (see
[targets](README.md#targets)). Any other address is refused as `invalid-request`. A `.update` without `url` keeps
the existing address and needs no host approval.

Before a credential is resolved and before the file is opened, Qatlas checks in this order that `owner` is an
organization the targets name as an owner and no exclusion covers, the address, the names and the limits of the
other arguments, and `selected_repositories`: each is `OWNER/REPO` of that organization, inside the connection's
repository targets and not excluded, at most 50. `all` and `private` are refused when the targets or exclusions
narrow the repositories of the organization, since the registry would reach repositories outside them; `selected`
with repositories inside the targets is the way. Qatlas resolves the repositories to ids with one read each, then reads
the key. A refusal in this stage sends no request and reads no file.

A create sends one `POST` and an update one `PATCH`, each exactly once and never repeated. GitHub names a created
registry itself; the answer carries `state` (`created` or `updated`), the `name`, and what was sent besides the value
(`registry_type`, `visibility`, `selected_repositories`), never the address or the value. A success status other than
201 for a create and 204 for an update, and a created name that is not a secret name, are reported as uncertain. GitHub
answers 403 for a token without the right, 404 for a missing organization or registry, and 422 for a request it does
not accept, for example a key replaced since it was read; each is a result with a reason, without GitHub's own text.

The token rights are listed under [Security](setup.md#security). Sources:
https://docs.github.com/en/rest/private-registries/organization-configurations.
