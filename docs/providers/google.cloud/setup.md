---
description: >
  Guides a user through connecting Qatlas to Google Cloud: the credential that issues access tokens, the rights the token needs per tool, the targets that limit a connection, what the connection test proves, and what to do for each symptom.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Set up Google Cloud

This page shows how to connect Qatlas to Google Cloud. A Qatlas connection searches organizations, folders,
and projects and reads projects and the state of long-running operations from Cloud Resource Manager, and
changes nothing. Tools, targets, errors, and limits are described in the [Google Cloud provider](README.md).

## What you need

- A Google Cloud project in which the **Cloud Resource Manager API** is enabled. Google reports a disabled API
  as a refused request, and the connection test then fails with the class permission.
- Either an OAuth client of that project for a person's consent, or the key file of a service account. A
  connection that names a service account to impersonate needs the OAuth client and the consent of a person
  who may create tokens for that service account.
- The service is always `https://cloudresourcemanager.googleapis.com`; there is nothing to change for it.

## Rights of the token

The token needs the rights of its person or service account in Google Cloud. Qatlas asks only for the read
scope `cloud-platform.read-only`, so even a broad role cannot change anything through this provider. Grant
the account a viewer role at the highest level you want it to see, for example the Organization Viewer, Folder
Viewer, or Project Viewer role, or any role with these permissions:

| Tool | Permission Google checks |
| --- | --- |
| `organizations.search` | `resourcemanager.organizations.get` on each organization |
| `folders.search` | `resourcemanager.folders.get` on each folder |
| `projects.search`, `projects.get` | `resourcemanager.projects.get` on each project |
| `operations.get` | Google states none for reading an operation |

A search lists only what the account may see. Qatlas also reads the folders above a project to prove where it
sits, so an account that sees a project but not its folder cannot bind that project to an organization or
folder target. In that case bind the connection to the project itself.

## Targets

A connection may be limited to organizations (`organizations/ID`), folders (`folders/ID`), projects
(`projects/ID`, by project ID or number), and billing accounts (`billingAccounts/ID`). An organization takes in
its folders and projects, and a folder its subfolders and projects. A billing account belongs to none of them:
a connection reaches one only if it names it. Without targets a connection reaches what its credential
reaches, except billing accounts.

Which targets a tool needs:

- Searching projects and reading a project works without targets or with any organization, folder, or project.
  Searching folders needs no targets or an organization or folder.
- Searching organizations works only on a connection that names an organization. A connection without
  targets cannot search organizations, and the search returns only the organizations the targets name.
- Reading an operation works without targets or with an organization among them, because Qatlas cannot tie an
  operation to a folder or a project.

## What the connection test proves

The test obtains an access token and reads one project, which costs nothing. It then reads every
organization, folder, and project target to prove that it exists. It does not read billing accounts.

## Troubleshooting

| Symptom | Likely cause and fix |
| --- | --- |
| The test ends as permission and says the API is not enabled | Enable the Cloud Resource Manager API in the Cloud project of the OAuth client or service account, wait a minute, and test again. |
| The test ends as permission without that message | The account may not see any project. Grant it a viewer role on the organization, folder, or project. |
| The test ends as consent required or consent expired | Run `qconnect credential consent` for the credential again. |
| The test ends as auth | The client ID or key is wrong or was deleted; check the roles of the credential. |
| The test ends as not found for a target | The target does not exist or the account may not see it; check the ID. |
| A tool is missing from the list | The targets of the connection do not reach it; see Targets above. Organization search needs an organization target. |
| A search answers with an empty page and a cursor | Rows outside the targets are dropped. Follow the cursor, or narrow the search with `parent`. |
| A search is refused as needing too many folders | Narrow it with `parent` or a smaller `page_size`, or bind the connection to the folder you need. |
| A project is refused although it lies in the organization | The account cannot read a folder above the project. Bind the connection to the project instead. |
| An operation is refused | The connection has targets without an organization; name the organization or use a connection without targets. |
| A cursor is refused | A cursor continues only the search that issued it, with the same filters on the same connection. Start again without it. |
| Rate-limited | Wait and try again; Qatlas spaces its requests and never repeats a failed one. |
