---
description: >
  Describes the AWS provider as a whole: the optional role assumption, the account and region targets and the account check, global services, endpoints, how Qatlas shows up in CloudTrail, error shapes, limits, and how the tools are generated from pinned models and curation tables; setup and each tool group are on their own pages.
type: knowledge
edit: shared
created: 2026-11-06
updated: 2026-11-12
---

# Amazon Web Services

The `aws` provider gives agents controlled access to one AWS account, read-only except for the IAM role tools and the two
IAM report generators a connection names in its tools list, the SES identity tools, the S3 bucket tools, and the Route 53
hosted zone, record, and health check tools, which change state with confirmation, the Route 53 query logging writes, and the Route 53 DNSSEC writes a connection names in its tools list. Qatlas signs requests with
Signature Version 4 using the Go standard library; it uses no AWS SDK. It ships the foundation (credential, role
assumption, account and region binding, connection test, endpoint table, error mapping), the Security Token Service
(STS) tools, the IAM tools that read identities, the metadata of their credentials, policies with their simulation,
the account state, identity providers, and server certificates, the IAM report tools (credential report and service
last accessed), the IAM tools that write roles and instance profiles, the SES v2 tools that read, create, configure, tag, and delete
sender identities with DKIM and MAIL FROM, read the account status and change its attributes and production access request, create, configure, and delete configuration sets, manage the suppression list, keep email templates, contact lists, and contacts, and send mail, and the S3 tools that read buckets and create, delete, and configure them
(versioning, default encryption, tags, public access block, object ownership), remove their lifecycle and CORS rules,
and read the rest of their configuration and the Object Lock state of their objects, and the Route 53 tools that list, read, create,
and delete hosted zones, change their comments and tags, list and change their records in atomic batches, test a DNS answer, read the status of a change, list, read, create, change, and delete health checks with their status and tags, read DNSSEC and manage key signing keys, create, version, and delete traffic policies, create, switch, and delete traffic policy instances, and read query logging configurations, reusable delegation sets, traffic policies and their instances, CIDR collections, limits, geolocations, health checker addresses, and VPC associations. Further services build on it.

[Set up AWS](setup.md) lists the secret roles, the permissions, and what the connection test proves. The tools are
described per group: [Security Token Service](sts.md), [Identity and Access Management](iam.md), [Simple Email Service](ses.md), [Amazon S3](s3.md), and [Amazon Route 53](route53.md).

## Role assumption

With `role_arn` set, Qatlas assumes the role itself with `sts:AssumeRole`, using the access key. It requests a
session of 3600 seconds, which is the default maximum of every role and the limit for role chaining, so it is never
above what the role allows; a higher maximum duration configured on the role is not used. The session lives in
memory only. It is reused until five minutes before it expires and then replaced. One session is held per
credential set and region, because it is fetched from the regional STS endpoint of the region of the call. Each
new session is checked against the bound account again.

The session credentials are internal. They are never a tool, an argument, a result, a log line, or part of an error
message. `AssumeRole` is not a tool, and neither is any other STS action that returns a credential or a token
(`AssumeRoleWithSAML`, `AssumeRoleWithWebIdentity`, `AssumeRoot`, `GetDelegatedAccessToken`, `GetFederationToken`,
`GetSessionToken`, `GetWebIdentityToken`).

The role must live in the bound account: a `role_arn` of another account is refused before any request. Role
ARNs of the partitions `aws-cn` and `aws-us-gov` are refused.

## Targets

A connection binds exactly one account and a list of regions.

- `account/<12 digits>`: required, exactly one. Global services hang on it.
- `region/<name>`: required, at least one, at most 40, no repeats. The list is an allowlist. Only regions of the
  `aws` partition are accepted; `cn-*`, `us-gov-*` and the isolated partitions are refused.

Several accounts mean several connections. A regional tool takes an optional `region` argument. It may be left out
when the connection lists exactly one region; otherwise it must name one of the allowed regions. A region outside
the list is refused before any secret is read or any request is built.

### Account check

Before every call Qatlas proves with `sts:GetCallerIdentity` that the credential belongs to the bound account,
and refuses the call otherwise. With a role, the account of the assumed session counts. The refusal names no
account ID. The proof is remembered in memory for each credential set (access key ID, a digest of its secret and
token, and the bound account), because an access key belongs to one account for good, so the second request is paid
once and not on every call. Only a match is remembered; a refusal is checked again on the next call. The tool
`aws.sts.caller-identity.get` always asks AWS afresh and is its own proof.

## Global services and endpoints

Qatlas resolves the host of every request from a fixed table, never from an argument or from the configuration, and
sends only over HTTPS without following redirects.

| Service | Host | Signing region |
| --- | --- | --- |
| STS | `sts.<region>.amazonaws.com` | the region |
| IAM (global) | `iam.amazonaws.com` | `us-east-1` |
| Amazon S3 | `s3.<region>.amazonaws.com` | the region |
| SES v2 | `email.<region>.amazonaws.com` | the region |
| Route 53 (global) | `route53.amazonaws.com` | `us-east-1` |
| Account (global) | `account.us-east-1.amazonaws.com` | `us-east-1` |

