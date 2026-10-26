# Versioning and releases

Nylzo uses stable [Semantic Versioning](https://semver.org/) tags (`vMAJOR.MINOR.PATCH`) and the pinned [GoReleaser](https://goreleaser.com/) to build archives, generate grouped release notes, create SBOMs, and upload signed artifacts. The maintainer chooses the version deliberately; publishing is gated by a signed tag and approval of the `release` environment.

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

GitHub release notes are the canonical changelog. GoReleaser generates them from commits since the previous tag, with links to commits and pull requests. The first release includes the repository history.

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

Scopes are optional, such as `fix(cli): handle an interrupted download`. For a breaking change, add `!` before the colon, such as `feat(server)!: change the storage format`, and explain migration in the PR body. Breaking subjects appear in a dedicated first section. The grouping operates on commit subjects; a breaking-change footer alone does not put an entry in that section. Historical nonconforming subjects remain visible under Other changes.

The main-branch review rules remain in force for these changes. Security fixes must still follow the private disclosure process before public release notes disclose them.

## Publish a stable version

After the intended change is merged and CI and all CodeQL analyses on `main` are successful:

```sh
git switch main
git pull --ff-only
mise run release:prepare -- 0.1.0
```

This requires `gh`, Git with signing configured, and an unlocked signing key registered on GitHub. It checks a clean checkout, exact agreement with `origin/main`, the expected tagger email, successful source checks, and absence of the tag. It creates a signed annotated tag and pushes that tag only. The workflow repeats the source/signature checks independently before building.

Review the workflow's source commit, version, and protected-environment request before approving `Release` in GitHub Actions. The privileged job receives only the separately built and hash-checked web client. It uploads into a draft, verifies that every platform archive and SBOM exists, adds signatures and provenance, and then publishes the immutable release.

For now this process accepts stable tags only. Do not use it for prereleases or nightly builds; they require a separately reviewed publication policy so they cannot replace stable downloads or the container's `latest` tag. Never move or reuse a release tag, including after a failed attempt. Investigate the failed run and any unpublished draft before retrying.

See the [release checklist](release-checklist.md) for the approval and post-publication verification steps.

## Downloads and authenticity

Every published release contains six archives:

- Windows amd64 and arm64 ZIP files with `nylzo.exe`.
- Linux amd64 and arm64 tarballs with `nylzo`.
- macOS Intel and Apple silicon tarballs with `nylzo`.

The binary provides the CLI and the embedded web client. The server runs on Linux and macOS; Windows users run the server through WSL or Docker. Archives contain the license and README. Releases also include `checksums.txt`, its Sigstore bundle, per-archive SPDX SBOMs, and GitHub build-provenance attestations. The workflow publishes a signed multi-architecture Linux image at `ghcr.io/misaon/nylzo`.

Version strings, commit IDs, and server JSON are identification data, not cryptographic proof. Authenticity comes from verifying the checksum signature and artifact provenance against the canonical repository, release workflow, and exact tag. The checked files then bind the version metadata to the artifact. These Sigstore signatures do not imply Apple notarization or a Windows Authenticode publisher certificate.

Use the commands in [Verify a release](wiki/Verify-a-release.md). For an independent rebuild, check out the clean release tag and run `mise run release:verify`; it verifies the published manifest's signature before comparing rebuilt bytes. Archive timestamps use the source commit date and TAR ownership is normalized to `root` (UID/GID 0), so the build host's account does not affect the archive checksum. CI compares both binaries and complete archives across runner architectures and checks normalized TAR ownership. `mise run release:verify-image` additionally verifies image signatures and provenance, resolves the signed digest, and pulls by that immutable digest before comparing binaries.
