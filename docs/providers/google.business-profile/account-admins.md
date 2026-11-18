---
description: >
  Describes the account admin tools of the Google Business Profile provider: listing the admins of an account with their role and pending invitations, changing the role of an existing admin with the previous name and role as required comparison arguments, the Google methods behind them, the roles that are allowed, what is refused, the personal data, and how a target narrows them.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: account admins

The account admin tools of the [Google Business Profile provider](README.md). Credential, target, approval,
quotas, bounds, and errors are described there. A connection bound to a location does not offer them.

An admin is a person (or a location group) who administers an account. The tools only read admins and change
the role of an admin that already exists. No tool invites anyone or creates an admin, see
[Left out on purpose](README.md#boundary).

## google.business-profile.account-admins.list

`GET https://mybusinessaccountmanagement.googleapis.com/v1/accounts/{account}/admins`. Lists the admins of one
account. Google offers no paging here; Qatlas shows at most 250 entries. `account` is optional on a connection
bound to an account, and then it must be that account.

Each entry carries every documented member: `name` (`accounts/ACCOUNT/admins/ADMIN`), `admin`, `account` (for an
admin that is a location group), `pendingInvitation`, and `role` (`PRIMARY_OWNER`, `OWNER`, `MANAGER`, or
`SITE_MANAGER`).

**Personal data.** `admin` is the first and last name of the person. While an invitation is pending, Google
returns the email address of the invitee instead. The tool is part of the profile `read` and the result is
untrusted third-party content, so a connection that should not show other people's names and addresses leaves it
out with a `tools` list.

## google.business-profile.account-admins.update

`PATCH https://mybusinessaccountmanagement.googleapis.com/v1/accounts/{account}/admins/{admin}?updateMask=role`.
Changes the role of an existing admin. It needs confirmation and is part of the profile `write`.

| Argument | Meaning |
| --- | --- |
| `admin` | required, `accounts/ACCOUNT/admins/ADMIN` of an admin as `account-admins.list` shows it |
| `role` | required, the new role: `OWNER`, `MANAGER`, or `SITE_MANAGER` |
| `previous_admin` | required, the `admin` member of the entry as last read; only compared, never sent |
| `previous_role` | required, the role as last read; only compared, never sent |
| `update_mask` | optional, only `role`, which is also the default |

The body sent to Google holds only `role`. The tool has no argument for an email address, so it cannot invite or
create anyone. Before it sends the change, it reads the admin list of the account and refuses, without a change,
when

- the admin is not in the list (within the first 250 entries),
- the invitation of the admin is still pending,
- the admin holds `PRIMARY_OWNER`, or
- `previous_admin` or `previous_role` is not exactly what the list holds now.

The confirmation shows the path and name of the admin and the old and the new role. Refusals do not repeat a name.

**Roles.** `PRIMARY_OWNER` is not a target role: it transfers the ownership of the account. For the same
reason an admin who holds it is never changed. A higher role gives the admin rights over the locations and over
the other admins.

**Target.** An account target accepts only admins below that account. The path is checked against its form (the
admin ID is up to 64 letters, digits, `_`, or `-`) and against the target before a secret is read or a request
is sent.

**Spacing.** Google allows 10 changes per minute and profile. Qatlas spaces the changes of one account by 6
seconds, counted per account for this tool and for [`accounts.update`](accounts.md),
since an account has no location to count against. The time between the read of the list and the change is a
limit of the comparison. A change whose outcome is unclear (timeout, `5xx`, unreadable answer) is reported as
such and never repeated.
