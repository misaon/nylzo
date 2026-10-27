# Automatic security updates

Nylzo can merge a qualifying Dependabot fix and publish a patch release after full verification. Automatic merge and tagging remain disabled until the dedicated GitHub App, environment restrictions, and ruleset entries have been configured and checked.

## What qualifies

The policy accepts one signed Dependabot commit from the canonical repository, targeting current `main`. It checks the bot's numeric account ID, commit signature, complete changed-file list, and actual manifest contents. A branch name, label, or PR description alone cannot authorize a release.

Only stable Go and npm dependency updates qualify. Major upgrades, pre-1.0 minor upgrades, prereleases, grouped updates, edited bot branches, workflow changes, toolchain changes, and custom package sources require maintainer review. npm manifest changes may update only the named package version; scripts and other settings must remain identical. Go module directives and replacement rules must remain identical.

The previous version must match a reviewed public GitHub advisory. The proposed version must no longer match that advisory and must meet its published patched-version boundary. Advisories without a known fix, private advisories, and ambiguous version metadata take the manual path. This is deliberately narrower than merging every PR with a security label.

## Merge and release sequence

1. Dependabot opens a fix. Its security-update service operates independently of the weekly version-update schedule and cooldown.
2. PR CI runs the required tests, integration checks, reproducibility checks, dependency review, and all three CodeQL analyses. The automation rechecks the completed run, current base, signed commit, manifests, and advisory before merging the exact head SHA.
3. The App merges through a PR. The main integrity ruleset still enforces required checks, signed commits, linear history, and resolved conversations. Only the separate human-review requirement has a PR-only App exception.
4. The merge starts a new full CI run on `main`. Release eligibility requires the successful `Release verification` marker and every required analysis from that run.
5. Every commit since the latest published stable release must be an eligible merged security PR. Unreleased features, documentation, or infrastructure changes stop automatic publication and require a maintainer release. The next version increments the latest stable patch number.
6. Gitsign signs the annotated tag using an ephemeral GitHub OIDC identity. The ordinary release workflow verifies that identity, issuer, source SHA, event type, and embedded tag name. It then builds, signs, attests, and scans the release through the existing pipeline. High/critical findings or scanner failures stop promotion.

Manual releases retain the owner's verified signing key and approval in the `release` environment. Automated tags use `release-automatic`, selected only after the keyless signature passes the guard. Personal signing keys remain outside Actions. GitHub does not currently display Gitsign signatures as Verified; the guard verifies them cryptographically through Sigstore, including transparency-log evidence.

## Fresh npm fixes

The ordinary minimum package age remains 24 hours. For a verified security fix, the shared installer temporarily adds only the exact `package@version` to `minimumReleaseAgeExclude`. It verifies the signed PR and public advisory before doing so, preserves the other install policies, and restores the workspace configuration even when installation fails.

An API failure or an update that does not qualify receives no exception. Newly published transitive packages outside the validated fix can still be held by the normal age policy. Do not set the global delay to zero to make a security PR pass.

## App and workflow trust

Install a private **Nylzo Security Automation** GitHub App on **misaon/nylzo only**. Grant Contents and Pull requests read/write, Actions read, and the mandatory Metadata read permission. Do not grant Workflows, Administration, Secrets, or organization/account permissions.

Store its private key as `SECURITY_APP_PRIVATE_KEY` only in the `security-automation` environment. Restrict that environment to the `main` branch. Set repository variables `SECURITY_APP_ID`, `SECURITY_APP_SLUG`, and, after validation, `SECURITY_AUTOMATION_ENABLED=true`. Installation tokens are scoped to this repository, reduced to each job's needs, and revoked when the token action finishes. GitHub App private keys do not expire automatically; rotate them periodically and immediately after suspected exposure.

Add the App only to the PR-only bypass list of the separate maintainer-review ruleset and the release-tag creation ruleset. Keep the main integrity and tag immutability rulesets without bypass actors. Restrict `release-automatic` to stable version tags; it contains no App private key. Keep manual release approval intact.

Privileged jobs never execute PR code or download PR artifacts. Before using App credentials or OIDC signing, workflow-pinned SHA-256 checks verify the policy tools and runtime configuration, then copy them to an isolated directory outside the checkout. The workflow permission is deliberately absent from the App, so its key cannot authorize a new policy. These pins use file contents, not historical commit references, and survive a repository history reset.

The App is still a trusted writer: a compromised key could attempt to merge other PRs through the review exception, subject to the remaining integrity rules. The immutable automation policy prevents those changes from qualifying for an automatic release. This does not remove the need to protect and rotate the App key.

When changing the policy, update its embedded checksums and the bootstrap checksum in both workflows. The bootstrap regression test rejects stale pins and modified policy files. Treat changes to those workflow pins as changes to publishing authority.

## Activation and recovery

Run `gh workflow run security-updates.yml --ref main` to test the App credentials with a read-only installation token and test keyless signing. The signing probe verifies a local non-release tag without pushing it and retains its public signature object for diagnosis. Verification can retry briefly for transparency-log indexing; it still fails unless every identity and log check passes. Establish a reviewed maintenance release after installing the mechanism; otherwise the unreleased infrastructure changes correctly block the first automated release.

To stop the bot, set `SECURITY_AUTOMATION_ENABLED=false`. Revoke an exposed App key and remove the installation or its ruleset exceptions if necessary. Failed publication leaves the stable download pointers unchanged. Never move a release tag or replace immutable assets; investigate and issue a new version.

No production VPS is connected by this mechanism. Publishing a verified release and deploying it are separate operations. A production updater must verify the candidate digest and release identity before installation, run health checks, and have a tested recovery procedure. Do not use an unchecked `latest` pull as a security boundary.

## References

- [GitHub: automating Dependabot](https://docs.github.com/en/code-security/tutorials/secure-your-dependencies/automate-dependabot-with-actions)
- [GitHub: triggering workflows with App tokens](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)
- [GitHub: global advisory API](https://docs.github.com/en/rest/security-advisories/global-advisories)
- [GitHub: managing App private keys](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps)
- [Gitsign signing and verification](https://github.com/sigstore/gitsign)
