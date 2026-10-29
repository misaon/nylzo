# Dependency updates and automatic patches

Nylzo uses self-hosted [Renovate](https://docs.renovatebot.com/) to propose updates and an independently credentialed coordinator to merge and release them. `SECURITY_AUTOMATION_ENABLED=false` stops automatic merging and tagging.

## Schedules and coverage

Renovate is the only update-PR producer. Dependabot alerts and GitHub's dependency graph stay enabled; duplicate Dependabot version and security PR generation are disabled.

Security discovery runs every six hours (`17 */6 * * *`, UTC). It checks the published source and protected `main`, reusing identical inventories. Coverage includes all Go, web and release-tooling lockfiles, reviewed GitHub Actions advisories, verified release SBOMs, both container architectures, pinned development tools, Caddy images, and the Renovate image. Scanner or integrity failures stop discovery. The weekly run is Monday at 02:47 UTC and also considers ordinary stable updates. GitHub schedules are best effort and can be delayed; they are not a six-hour delivery guarantee.

Renovate's native managers cover Go modules, npm/pnpm, GitHub Actions, mise and its lockfiles, Docker Compose, Node version files, and OpenTofu providers and provider locks. Custom managers cover inline govulncheck, annotated workflow tool versions, the release base-image digest, the Renovate image, the Ubuntu LTS test image, Flatcar stable versions, and architecture-specific host/validator binary checksums. The six-hour inventory also downloads and scans both Linux architectures of every locked provider, GitHub CLI, Cosign, jq, the Ignition validator, Syft, mise and Codecov CLI. Provider archives must match committed lockfile checksums; host assets must match their pinned SHA-256. Binaries are catalogued without execution. Syft, mise and Codecov CLI also receive direct advisory lookups; opaque vendor binaries may not expose every bundled component. Missing platforms or integrity failures stop discovery. Go module updates run the native `gomodTidy` step before a proposal is committed, so obsolete checksum entries do not fail CI. Project generators remain disabled. A deterministic generator synchronizes derived host metadata, the OpenTofu launcher, isolated signing-runtime locks, and policy checksums. Protocol versions, test fixtures and golden fingerprints are not software updates.

A vulnerability inventory is a detection layer, not proof that every dependency has a public advisory. Some upstream tools can contain known vulnerabilities even in their latest release. Reports retain these findings. CI blocks automatic upgrades to tools with high/critical findings; unchanged build-tool findings remain visible without preventing unrelated application fixes. Every shipped Nylzo archive and container still passes the strict high/critical release gate.

## Isolation from pending features

A scanner bootstrap change must first pass a planned release, so maintenance CI includes the new coverage. Proposals based on an older inventory contract are deferred.

The updater creates `release/vX.Y.Z` at the exact commit of the latest immutable release. Production patch PRs target that branch. Separate forward-update PRs target `main`; those wait in the ordinary aggregate release flow.

After a qualifying maintenance PR merges, full push CI runs again. A failed post-merge run gets at most three complete attempts on the same reviewed commit. GitHub may mark a job timeout as a cancelled run; retrying it requires GitHub's timeout annotation from that attempt. Manual cancellation alone never restarts CI. Exhaustion labels the merged PR `requires-attention` and assigns the maintainer; publication still requires a complete successful attempt. The scheduled monitor detects maintenance commits left unpublished for over one hour. The coordinator requires exactly one reviewed dependency update since the published source and issues the next patch version. It does not copy unreleased `main` changes into the maintenance release or stack another patch onto an unpublished one. Release coordination retains every queued completion event and holds its lock until immutable publication finishes. A busy published baseline is deferred without stacking changes. Once publication succeeds, discovery runs again on the new release baseline with the same security/weekly mode, so remaining safe updates can finish without waiting for the next cron.

## Automatic merge conditions

An unresolved artifact-generation warning from the proposal App blocks automatic merging and tagging, independently of its optional commit statuses. The coordinator checks the proposal App's exact login and numeric identity, the canonical repository, one verified signed commit, current base and head, the complete changed-file list, and dependency-only contents. It rechecks full CI, reproducibility and release verification, plus all three CodeQL analyses, from the same run before merging the exact tested SHA. Required branch checks and signed commits remain enforced.

Stable, forward patch/minor updates may qualify. Major upgrades, pre-1.0 minor upgrades, prereleases, downgrades, source migrations, custom sources, command changes and ambiguous integrity data require review. A checksum update must match GitHub's upstream release asset digest; an Action SHA must resolve to its upstream version tag. A passing test suite cannot prove that an update has no behavioral migration.

Failed tests add `requires-attention` and `automation:ci-failed`, and assign the PR to `misaon`. Renovate may propose a later fix. The coordinator clears a CI-only hold only after verifying the current signed dependency-only commit, complete CI and all CodeQL analyses. A `manual-hold`, or an attention label without the CI marker, always blocks automatic merge. Rejected dependency contents and migrations receive `manual-hold`, which also freezes Renovate updates. Remove that label only when handing the branch back to automation.

Ordinary discovery allows two active proposals, with one additional slot for native GitHub vulnerability alerts. Holds on main and the current release do not consume ordinary slots; obsolete release holds cannot enlarge the budget. Breaking upgrades are draft PRs against main, not repeated production-patch proposals. Node and its TypeScript declarations share one planned migration. Mark a reviewed draft ready for a fresh CI run. After a maintenance merge, the coordinator cancels CI only for unchanged, signed App proposals based on the superseded source. Push and release runs, foreign repositories and human-edited commits are excluded. Remaining proposals need a fresh base and complete CI. The aggregate release PR remains open until the maintainer approves a planned release. A stale base is rebased and retested instead of being treated as a migration. No process can automatically manufacture an upstream fix that does not exist; inspect retained findings and maintainers' advisories in that case.

## Credentials and publication

Two private Apps are installed on `misaon/nylzo` only:

| App | Purpose | Repository permissions |
| --- | --- | --- |
| Nylzo Dependency Updates | Propose signed updates and maintain the release PR | Contents, Pull requests, Workflows write; Actions and Dependabot alerts read |
| Nylzo Security Automation | Independently validate, merge and tag | Contents, Pull requests, Workflows write; Actions read |

The proposal App has no review bypass or release-tag permission. Its private key lives only in the main-only `dependency-updates` environment. The coordinator key lives only in main-only `security-automation`. Job tokens request reduced permissions and are revoked at job completion. Renovate cannot read the coordinator key. Its optional commit-status reports are disabled because the proposal token cannot write statuses. Its lock synchronizer uses a private read-only mise mount, separate from Containerbase's writable installation. Both use the same exact version selected from protected main and the latest immutable release; the newer reviewed pin wins. The mounted artifact must match its pinned SHA-256. The inventory job uses that reviewed version too, including when production is ahead of main; Mise Action verifies the binary against the hash derived from the pinned archive before running it. Native mise lock generation runs in safe mode. Custom managers own supported CLI inputs and their checksums; the native Action manager still updates Action commit SHAs. Native release-age filtering still applies; independent required CI and CodeQL decide merge eligibility. Workflows write is required for Action-pin updates; neither App has Administration or Secrets access.

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
