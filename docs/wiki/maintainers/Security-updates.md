# Dependency updates and automatic patches

Nylzo uses self-hosted [Renovate](https://docs.renovatebot.com/) to propose updates and an independently credentialed coordinator to merge and release them. `SECURITY_AUTOMATION_ENABLED=false` stops automatic merging and tagging.

## Schedules and coverage

Renovate is the only update-PR producer. Dependabot alerts and GitHub's dependency graph stay enabled; duplicate Dependabot version and security PR generation are disabled.

Security discovery runs every six hours (`17 */6 * * *`, UTC). It checks the published source and protected `main`, reusing identical inventories. Coverage includes all Go, web and release-tooling lockfiles, reviewed GitHub Actions advisories, verified release SBOMs, both container architectures, pinned development tools, Caddy images, and the Renovate image. Scanner or integrity failures stop discovery. The weekly run is Monday at 02:47 UTC and also considers ordinary stable updates. GitHub schedules are best effort and can be delayed; they are not a six-hour delivery guarantee.

Renovate's native managers cover Go modules, npm/pnpm, GitHub Actions, mise and its lockfiles, Docker Compose, Node version files, and OpenTofu providers and provider locks. Custom managers cover inline govulncheck, annotated workflow tool versions, the release base-image digest, the Renovate image, the Ubuntu LTS test image, Flatcar stable versions, and architecture-specific host/validator binary checksums. The six-hour inventory also downloads and scans both Linux architectures of every locked provider, GitHub CLI, Cosign, jq, the Ignition validator, Syft, mise and Codecov CLI. Provider archives must match committed lockfile checksums; host assets must match their pinned SHA-256. Binaries are catalogued without execution. Syft, mise and Codecov CLI also receive direct advisory lookups; opaque vendor binaries may not expose every bundled component. Missing platforms or integrity failures stop discovery. Go module updates run the native `gomodTidy` step before a proposal is committed, so obsolete checksum entries do not fail CI. Project generators remain disabled. A deterministic generator synchronizes derived host metadata, the OpenTofu launcher, isolated signing-runtime locks, and policy checksums. Protocol versions, test fixtures and golden fingerprints are not software updates.

A vulnerability inventory is a detection layer, not proof that every dependency has a public advisory. Some upstream tools can contain known vulnerabilities even in their latest release. Reports retain these findings. CI blocks automatic upgrades to tools with high/critical findings; unchanged build-tool findings remain visible without preventing unrelated application fixes. Every shipped Nylzo archive and container still passes the strict high/critical release gate.

## Isolation from pending features

A scanner bootstrap change must first pass a planned release, so maintenance CI includes the new coverage. Proposals based on an older inventory contract are deferred.

The updater creates `release/vX.Y.Z` at the exact commit of the latest immutable release. Production patch PRs target that branch. Each security or weekly discovery groups compatible updates across ecosystems into one batch PR per target branch. Three available fixes therefore share one signed commit, one shared PR verification run, and one patch release with one changelog. Native vulnerability alerts join the same batch. A single available fix proceeds without waiting for more updates; the batch contains what discovery can resolve at that time, not versions published after discovery. Separate forward-update batches target `main`; those wait in the ordinary aggregate release flow. A verified dependency merge on main refills the same update queue without authorizing a release or deployment.

After a qualifying maintenance PR merges, full push CI runs again. A failed post-merge run gets at most three complete attempts on the same reviewed commit. GitHub may mark a job timeout as a cancelled run; retrying it requires GitHub's timeout annotation from that attempt. Manual cancellation alone never restarts CI. Exhaustion labels the merged PR `requires-attention` and assigns the maintainer; publication still requires a complete successful attempt. The scheduled monitor detects maintenance commits left unpublished for over one hour. The coordinator requires exactly one reviewed dependency batch commit since the published source and issues the next patch version. It does not copy unreleased `main` changes into the maintenance release or stack another patch onto an unpublished one. Release coordination retains every queued completion event and holds its lock until immutable publication finishes. A busy published baseline is deferred without stacking changes. Once publication succeeds, discovery runs again on the new release baseline with the same security/weekly mode, so remaining safe updates can finish without waiting for the next cron.

## Automatic merge conditions

An unresolved artifact-generation warning from the proposal App blocks automatic merging and tagging, independently of its optional commit statuses. The coordinator checks the proposal App's exact login and numeric identity, the canonical repository, one verified signed commit, current base and head, the complete changed-file list, and dependency-only contents. It rechecks full CI, reproducibility and release verification, plus all three CodeQL analyses, from the same run before merging the exact tested SHA. Required branch checks and signed commits remain enforced.

Stable, forward patch/minor updates may qualify. Major upgrades, pre-1.0 minor upgrades, prereleases, downgrades, source migrations, custom sources, command changes and ambiguous integrity data require review. A checksum update must match GitHub's upstream release asset digest; an Action SHA must resolve to its upstream version tag. A passing test suite cannot prove that an update has no behavioral migration.

Failed tests add `requires-attention` and `automation:ci-failed`, and assign the PR to `misaon`. Renovate may propose a later fix. The coordinator clears a CI-only hold only after verifying the current signed dependency-only commit, complete CI and all CodeQL analyses. A `manual-hold`, or an attention label without the CI marker, always blocks automatic merge. Rejected dependency contents and migrations receive `manual-hold`, which also freezes Renovate updates. Remove that label only when handing the branch back to automation.

Ordinary discovery allows two active proposals, with one additional slot for native GitHub vulnerability alerts. Both security and weekly batches combine eligible patch, digest and stable minor updates; major and pre-1.0 minor migrations stay separate. A failed batch starts the diagnostic recovery below. No failed check is waived. Superseded, unedited signed App proposals close before the slot budget is calculated. Human-edited migration branches are preserved. Only holds that remain open receive budget compensation, so retired proposals cannot create extra active slots. Breaking upgrades are draft PRs against main, not repeated production-patch proposals. Node and its TypeScript declarations share one planned migration. Mark a reviewed draft ready for a fresh CI run. After a maintenance merge, the coordinator cancels CI only for unchanged, signed App proposals based on the superseded source. Push and release runs, foreign repositories and human-edited commits are excluded. Unedited proposals without a review hold are closed and recreated by discovery on the next published baseline, with fresh CI. Drafts, review holds and human edits remain for review. The aggregate release PR remains open until the maintainer approves a planned release. A stale base is rebased and retested instead of being treated as a migration. No process can automatically manufacture an upstream fix that does not exist; inspect retained findings and maintainers' advisories in that case.

## Batch recovery

The normal path tests and releases one combined PR. If it fails, the coordinator keeps its signed dependency snapshot and opens individual diagnostic PRs from the same source. Related pins for one tool stay together. At most two ordinary PRs run at once; completed diagnostics free their proposal slots. Diagnostics never merge or authorize a tag, even if someone merges one manually. An unresolved lockfile/artifact-generation warning from the exact proposal App starts isolation immediately. The trusted continuation job assigns attention and cancels CI only for that unchanged signed PR head, so known-broken artifacts do not consume a full test cycle. Push/release runs, other heads, foreign repositories and human holds are excluded.

A passing diagnostic requires the current signed head, complete CI, release verification and all three CodeQL analyses. Once every member has a result, Renovate combines the passing exact targets into a new PR. That PR must pass the complete combined suite and independent dependency validation again. Only its single squash commit can proceed through post-merge CI to one patch release. Failed targets remain assigned to the maintainer with `requires-attention`.

If individually passing updates conflict when combined, recovery removes one deterministic tool/dependency unit and tests the smaller combination. It never retries the same failed combination. A batch of N units permits at most N individual diagnostics and N recombinations. No passing remainder means no release. This finds a verified subset; it does not claim to identify every cause of an interaction failure or the largest possible subset.

Targets are bound to version and digest. A completed protected-main lookup that cannot prepare an exact target despite free proposal capacity records it for attention, so a moved image tag cannot stall the verified remainder indefinitely. This data-only record is accepted only from the GitHub Actions bot and is bound to the signed root head. Its effect is limited to the same source commit, target branch and discovery mode; a main-only lookup cannot block a production target. A matching signed diagnostic takes precedence over an earlier unavailable marker. A failed discovery job cannot declare a target unavailable. Failed diagnostic evidence survives baseline changes; newer versions or digests are eligible for fresh tests. Human holds remain enforced.

The recovery state lives in signed commit metadata and repository PRs. Neither a label nor a comment can authorize publication; the final PR still needs independent content validation, current full CI, signed release evidence and production provenance checks.

## Credentials and publication

Two private Apps are installed on `misaon/nylzo` only:

| App | Purpose | Repository permissions |
| --- | --- | --- |
| Nylzo Dependency Updates | Propose signed updates and maintain the release PR | Contents, Pull requests, Workflows write; Actions and Dependabot alerts read |
| Nylzo Security Automation | Independently validate, merge and tag | Contents, Pull requests, Workflows write; Actions read |

The proposal App has no review bypass or release-tag permission. Its private key lives only in the main-only `dependency-updates` environment. The coordinator key lives only in main-only `security-automation`. Job tokens request reduced permissions and are revoked at job completion. Renovate cannot read the coordinator key. Its optional commit-status reports are disabled because the proposal token cannot write statuses. Its lock synchronizer uses a private read-only mise mount, separate from Containerbase's writable installation. Both use the same exact version selected from protected main and the latest immutable release; the newer reviewed pin wins. The mounted artifact must match its pinned SHA-256. The inventory job uses that reviewed version too, including when production is ahead of main; Mise Action verifies the binary against the hash derived from the pinned archive before running it. Native mise lock generation runs in safe mode. The actual Mise release remains pinned by its two architecture-specific asset hashes and workflow version inputs. `min_version` stays a compatibility floor; discovery does not automatically raise it. The trusted synchronizer removes the old update annotation from published baselines without lowering that floor or regenerating unchanged locks. Custom managers own supported CLI inputs and their checksums; the native Action manager still updates Action commit SHAs. Native release-age filtering still applies; independent required CI and CodeQL decide merge eligibility. Workflows write is required for Action-pin updates; neither App has Administration or Secrets access.

Only the coordinator has a PR-only exception from human review and permission to create release tags. The integrity ruleset and tag immutability rules have no bypass. Privileged coordinator jobs verify hash-pinned policy files and execute an isolated copy from protected `main`; they never execute PR code or consume PR artifacts. Treat policy and bootstrap-pin changes as changes to publishing authority. Protect and rotate App keys: the coordinator remains a trusted writer with consequences if compromised.

Gitsign creates an ephemeral OIDC signature over the annotated tag, including its source, release kind, previous release and protected policy commit. The publication guard verifies that identity and evidence. Every archive and container is rebuilt without promoting CI caches, scanned, signed and attested before immutable publication. The production updater verifies the signed image digest and provenance. Minor/major upgrades additionally require signed evidence identifying an approved planned release. Health checks gate activation; automatic database rollback is refused.


## Diagnostic PR lifecycle

Both `renovate/diagnose-…` and `renovate/<base>-diagnose-…` identify diagnostic proposals; regroup branches have the corresponding two forms. Renovate can omit the extra base prefix when processing one base. Branch spelling never substitutes for the verified App identity, single signed commit, exact source and current CI checks.

Passing diagnostics remain open while their current root needs them for independent regroup verification. Once a source is superseded, discovery closes consumed passing diagnostics and failed aggregate roots. An aggregate whose individual diagnostics all failed can close after those failures have been recorded. Failed individual targets remain open for maintainer attention and exact-version quarantine. A newer identical failed diagnostic can replace a stale duplicate. Human holds, draft PRs and human-edited heads are preserved; discovery rechecks the head and holds immediately before closing anything. Closed PRs retain their diffs and CI history.

A failed recombination proves that a combination failed, not that every member is unsafe. Recovery tests smaller combinations without globally quarantining each member. If every bounded recombination fails, the exhausted root is held on its own base instead of retrying forever. The planned release PR is separate and remains under maintainer control.

## Fresh npm security fixes

The ordinary install policy keeps a 24-hour minimum age. Only an independently verified advisory fix in an exact signed update PR can temporarily exempt its precise `package@version`. Other packages retain the delay; configuration is restored even after installation failure. The weekly updater also observes a three-day publication age for normal updates.

## Recovery

Run `gh workflow run security-updates.yml --ref main` to test repository-scoped App access and local keyless tag signing without publishing. Investigate a failed release; never move a tag, replace immutable assets, or manually publish an incomplete draft. Revoke exposed keys and disable automation before repair. GitHub Actions failure notifications and the host monitor provide operational alerts; verify delivery after changing notification settings.

## References

- [Renovate update groups](https://docs.renovatebot.com/configuration-options/#groupname)
- [Renovate vulnerability alerts](https://docs.renovatebot.com/configuration-options/#vulnerabilityalerts)
- [Mise compatibility minimum](https://mise.jdx.dev/configuration.html#min-version)
- [Renovate managers](https://docs.renovatebot.com/modules/manager/)
- [Renovate self-hosting](https://docs.renovatebot.com/examples/self-hosting/)
- [GitHub workflow triggers and App tokens](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)
- [GitHub App private keys](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps)
- [Gitsign](https://github.com/sigstore/gitsign)