Global services hang on the account and ignore the region allowlist; a connection reaches them with its account
target alone. Of the services in the table, STS, IAM, SES v2, S3, and Route 53 have tools.

## CloudTrail

Every request carries `User-Agent: qatlas/<version>`. A role session is named `qatlas-<hash>`, where the hash is
the first 16 hex digits of the SHA-256 of the connection name; it is no secret and does not reveal the name. In
CloudTrail, look for the user agent, or for the assumed-role session name.

## Errors

AWS answers `awsQuery` calls with `<ErrorResponse><Error><Type><Code><Message>`. Qatlas keeps the code and type
tokens and never shows the provider's `Message`.

| Answer | Class |
| --- | --- |
| `Throttling`, `ThrottlingException`, `RequestLimitExceeded`, HTTP 429 | `rate-limited` |
| `AccessDenied`, HTTP 403 | `permission` |
| `InvalidClientTokenId`, `SignatureDoesNotMatch`, `ExpiredToken`, HTTP 401 | `auth` |
| HTTP 5xx | `provider-error` |
| unreadable or oversized body | `invalid-provider-response` |
| deadline | `timeout` |

Reads are repeated automatically, at most twice: a rate limit only when AWS sends a `Retry-After` of at most ten
seconds, and a server failure once more after a short pause. A rate limit without `Retry-After` is reported and not
repeated. A call has 50 seconds in total, each request 15 seconds. Requests are spaced by 250 ms per connection and
service.

## Limits

- No live test has been run against AWS. Behaviour for roles that require MFA, role chaining, and the role
  maximum is documented by AWS only and has not been verified.
- A role that requires multi-factor authentication cannot be assumed. AWS answers it with the same `AccessDenied` as
  a wrong trust policy, so the message names both causes.
- Role chaining (an access key that itself carries a session token and assumes a role) is limited to one hour by
  AWS; the requested 3600 seconds fit.
- Identity Center, Roles Anywhere, and `credential_process` are not supported. Partitions `aws-cn` and
  `aws-us-gov` are not supported.
- Everything AWS returns, including the decoded authorization message, is untrusted data.

## How the tools are built

Every AWS tool is generated from the AWS service model and a curation table, not written by hand. Each tool is one
fixed model operation; there is no general call.

- **Model pin.** The models come from the AWS repository `api-models-aws` at one pinned commit. The repository keeps
  an extract of each model, not the upstream file: the documentation and example traits are removed, nothing else
  changes. The pin file beside the extracts names the commit, the day the files were retrieved, and the SHA-256 of
  every upstream file and every extract. The upstream `LICENSE` and `NOTICE` (Apache-2.0) lie beside them. A test
  checks the extracts against the pin file without network access.
- **Curation table.** One table per service decides which operations become tools and how: the tool ID, effect,
  confirmation, `listed-only`, idempotency, title and descriptions, renames for readability, members hidden for a
  security reason (also members nested in an input), enum values left out of an argument, the target check and client-side policy check that run before the call, and the own-role check
  of a tool that changes or deletes a role. Every operation that is not a read requires confirmation.
- **Completeness.** Every operation of a model is a tool, in `exclude`, or in `defer`, each with a reason; a test
  fails otherwise, so a new operation of an updated model never becomes a tool or disappears silently. `exclude`
  means never a tool, such as every operation that returns a credential; a test proves that none of them is one.
  `defer` means not yet.
- **`listed-only`.** Such a tool is offered only by a connection whose tools list names it
  (`requires_tool_allow_list`).
- **From the model.** Arguments, fields and their limits come from the model. Deprecated members are left out. A
  member the model marks sensitive, or whose name says it holds a credential, stops the generation unless the table
  hides it; a table can hide a top-level or a nested input member, and can exclude single values of an enum
  argument, so a value such as a key origin is never accepted. A table may name string shapes the service returns
  percent-encoded, such as the IAM policy documents; their values are decoded before the cap applies, and a
  malformed value is an invalid answer. A table can also keep open a member the model marks sensitive when it is an identifier and not a secret, such
  as the ID of a KMS key; the release names the member and gives a reason, covers input and output, and never applies to a
  member whose name looks like a credential. A paged operation takes `page_size` (where the service has a page size) and `cursor` and returns
  `next_cursor` and `has_more`. An operation with an idempotency token generates the token, accepts `client_token`
  to repeat a request after a timeout, and returns the token it sent. A table can name a required string member of a
  `restXml` create operation as the token when the model gives it no `idempotencyToken` trait, as it does for the
  `CallerReference` of a Route 53 zone.

