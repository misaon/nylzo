# Verify a release

This page is for people who want to check that their `nylzo`, archives or images come from a signed Nylzo release, and to run the web client from that binary so the server cannot hand their browser modified code.

Get Nylzo only from [misaon/homebrew-nylzo](https://github.com/misaon/homebrew-nylzo), the [releases page](https://github.com/misaon/nylzo/releases), `ghcr.io/misaon/nylzo` and `ghcr.io/misaon/nylzo-caddy`. You need [GitHub CLI](https://cli.github.com/) and [cosign](https://docs.sigstore.dev/cosign/system_config/installation/). The examples check the latest release; set `tag` to another version to check that one:

```sh
tag=$(gh release view --repo misaon/nylzo --json tagName --jq .tagName)
```

## Verify a Homebrew or Scoop install

Homebrew and Scoop refuse an archive whose SHA-256 differs from the one their formula or manifest pins. To check the build provenance of the archive Homebrew installed:

```sh
brew fetch --formula misaon/nylzo/nylzo
gh attestation verify "$(brew --cache --formula misaon/nylzo/nylzo)" --repo misaon/nylzo \
  --signer-workflow misaon/nylzo/.github/workflows/release.yml \
  --source-ref "refs/tags/v$(nylzo version)" --deny-self-hosted-runners
```

On Windows, Scoop keeps the archive in its cache, by default under `scoop\cache` in your profile:

```powershell
$version = nylzo version
$archive = Get-ChildItem -Path "$env:USERPROFILE\scoop\cache" -Filter "nylzo#$version#*" | Select-Object -First 1
gh attestation verify $archive.FullName --repo misaon/nylzo --signer-workflow misaon/nylzo/.github/workflows/release.yml --source-ref "refs/tags/v$version" --deny-self-hosted-runners
```

## Verify a downloaded archive

Archives are named `nylzo_<version>_<system>_<architecture>` with `linux`, `darwin` (macOS) or `windows` and `amd64` or `arm64`; Windows ones are ZIP files. For Linux x64:

```sh
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

Trust `checksums.txt` only after its signature checks out, and run the binary only when every command succeeds. Without `sha256sum`, compare the output of `shasum -a 256` on macOS or `Get-FileHash -Algorithm SHA256` on Windows with the archive's line in `checksums.txt`.

The same signed `checksums.txt` lists the Caddy archives (`nylzo-caddy_<version>_linux_<architecture>.tar.gz`), the Hetzner Flatcar host bundle (`nylzo-host_<version>.tar`), an SPDX SBOM for each archive and every file of the web client. Verify those the same way.

The macOS binaries carry an ad-hoc signature with the hardened runtime, but no Apple notarization: `codesign --display --verbose "$(command -v nylzo)"` shows `flags=0x10002(adhoc,runtime)`.

## Verify a container image

Verify each image by its digest and deploy exactly that digest:

```sh
for repository in nylzo nylzo-caddy; do
  digest=$(docker buildx imagetools inspect "ghcr.io/misaon/${repository}:${tag#v}" --format '{{.Manifest.Digest}}')
  image="ghcr.io/misaon/${repository}@${digest}"
  cosign verify "$image" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    --certificate-identity "https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/${tag}" &&
  gh attestation verify "oci://${image}" --repo misaon/nylzo \
    --signer-workflow misaon/nylzo/.github/workflows/release.yml \
    --source-ref "refs/tags/${tag}" --deny-self-hosted-runners &&
  echo "verified ${image}"
done
```

Put both verified references into `NYLZO_IMAGE` and `NYLZO_CADDY_IMAGE` in your Compose `.env`. A tag alone does not pin the image you checked. [Self-hosting](Self-hosting.md) covers the deployment.

## Verify the dependency evidence

Each release also publishes its source SPDX inventory, dependency scan and any OpenVEX statement, covered by a separately signed `nylzo_<version>.evidence.sha256`. Check it with the same `cosign verify-blob` command as `checksums.txt`, using its `.sigstore.json` bundle, then run `sha256sum -c` on it. An OpenVEX statement applies only to the product, component and vulnerability it names.

## Rebuild a release yourself

In a clean checkout of the release tag, with [mise](https://mise.jdx.dev/getting-started.html) and a signed-in `gh`:

```sh
git checkout "$tag"
mise trust
mise install
mise run release:verify
mise run release:verify-image
```

The tasks run code on your machine, so read them first. `release:verify` checks the signature of `checksums.txt`, rebuilds every archive with the embedded web client, compares the hashes and checks the rebuilt archives against the release provenance. `release:verify-image` needs Docker and compares the binaries in both images with your rebuild.

## Use the local web client

The website loads its code from the server on every visit, so whoever controls the server, its hosting or a valid certificate for its name can send you a page that reads your message without you noticing. `nylzo web` and `nylzo open` run the web client built into your verified `nylzo` in your own browser instead, and the server receives only encrypted API calls. A message is safe from such a server only when the sender and the reader both use a verified client: the CLI or this local client.

To write a message:

```sh
nylzo web --server https://nylzo.example
```

Your browser opens the usual page at a new random `http://….localhost` address. Its links point to the instance, so the reader can open them with any client. Press Ctrl-C when you are done. Track in Sent lasts only until you close the tab; to cancel a message later, create it with `nylzo create --manage-token-file`.

To read a message, run this and paste the link at the hidden prompt, or pipe it in:

```sh
nylzo open --server https://nylzo.example
```

The browser gets a one-time address that carries no part of the link and works once, within two minutes, so the link stays out of commands, the address bar and the browser history. `nylzo open` warns if something uses that address twice, and refuses a link for another instance than the one you named.

The local client listens only on loopback, answers only its own random name and loads nothing from the instance. Use Chrome, Edge or Firefox, or Safari from macOS 26; on older macOS the commands print the address for another browser. `--no-browser` prints it instead.

No client protects against malware or an administrator on your computer, another browser extension that can read the page, or a recipient who shares the message. The [protocol, section 14](../reference/Protocol.md#14-client-code-integrity), states the exact rules.
