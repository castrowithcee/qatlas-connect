---
description: >
  Describes the program tools of the Google Merchant provider: listing and reading the programs of an account with state, active regions, and unmet requirements, enabling and disabling a program, the program ID form, the account check, the fixed request, the role needed, confirmation and the tools list, delayed and public effect, result bounds, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Google Merchant: programs

The program tools of the [Google Merchant provider](README.md). The account argument, targets, quotas, cursors,
and errors are described there.

A program adds functionality to a Merchant Center account, such as free product listings or shopping ads. An
account is not eligible, eligible, or enabled for each program. The tools read that participation and enable or
disable it.

| Tool | Does |
| --- | --- |
| `programs.list` | reads the programs of an account, a batch per call with `cursor` |
| `programs.get` | reads one program |
| `programs.enable` | enables participation of the account in a program |
| `programs.disable` | disables participation of the account in a program |

`programs.list` and `programs.get` are in the `read` profile and need no confirmation. `programs.enable` and
`programs.disable` are in no profile: a connection offers them only when its `tools` list names them, and every call
needs a confirmation that shows the program and the account.

## Arguments

`account` is the numeric Merchant Center account ID. It is checked against the targets of the connection before
any secret is read; a sub-account of a bound advanced account is proven by one read of the sub-accounts first, and
no read or change follows an account that is not proven. A refusal does not name the account.

`program` is the program ID, one path segment. The Merchant API documents these IDs: `checkout`, `free-listings`,
`loyalty`, `product-ratings`, `shopping-ads`, `ucp-integration` (limited access), `youtube-affiliate`, and
`youtube-shopping-checkout`. Qatlas does not fix the list, because Google may add programs, but it accepts only one
lowercase letter followed by up to 39 lowercase letters, digits, and dashes. A dot, a slash, an underscore, an
uppercase letter, or an escape is refused before any secret is read or request is sent. Whether the ID names a
program is Google's answer (`not-found`).

`programs.list` also takes `page_size` (1 to 1000) and `cursor`. Google sends 1000 programs when no page size is sent;
Qatlas sends 100 when `page_size` is left out. A cursor is tied to the tool, the account, the page size, and the
targets.

## Result

A program shows `name` (`accounts/ACCOUNT/programs/PROGRAM`), `state` (`NOT_ELIGIBLE`, `ELIGIBLE`, or `ENABLED`),
`activeRegionCodes` (CLDR region codes where all requirements are met; `001` is the whole world), `unmetRequirements`,
and `documentationUri`. Each unmet requirement shows `title`, `affectedRegionCodes`, and `documentationUri`. All of
these are output only. A requirement that is not met keeps the program from being active in its affected regions, so
`unmetRequirements` is the list to read before enabling a program.

Titles and addresses come from Google and are untrusted: titles are cut at 300 characters, addresses at 2048, codes
at 100, `activeRegionCodes` and the lists inside a requirement hold at most 100 entries, a program holds at most 100
requirements, and a batch at most 1000 programs. Addresses are text that is never fetched, and a member that the
discovery document does not list is dropped.

## Enabling and disabling

Both tools send one `POST` with the documented empty request body; nothing of the request comes from free input. The
result is `accepted: true` and the program as Google returned it. It says only that Google accepted the request:
Google processes the change afterwards, so read the program again to see the state.

- **Public effect.** Enabling or disabling a program changes what Google can show in Google Shopping and other
  surfaces for the account. There is no dry run. A test account is the safe way to try it.
- **Role.** Changing participation needs the role Admin in Merchant Center. According to the Merchant Center help,
  the role Standard cannot enable programs. Google answers a missing role with `403`, class `permission`; Qatlas
  cannot check the role beforehand.
- **No repetition.** A change is sent once. A rate limit (`429`) holds the next request of the credential and is not
  repeated. After a transport failure, a timeout, a `5xx`, or an unreadable answer the message says that the outcome
  is unknown; read the program before changing it again.
- **Idempotency.** Enabling an enabled program or disabling a disabled one has the same effect as one call, but each
  call counts against the quota of the method group, also when it fails.

## Boundary

Checked against the discovery document of the accounts API v1 (revision 20261118): `programs.list`, `get`, `enable`,
and `disable` take only the account and program as the path and, for the list, `pageSize` and `pageToken`.
`programs.list` returns 1000 programs by default and at most 1000 (values above are lowered to 1000);
Qatlas asks for 100 by default. The resource name pattern is `accounts/{account}/programs/{program}`
with no documented form for the program ID beyond the list above.

Not verified:

- No live call was made. Answers are taken from the discovery document and the guides of the Merchant API.
- Which roles Google accepts for each method. The Merchant Center help names Standard as unable to enable programs; the
  discovery document names no role for these methods.
- What programs an account may enable: for programs such as `checkout` or `loyalty`, Google may require a setup in
  Merchant Center first, and `ucp-integration` has limited access. Qatlas does not check this; Google refuses.
- How long Google needs until a change shows in `state`.
