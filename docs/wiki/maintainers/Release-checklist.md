# Release checklist

## Planned releases

1. Merge changes through PRs with Conventional Commit titles. Review the aggregate `chore(main): release X.Y.Z` PR, its changelog, version and included changes.
2. Merge the aggregate PR when ready to publish. The merge authorizes the release; ordinary PR merges do not deploy.
3. Wait for full CI and all three CodeQL analyses on the merged source. The coordinator signs a tag only after those checks pass. Investigate failures without bypassing checks.
4. Confirm the immutable GitHub Release has all six platform archives, checksums and their Sigstore bundle, SBOMs, signed evidence including release-policy metadata, and build provenance. Verify the image was promoted only after the vulnerability gate passed.
5. Follow [Verify a release](../guides/Verify-a-release.md), then confirm the host reports the expected version and the monitor passes. An emergency maintainer-signed release uses the separate manual approval path described in [Releasing](Releasing.md).

## Automatic dependency patches

The [dependency coordinator](Security-updates.md) performs the checks and approval for qualifying isolated patches. Review PRs assigned to `misaon` with `requires-attention`; they do not merge automatically. Confirm that the dependency Apps remain restricted to this repository and their separate main-only environments. Keep required checks, signatures and immutable tag rules active.

## Independent verification

From a clean release tag with the pinned tools installed, run:

```sh
mise run release:verify
mise run release:verify-image
```

These compare signed published archives, client files and image binaries against an independent rebuild. Record the platform, verifier and checksum-manifest SHA-256 when reporting a rebuild. If verification fails, stop deployment and investigate; never overwrite a published artifact or move its tag. Run `nylzo verify-client --server https://…` after deployment using a verified release binary.
