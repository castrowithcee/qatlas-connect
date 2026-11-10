---
description: >
  Describes the GitHub environment and deployment tools: reading environments, the listed-only set and delete with their plan limits and reviewers, the deployment branch policies and protection rules of an environment, and reading deployments with their statuses.
type: knowledge
edit: shared
created: 2026-11-10
updated: 2026-11-10
---

# GitHub environments and deployments

The environment and deployment tools of the [GitHub provider](README.md), in the tool group Actions. Every tool
takes `repository` as described under [the target of a call](README.md#the-target-of-a-call).

| Tool | Does |
| --- | --- |
| `github.environments.list` | lists the deployment environments of a repository with their protection rules |
| `github.environments.get` | reads one environment by name |
| `github.environments.set` | creates an environment or replaces its protection settings; listed-only |
| `github.environments.delete` | deletes an environment with its secrets and variables; listed-only |
| `github.deployment-branch-policies.list` | lists the branch and tag patterns of one environment |
| `github.deployment-branch-policies.create` | adds one pattern to an environment |
| `github.deployment-branch-policies.update` | changes the pattern of one policy; its type stays |
| `github.deployment-branch-policies.delete` | deletes one pattern; listed-only |
| `github.deployment-protection-rules.list` | lists the custom protection rules (GitHub Apps) of one environment |
| `github.deployment-protection-rules.get` | reads one custom protection rule |
| `github.deployment-protection-rules.delete` | disables one custom protection rule; listed-only |
| `github.deployments.list` | lists the deployments of a repository, by `sha`, `ref`, `task`, or `environment` |
| `github.deployments.get` | reads one deployment |
| `github.deployment-statuses.list` | lists the statuses of one deployment |
| `github.deployment-statuses.get` | reads one status of a deployment |

`environment` names the environment; GitHub matches it without regard to case, and a `/` in it stays part of
the name. No profile selects these tools. A connection that selects by `effects` offers the reads and the pattern
create and update; `github.environments.set` and the three deletes need their name in the connection's `tools`
list, because an environment and its rules decide who must approve a deployment and which branches may deploy.

**Plan.** Environments of a private or internal repository need GitHub Pro, Team, or Enterprise, and on Free,
Pro, or Team a wait timer and reviewers apply only to a public repository. GitHub answers a repository whose plan
lacks them like a missing or refused resource, without telling the two apart, so the not-found, permission, and
rejection messages of these tools name the plan as a possible cause.

**Setting an environment.** `github.environments.set` needs `wait_timer` (minutes, 0 for none),
`prevent_self_review`, `reviewers`, and `deployment_branch_policy` (`all`, `protected`, or `custom`) in every
call, since GitHub does not document that a value left out keeps its current value. To change one value, read
the environment first and send the others as they are. `reviewers` holds at most six `{type, name}` entries:
`User` with a login, or `Team` with the slug of a team of the organization that owns the repository; numeric
identifiers are not accepted. Qatlas reads every reviewer from GitHub first and refuses an unknown login, an
account that is not a user, and a team its organization does not hold, before anything is changed; a user
whose `users/LOGIN` a connection excludes is refused before any request. Setting reviewers, or clearing them
with `[]`, changes who must approve a deployment.

The answer names a reviewer as `user` or `team` by its `type`, where GitHub uses one `reviewer` member for both.
A missing `deployment_branch_policy` means any branch may deploy.

**Deleting an environment** removes it permanently with its secrets, variables, protection rules, and deployment
branch policies; no other tool reads or restores them.

**Deployment branch policies** apply only while the environment's `deployment_branch_policy` is `custom`;
otherwise GitHub answers them as not found, and the message says so. A pattern follows Ruby's `File.fnmatch`,
and `*` does not match `/`. GitHub answers a pattern the environment already holds with a redirect, which
Qatlas does not follow; it refuses the call, and nothing is created.

**Deployment protection rules** are only the custom rules of GitHub Apps; wait timers and reviewers are part of the
environment. Their answer names no repository, so the route below the environment binds them. Disabling one reads
it first; between that read and the delete, the rule may still change. Enabling a rule and listing the apps that
could provide one are not offered.

**Deployments and their statuses** are read only; creating or deleting them is not offered.

Every change needs confirmation, is sent once, and reports an unclear outcome instead of repeating it. The
token rights are listed under [GitHub Actions](setup.md#github-actions).
