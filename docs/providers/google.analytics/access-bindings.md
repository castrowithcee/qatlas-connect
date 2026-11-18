---
description: >
  Describes the Google Analytics access binding tools, all on the alpha version of the Admin API: access-bindings.list, access-bindings.get and access-bindings.batch-get read the users with their email address and roles of an account or property, access-bindings.update replaces the roles of an existing binding without a user argument, access-bindings.delete and access-bindings.batch-delete remove bindings, with the personal data warning, the exclusion of creating a binding and changing its user, the scopes and profiles, the confirmation and the lockout warning, how targets restrict an account parent and a property parent, untrusted values, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Analytics access bindings

The access binding tools of the [Google Analytics provider](README.md). An access binding gives one user a set of
roles on an account or a property: it is the list behind Account access management and Property access
management in Analytics. All tools use the alpha version of the Admin API (`v1alpha`, Discovery revision
20261115) and follow the [alpha tools](README.md#alpha-tools) rule: Google can change or remove the methods
without notice, and then the arguments and the answer of the tools change with them. Each description ends with
the `ALPHA:` note.

The three reading tools change nothing and a repeat is safe. `access-bindings.update`, `access-bindings.delete`,
and `access-bindings.batch-delete` change who may see and change the data, at once, and each call needs the
confirmation of a person. No tool reads report data or uses reporting quota. Which Analytics role Google requires
for reading or changing the bindings is not stated in the Discovery document; see [Not verified](#not-verified).

## Personal data

A binding names the email address of a user and the roles the user holds, which say who may see and change the
data. These are personal data. The results are classified as such (`data_sensitivity` is
`analytics-access-bindings`, apart from `analytics-configuration` of the account tools, `web-analytics` of the
reports, and `analytics-access-records` of the [access reports](access-reports.md#personal-data)), and the
description of every tool carries a warning. Read only what the task needs and do not pass the addresses on.
Whoever runs the tools is responsible for the lawful use of the addresses. Qatlas never writes arguments or
results to the invocation log. The profile `read` is the recommended profile and includes the three reading tools,
so a connection with it can read these addresses; leave the tools out with a `tools` list when that is not wanted.

## Scopes and profiles

The reading tools need `analytics.manage.users.readonly`, the smaller of the two scopes the Discovery document
lists for them. It does not allow changing a binding. It is the one read scope of the provider that is not
`analytics.readonly`, so a credential used for these tools needs a new consent when it was given before; see
[setup](setup.md#rights-and-scopes).

The changing tools need `analytics.manage.users`, the only scope Google accepts for them. It also allows adding
users, which no Qatlas tool does. So `access-bindings.update` and `access-bindings.delete` are in neither `read`
nor `write` but in their own profile `access-bindings`, which is `read` plus these two tools and is not
recommended. `access-bindings.batch-delete` is in no profile, because it removes up to 1000 users at once: a
connection offers it only when its `tools` list names it.

## No new access

Granting a user access would open a new way into the data. There is no tool for `create` or `batchCreate`, and
`access-bindings.update` has no argument for the user and sends the roles alone, so it cannot move a binding to
another user. `batchUpdate` is not a tool either, because each of its entries could change the user. A person
adds users in the Analytics interface.

## Result members

A binding has `name` (`accounts/ID/accessBindings/ID` or `properties/ID/accessBindings/ID`), `user` (the email
address), and `roles`. The documented roles are `predefinedRoles/viewer`, `predefinedRoles/analyst`,
`predefinedRoles/editor`, `predefinedRoles/admin`, `predefinedRoles/no-cost-data`, and
`predefinedRoles/no-revenue-data`; a binding can hold several. A member Google does not send is left out.

## google.analytics.access-bindings.list

Reads `accounts.accessBindings.list` for an account and `properties.accessBindings.list` for a property.
`parent` (`accounts/ID` or `properties/ID`) is required. `page_size` (1 to 500; Google returns at most 200
without it) and `cursor` work as in the provider page; the cursor fits only the same connection and parent. A
binding in the answer that does not carry a valid name of the asked parent is left out. An empty answer is an
empty list.

## google.analytics.access-bindings.get

Reads `accounts.accessBindings.get` or `properties.accessBindings.get`, chosen by the name: one binding with all
members above. `name` is required. An answer for another binding than the one asked for is refused. Google answers
a binding that is missing like one the credential cannot see; both are `not-found`.

## google.analytics.access-bindings.batch-get

Reads `accounts.accessBindings.batchGet` or `properties.accessBindings.batchGet` in one request: `parent` and
`names`, 1 to 1000 binding names of that parent without repeats. A name of another parent is refused before any
request. Only bindings that were asked for are returned, each once. Google sends the names in the query, so a long
list can exceed the length Google accepts for an address; Google then refuses the call.

## google.analytics.access-bindings.update

Replaces the roles of the user of one existing binding with `accounts.accessBindings.patch` or
`properties.accessBindings.patch`. `name` and `roles` (1 to 6 of the documented roles, without repeats) are
required. An empty role list is refused before any request, because Google deletes a binding whose roles are
empty. The request body holds `roles` alone.

`previous_user` and `previous_roles` are also required: the binding as last read, for example with
`access-bindings.get`. The confirmation shows the user, the roles before and after, which roles are added and
removed, and a note when `admin` is added, since that role lets the user manage users and every setting. Qatlas
does not treat adding `admin` more strictly than other roles; the confirmation of a person decides. Before the
change the tool reads the binding once and changes nothing when the address (compared without regard to case) or
the roles (in any order) differ. The previous values are never written. The answer is the binding as Google
returns it; an answer for another binding leaves the outcome unknown.

## google.analytics.access-bindings.delete

Removes one binding with `accounts.accessBindings.delete` or `properties.accessBindings.delete`. `name` is
required. The user loses every role the binding gives at once. The result is the `name` and `deleted: true`.

## google.analytics.access-bindings.batch-delete

Removes up to 1000 bindings of one parent with `accounts.accessBindings.batchDelete` or
`properties.accessBindings.batchDelete`: `parent` and `names` as in `batch-get`. The confirmation starts with the
number of bindings and lists the names. Google answers a success with an empty body, and the result is `parent`,
the `names`, and `deleted: true`. Google documents no partial result: when it refuses the call, the error says
that some bindings may have been removed, and `batch-get` shows which are left. A rate limit is the exception,
because Google did not act.

## Lockout

Removing the binding of the user or service account behind the credential, or taking its admin role, can lock the
connection out of the account or property. Qatlas cannot tell whether a binding is its own, because no answer of
Google names the user of the token. Each changing tool therefore warns in its description and in its
confirmation.

## Targets and proof

Targets are checked before a secret is read or a request is sent, and a refusal does not name the resource.

- An account parent, in `parent` or in a name, is account-wide. It runs only on a connection without targets or
  one that binds exactly this account. A connection bound to properties only is refused, also for the account
  of a bound property.
- A property parent follows the other property tools: a bound property is used at once, a property of a bound
  account only after one read that shows its account, and any other property is refused.
- In the batch tools every name must belong to `parent`.

## Untrusted values

Email addresses are third-party content and can hold instructions. Qatlas cuts an address at 300 characters,
marking the cut with `…`, keeps at most 16 roles per binding and only roles that are printable ASCII of at most 64
bytes, and returns no member of the answer that the type does not list. A name that is not in its documented
form is left out. Qatlas never fetches an address from a result.

## Not verified

- No live call was made. The behavior is that of the Discovery document and of fake-transport tests.
- The members were read from the Discovery document `analyticsadmin` v1alpha, revision 20261115, and Google may
  add members, such as other kinds of principals than a user. The alpha version can change.
- `patch` has no update mask in the Discovery document, and its body documents `user` as optional ("if set").
  That Google keeps the user when the body holds only `roles` is taken from this, not seen live.
- Whether `batchDelete` can remove some bindings before it fails is not documented.
- The Analytics role the credential needs to read or change the bindings is not stated by the Discovery
  document. A refusal is a `403`, and the class follows the [provider page](README.md#errors).
- Whether bindings of a user group or of an inherited role are listed on a property is not documented; Qatlas
  shows what Google sends.
