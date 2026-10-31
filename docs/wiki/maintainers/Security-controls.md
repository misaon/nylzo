# Security control policies

These policies apply to contributions, dependency changes, builds, and releases. [Repository governance](Repository-governance.md) records the active permissions and exceptions. The [security assessment](Security-assessment.md) records the application's threat model and accepted limits.

## Dependencies and licenses

Use Go modules and pnpm through the versions pinned in `mise.toml`; retain module/package checksums and the locked dependency graph. Obtain ordinary Go modules through the configured Go proxy and checksum database, and npm packages through the normal registry. Custom registries, replacement directives, executable build hooks, new tools, or a different package source require explicit review. Select dependencies for a necessary function, compatible licensing, maintained upstream code, and a reviewable update history. Prefer the standard library when it already meets the requirement.

`go.mod`/`go.sum`, the web workspace manifests, `pnpm-lock.yaml`, and the tool lockfile record the selected versions. Renovate proposes and merges updates across application, CI and infrastructure dependencies under the [automatic update policy](Security-updates.md); the required dependency guard checks every automatic merge. `pnpm` keeps a 24-hour package-age policy for every package, security fixes included, and rejects dependency build scripts except where explicitly configured.

The required SCA job scans both Go and npm lockfiles on every code, build, configuration, or dependency change. Documentation-only changes retain validation and mandatory SAST without rebuilding the application. OSV-Scanner reports known vulnerabilities, known malware present in its database, and dependency licenses. The scanner is pinned; API errors, incomplete inventories, and unsupported results fail the check.

The Caddy module in [deploy/caddy](../../../deploy/caddy) is scanned with the others. Its module graph also lists storage drivers and macOS code that the release build leaves out, so for that lockfile the gate reviews licenses and advisories only for the modules its two Linux release builds link. Known malware blocks in every module, linked or not. The import evidence lists the linked modules for each build.

All known vulnerability findings block unless the exact lockfile, component version and advisory have a current assessment backed by executable evidence of non-exploitability or an applied fix. Known malware always blocks, including when its identifier appears as an alias. An exception may not suppress an entire package or future versions. Remediation is an update, removal, a verified local patch, or a reviewed and time-bounded non-exploitability assessment; severity alone does not waive a source finding.

The machine-readable [SCA policy](../../../security/sca-policy.json) lists accepted license identifiers and the narrowly scoped assessments. Each assessment names the lockfile it covers, so an assessment of Nylzo's `go.mod` cannot hide the same finding in the Caddy module. A missing license, license outside that list, or scanner license violation blocks until reviewed and resolved. Adding a license to the policy requires review of the intended use and distribution obligations. An allowlisted identifier is a triage decision, not a substitute for preserving required notices or reviewing a changed distribution model. Strong copyleft, proprietary, custom, and unknown terms are not silently accepted.

Before any release, the readonly web job repeats the source vulnerability/license gate and creates a source SPDX SBOM covering both ecosystems, including build/development dependencies. Archive and image SBOMs describe the compiled Go artifacts separately. The source report, import evidence, source SBOM, and applicable VEX are bound to a signed evidence checksum manifest and GitHub provenance. Grype then checks the six archive SBOMs and both image architectures; high/critical findings, an expired database, scanner errors, or missing inputs block stable promotion. The source policy may block findings below Grype's threshold.

## VEX and review expiry

VEX describes the exploitability of a particular vulnerability in a particular Nylzo build. It does not state that an affected upstream package is safe for other consumers.

The current `GO-2026-5932` assessments apply only to `golang.org/x/crypto` version `0.58.0`, one for Nylzo and one for Caddy. The deprecated OpenPGP code must be absent from the import graph of all six supported Nylzo targets and of both Linux Caddy builds with their release tags. A changed version, a new OpenPGP import, missing target evidence, or an expired review invalidates the exception and blocks the scan. The raw advisory stays in the report; the generated OpenVEX statement uses `vulnerable_code_not_present` and identifies Nylzo's source commit or release version and the exact subcomponent. A statement about Caddy names the `deploy/caddy` directory of that commit or release as its product.

