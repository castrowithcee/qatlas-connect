---
description: >
  Describes the environment tools of the Google Tag Manager provider: listing, reading, creating, replacing, and deleting the custom environments of a container, the fingerprint, the confirmation that shows the preview URL, why the authorization code is never returned, why there is no reauthorize tool, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Tag Manager: environments

The environment tools of the [Google Tag Manager provider](README.md). The fingerprint, cursor, and target rules
shared with the other groups are described there.

An environment of a container decides where a preview and the debug panel run. Its `url` is the page a preview
opens, and `enableDebug` turns the debug panel on there. An environment belongs to the container, not to a
workspace. Nothing here publishes: a version goes live only through a container version, which no tool of this
provider makes.

| Tool | Does |
| --- | --- |
| `environments.list` | reads the environments of a container, a batch per call with `cursor` |
| `environments.get` | reads one environment by its path, the fingerprint included |
| `environments.create` | creates a custom environment; a repeated call creates a second one |
| `environments.update` | replaces a custom environment as a whole; needs `fingerprint` |
| `environments.delete` | deletes a custom environment |

`container` and `environment` are paths such as `accounts/1/containers/2/environments/4`; both are checked
against the target of the connection. `environments.list` and `environments.get` are in the `read` and `write`
profiles, the three changing tools in `write`. Deleting one environment is in `write` like deleting a tag: it
affects a single object and publishes nothing. Every change is confirmed, and the confirmation shows the name,
the container or environment path, the preview URL, and whether debug is on, never the description.

## Members

The arguments of `create` and `update` are `name` (needed), `description`, `url`, `enableDebug`, `type`, and
`containerVersionId`. `url` must be an `http` or `https` URL. `type` accepts only `user`, and `user` is sent
when it is left out, because Live, Latest, and the workspace previews belong to Google. The other members of an
environment are results only and are never sent.

Names, descriptions, and URLs are untrusted content of whoever edits the account: they are data, are never
followed or fetched, and are returned uncut, so what a read returns is what an update takes. An answer above
4 MiB is `invalid-provider-response` instead of a shorter text.

`environments.update` replaces the environment as a whole: a `description`, `url`, `enableDebug`, or
`containerVersionId` that is left out is removed, so send every member as `environments.get` returned it, with the
changes made.

```json
{"environment": "accounts/1/containers/2/environments/4", "fingerprint": "1700000000000",
 "name": "Renamed environment", "url": "https://staging.example.com/", "enableDebug": true}
```

## Authorization code and reauthorize

Google gives each environment an `authorizationCode`, the key that lets a preview link open it. It is never
returned by any tool, and its value is added to the redactor, so a diagnostic that quotes it is covered. The
result keeps `authorizationTimestamp`, which tells when the code was last issued.

There is no tool for Google's `reauthorize` method. It issues a new authorization code, which would be the output
of a key, and no tool of this provider returns one.

## Deleting

`environments.delete` answers with `environment` and `deleted: true`. Google does not delete the system
environments Live, Latest, and the workspace previews. The tool does not read the environment first, so such a
call reaches Google and ends with the error Google answers, mapped like every other error of the provider and
without Google's text.

Not verified: the answer to a stale fingerprint, the exact answer to deleting a system environment, and whether
Google accepts an update that omits `type`, which is why `user` is always sent.
