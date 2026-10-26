# Release checklist

For the maintainer publishing Nylzo. Release mechanics are defined in [.github/workflows/release.yml](../.github/workflows/release.yml) and [.goreleaser.yaml](../.goreleaser.yaml). Repository protections are documented in [github-governance.md](github-governance.md).

## Before the first release

- Register a signing key on the maintainer's GitHub account and verify its email. Keep the private key outside the repository and GitHub Actions.
- Set repository variable `RELEASE_TAGGER_EMAIL` to that verified email. The workflow rejects tags signed by a different email or not verified by GitHub.
- Confirm the `release` environment requires approval by `misaon`, allows only `v*` tags, and disallows administrator bypass. With one maintainer, self-review remains allowed; add an independent release reviewer and enable prevention of self-review when one is available.
- Confirm the branch and tag rulesets and immutable releases setting are active. There must be no bypass for tag updates or deletion.
- After the first image is published, make `ghcr.io/misaon/nylzo` public and confirm anonymous pull access.

No signing key was registered on the account during the repository setup on 2026-10-26. Registering and securing that key is an account-owner task; the release workflow must remain blocked until this prerequisite is met.

## Publish

1. Check that the intended `main` commit passed `CI required` and CodeQL. Resolve security alerts and verify the commit you are about to tag. Never tag an unreviewed branch commit.
2. Create and push a signed, annotated version tag (replace the example version):

   ```sh
   git switch main
   git pull --ff-only
   git tag -s v0.1.0 -m "nylzo 0.1.0"
   git push origin v0.1.0
   ```

3. Review the tag, source commit, and workflow run in Actions. Approve the `Release` job in the `release` environment. The guard requires GitHub's verified tag signature, the configured tagger email, a commit reachable from `main`, and a protected release environment.
4. The web client builds in a job with read-only access. The release job checks its manifest, builds archives and the container, signs them, creates SBOMs, and adds provenance. GoReleaser uploads to a **draft** release; the workflow publishes it only after the provenance steps succeed. Publication locks the assets and release tag through GitHub immutable releases.
5. Confirm the published release contains every expected archive, checksums, the Sigstore bundle, and SBOMs, and that provenance verification succeeds. Never publish a partially uploaded draft manually to work around a failed job. Investigate a failed run before retrying; published immutable assets cannot be replaced.

## Verify after publication

1. Verify the checksum signature and archive provenance using the commands in the [README](../README.md#verify-a-release). Verify image provenance and signatures as described in [deploy/compose/README.md](../deploy/compose/README.md). Use the exact repository, workflow identity, and tag.
2. Independently rebuild the same tag with the pinned tools:

   ```sh
   git fetch --tags
   git checkout v0.1.0
   mise install
   mise run release:verify
   ```

   This compares the published archives and embedded client files with the rebuild and prints the SHA-256 of the published checksum manifest. It supplements, rather than replaces, signature verification.

3. With Docker available, compare the image binaries with the local build:

   ```sh
   mise run release:verify-image
   ```

4. Record who rebuilt it, the operating system and architecture, and the `checksums.txt` SHA-256 in the release notes. For example: "Maintainer rebuild on macOS arm64: checksums.txt SHA-256 …; archives, web client, and image binaries identical."
5. If verification fails, stop deployment and investigate. Mark the release as unsafe in its notes and issue an advisory or corrective release as appropriate. Do not move the tag or overwrite assets.
6. After deployment, run `nylzo verify-client --server https://…` using the verified binary from that release. Set `NYLZO_MONITOR_URL` only after the public instance serves a published, verified version; the existing monitor otherwise remains disabled.
