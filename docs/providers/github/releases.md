---
description: >
  Describes the GitHub release tools: listing and reading the releases of a repository and the metadata of their assets, generating release notes, creating and updating a release, renaming or deleting an asset, and deleting a release, with their limits.
type: knowledge
edit: shared
created: 2026-10-22
updated: 2026-11-11
---

# GitHub releases

The release tools of the [GitHub provider](README.md). Every tool takes `repository` as described under
[the target of a call](README.md#the-target-of-a-call); a change needs the confirmation described under
[confirmed changes](README.md#confirmed-changes).

## Releases

The tools list, read, and change the releases of a repository and the metadata of their assets; asset
content and tags stay out of scope. The seven reads and changes are offered by the not-recommended setup
profile `releases`; deleting a release or an asset is in no profile, since each is offered only where a
connection's `tools` list names `github.releases.delete` or `github.release-assets.delete`. Every route lies below the chosen
repository; no tool accepts an owner or a free REST path.

| Tool | Does |
| --- | --- |
| `github.releases.list` | lists releases, in GitHub's own order |
| `github.releases.get` | reads one release by `id`, `tag`, or `latest` |
| `github.release-assets.list` | lists the asset metadata of one release |
| `github.release-notes.generate` | computes release notes for `tag` since the previous tag; changes nothing |
| `github.releases.create` | cuts one release from `tag` |
| `github.releases.update` | replaces fields of one release; left-out fields stay |
| `github.releases.delete` | deletes one release by `id`; listed only |
| `github.release-assets.update` | renames or relabels one asset by `asset_id` |
| `github.release-assets.delete` | deletes one asset by `asset_id`; listed only |

```yaml
connections:
  release-reader:
    service: github
    credential: github-reader
    target: repos/octo-org/example
    tools: [github.releases.list, github.releases.get, github.release-assets.list]
```

`github.releases.list` pages by GitHub's `Link` response header, like the Actions and pull request lists that
carry no total count. A release `name` is untrusted data. A draft appears only while the configured token can
see it. `github.releases.get` takes exactly one of `id`, `tag`, or `latest`; a lookup by `tag` never finds a
draft, because GitHub does not index a draft's tag, so a draft is only reachable by `id`.

A release answers GitHub's release in full, with its assets. Rendered variants repeat the text field in
another format and add no content, so `body_html` and `body_text` are left out. In a list, the release notes
are cut and marked, since a batch of full notes would exceed a useful answer; `github.releases.get` answers
them in full. A release answers at most 100 of its assets and marks a longer list; the rest is read with
`github.release-assets.list`. The `upload_url` is a template address, not a credential, and every address is
untrusted data that is never fetched.

`github.release-assets.list` reads only metadata, with the name and label untrusted data; an asset's content
is never read, and its `browser_download_url` is only an address.

### Changes

`github.releases.create` requires `tag`; when the tag does not exist yet, GitHub creates it at `target` (a
branch or a commit) once the release is published. A second create for a tag that already has a release is
refused as `invalid-request` naming the tag, without a second attempt, since GitHub holds at most one release
per tag. `github.releases.update` writes only the fields given; `draft: false` publishes a draft, and
`draft: true` converts a published release back to one. `make_latest` takes `"true"`, `"false"`, or
`"legacy"`, exactly as GitHub's REST field does.

`github.releases.delete` deletes one release by `id`, with its release notes and asset attachments; the tag
it was cut from stays. It is high risk, on the scale of the
[pull request merge](pull-requests.md#merge-and-conflicts): a connection offers it only while its `tools` list
names it, whatever its `effects`, and no profile a new connection starts with, recommended or not, may select
it. A release already deleted answers `not-found` naming it, like the other delete tools in this provider.

The token rights are listed under [Releases](setup.md#releases).

### Release notes

`github.release-notes.generate` asks GitHub to compute notes for `tag` from the merged pull requests and
commits since the previous tag. GitHub answers this calculation to a `POST` request, but nothing is created or
changed, so the tool is a read: it needs no confirmation and a failure never claims a possible change. It
requires `tag`, which may not exist yet; `target` names the branch or commit the tag would be created at and
is needed by GitHub only for a tag that does not exist. `previous_tag` fixes the starting point instead of
GitHub's choice, and `configuration_file_path` names a notes configuration file in the repository, a path
without `..`, a leading `/`, or control characters; without it GitHub reads `.github/release.yml`. The answer
holds the suggested `name` and `body`, both untrusted data. The body is cut at 4000 characters and marked with
`body_truncated`, so the result is a suggestion to read, not a text to store unchecked; pass it to
`github.releases.create` or `github.releases.update` as `body` after review.

### Assets

`github.release-assets.update` renames an asset with `name`, changes its `label`, or sets `state` to
`uploaded`; at least one is required, and an empty `label` clears it. The name is a file name of at most 255
characters without path parts or control characters. The tool never changes an asset's content. The asset is
read below the chosen repository first, and only then is one `PATCH` sent: an identifier GitHub does not find
there answers `not-found` naming the asset, and no change is sent. The answer has the members of
`github.release-assets.list`, with `name` and `label` untrusted data, but its `links` hold only the REST `url`.

`github.release-assets.delete` deletes one asset by `asset_id` after the same read. It is high risk: a
connection offers it only while its `tools` list names it, whatever its `effects`, and no profile selects it.
Both asset tools send exactly one request without a retry; after a timeout, a server error, or a dropped
connection the outcome is reported as possibly applied. A release that GitHub marks immutable refuses both
changes, and the tool reports that as a `permission` failure without GitHub's text.

#### Limits

- The update answer leaves out `browser_download_url`, because the address can grant access without sign-in
  and only a tool that a connection names explicitly may output such a value.
- Between the read that binds the asset and the change there is a short window in which the asset can change
  at GitHub; the tool does not close it.
- GitHub reports an immutable release through a refusal text, which the tool matches only to recognise it; a
  refusal GitHub words differently appears as the generic permission or rejection message.
- Asset content is neither read nor uploaded.
