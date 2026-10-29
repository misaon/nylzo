# Dependency updates and automatic patches

Nylzo uses self-hosted [Renovate](https://docs.renovatebot.com/) to propose updates and an independently credentialed coordinator to merge and release them. `SECURITY_AUTOMATION_ENABLED=false` stops automatic merging and tagging.

## Schedules and coverage

Security discovery runs every six hours (`17 */6 * * *`, UTC). It checks the published source and protected `main`, reusing identical inventories. Coverage includes all Go, web and release-tooling lockfiles, reviewed GitHub Actions advisories, verified release SBOMs, both container architectures, pinned development tools, Caddy images, and the Renovate image. Scanner or integrity failures stop discovery. The weekly run is Monday at 02:47 UTC and also considers ordinary stable updates. GitHub schedules are best effort and can be delayed; they are not a six-hour delivery guarantee.

Renovate's native managers cover Go modules, npm/pnpm, GitHub Actions, mise and its lockfiles, Docker Compose, Node version files, and OpenTofu providers and provider locks. Custom managers cover inline govulncheck, annotated workflow tool versions, the release base-image digest, the Renovate image, the Ubuntu test image, Flatcar stable versions, and architecture-specific host/validator binary checksums. A deterministic generator synchronizes derived host metadata, the OpenTofu launcher, isolated signing-runtime locks, and policy checksums. Protocol versions, test fixtures and golden fingerprints are not software updates.

A vulnerability inventory is a detection layer, not proof that every dependency has a public advisory. Some upstream tools can contain known vulnerabilities even in their latest release. Reports retain these findings. CI blocks automatic upgrades to tools with high/critical findings; unchanged build-tool findings remain visible without preventing unrelated application fixes. Every shipped Nylzo archive and container still passes the strict high/critical release gate.

## Isolation from pending features

The updater creates `release/vX.Y.Z` at the exact commit of the latest immutable release. Production patch PRs target that branch. Separate forward-update PRs target `main`; those wait in the ordinary aggregate release flow.

After a qualifying maintenance PR merges, full push CI runs again. The coordinator requires exactly one reviewed dependency update since the published source and issues the next patch version. It does not copy unreleased `main` changes into the maintenance release or stack another patch onto an unpublished one. Once publication succeeds, discovery runs again on the new release baseline with the same security/weekly mode, so remaining safe updates can finish without waiting for the next cron.

## Automatic merge conditions

The coordinator checks the proposal App's exact login and numeric identity, the canonical repository, one verified signed commit, current base and head, the complete changed-file list, and dependency-only contents. It rechecks full CI, reproducibility and release verification, plus all three CodeQL analyses, from the same run before merging the exact tested SHA. Required branch checks and signed commits remain enforced.

Stable, forward patch/minor updates may qualify. Major upgrades, pre-1.0 minor upgrades, prereleases, downgrades, source migrations, custom sources, command changes and ambiguous integrity data require review. A checksum update must match GitHub's upstream release asset digest; an Action SHA must resolve to its upstream version tag. A passing test suite cannot prove that an update has no behavioral migration.

Failed tests or a rejected dependency diff add `requires-attention` and assign the PR to `misaon`. That label prevents automatic merge. Blocked PRs do not consume the four active proposal slots; discovery continues after a proposal first needs attention, so a queue of manual migrations cannot starve unrelated safe updates. A stale base is rebased and retested instead of being treated as a migration. No process can automatically manufacture an upstream fix that does not exist; inspect retained findings and maintainers' advisories in that case.

## Credentials and publication

Two private Apps are installed on `misaon/nylzo` only:

| App | Purpose | Repository permissions |
| --- | --- | --- |
| Nylzo Dependency Updates | Propose signed updates and maintain the release PR | Contents, Pull requests, Workflows write; Actions and Dependabot alerts read |
| Nylzo Security Automation | Independently validate, merge and tag | Contents, Pull requests, Workflows write; Actions read |

The proposal App has no review bypass or release-tag permission. Its private key lives only in the main-only `dependency-updates` environment. The coordinator key lives only in main-only `security-automation`. Job tokens request reduced permissions and are revoked at job completion. Renovate cannot read the coordinator key. Workflows write is required for Action-pin updates; neither App has Administration or Secrets access.

Only the coordinator has a PR-only exception from human review and permission to create release tags. The integrity ruleset and tag immutability rules have no bypass. Privileged coordinator jobs verify hash-pinned policy files and execute an isolated copy from protected `main`; they never execute PR code or consume PR artifacts. Treat policy and bootstrap-pin changes as changes to publishing authority. Protect and rotate App keys: the coordinator remains a trusted writer with consequences if compromised.

Gitsign creates an ephemeral OIDC signature over the annotated tag, including its source, release kind, previous release and protected policy commit. The publication guard verifies that identity and evidence. Every archive and container is rebuilt without promoting CI caches, scanned, signed and attested before immutable publication. The production updater verifies the signed image digest and provenance. Minor/major upgrades additionally require signed evidence identifying an approved planned release. Health checks gate activation; automatic database rollback is refused.

## Fresh npm security fixes

The ordinary install policy keeps a 24-hour minimum age. Only an independently verified advisory fix in an exact signed update PR can temporarily exempt its precise `package@version`. Other packages retain the delay; configuration is restored even after installation failure. The weekly updater also observes a three-day publication age for normal updates.

## Recovery

Run `gh workflow run security-updates.yml --ref main` to test repository-scoped App access and local keyless tag signing without publishing. Investigate a failed release; never move a tag, replace immutable assets, or manually publish an incomplete draft. Revoke exposed keys and disable automation before repair. GitHub Actions failure notifications and the host monitor provide operational alerts; verify delivery after changing notification settings.

## References

- [Renovate managers](https://docs.renovatebot.com/modules/manager/)
- [Renovate self-hosting](https://docs.renovatebot.com/examples/self-hosting/)
- [GitHub workflow triggers and App tokens](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)
- [GitHub App private keys](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps)
- [Gitsign](https://github.com/sigstore/gitsign)
