# Releasing

This page is for maintainers. It explains how Nylzo versions are chosen, how a release is cut and checked, and how to publish this Wiki.

## Versions

Nylzo uses [Semantic Versioning](https://semver.org/). Builds take their version from the release tag `vX.Y.Z`. There are two kinds of release:

- A planned release `vX.Y.0` comes from `main`. [release-please](https://github.com/googleapis/release-please) keeps an open `chore(main): release X.Y.0` pull request that always raises the minor version and updates `.github/CHANGELOG.md` and `.release-please-manifest.json`. Pull request titles decide the changelog sections.
- A patch release `vX.Y.Z` comes from the maintenance branch `release/vX.Y`, which opens when `vX.Y.0` is published. It carries only dependency updates and security fixes, and automation tags it.

Builds from a checkout without a release tag report `0.0.0-dev.g<commit>`, with `.dirty` added when the source has uncommitted changes. `nylzo version --json` prints the version, commit, commit date and dirty flag. The server reports the same `build` object in `/api/v1/instance` and `/.well-known/nylzo/transparency.json`.

Only stable versions are released. Never move, delete or reuse a tag, even after a failed run.

## Cut a planned release

1. Review the release pull request: version, changelog and the changes it includes.
2. Merge it when the release is ready. The merge is the approval; never merge it only to clear the queue.
3. CI runs the full suite on the merge commit. When `Release verification` and the three CodeQL analyses pass, [release-tag.yml](../../../.github/workflows/release-tag.yml) signs an annotated tag with [gitsign](https://github.com/sigstore/gitsign) and the workflow's GitHub OIDC identity, marks it `Release-Kind: planned`, and pushes it with the Nylzo Dependency Updates App.
4. The tag starts [release.yml](../../../.github/workflows/release.yml). It checks the tag signature and that the commit passed full CI, builds the web client, binaries and images in jobs without write access, signs `checksums.txt`, uploads a draft release and attests provenance.
5. Grype scans all eight archives and both images. A high or critical finding, a scanner error or a vulnerability database older than 48 hours stops the release.
6. Only then does the workflow give the images their version tag, publish the release, which locks its assets, and move `latest`. Last, it checks that both images can be pulled anonymously.
7. When the release is published, `release-tag.yml` opens `release/vX.Y`.

## Patch releases

[Renovate](Repository-security.md) merges updates into `release/vX.Y`.

- After CI passes on a merge of a pull request labelled `security`, `release-tag.yml` tags the next patch with `Release-Kind: security`.
- Other updates wait for a dependency window: on Monday, Wednesday and Friday from 13:47 UTC the production watch starts `release-tag.yml`, which runs full CI on the branch and tags the next patch with `Release-Kind: dependencies` when it passes.
- [monitor.yml](../../../.github/workflows/monitor.yml) fails when a merged security fix waits more than an hour for its release, or another update more than four days. Rerun the failed CI; do not tag by hand.

Setting the repository variable `SECURITY_AUTOMATION_ENABLED` to anything but `true` stops automatic tagging.

## Emergency release

If the automation cannot tag, sign a tag yourself from a clean `main` that matches `origin/main` and has passed full CI:

```sh
mise run release:prepare -- X.Y.Z
```

Your Git committer e-mail must match the repository variable `RELEASE_TAGGER_EMAIL`, and GitHub must verify your tag signature. `release.yml` then waits for approval in the `release` environment.

## Check a release

A complete release has:

- six `nylzo` archives: Linux and macOS `.tar.gz`, Windows `.zip`, each for amd64 and arm64;
- two `nylzo-caddy` archives for Linux amd64 and arm64;
- `nylzo-host_<version>.tar`, the host bundle of the [Hetzner Flatcar profile](../guides/Hetzner-Flatcar.md);
- `checksums.txt` with its Sigstore bundle and one SBOM per archive;
- the dependency evidence, listed in `nylzo_<version>.evidence.sha256` and signed;
- `nylzo_<version>.intoto.jsonl` with the build provenance;
- the images `ghcr.io/misaon/nylzo` and `ghcr.io/misaon/nylzo-caddy` under the version tag.

After publication:

1. Follow [Verify a release](../guides/Verify-a-release.md).
2. Confirm that production reports the new version and that the monitor passes.
3. Within four hours, `brew info misaon/nylzo/nylzo` and the [Scoop manifest](https://github.com/misaon/homebrew-nylzo/blob/main/bucket/nylzo.json) should offer the release. If not, check the [tap's workflow](https://github.com/misaon/homebrew-nylzo/actions); the monitor fails too.

To rebuild a release independently, check out its tag in a clean tree and run:

```sh
mise run release:verify
mise run release:verify-image
```

The first verifies the signed `checksums.txt`, rebuilds the archives and compares them and their provenance. The second verifies the image signatures and compares the image binaries with the rebuild. If either fails, stop the deployment and investigate. A bad release is never overwritten: publish a new version and an advisory.

## Publish the Wiki

The Wiki sources live in `docs/wiki/`. Publish them after the pull request that changes them is merged.

1. In a clean checkout of `main`, render the pages:

   ```sh
   mise exec -- bun tools/wiki.mts
   ```

   This writes `dist/wiki`, rewrites links for the Wiki, fails on a broken link and contacts nothing.
2. Clone `https://github.com/misaon/nylzo.wiki.git` outside the checkout, or pull the latest state into an existing clone.
3. Copy the files from `dist/wiki` into the clone.
4. Delete the Wiki pages whose source no longer exists in `docs/wiki`.
5. Review the diff, sign the commit and push without force. Check the Home page, the sidebar and the changed pages on GitHub.

Wiki pushes bypass the pull request rules of the main repository, so keep Wiki editing limited to collaborators and change content only through `docs/wiki/`.
