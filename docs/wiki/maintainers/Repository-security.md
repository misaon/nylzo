# Repository security

This page is for maintainers: the GitHub rules and automation that protect `misaon/nylzo` and its releases, the scans CI runs, and where their reports go.

## Branch and tag rules

[.github/rulesets](../../../.github/rulesets) keeps the rulesets as JSON. Editing a file there changes nothing on GitHub; change both.

| Ruleset | Applies to | Rules | Bypass |
| --- | --- | --- | --- |
| Main: integrity and required checks | `main`, `release/v*` | Pull requests only, squash merge, verified signatures, no deletion or force push, resolved conversations, required checks on the current head, CodeQL results | None |
| Main: maintainer review | `main`, `release/v*` | One approval from a code owner, covering the last push; new commits dismiss approvals | Administrators and the Nylzo Dependency Updates App, through a pull request only |
| Main: linear history | `main` | Linear history | None |
| Release tags: immutable | `v*` tags | No update, deletion or force push | None |
| Release tags: maintainer creation | `v*` tags | Only the bypass actors create tags | Administrators and the Dependency Updates App |

The review bypass exists because Nylzo has one maintainer and GitHub does not let authors approve their own pull requests. Remove it once a second maintainer can review. Required checks are skipped only when a branch is created from an existing commit, which is how `release/vX.Y` opens.

## Required checks

