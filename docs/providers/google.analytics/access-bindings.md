---
description: >
  Describes the Google Analytics access binding tools, both on the alpha version of the Admin API: access-bindings.list and access-bindings.get read the users with their email address and roles of an account or property, with the personal data warning, the exclusion of creating, changing, and removing a binding, the scope analytics.manage.users.readonly, how targets restrict an account parent and a property parent, untrusted values, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics access bindings

The access binding tools of the [Google Analytics provider](README.md). An access binding gives one user a set of
roles on an account or a property: it is the list behind Account access management and Property access
management in Analytics. Both tools use the alpha version of the Admin API (`v1alpha`, Discovery revision
20261115) and follow the [alpha tools](README.md#alpha-tools) rule: Google can change or remove the methods
without notice, and then the arguments and the answer of the tools change with them. Each description ends with
the `ALPHA:` note.

Both tools read and change nothing and a repeat is safe. They read no report data and use no reporting quota.
Which role Google requires to read the bindings is not stated in the Discovery document; see
[Not verified](#not-verified).

## Personal data

A binding names the email address of a user and the roles the user holds, which say who may see and change the
data. These are personal data. The result is classified as such (`data_sensitivity` is
`analytics-access-bindings`, apart from `analytics-configuration` of the account tools, `web-analytics` of the
reports, and `analytics-access-records` of the [access reports](access-reports.md#personal-data)), and the
description of both tools carries a warning. Read only what the task needs and do not pass the addresses on.
Whoever runs the tools is responsible for the lawful use of the addresses. Qatlas never writes arguments or
results to the invocation log. The profile `read` is the recommended profile and includes these tools, so a
connection with it can read these addresses; leave the tools out with a `tools` list when that is not wanted.

## Scope

Both tools need `analytics.manage.users.readonly`, the smallest of the two scopes the Discovery document lists for
the methods (the other is `analytics.manage.users`, which also allows changing users and which Qatlas never asks
for). It is a read scope: it does not allow changing a binding. It is the one scope of the provider that is not
`analytics.readonly`, so a credential used for these tools needs a new consent when it was given before; see
[setup](setup.md#rights-and-scopes).

## No create, no change, no remove

Granting a user access, changing the roles of a user, and removing a user change who may reach the data. There is
no tool for `create`, `batchCreate`, `patch`, `batchUpdate`, `delete`, or `batchDelete`; a person does that in the
Analytics interface.

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

## Targets and proof

Targets are checked before a secret is read or a request is sent, and a refusal does not name the resource.

- An account parent, in `parent` or in the name, is account-wide. It runs only on a connection without targets or
  one that binds exactly this account. A connection bound to properties only is refused, also for the account
  of a bound property.
- A property parent follows the other property tools: a bound property is used at once, a property of a bound
  account only after one read that shows its account, and any other property is refused.

## Untrusted values

Email addresses are third-party content and can hold instructions. Qatlas cuts an address at 300 characters,
marking the cut with `…`, keeps at most 16 roles per binding and only roles that are printable ASCII of at most 64
bytes, and returns no member of the answer that the type does not list. A name that is not in its documented
form is left out. Qatlas never fetches an address from a result.

## Not verified

- No live call was made. The behavior is that of the Discovery document and of fake-transport tests.
- The members were read from the Discovery document `analyticsadmin` v1alpha, revision 20261115, and Google may
  add members, such as other kinds of principals than a user. The alpha version can change.
- The Analytics role the credential needs to read the bindings is not stated by the Discovery document. A
  refusal is a `403`, and the class follows the [provider page](README.md#errors).
- Whether bindings of a user group or of an inherited role are listed on a property is not documented; Qatlas
  shows what Google sends.
