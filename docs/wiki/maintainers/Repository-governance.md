# GitHub repository governance

The canonical repository is [misaon/nylzo](https://github.com/misaon/nylzo). This document records the settings reviewed on 2026-10-29, their limits, and the account settings the owner must configure. [.github/rulesets](../../../.github/rulesets) contains the reviewed ruleset JSON. Editing those files does not apply the settings to GitHub.

## Branch and release protection

| Ruleset                                                                               | Enforcement                                                                                                                                                                                                                                                                                                               |
| ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [Main: integrity and required checks](https://github.com/misaon/nylzo/rules/25868781) | Pull requests only; squash merges; verified commit signatures; no deletion or force pushes; resolved conversations; current-branch `CI required` and `Conventional PR title` from GitHub Actions; CodeQL results with errors and medium-or-higher security findings blocking merge. **No bypass actors.** |
| [Main: maintainer review](https://github.com/misaon/nylzo/rules/25868783)             | One approval, code-owner review, stale approvals dismissed on new changes, approval by someone other than the last pusher. The administrator and the Nylzo Dependency Updates App may bypass this review rule **only through a pull request**; the required dependency guard limits what the App can merge.                                                                                                                 |
| [Release tags: immutable](https://github.com/misaon/nylzo/rules/25868785)             | `v*` tags cannot be updated, deleted, or force-pushed. **No bypass actors.**                                                                                                                                                                                                                                              |
| [Release tags: maintainer creation](https://github.com/misaon/nylzo/rules/25868787)   | Repository administrators and the Nylzo Dependency Updates App can create `v*` tags. This creation exception does not bypass tag immutability.                                                                                                                                                                                                            |

[Main: linear history](https://github.com/misaon/nylzo/rules/26117344) separately enforces linear history on `main`, without bypass. The integrity and review rules also cover `release/v*` maintenance branches. Required checks are skipped only when creating a branch from an existing commit; all subsequent changes still require PRs, signatures, CodeQL and CI. The Dependency Updates App has no integrity bypass. Maintenance branches retain the historical ancestors of their published source; their PRs permit only squash merges. This permits branch creation without rewriting published history or granting an integrity bypass.

The review exception supports a repository with a single maintainer: GitHub does not let authors approve their own pull requests. It leaves a pull-request audit trail and cannot bypass the separate CI, CodeQL, signature, conversation-resolution, or history rules. Remove the exception once independent maintainers can review all changes. Review approvals do not make untrusted code safe to run; inspect executable changes before approving workflows.

The administrator can still change repository settings or rulesets. These controls do not protect against complete compromise of the owning account. A personal repository cannot enforce organization-wide authentication policies or independent administration.

Planned releases are approved by merging the aggregate release PR; maintenance patches are tagged after a dependency-guarded merge into the maintenance branch. Both use verified keyless tags and the protected `release-automatic` environment. The manual emergency path remains separate: the `release` environment requires approval by `misaon`, disables administrator bypass, and accepts only tags matching `v*`. Self-review remains allowed because there is one maintainer. This is an explicit human confirmation, not independent two-person approval. When another release reviewer is available, require independent approval and prevent self-review.

Immutable releases are enabled. The workflow uploads into a draft and publishes only after its signature and provenance steps; publication locks the release's tag and assets. Do not overwrite a compromised release: publish a corrective version and an advisory. See [release-checklist.md](Release-checklist.md).

## Automation and secrets

- `GITHUB_TOKEN` defaults to read-only access and cannot approve pull requests. Jobs grant additional permissions only where needed. CI checks out without persisted credentials.
- Every external contributor requires workflow-run approval, including returning contributors. Pull-request CI uses `pull_request`, not `pull_request_target`, and receives no repository secrets. Review the whole diff before approving a run, including configuration, dependency changes, build hooks, and scripts.
- GitHub Actions requires full commit-SHA pins. GitHub-owned actions are allowed; other actions are restricted to the release and build tools. Verified Marketplace status alone does not grant execution permission.
- Secret scanning and push protection are enabled. Dependabot alerts and the dependency graph remain enabled. Renovate handles six-hour security fixes and weekly dependency updates across supported managers; see [automatic updates](Security-updates.md). CodeQL advanced setup in `ci.yml` uses the extended suite and remote threat model for Go, JavaScript/TypeScript, and GitHub Actions. Every pull request scans all three languages to satisfy the required-results rule. Main pushes select the affected languages; weekly and manual runs scan all three.
- Dependency review blocks pull requests that introduce known vulnerabilities of moderate or greater severity when dependencies or shared build inputs change. The final `CI required` job checks every job against the change plan and rejects failed, cancelled, missing, or unexpectedly skipped work. Documentation changes validate the Wiki and skip test, lint, build, and browser jobs. Pull requests retain the mandatory CodeQL analyses. See [CI selection](../guides/Development.md#which-ci-checks-run).
- Private vulnerability reporting is enabled. Report through [Security Advisories](https://github.com/misaon/nylzo/security/advisories/new), following [SECURITY.md](../community/Security-policy.md).
- OpenSSF Scorecard publishes repository posture, Codecov publishes scoped test coverage, and Grype gates release candidates and rescans published artifacts. Harden-Runner monitors workflow activity. See [security checks and public reports](Security-checks.md) for their scope and limits.

GitHub returned `disabled` for generic/non-provider secret patterns and secret validity checks after an enable request. Do not assume those checks are active. Availability depends on GitHub's repository/account features; [validity-check availability](https://docs.github.com/en/code-security/how-tos/secure-your-secrets/customize-leak-detection/enable-validity-checks) differs from free public-repository secret scanning. Never rely on scanning to make it safe to commit a secret.

At setup, `misaon` was the only collaborator, with no deploy keys, webhooks, Actions secrets, or self-hosted runners. Recheck these inventories when integrations or maintainers change. Keep production secrets out of pull-request jobs and avoid persistent runners for untrusted community code.

## Wiki documentation

The public [Wiki](https://github.com/misaon/nylzo/wiki) presents the user and operations guides. Its sources stay in the main repository for review; [Publishing the GitHub Wiki](Publishing.md) describes rendering and publication. Keep editing restricted to collaborators, GitHub's default for a newly enabled Wiki. Wiki commits are outside the main branch's pull-request rules. Maintainers review source PRs, then publish the merged source. No privileged publication workflow or extra publishing credential is configured.

## Account-owner prerequisites

The maintainer registered an Ed25519 SSH signing key on GitHub on 2026-10-26. Keep it secure and configure Git to use it. `RELEASE_TAGGER_EMAIL` is set to `email@ondrejmisak.cz`; verify that email on the account and use it for release tag signatures. The workflow intentionally fails until GitHub verifies the tag signature. No personal signing key should be uploaded to Actions. The [security-update App](Security-updates.md), when activated, uses a separate environment-scoped App key and ephemeral OIDC tag signatures.

Protect the owner account with a passkey or hardware-backed two-factor authentication, securely stored recovery codes, and narrowly scoped credentials. These are account settings; the repository API did not establish the account's current 2FA state. Review authorized apps, tokens, and active sessions in account settings. Add independent release approval when another trusted maintainer joins.

A GitHub "Verified" commit signature attests to a signature GitHub can verify; it is not a product security certification. Do not claim an independent audit, verified organization/domain, OpenSSF badge, or certification without completing the corresponding verification.

## Periodic settings audit

After changing repository permissions, adding an integration, or modifying the release process, check these endpoints with an administrator's `gh` session. Review metadata only; do not export secret values.

```sh
gh api repos/misaon/nylzo/rulesets
gh api repos/misaon/nylzo/rules/branches/main
gh api repos/misaon/nylzo/actions/permissions
gh api repos/misaon/nylzo/actions/permissions/selected-actions
gh api repos/misaon/nylzo/actions/permissions/workflow
gh api repos/misaon/nylzo/actions/permissions/fork-pr-contributor-approval
gh api repos/misaon/nylzo/environments/release
gh api repos/misaon/nylzo/environments/release/deployment-branch-policies
gh api repos/misaon/nylzo/immutable-releases
gh api repos/misaon/nylzo/private-vulnerability-reporting
gh api repos/misaon/nylzo/code-scanning/default-setup
```

Also review collaborator roles, deploy keys, webhooks, Actions secrets/variables, self-hosted runners, and open security alerts in GitHub settings. Compare each ruleset's detail endpoint (`repos/misaon/nylzo/rulesets/ID`) with the versioned policy. Test that the effective branch rules still include the integrity rules without a bypass.

## Initial CodeQL triage

The first extended scan completed for all three language groups. Five alerts were individually inspected and dismissed as false positives, each with a rationale recorded in GitHub:

| Alert                                                         | Evidence for the decision                                                                                                                                                                                    |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| [1](https://github.com/misaon/nylzo/security/code-scanning/1) | `web/e2e/tests/home.spec.ts` strips tags only for a Playwright string assertion. The result is never rendered and is not a production sanitizer.                                                             |
| [2](https://github.com/misaon/nylzo/security/code-scanning/2) | `kdf-worker.ts` is a dedicated worker created in `kdf.ts`; its private creator channel is not a cross-origin Window message handler. The URL is a fixed build asset with a restrictive Trusted Types policy. |
| [3](https://github.com/misaon/nylzo/security/code-scanning/3) | `RequestMessage` uses SHA-256 to bind an HTTP request body into an Ed25519 transcript, not to store or stretch a password.                                                                                   |
| [4](https://github.com/misaon/nylzo/security/code-scanning/4) | `HSalt` hashes a public label and public message ID to derive HKDF salt. Passphrase stretching uses scrypt in `internal/core/keys/passphrasekey.go`.                                                         |
| [5](https://github.com/misaon/nylzo/security/code-scanning/5) | The shared vectors hash the public embedded weak-passphrase list to check Go/TypeScript consistency; no user password is being hashed.                                                                       |

These decisions apply to the inspected code, not to future uses of the same functions. Re-evaluate if the data flows or trust boundaries change. No scanning rule was disabled.

## Members, roles, and access review

`misaon` (Ondřej Misák) is the sole human repository administrator and maintainer. Responsibilities include code review, release approval, security-report triage, Wiki publication, dependency decisions, and repository access/settings. Contributors can propose changes through forks and PRs; contribution history does not itself grant write access. Instance operators separately control their own hosts, TLS, data, peppers, and abuse-reporting keys.

Nylzo Dependency Updates (App ID `5523339`) is installed on this repository only. It has Contents, Pull requests and Workflows write, Actions and Dependabot alerts read, and mandatory Metadata read. Its review and tag-creation exceptions are separate from the integrity rules, which have no bypass. Automatic merging and tagging are controlled by `SECURITY_AUTOMATION_ENABLED`; see [credentials and recovery](Security-updates.md#recovery). Codecov and Scorecard publication use job-scoped OIDC; they receive no repository App key or personal signing key.

Before granting or increasing sensitive access, the maintainer must verify the person's identity through an established channel, review their contribution and review history, document the role and reason, and choose the lowest required permission. Start new collaborators with read/triage where possible; grant write, release, settings, or secret access only for an explicit responsibility. Do not promote someone solely because of an unsolicited request or apparent urgency. Review access at least quarterly, when a role changes, and after an incident; remove access that is no longer needed. Review installed Apps and their selected repositories at the same time.

The repository cannot provide independent human review while it has one human maintainer and review exceptions. This remains an unmet OpenSSF Baseline Level 3 control even if every automated check passes.