The build-only Svelte plugin uses `deepmerge@4.3.1`, affected by [GHSA-gcqx-w48m-w2h4](https://github.com/advisories/GHSA-gcqx-w48m-w2h4). Until an upstream fix is available, [the local pnpm patch](../../../web/patches/deepmerge@4.3.1.patch) excludes `__proto__` from source and target keys, including recursive clones. Every web installation and SCA run checks the patch hash, lockfile references, the three installed source hashes, poisoning regressions, and the option-merging behavior Svelte needs. A missing or changed patch, unpatched consumer, failed regression, or expired review blocks the build. The VEX status is `fixed` for this Nylzo source revision; the unpatched upstream package remains affected. Review this patch by November 27, 2026 and replace it when upstream ships a fix.

Published releases provide `nylzo_VERSION.openvex.json` when a fixed or non-affected finding needs explanation, alongside `nylzo_VERSION.source.spdx.json`, the raw dependency report, and import evidence. Verify their `nylzo_VERSION.evidence.sha256` Sigstore bundle and provenance using the exact release workflow identity. Older immutable releases are not modified to add a new assessment. Reassess a claim when the advisory, dependency version, imports, build configuration, or review deadline changes.

## Static analysis

Every PR runs CodeQL for Go, JavaScript/TypeScript, and GitHub Actions. The repository blocks analysis errors and medium-or-higher security findings through its required-results rule, in addition to the required CI gate. Workflow changes also run actionlint, ShellCheck, and pedantic zizmor. Lower-severity findings still need triage; an ignored check is not evidence that a defect is harmless.

Resolve a blocking weakness before merge and before release. A false-positive or non-exploitable finding may be dismissed only after review of the actual data flow and an explicit rationale recorded on the alert. Do not disable a query or add a broad suppression to obtain a green result. Revisit a dismissal if the relevant code or trust boundary changes. The [initial alert decisions](Repository-governance.md#initial-codeql-triage) are examples with their inspected scope.

## Secrets and credentials

Never commit plaintext credentials, real message capabilities, peppers, private reporting keys, or personal signing keys. Use synthetic test data. Local environment files, deployment secrets, private instructions, and build output are ignored; GitHub secret scanning and push protection provide an additional check. If a secret reaches Git, treat it as exposed and revoke or rotate it before considering history cleanup.

| Credential | Storage and access | Rotation or revocation |
| --- | --- | --- |
| Owner GitHub authentication and personal SSH signing key | Owner-controlled account and workstation/agent; prefer hardware-backed authentication; never copied into Actions | Review sessions and authorized credentials regularly; revoke on loss or suspected exposure and register the replacement |
| Dependency Updates App private key | Only the `dependency-updates` environment secret, restricted to `main`; install the App on this repository only | Review at least quarterly; rotate at least every 90 days and immediately on suspected exposure; verify the new key before revoking the old one |
| Installation token and `GITHUB_TOKEN` | Job-scoped environment/action inputs, minimum permissions, no public logs or test artifacts | Installation token action revokes its token at job completion; revoke an App key/installation if issuance authority is exposed |
| Release signing identity | Ephemeral GitHub OIDC certificate and keyless Sigstore signatures; exact workflow/ref/issuer claims | Disable the automation or remove its publishing authority after compromise; never weaken identity checks to accept an old job |
| Server pepper | Operator-managed secret file/store, separate from message data, readable only by the service identity | Follow the [operations](../guides/Operations.md) rotation procedure; do not overwrite a live pepper during an ordinary upgrade |
| Abuse-report private key | Operator's protected workstation, off the application server | Rotate after exposure and retain old keys only as needed to decrypt retained reports securely |

Do not place App or production secrets in fork/PR test jobs, caches, coverage reports, or persistent self-hosted runners. Logs must not include token values, private keys, message links, or environment dumps. Preserve only the minimum required local backup of an automation key in an encrypted credential store; remove unnecessary plaintext download copies after secure storage.

For a credential incident: disable the relevant automation, revoke the exposed credential, review repository rules/settings and recent releases, restore the intended policy, rotate affected downstream credentials, and publish an advisory if consumers may have received a compromised artifact. A new signature cannot repair an already compromised immutable release; publish a new version and identify the affected versions.
