# Verify a release

Use only [misaon/nylzo releases](https://github.com/misaon/nylzo/releases) and `ghcr.io/misaon/nylzo`. Published release tags and assets are immutable. Choose a stable version from the releases page; the examples below use `v0.1.1`.

Install [GitHub CLI](https://cli.github.com/) and [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) from their official sources. Choose the archive for your platform: `linux`, `darwin` (macOS), or `windows`; architecture `amd64` (x64) or `arm64`. Windows archives are ZIP files; Linux and macOS use `tar.gz`.

## Download and verify an archive

This example selects Linux x64:

```sh
tag=v0.1.1
archive="nylzo_${tag#v}_linux_amd64.tar.gz"
gh release download "$tag" --repo misaon/nylzo \
  --pattern "$archive" --pattern checksums.txt --pattern checksums.txt.sigstore.json

cosign verify-blob checksums.txt --bundle checksums.txt.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity "https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/${tag}"
sha256sum --ignore-missing -c checksums.txt

gh attestation verify "$archive" --repo misaon/nylzo \
  --signer-workflow misaon/nylzo/.github/workflows/release.yml \
  --source-ref "refs/tags/${tag}" --deny-self-hosted-runners
```

Verify the signature **before** trusting the checksums, and require all checks to succeed before extracting or running the binary. On macOS without `sha256sum`, calculate `shasum -a 256 "$archive"` and compare its full value with that archive's entry in the signature-verified manifest. On Windows, use `Get-FileHash -Algorithm SHA256` for the same comparison.

Each release includes per-archive SPDX SBOMs. The signed manifest also lists the embedded web files. [Versioning and releases](../releasing.md) explains the version, source commit, source date, and modified-source flag embedded in the CLI, server, and web client.

## Verify a container image

Resolve the selected version to a digest, verify that digest, then deploy that same digest:

```sh
tag=v0.1.1
image="ghcr.io/misaon/nylzo:${tag#v}"
digest=$(docker buildx imagetools inspect "$image" --format '{{.Manifest.Digest}}')
image="ghcr.io/misaon/nylzo@${digest}"

cosign verify "$image" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity "https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/${tag}"
gh attestation verify "oci://${image}" --repo misaon/nylzo \
  --signer-workflow misaon/nylzo/.github/workflows/release.yml \
  --source-ref "refs/tags/${tag}" --deny-self-hosted-runners
printf 'NYLZO_IMAGE=%s\n' "$image"
```

Copy the printed `NYLZO_IMAGE` into your Compose `.env` only after verification succeeds. A mutable `latest` tag alone does not pin the image you verified. See [Self-hosting](Self-hosting.md) for deployment.

## Rebuild independently

Use a clean checkout of the release tag, install its pinned tools, and sign in with `gh`:

```sh
git fetch --tags
git checkout v0.1.1
mise trust
mise install
mise run release:verify
mise run release:verify-image
```

Review repository tasks before trusting them. `release:verify` verifies the published manifest signature, rebuilds all six archives and the embedded client, and compares their hashes. It skips SBOM generation; verify downloaded SBOMs against the signed manifest separately.

`release:verify-image` requires Docker. It verifies the image signature and provenance, then compares both Linux image binaries with the local rebuild.

Use `v0.1.1` or later for byte-identical complete archives. The v0.1.0 TAR file payloads reproduce, but its headers contain build-host ownership metadata; the release notes record this packaging limitation.

Sigstore signatures do not imply Apple notarization or a Windows Authenticode publisher certificate. Read the [security model](Security-model.md) for the remaining client and server trust boundaries.