| Check | Source |
| --- | --- |
| `CI required` | [ci.yml](../../../.github/workflows/ci.yml): every job the change plan selected succeeded |
| `Conventional PR title` | [pr-title.yml](../../../.github/workflows/pr-title.yml) |
| `Dependency guard` | [dependency-guard.yml](../../../.github/workflows/dependency-guard.yml), see [below](#dependency-guard) |
| `Socket Security: Pull Request Alerts` | The Socket GitHub App, which analyses package behavior |
| `Analyze (go)`, `Analyze (javascript-typescript)`, `Analyze (actions)` | CodeQL in `ci.yml`: no analysis error and no security alert of medium or higher severity |

Every pull request runs all three CodeQL analyses, because the code scanning rule needs their results.

## Signatures

- Commits on `main` and `release/v*` need a signature GitHub verifies.
- [gitsign](https://github.com/sigstore/gitsign) signs automatic release tags with the GitHub OIDC identity of `release-tag.yml` on `main`, so no key is stored. `release.yml` accepts such a tag only from the Dependency Updates App while `SECURITY_AUTOMATION_ENABLED` is `true`.
- A maintainer-signed tag must come from `main` with a tagger e-mail equal to the variable `RELEASE_TAGGER_EMAIL`. Its release waits for approval in the `release` environment, which admits only `v*` tags and has no administrator bypass.
- Immutable releases are on: publishing a release locks its tag and assets.

To test keyless signing without publishing:

```sh
gh workflow run release-tag.yml --ref main
```

## GitHub Actions

- `GITHUB_TOKEN` is read-only by default and cannot approve pull requests.
- Runs from outside contributors wait for approval. Read the whole diff, workflows and lockfiles included, before you approve.
- Pull request CI uses `pull_request` and gets no secrets. `Dependency guard` uses `pull_request_target`: it runs the base branch's definition and reads the pull request only through the API.
- Actions are pinned to full commit SHAs. Only GitHub-owned actions and a short allowlist of build and release actions may run.
- Jobs start with Harden-Runner, which records network, process and file activity. Jobs that hold a secret or a write token and run often (Renovate, the release plan and tag, Scorecard, the watch scheduler) block every connection outside their `allowed-endpoints`; a new endpoint fails the job until it is added. The rest stay in audit mode until their run history is long enough to build a reliable list; the StepSecurity insights page of a run shows the recommended list.
- Secret scanning and push protection are on.

## Dependency guard

[The guard](../../../automation/dependency-guard) passes a pull request when:

- a code owner opened it and every commit is a verified commit by a code owner; or
- the Dependency Updates App opened it as one signed commit on the current base head that changes only versions, digests and checksums, each matching its upstream release; or
- a code owner approved the current head and reran the check.

Downgrades, prereleases, added or removed dependencies and changed scripts, workflow commands or permissions never pass as an automatic update. The guard therefore limits what the App's review bypass can merge.

## Renovate

Self-hosted [Renovate](https://docs.renovatebot.com/) runs in [renovate.yml](../../../.github/workflows/renovate.yml) as the Dependency Updates App, configured by [renovate.json](../../../renovate.json). The production watch starts it every six hours, and so does each CI run on `main`, `release/v*` or a `renovate/` branch. `SECURITY_AUTOMATION_ENABLED` set to anything but `true` stops it.

- It updates `main` and the maintenance branch of the latest release.
- Security fixes come from GitHub advisories, OSV and the vulnerability inventory. Each gets its own pull request labelled `security`, with no schedule or minimum age.
- Ordinary updates open on Monday, Wednesday and Friday before 15:00 Prague time for versions at least three days old, grouped as `frontend`, `backend`, `ci` and `infrastructure`.
- Bun installs only npm versions at least 24 hours old, security fixes included.
- Minor, patch and digest updates merge by squash once every check on their head passes.
- Majors and replacements open as drafts labelled `requires-attention` and need a code owner's approval.
- Failed pull requests are assigned to `misaon`. The label `manual-hold` keeps Renovate off a branch.

[Releasing](Releasing.md#patch-releases) describes the patch releases that follow.

## Scans and reports

| Scan | Runs | Blocks | Report |
| --- | --- | --- | --- |
| Dependency review | Pull requests that change dependencies | New vulnerabilities, moderate or higher | Check result |
| OSV-Scanner gate (`mise run lint:dependencies`) | Code, build, configuration and dependency changes; releases | Vulnerabilities, malware and unapproved licenses in `go.mod`, `web/bun.lock`, `automation/bun.lock`, `deploy/caddy/go.mod` | Artifact `dependency-security-reports`; release evidence |
| `bun audit`, `govulncheck`, actionlint, zizmor, ShellCheck | `mise run lint` | Any finding | Job log |
| Tool and image gate | Integration changes | A changed pin that adds a high or critical vulnerability | Artifact `tool-vulnerability-reports` |
| Grype on the Compose images | Integration changes | High or critical | Artifact `compose-vulnerability-report` |
| CodeQL | Every pull request; changed languages on `main`; all weekly | Errors, alerts of medium or higher | Code scanning |
| Grype release gate | Releases | High or critical, scanner errors, database older than 48 hours | Artifact `release-vulnerability-reports` (30 days) |
| Vulnerability inventory | Every six hours | Nothing; affected dependencies skip Renovate's schedule and minimum age | Artifact `vulnerability-inventory` |
| OpenSSF Scorecard | Weekly and on relevant pushes to `main` | Nothing | [scorecard.dev](https://scorecard.dev/viewer/?uri=github.com/misaon/nylzo), Code scanning |

Other artifacts are kept for 14 days. The inventory scans the latest release, the latest release of every maintained line, their images, `main` and the pinned tools.

## Handling a finding

- Fix a dependency finding by updating, removing or patching the package. An exception goes into [security/sca-policy.json](../../../security/sca-policy.json) with the lockfile, package, version, advisory, evidence and a `reviewUntil` date; once that date passes, the finding blocks again. The gate accepts two kinds of "not affected": Go code whose release builds never import the vulnerable package, and an npm package that `bun.lock` shows no workspace needs at runtime, so only lint, build and test tools run it. `bun audit` skips the same assessed advisories until their review date. Known malware always blocks.
- Dismiss a CodeQL alert only after reading its data flow, with the reason on the alert. Never disable a query to get a green run.

## Credentials and incidents

If the Dependency Updates App key may be exposed:

1. Set `SECURITY_AUTOMATION_ENABLED` to `false`.
2. Revoke the key and store a new one in the `dependency-updates` environment.
3. Review rulesets, settings and recent releases, then turn the automation back on.

Never alter a compromised release; publish a new version and an advisory.
