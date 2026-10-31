# Dependency updates and automatic patches

Self-hosted [Renovate](https://docs.renovatebot.com/) proposes and merges dependency updates. A required check on `main` and the maintenance branch, the dependency guard, decides what may merge without a maintainer. A small release workflow tags merged maintenance fixes as patch releases. Setting the repository variable `SECURITY_AUTOMATION_ENABLED` to anything but `true` stops Renovate and automatic tagging.

## Schedules

- `renovate.yml` runs every six hours (`17 */6 * * *`, UTC), after every completed CI run on `main`, a maintenance branch or a `renovate/` branch, and on manual dispatch. Only one Renovate run is active at a time. GitHub schedules are best effort.
- Security fixes are proposed in any run. They come from GitHub advisories (Dependabot alerts read through the App), from OSV for direct Go and npm dependencies, and from the vulnerability inventory below.
- Ordinary updates are proposed only on Monday before 08:00 UTC and only for versions at least three days old (`minimumReleaseAge`, strict). Registries that publish no release time, such as GHCR, therefore get ordinary updates only through the inventory's fast path.
- pnpm keeps its own 24-hour minimum release age for every package, including security fixes. A fresh npm fix fails its lockfile update until it is a day old; a later Renovate run retries it.

## What merges automatically

- Each security fix has its own PR. Ordinary updates are grouped: Go modules, web packages and GitHub Actions each in one PR, and each pinned tool in its own PR together with all files that pin it.
- Renovate merges a PR itself, by squash, once every check on its current head is green, including the dependency guard. It merges one PR per branch per run; the run after the merge rebases the rest. GitHub auto-merge is not used, because a ruleset bypass applies only to a direct merge.
- Majors, and minor updates of packages below 1.0, open as draft PRs labelled `requires-attention`. They merge only after a code owner approves them on the current head and re-runs the dependency guard.
- When CI fails, Renovate assigns the PR to `misaon`. A failed group is not split automatically; fix it, close it, or let a later version replace it. The `manual-hold` label stops Renovate from rebasing a branch someone is editing.

## Maintenance branch and patch releases

- Every planned release `vX.Y.0` opens `release/vX.Y` at its tag. Renovate processes `main` and the maintenance branch of the latest release only.
- The maintenance branch takes only security fixes. Ordinary updates reach production with the next planned release.
- After CI succeeds on a merge into `release/vX.Y`, `release-tag.yml` signs `vX.Y.(Z+1)` with gitsign and the workflow's GitHub OIDC identity and pushes it with the Nylzo Dependency Updates App. The tag carries `Release-Kind: security`, `Base-Release` and `Policy-Commit`.
- `release.yml` publishes only a tag whose keyless signature names `release-tag.yml` on `main`, whose base is the latest release, and which sits on the tip of `release/vX.Y`. It rebuilds, scans, signs and attests every artifact as for any release.
- If a merged maintenance fix waits more than an hour for its release, the `monitor.yml` watchdog fails and GitHub notifies the maintainer. Re-run failed CI; a tag is never moved or reused. GitHub may drop scheduled runs under load, so the watchdog also runs after every `ci`, `Release tag` and `release` run that did not come from a pull request.

## Planned releases

`release-please.yml` keeps the `chore(main): release X.Y.0` PR current with `versioning: always-bump-minor`, so planned releases raise the minor version and patches stay on the maintenance branch. Merging that PR approves the release; `release-tag.yml` then tags the manifest version with `Release-Kind: planned`. Production hosts install minor and major releases only with that signed evidence. See [releasing](Releasing.md).

## The dependency guard

`dependency-guard.yml` runs on `pull_request_target`, so it always executes the definition from the base branch. It never checks out or runs PR code and reads the PR only through the API. It passes:

- a PR from a code owner,
- a PR from the Dependency Updates App whose single, signature-verified commit is the App's own and changes only dependency values: versions, digests and checksums in the managed files, Action SHAs that match their upstream tags, and binary checksums that match the upstream release asset digests. Downgrades, prereleases, added or removed dependencies, changed scripts, and changed workflow commands or permissions fail,
- any other PR, and any App PR that is not a safe update, only after a code owner approves its current head.

The integrity ruleset has no bypass, so this check also limits what the App's review exception can merge.

## Vulnerability inventory

The six-hour run first verifies the signatures of the latest release and scans its SBOMs and both container platforms, both source trees, the pinned tools, the Caddy and Renovate images and the host binaries with Grype and osv-scanner. Integrity or scanner failures fail the job. The affected dependencies go to Renovate as a fast path that ignores the schedule and the minimum release age. Reports stay as workflow artifacts for 14 days.

In CI, a changed tool or image fails only when it adds a high or critical vulnerability that the current version does not have. Findings that an upstream release has not fixed stay visible in the reports; every shipped archive and container still passes the strict release gate.

## Credentials

The Nylzo Dependency Updates App is installed on `misaon/nylzo` only. Its key lives in the main-only `dependency-updates` environment. Jobs request reduced, revocable tokens: Renovate gets contents, pull requests and workflows write and actions and Dependabot alerts read, and reads the check results of this public repository without a separate permission; the tagger gets contents and workflows write. The App may bypass required review on pull requests and may create release tags; tag immutability and the integrity ruleset have no bypass. Renovate runs with scripts, plugins and unsafe executions disabled; its only post-upgrade command is the read-only mounted `automation/dependency-sync/sync.ts` from `main`.

## Recovery

- Run `gh workflow run release-tag.yml --ref main` to test keyless signing without publishing.
- Never move a tag, replace immutable assets or publish an incomplete draft by hand. Investigate the failed run instead.
- On a suspected key exposure, set `SECURITY_AUTOMATION_ENABLED` to `false`, revoke the App key, and rotate it before re-enabling.

## References

- [Renovate configuration options](https://docs.renovatebot.com/configuration-options/) and [automerge](https://docs.renovatebot.com/key-concepts/automerge/)
- [Renovate vulnerability alerts](https://docs.renovatebot.com/configuration-options/#vulnerabilityalerts)
- [release-please versioning strategies](https://github.com/googleapis/release-please/blob/main/docs/customizing.md)
- [GitHub App private keys](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps)
- [Gitsign](https://github.com/sigstore/gitsign)
