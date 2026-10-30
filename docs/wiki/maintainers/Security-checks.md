# Security checks and public reports

Nylzo publishes evidence about its development process and releases. These checks have different scopes; none is an independent audit of the protocol or implementation.

| Check             | What it covers                                                                                 | When it runs                                                                                       |
| ----------------- | ---------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| CodeQL            | Go, JavaScript/TypeScript, and GitHub Actions security analysis                                | Every PR; affected languages on main; full weekly and manual runs                                  |
| OpenSSF Scorecard | Repository protections, workflow permissions, dependency practices, and other project controls | Relevant main changes and weekly                                                                   |
| Codecov           | Go race-test shard coverage and TypeScript unit coverage                                       | Existing relevant test jobs; reports uploaded after successful tests                               |
| Grype             | Known vulnerabilities in the release archive SBOMs and both container platforms                | Before stable release promotion; daily for the latest published release; Compose integration tests |
| Harden-Runner     | Network, process, and file activity on CI runners                                              | Jobs that execute source code or automation tools                                                  |
| OpenSSF Baseline  | Maintainer assertions against the published baseline requirements                              | Maintainer review when the project or its controls change                                          |

## Dependency checks before execution

The required SCA job checks the application, frontend workspace and release-tooling lockfiles with pinned OSV-Scanner. It rejects known malware, vulnerabilities and unapproved licenses, subject only to the existing exact, reviewed patch/VEX assessments. It generates an SPDX 2.3 SBOM for every checked source commit and retains it with the dependency report. Release SBOMs keep their versioned names, signatures and attestations.

Frontend builds and tests, release-tooling tests, Compose and provisioning checks wait for SCA to pass. The release workflow also scans before executing the frontend build. Installation uses frozen lockfiles, a release-age policy and disabled dependency lifecycle builds. The only package executed by the installer for patch verification must first match the reviewed content hashes.

[Socket Security](https://socket.dev/dashboard/org/nylzo) adds package-behavior analysis through a GitHub App installed only on `misaon/nylzo`. It receives dependency manifests and lockfiles. The comprehensive policy blocks known and suspected malware, critical CVEs and non-registry dependency sources; custom rules also block possible typosquats, obfuscated code and protestware. It has no review or tag-protection bypass. Renovate remains the update producer. Do not treat a Socket warning, an SBOM or a clean vulnerability report as proof that a package is safe.

The repository configuration does not ignore bot authors or disable alert categories. Keep the Socket check required on both main and maintenance branches, bound to the Socket App identity. Review new findings individually; do not silence a category to unblock an update. See [Socket permissions](https://docs.socket.dev/docs/permissions) and [repository configuration](https://docs.socket.dev/docs/socket-yml).

## Repository posture

The [Scorecard report](https://scorecard.dev/viewer/?uri=github.com/misaon/nylzo) publishes the individual checks as well as a combined score. The workflow uses the ordinary GitHub token to inspect repository rulesets and OIDC to authenticate publication. It does not use a personal access token.

Review findings individually. A young project with one maintainer can score lower on contributor diversity or independent review; a high combined score does not establish that its cryptography is correct. The [governance page](Repository-governance.md) records the single-maintainer review exception.

The [OpenSSF Baseline profile](https://www.bestpractices.dev/en/projects/15969) links assertions to supporting evidence. Its badge is a self-assessment, separate from the automated Scorecard report. Keep assertions current and display only earned badge levels.

The [control policies](Security-controls.md) define source-analysis, license, VEX, and credential requirements. The [security assessment](Security-assessment.md) maps actors, interfaces, and threats to implementation controls and tests.

## Test coverage

[Codecov](https://app.codecov.io/gh/misaon/nylzo) receives the existing Go race-test shard profiles and the core and web-client Vitest reports. Go profiles use atomic counters and instrument first-party packages. The TypeScript core and web client use Istanbul to avoid profiling third-party cryptographic dependencies in hot loops.

TypeScript reports include unexecuted source files. Go reports cover the packages compiled for the Linux test jobs. Test files and TypeScript declaration files are excluded. Go timing acceptance tests, fuzzing, and Playwright tests still run separately; they are not represented in these unit-coverage percentages. The component breakdown separates cryptographic code from the browser UI and server.

Go and TypeScript have separate carryforward flags, so a skipped, unchanged component can retain its previous report. Documentation changes do not start test jobs to refresh a badge. Repository-relative LCOV paths prevent identically named files in the two TypeScript workspaces from being combined incorrectly.

Test jobs have no Codecov credentials. A separate job uploads reports using OIDC and checks out the tested commit for source mapping; it does not execute PR source code. Fork PRs retain reports as Actions artifacts and do not publish through the privileged upload step. Coverage service availability does not block an otherwise valid security fix. Initial percentage checks are informational; the test suites themselves remain required.

## Release vulnerability gate

The pinned Grype binary checks all six archive SBOMs and the container by digest for both `linux/amd64` and `linux/arm64`. High and critical findings stop publication. Lower-severity findings remain in the reports. Scanner errors and missing SBOMs also fail the gate. The vulnerability database must be no more than 48 hours old.

GoReleaser first uploads a draft release and a `staging-VERSION` container tag. The release workflow signs and attests the candidate, then scans it. Only a passing candidate is promoted to the stable version tag. Its digest must remain identical during promotion. The GitHub release is published next; `latest` is updated last. A failed candidate cannot replace `latest`.

The [scheduled scan](https://github.com/misaon/nylzo/actions/workflows/vulnerabilities.yml) verifies the published checksum signature and image signature before scanning. This catches vulnerabilities disclosed after publication. Reports are retained as workflow artifacts. SBOM and vulnerability reports are inventories and findings, not proof of safety.

Module-level matches need review. For example, [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) concerns the deprecated `golang.org/x/crypto/openpgp` packages. Nylzo uses other packages from that module and does not link OpenPGP. A module inventory can still report the advisory; the report retains that finding. The required `govulncheck` analysis checks package and symbol reachability.

## Runner monitoring

Harden-Runner starts before source checkout or other tools. The initial policy records outbound traffic for review. Audit mode does not enforce a project-specific domain allowlist. Review the observed destinations before adopting a blocking policy; do not disable monitoring or widen a policy merely to hide a finding.

## Badge policy

Each README badge must link to its report or verification instructions. Keep release identity, test coverage, repository posture, and an audit claim distinct. Do not add a static "secure" or "audited" badge without evidence. [Verify a release](../guides/Verify-a-release.md) before running it.
