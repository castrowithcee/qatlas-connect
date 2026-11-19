---
description: >
  Guides a user through connecting Qatlas to the Google Route Optimization API: the credential that issues access tokens, the rights, the enabled API, and the billing the token needs, the cost of an optimization, the project targets, what the connection test proves and what it does not, and what to do for each symptom.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-19
---

# Set up Google Route Optimization

This page shows how to connect Qatlas to the Google Route Optimization API. A Qatlas connection optimizes
vehicle tours, which Google bills, and reads the state of a Route Optimization operation. Tools, targets,
errors, and limits are described in the [Google Route Optimization provider](README.md) and its
[tools](tools.md).

## What you need

- A Google Cloud project with the **Route Optimization API** (`routeoptimization.googleapis.com`) enabled and
  billing enabled for the project. Google reports a disabled API as a refused request, and the connection test
  then fails with the class permission. Optimizations are billed to this project.
- Either an OAuth client of that project for a person's consent, or the key file of a service account. A
  connection that names a service account to impersonate needs the OAuth client and the consent of a person who
  may create tokens for that service account.
- The service is always `https://routeoptimization.googleapis.com`; there is nothing to change for it.

## Rights of the token

The account of the token needs this permission in the Cloud project:

| Tool | Permission Google checks |
| --- | --- |
| `operations.get` | `routeoptimization.operations.get` on the operation |
| `tours.optimize` | `routeoptimization.locations.use` on the project |

Qatlas asks for the scope `cloud-platform`, because it is the only scope Google lists for the Route Optimization
API. It is a broad scope: the token can do whatever its account may do in any Google API. Therefore give the
account only the roles it needs, and prefer a service account of its own over the consent of a person. The
profile `read` is the recommended one and holds `operations.get`. The profile `optimize` adds `tours.optimize`.

A connection saved earlier keeps the tools it lists and so the scope it asked for. If the tool ends as
`scope-missing`, run `qconnect credential consent` again for the credential.

## Cost

Every call of `tours.optimize` is billed to the project per shipment, as Single Vehicle Routing for a model with
one vehicle and as Fleet Routing for two or more. Google lists no free tier and allows 60 requests a minute.
Qatlas checks neither price nor quota and asks a person to confirm each call, naming the shipments, vehicles,
and SKU. Check the current prices and set a budget alert for the project in Google Cloud. A call that timed out
or ended unclear may have been billed, and Qatlas does not repeat it.

## Targets

A connection may be limited to projects (`projects/PROJECT`, by project ID or project number), several at once.
Without targets it reaches what its credential reaches. With targets, `tours.optimize` accepts only those
projects as `project`, and `operations.get` reads only operations whose name holds one of them. Both check that
before any secret is read. Qatlas does not turn a
project ID into a project number: if the target is `projects/my-project-123`, the name must hold
`my-project-123` too, and if it is the number, the name must hold the number.

## What the connection test proves

The test obtains an access token and checks that the consent holds the scope. For each project target it then
reads an operation that does not exist. An answer of "not found" passes, because the API is then enabled in the
project and the credential could ask. The test never sends an optimization and costs nothing. It does not prove
that the account holds `routeoptimization.locations.use`, because that is checked only when an optimization is
sent.

A connection without a project target proves only the token and its scope. It does not prove that the API is
enabled or that the account holds the right. Add a project target if you want the test to check them.

Google does not say whether it answers "not found" before it checks the right. If it does, a passed test shows
that the API is enabled but not that the right is held. Reading a real operation shows the right.

## Troubleshooting

| Symptom | Likely cause and fix |
| --- | --- |
| The test ends as permission and says the API is not enabled | Enable the Route Optimization API in the Cloud project of the OAuth client or service account, wait a minute, and test again. |
| The test ends as permission without that message | The account lacks `routeoptimization.operations.get` in the project, or billing is not enabled for it. Grant the right or enable billing. |
| The test ends as consent required or consent expired | Run `qconnect credential consent` for the credential again. |
| The test ends as scope missing | The stored consent lacks the scope `cloud-platform`. Run `qconnect credential consent` again. |
| The test ends as auth | The client ID or key is wrong or was deleted; check the roles of the credential. |
| The test ends as invalid provider response | Google answered in a form Qatlas does not know, for example a "not found" without a status. Try again later and report it if it stays. |
| `tours.optimize` ends as permission | The account lacks `routeoptimization.locations.use` in the project, billing is not enabled, or the API is not enabled. Grant the right or enable billing or the API. |
| `tours.optimize` ends as unclear and says it may have been billed | The request timed out, was aborted, or got a 5xx or unreadable answer. Check the billing of the project before you repeat the call. |
| `tours.optimize` is refused as invalid before any request | The model is too large, `timeout` is above 45 seconds, or an argument has the wrong form. The message names the argument. |
| A read is refused as outside the targets | The project in the name is not one of the targets, or it is spelled another way (ID instead of number). Use the spelling of the target. |
| A read is refused as an invalid name | The name must be `projects/PROJECT/locations/LOCATION/operations/ID`, as the operation returned it. |
| A read ends as not found | The operation does not exist, or the credential may not see it. Check the name. |
| A finished operation is reported as an error | The optimization itself failed. The code is in the message and Google's text in the original of the error. |
| Rate-limited | Wait and try again; Qatlas spaces its requests and never repeats a failed one. |
