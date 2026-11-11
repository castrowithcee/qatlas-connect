---
description: >
  Describes the GitHub security tools: the code scanning, Dependabot, and secret scanning alerts of a repository, the global, repository, and organization security advisories, code quality findings, the security switches of a repository, and its code scanning default setup, with their targets and cuts.
type: knowledge
edit: shared
created: 2026-10-22
updated: 2026-11-11
---

# GitHub security

The security tools of the [GitHub provider](README.md). The advisory and finding tools only read, and so do the
alert reads. Three tools change an alert's state, and [the security switches](#security-switches) and
[the code scanning default setup](#code-scanning-default-setup) read and, listed-only, change; each change is
offered only where a connection's `tools` list names it. The not-recommended setup profile `security` selects the
six alert tools, the four reads of instances, analyses, and locations, the five advisory and finding tools, and the
two reads of the switches and the default setup, seventeen in all and all reading; it holds none of the five
changes. The recommended profile `read` is unchanged.

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

Five more tools read advisories and code quality findings; they change nothing, so an advisory is neither
created, edited, published, nor closed.

| Tool | Does |
| --- | --- |
| `github.global-advisories.list` | lists global security advisories |
| `github.global-advisories.get` | reads one global security advisory |
| `github.repository-advisories.list` | lists the security advisories of a repository |
| `github.organization-advisories.list` | lists the security advisories of an organization's repositories |
| `github.code-quality-findings.get` | reads one code quality finding |

`github.global-advisories.list` reads the GitHub Advisory Database, GitHub-wide, and gives each entry every
member `.get` does; GitHub answers the type `reviewed` when `type` is omitted, and `is_withdrawn` false
adds no filter. `github.code-quality-findings.get` reads one finding by `finding_number`; GitHub also lists
findings, and that list is not offered.

The global tools name no repository: a connection without targets, or one whose targets name only owners, may
use them, and a connection whose targets name a repository or a project is refused as `invalid-request`
before a credential is resolved. The organization list takes the optional `owner` argument as `orgs/LOGIN`,
which must be an organization the connection's targets name as an owner (or any organization on a connection
without targets); a user owner and a repository or project target alone are refused the same way. The
repository list and the finding read take the optional `repository` argument like the alert tools.

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

The token rights are listed under [Security](setup.md#security). Sources:
https://docs.github.com/en/rest/security-advisories/global-advisories,
https://docs.github.com/en/rest/security-advisories/repository-advisories, and
https://docs.github.com/en/rest/code-quality/code-quality.

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
