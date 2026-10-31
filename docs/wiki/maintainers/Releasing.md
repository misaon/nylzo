# Versioning and releases

Nylzo uses [Semantic Versioning](https://semver.org/), Conventional Commit PR titles, an aggregate release PR maintained by [release-please](https://github.com/googleapis/release-please), and pinned [GoReleaser](https://goreleaser.com/) builds. Merging an ordinary PR does not publish or deploy a release. Merging the aggregate release PR authorizes publication after full verification. [Automatic dependency updates](Security-updates.md) use a separate release branch so they cannot ship pending features.

## One source of version identity

The release tag is the application version. For example, tag `v0.1.0` produces application version `0.1.0`. There is no release-time commit that rewrites version files, and private workspace package versions do not determine the application version.

The Go binary and embedded web client contain the same metadata:

| Field     | Meaning                                                                                   |
| --------- | ----------------------------------------------------------------------------------------- |
| `version` | Release version, snapshot version, or an explicitly marked development version            |
| `commit`  | Full source commit SHA                                                                    |
| `date`    | Source commit time in UTC, rather than wall-clock build time, to preserve reproducibility |
| `dirty`   | Whether the build used uncommitted source changes                                         |

`mise run build` derives the identity from the checkout and injects it into Go and Vite. Untagged builds use `0.0.0-dev.g<commit>`; modified source adds `.dirty`. Snapshot archives use GoReleaser's `-snapshot` version. A source archive without Git metadata remains an explicitly marked development build.

Read the identity with:

```sh
nylzo version
nylzo --version
nylzo version --json
```

The server logs it at startup and exposes `build` in `/api/v1/instance`, `/.well-known/nylzo/transparency.json`, and `/.well-known/nylzo/client.json`. Existing API/protocol version fields keep their original meanings. The web client embeds its own metadata, displays its version in the footer, and shows client and reported server versions separately on `/transparency`. Its `/assets/build-info.json` is included in the hashed client manifest and signed release checksums. CI checks exact agreement between that JSON and the built binary.

## Changelog policy

The aggregate release PR updates `.github/CHANGELOG.md` and `.release-please-manifest.json`. Release-please groups changes and links their PRs. GitHub Releases also contain grouped GoReleaser notes and signed downloads.
Pull requests use English Conventional Commit titles. The title becomes the squash commit; a required check validates the format, including title edits. Types determine the sections:

| Type       | Section       |
| ---------- | ------------- |
| `feat`     | Features      |
| `fix`      | Fixes         |
| `perf`     | Performance   |
| `docs`     | Documentation |
| `refactor` | Refactoring   |
| `test`     | Tests         |
| `build`    | Build         |
| `ci`       | CI            |
| `style`    | Style         |
| `chore`    | Maintenance   |
| `revert`   | Reverts       |
| `security` | Security      |

Scopes are optional, such as `fix(cli): handle an interrupted download`. For a breaking change, add `!` before the colon, such as `feat(server)!: change the storage format`, and explain migration in the PR body. Breaking subjects appear in a dedicated first section. The version planner recognizes both `!` and a `BREAKING CHANGE:` footer. `feat` increments the minor version; breaking changes increment the major version, including before 1.0. Other recognized types produce a patch. GoReleaser groups release-note subjects; explain breaking changes in the PR title as well as its body. Historical nonconforming subjects remain visible under Other changes.

The main-branch review rules remain in force for these changes. Security fixes must still follow the private disclosure process before public release notes disclose them.

## Publish a stable version

1. Merge ordinary changes through reviewed PRs with English Conventional Commit titles.
2. Review the open `chore(main): release X.Y.Z` PR. It aggregates changes since the last planned release on `main` and always raises the minor version; patches belong to the maintenance branch. The PR remains open until a maintainer chooses to release.
3. Merge that PR when its changelog, version, and included changes are ready. Full CI on the resulting commit must pass, including release verification and all three CodeQL analyses.
4. `release-tag.yml` signs an annotated tag with an ephemeral GitHub OIDC identity and opens the `release/vX.Y` maintenance branch after publication. The release guard checks the exact source, workflow identity, protected policy revision, and tag metadata. This approved path uses `release-automatic` without a second manual environment approval.
5. GoReleaser uploads a draft, adds signatures, SBOMs and provenance, and publishes only after all release gates pass. The production updater accepts the signed release; merging unrelated PRs into `main` does not change production.

The aggregate PR's merge is the release approval. Never merge it merely to clear a queue. Emergency maintainer-signed tags remain available through `mise run release:prepare -- X.Y.Z` and the separately approved `release` environment; they require a clean, fully verified `main` source.
Before publication, Grype checks every archive SBOM and both container platforms. High or critical findings, scanner errors, or missing inputs stop the release. The container is built under a staging tag; the stable version tag and `latest` are promoted only after the gate passes, preserving the scanned digest. See [security checks](Security-checks.md#release-vulnerability-gate).

For now this process accepts stable tags only. Do not use it for prereleases or nightly builds; they require a separately reviewed publication policy so they cannot replace stable downloads or the container's `latest` tag. Never move or reuse a release tag, including after a failed attempt. Investigate the failed run and any unpublished draft before retrying.

See the [release checklist](Release-checklist.md) for the approval and post-publication verification steps.

## Downloads and authenticity

Every published release contains six archives:

- Windows amd64 and arm64 ZIP files with `nylzo.exe`.
- Linux amd64 and arm64 tarballs with `nylzo`.
- macOS Intel and Apple silicon tarballs with `nylzo`.

The binary provides the CLI and the embedded web client. The server runs on Linux and macOS; Windows users run the server through WSL or Docker. Archives contain the license and README. Releases also include `checksums.txt`, its Sigstore bundle, per-archive SPDX SBOMs, and GitHub build-provenance attestations. The workflow publishes a signed multi-architecture Linux image at `ghcr.io/misaon/nylzo`.

The same release builds Caddy from [deploy/caddy](../../../deploy/caddy): Linux amd64 and arm64 tarballs `nylzo-caddy_<version>_linux_<arch>.tar.gz` with `caddy` and Caddy's Apache 2.0 license, and the image `ghcr.io/misaon/nylzo-caddy` on the same distroless base. They share `checksums.txt`, SBOMs, signatures, provenance and the vulnerability gate with the Nylzo artifacts. The Caddy build omits VCS stamping, so releases with unchanged Caddy sources and Go version produce the same Caddy binary.

Version strings, commit IDs, and server JSON are identification data, not cryptographic proof. Authenticity comes from verifying the checksum signature and artifact provenance against the canonical repository, release workflow, and exact tag. Those checks link the embedded version metadata to the downloaded artifact. These Sigstore signatures do not imply Apple notarization or a Windows Authenticode publisher certificate.

Use the commands in [Verify a release](../guides/Verify-a-release.md). For an independent rebuild, check out the clean release tag and run `mise run release:verify`; it verifies the published manifest's signature before comparing rebuilt bytes. Archive timestamps use the source commit date and TAR ownership is normalized to `root` (UID/GID 0), so the build host's account does not affect the archive checksum. CI compares both binaries and complete archives across runner architectures and checks normalized TAR ownership. `mise run release:verify-image` additionally verifies image signatures and provenance, resolves the signed digest, and pulls by that immutable digest before comparing binaries.
