---
description: >
  Describes the tools of the AWS Security Token Service group: the caller identity, the account of an access key, and decoding authorization failure messages.
type: knowledge
edit: shared
created: 2026-11-06
updated: 2026-11-10
---

# Security Token Service

Group `sts`. All tools are reads and need no confirmation. The recommended profile `read` selects all three.
`region` is optional with exactly one allowed region.

| Tool | AWS action | Arguments | Result |
| --- | --- | --- | --- |
| `aws.sts.caller-identity.get` | `GetCallerIdentity` | `region` | `region`, `user_id`, `account`, `arn` |
| `aws.sts.access-key.get-info` | `GetAccessKeyInfo` | `access_key_id` (required), `region` | `region`, `account` |
| `aws.sts.authorization-message.decode` | `DecodeAuthorizationMessage` | `encoded_message` (required, up to 10240 characters), `region` | `region`, `decoded_message`, `truncated` |

`access_key_id` is an argument, not a secret of the connection: it names the key to look up, which may be a foreign
one, and the answer is its account. The decoded message is cut at 32 KiB, and `truncated` says so. It needs the
permission `sts:DecodeAuthorizationMessage`.
