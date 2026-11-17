---
description: >
  Describes the Google Cloud IAM binding tools iam-bindings.add-member, remove-member, and change-role: the read-modify-write with etag, the exact match of role and condition, which members may be granted, idempotence, conflicts and unknown outcomes, the answer, targets, scope, profile, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud IAM bindings

Three tools change the IAM policy of a project, folder, or organization, one binding at a time, through
`getIamPolicy` and `setIamPolicy` of Cloud Resource Manager v3 (discovery revision 20261021). A free
`setIamPolicy` is no tool: no argument takes a policy, an etag, a path, or a header. Every call has the effect
`update` and needs the confirmation of a person, whose text names the resource, the role, the member, and the
change. [The provider page](README.md) holds errors and rate spacing, [IAM](iam.md) the resource argument.

| Tool | Change | Arguments |
| --- | --- | --- |
| `iam-bindings.add-member` | adds the member to the binding of `role` and `condition`, which is created if missing | `resource_type`, `resource_id`, `role`, `member`, `condition` |
| `iam-bindings.remove-member` | removes the member from that binding; a binding left empty is dropped | as above |
| `iam-bindings.change-role` | moves the member from the binding of `from_role` to that of `to_role`, both with `condition`, in one write | `resource_type`, `resource_id`, `from_role`, `to_role`, `member`, `condition` |

`resource_type` is `project`, `folder`, or `organization`; a service account policy is not changed, because a
binding there grants the use of the account's identity. `role` is `roles/ID`, `projects/PROJECT/roles/ID`, or
`organizations/ORGANIZATION/roles/ID`. `member` is the principal exactly as the policy writes it, at most 1024
characters without spaces or control characters. `condition` is `{title, expression, description}` (title up to
256, expression up to 12800, description up to 1024 characters, Qatlas bounds); without it the tool names the
binding that has no condition.

## Course

1. The arguments are checked, and the resource against the targets, before a secret is read, as for
   `iam-policies.get`.
2. The policy is read with `requestedPolicyVersion` 3, so conditions are shown. A policy without `etag` is an
   invalid response and is not written.
3. The binding is the one whose role and condition equal the arguments exactly: expression, title, and
   description as text, and a binding with a condition `location` never matches. Two such bindings are an
   invalid response.
4. For a grant, the member is checked against the policy just read (below).
5. The whole policy goes back with `setIamPolicy` and the `etag` that was read, as one request. Only the changed
   binding is rewritten; every other binding, `auditConfigs`, `etag`, and every other member of the policy go
   back byte for byte. No `updateMask` is sent, so Google applies its default mask `bindings, etag`. When a
   binding with a condition remains, `version` becomes 3; otherwise it stays as read.

Adding a member the binding already holds, removing one it does not hold, and moving a member that is already
only in `to_role` succeed without a write, with `changed` false. `change-role` refuses a member that is in
neither binding.

## Which members may be granted

`add-member` and the target binding of `change-role` grant a member only if:

- exactly this member text, type prefix and value, already appears in a binding of the policy of the resource,
  or
- the resource is a project and the member is an existing service account of that project:
  `serviceAccount:NAME@PROJECT_ID.iam.gserviceaccount.com`, the Compute Engine default account
  (`PROJECT_NUMBER-compute` in the domain `developer.gserviceaccount.com`), or the App Engine default account
  (`PROJECT_ID` in the domain `appspot.gserviceaccount.com`). Qatlas reads the project for its ID and number and
  then the account with `projects.serviceAccounts.get` in that project; the answer must name the same email and
  project.

The comparison is exact text: Qatlas changes no case, spaces, prefixes, or suffixes, so
`User:a@example.com` is not `user:a@example.com`, and `deleted:user:a@example.com?uid=1` admits neither the
address nor itself. Service accounts are matched in Google's lowercase form only. On a folder or an organization
only the first rule applies.

Never granted, even when the policy already holds them: `allUsers`, `allAuthenticatedUsers`, and `deleted:`
members, in any case. New `user:`, `group:`, `domain:`, `principal:`, and `principalSet:` identities are refused
by the rule above. `remove-member` takes any member, these included, because revoking is always allowed. A
refusal never quotes the member.

## Conflicts and unknown outcomes

If the policy changed between read and write, Google answers 409 `ABORTED`; the call ends as `provider-error`
with a message that starts with `conflict`, writes nothing, and is not repeated. The caller reads again. A write
is never sent twice: a 401 only drops the cached token. After a transport failure that may have reached Google,
a 5xx, or an answer that cannot be read, the message says that the outcome is unknown. A successful answer that
does not show the change is an invalid response with the same note.

## Answer

`resource`, `changed`, `binding` (the binding of `role` or `to_role` and the condition after the call: role,
members, condition; members is empty when it no longer exists), `from_binding` for `change-role`, and the new
`etag` and `version`. The whole policy is not returned. Members are untrusted third-party text and cut like those
of `iam-policies.get`.

## Targets, scope, and profile

The targets apply as for `iam-policies.get`: an organization only on a connection that names exactly it, a
project or folder through the parent chain where the targets do not settle it. cloudresourcemanager v3 lists
only `cloud-platform` for `setIamPolicy`. The tools belong to the profile `iam-bindings` only, which is not
recommended; the connection needs the effect `update`. The rights are listed in [setup](setup.md).

## Boundary

Not verified:

- The 409 `ABORTED` answer to a stale `etag` and the default update mask are as Google documents them; neither
  was seen live.
- Whether a `user:` principal belongs to the own organization is not provable through the API, so Qatlas does
  not claim it; the rule only admits identities the policy already holds.
- The service account read and the write are two requests: an account deleted in between is still granted. The
  `etag` covers only the policy.

Left out on purpose:

- A block on far-reaching roles; the confirmation of a grant of `roles/owner` says that it gives full control.
- Policies of service accounts and every other resource, and audit configs.