- **Protocol layers.** The generator reads the protocol of each model from its service trait and refuses a model
  that names none or two. `awsQuery` (STS, IAM) sends a form body and reads XML. `restJson1` (Account, SES v2) binds
  each member by the model's HTTP traits: `http` method, URI and status, `httpLabel` (also greedy), `httpQuery`,
  `httpQueryParams`, `httpHeader`, `httpPrefixHeaders`, `httpPayload` and `httpResponseCode`, plus `jsonName`,
  `timestampFormat`, unions, the document type and `idempotencyToken`. The body is JSON; paging uses `NextToken`
  with `MaxResults` or `PageSize`. The HTTP bindings are a separate package (`httpbind`) that `restXml` reuses, so
  only the body format differs. The generator knows no operation names: an operation whose traits do not bind
  unambiguously, or that uses a trait the runtime does not carry, stops the generation and names the operation.
  A model gives `PageSize` no maximum, so the tool offers no `page_size` unless the table states the maximum of the
  service. A table can name a hand-written result step, which adds a member computed from the checked answer and
  the arguments, such as the SES `dns_records`; the generator declares the member in the result schema, and the
  step reads only values the answer validated. A table can also name guards, hand-written steps that judge the
  arguments, also below the top level, before any I/O (such as the KMS key ARN of a bucket encryption rule, or the
  location constraint of a new bucket), and one readback, a step that reads the current state with a call of its own
  after the account check and before a change and refuses the change on what it finds (the public access block of
  S3); only a tool whose table names a step runs it, and no argument can skip it. A table can cap an input list or
  string that the model leaves unbounded, and keep a deprecated output member whose absence would change the meaning of
  an answer (the old rule-level prefix of an S3 lifecycle rule); other deprecated members are never rendered. Tools are rendered for `awsQuery`, `restJson1`, and `restXml`; the table of
  `account` is a raw scaffold with every operation in `defer`, the `s3` table has the bucket tools, and the `route-53` table has the hosted zone, record, health check, DNSSEC, and reader tools.
- **`restXml` (S3, Route 53).** The body is XML in the form the model's traits describe: `xmlName`,
  `xmlAttribute`, `xmlNamespace`, `xmlFlattened`, wrapped lists and maps, and a structure payload. A character
  XML 1.0 cannot carry is refused in a request and read as U+FFFD in an answer. The error shape follows the
  model: `<Error>` for S3, `<ErrorResponse><Error><Type><Code>` for Route 53. S3 can answer `CopyObject` or
  `CompleteMultipartUpload` with status 200 and an `<Error>` body, which is an error that says the operation may
  not have completed. Of a Route 53 `InvalidChangeBatch` the single messages are kept, capped, as provider text. A list that continues at several members, such as S3 versions by key and version marker
  or Route 53 record sets by name, type and identifier, keeps them in one cursor. A table can name string shapes whose
  arguments may carry a leading text that is removed before the request, such as the `/hostedzone/` of a Route 53 zone
  ID and the `/change/` of a change ID; what is left must hold no slash, so an ID never adds a path segment. An operation the runtime cannot yet send, such as
  an event stream, is bound with the reason noted and never becomes a tool until the reason is gone.
- **S3 addressing.** The bucket name is checked against the naming rules of general purpose buckets before any
  I/O. A name that is one lower case DNS label goes in the host (`<bucket>.s3.<region>.amazonaws.com`), every
  other valid name, such as one with dots or upper case letters, goes in the path of the regional host. Access
  points, Express One Zone buckets and aliases are refused. The host is built from the checked name and the
  endpoint table only. A bucket in another region answers `PermanentRedirect` or `AuthorizationHeaderMalformed`.
  Qatlas does not follow it and reports the region of `x-amz-bucket-region` when it has the form of a region.
- **S3 signature and checksums.** The payload hash is sent as `x-amz-content-sha256`. A payload that is streamed
  is signed `UNSIGNED-PAYLOAD`, which only goes over HTTPS. Where the model requires a checksum of the body,
  Qatlas sends the SHA-256 of it, and it asks for the checksum of a downloaded object and compares it. These
  headers are never arguments.
- **`ExpectedBucketOwner`.** Every operation that has the expected-owner header, and the source-owner header of a
  copy, sends the account the connection is bound to. It is not an argument, so no other value is possible.
  `ListBuckets` and `CreateBucket` have none, and the account check of the connection covers them.
- **Streams.** An object is uploaded from and downloaded to a local file of a directory the connection releases,
  at most 100 MiB per call, exactly 100 MiB allowed. A larger upload is refused before a request, and a larger
  download before the first byte is written. A download counts as done only when all announced bytes arrived,
  so a partial file never remains. A stream is read once and its call is never repeated. No tool returns or
  creates a presigned URL.

A model update is a deliberate change. Set the commit, the retrieval day and the upstream hashes in
`internal/provider/aws/gen/model/pin.json`, download the upstream files into a directory laid out like
`api-models-aws`, and run `go run ./gen/awsgen extract <directory>` in `internal/provider/aws`; it refuses a file
whose hash differs and writes the extracts and their hashes. Then run `go generate ./internal/provider/aws/...` and
commit extracts, pin file and generated files together. A test fails when the generated files do not match the
extracts and the tables.
