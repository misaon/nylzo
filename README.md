<h1>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/brand/dark/nylzo-landscape-dark.svg">
    <img alt="Nylzo" src="assets/brand/light/nylzo-landscape-light.svg" width="301" height="105">
  </picture>
</h1>

[![CI](https://github.com/misaon/nylzo/actions/workflows/ci.yml/badge.svg)](https://github.com/misaon/nylzo/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Share a sensitive message or file through an expiring link, with encryption in the client.** Nylzo is open source and self-hosted. It includes a web client and a single Go binary that works as both a CLI and a server.

[Website](https://nylzo.com) · [Contributing](CONTRIBUTING.md) · [Discussions](https://github.com/misaon/nylzo/discussions) · [Releases and changelog](https://github.com/misaon/nylzo/releases) · [Security policy](SECURITY.md) · [Protocol](spec/protocol.md)

Nylzo is in early development. Passing automated checks does not constitute an independent security audit or certification. Read the [security boundaries](#security-boundaries) before using it for sensitive data.

## What it does

- Encrypts text and files up to 5 MiB before uploading them. The link carries the encryption secret in its URL fragment, which browsers do not send to the server.
- Supports expiration, limited opens, an optional passphrase, sender cancellation, and recipient deletion.
- Provides a Go CLI and a Svelte web client backed by Go and TypeScript protocol implementations checked against shared test vectors.
- Runs as one binary with an embedded web client, SQLite metadata, and file storage. PostgreSQL and S3 adapters are not implemented.
- Exposes an instance transparency page with its operator, limits, storage and logging policy, and web-client file hashes.
- Defines a release pipeline with signed checksums, SBOMs, provenance attestations, container signatures, and reproducibility checks across x64 and arm64 runners.

## Try it locally

Install [mise](https://mise.jdx.dev/getting-started.html), then read [mise.toml](mise.toml) and the scripts in `mise-tasks/` before trusting them. Repository tasks execute code on your machine.

```sh
mise trust
mise install
mise run dev
```

Open `http://localhost:8080`, create a message, and open its link in a private browser window. This setup is for development: creation and read rate limits are disabled. It listens only on loopback and stores local data and keys in `.dev/`. `mise run dev:reset` deletes them. Use `NYLZO_DEV_PORT=8081` to select another port; the browser tests use 8090. A phone accessing a plain HTTP LAN address cannot use Web Crypto, which needs HTTPS or localhost.

For hot reload, run `mise run dev:web` in another terminal and open `http://localhost:5173`. Vite proxies `/api` to the development server; `NYLZO_DEV_API` overrides that address. Test the server's CSP and nonces through the embedded client or end-to-end tests, because Vite does not serve the production CSP.

The pinned toolchain is Go 1.27.3, Node.js 24.22.1, and pnpm 12.14.0. `mise.lock` records tool checksums. Development tasks need a POSIX shell on Linux, macOS, or WSL; Go's race detector also needs a C compiler.

## Use the CLI

Build the binary with `mise run build`. Run `bin/nylzo` for command help or follow the [CLI guide](docs/cli.md) for examples and safe input/output handling. Client commands include `create`, `read`, `status`, `cancel`, `burn`, and `report`; `verify-client` checks the web-client bytes served by an instance. The [Nylzo agent skill](skills/nylzo) explains using the CLI without placing secrets in an agent conversation.

Never paste a real message link, passphrase, or private key into a public issue, terminal recording, or support transcript. The link is a bearer secret even when a passphrase protects the message content.

## Self-host

Use the [Docker Compose example](deploy/compose/README.md) for deployment behind Caddy with TLS. The server needs a random 32-byte pepper and an operator reporting public key. See the [operations guide](docs/operations.md) for setup, configuration, proxy trust, limits, data permissions, cleanup, and handling abuse reports.

The default listener is `127.0.0.1:8080` over plain HTTP. Public deployments require a TLS proxy. Set `NYLZO_LISTEN=:8080` when a container or a proxy on another host must reach the server, and configure proxy trust deliberately. The server supports Linux and macOS; use Docker or WSL on Windows.

## Security boundaries

The [protocol](spec/protocol.md), [security policy](SECURITY.md), and [design research](docs/designs/nylzo-architecture-research.md) describe the threat model and known limits. In particular:

- An operator who controls the server or hosting can serve a modified web client that reads secrets. A CLI from a verified release avoids trusting the served JavaScript. `nylzo verify-client` can detect changes, but a hostile server can distinguish that check from a browser request.
- Anyone with the link can delete a message or exhaust passphrase attempts. A weak passphrase can be guessed offline by someone who has both the link and a copy of the server's data.
- The server observes padded message sizes and some timing metadata. Encryption does not protect a compromised sender or recipient device.

Report vulnerabilities through [private reporting](https://github.com/misaon/nylzo/security/advisories/new), following [SECURITY.md](SECURITY.md). Use an instance's `/report` page for abusive content on that instance.

## Versions and releases

Download the latest binaries for Windows, Linux, and macOS from [GitHub Releases](https://github.com/misaon/nylzo/releases/latest), with amd64 and arm64 archives for every platform. Release notes are generated from Conventional Commits and grouped into features, fixes, performance, documentation, and other changes, with breaking changes first.

`nylzo version --json` shows the version, source commit, source date, and whether the source was modified. The web footer shows its embedded client version; `/transparency` distinguishes it from the reported server version. The server exposes the same build metadata in its instance and well-known JSON endpoints. See [versioning and releases](docs/releasing.md) for the full process.

## Verify a release

Use the canonical [GitHub Releases page](https://github.com/misaon/nylzo/releases) and `ghcr.io/misaon/nylzo`. The repository has immutable releases enabled: publishing locks the release assets and tag. No release is implied by the example version below.

Download the archive, `checksums.txt`, and `checksums.txt.sigstore.json` for the **same tag**, then verify the signature before trusting the checksums:

```sh
cosign verify-blob checksums.txt --bundle checksums.txt.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity 'https://github.com/misaon/nylzo/.github/workflows/release.yml@refs/tags/v0.1.0'
sha256sum --ignore-missing -c checksums.txt
```

Use the platform's SHA-256 tool on systems without `sha256sum`. Also verify provenance for the archive you downloaded:

```sh
gh attestation verify nylzo_0.1.0_linux_amd64.tar.gz --repo misaon/nylzo \
  --signer-workflow misaon/nylzo/.github/workflows/release.yml \
  --source-ref refs/tags/v0.1.0 --deny-self-hosted-runners
```

For image verification and immutable digests, follow [the Compose guide](deploy/compose/README.md). The [release checklist](docs/release-checklist.md) covers signed tags, the approval gate, publication, and independent rebuilds. `mise run release:verify` compares a rebuild of the checked-out release tag with the published archives and web-client hashes; it complements signature verification.

## Contribute and test

Start with [CONTRIBUTING.md](CONTRIBUTING.md), the [code of conduct](CODE_OF_CONDUCT.md), and [repository governance](docs/github-governance.md). Issues, pull requests, and new contributor documentation use English. Some earlier design and research documents remain in Czech.

```sh
mise run test
mise run lint
mise run build
mise run e2e:browsers
mise run e2e
```

`test` runs Go with the race detector and the TypeScript suites. `lint` runs the Go, TypeScript, workflow, and shell checks. CI additionally tests browsers, fuzz targets, Compose, dependencies, and reproducible builds. Run `mise tasks` for the full task list.

Regenerate shared vectors after protocol-core changes:

```sh
go run ./cmd/nylzo-vectors -out spec/vectors.json
```

The Go generator and TypeScript implementation must agree with the committed vectors. The largest RFC 7914 scrypt vector uses 1 GiB of memory. TypeScript runs it with `NYLZO_HEAVY_TESTS=1` (enabled in CI); `go test -short` skips its recalculation in the core, while the vector generator still computes it. The web app and ESLint currently use TypeScript 6.0.3 for tool compatibility; the core and browser tests use TypeScript 7.

## Repository map

| Path                                           | Purpose                                                                      |
| ---------------------------------------------- | ---------------------------------------------------------------------------- |
| `spec/`                                        | Normative protocol and shared test vectors                                   |
| `internal/core`, `web/core`                    | Go and TypeScript protocol implementations                                   |
| `internal/pepper`                              | Purpose-separated server keys and pepper rotation                            |
| `internal/store`                               | SQLite and file-storage adapters and shared storage tests                    |
| `internal/server`                              | HTTP endpoints, limits, proof of work, headers, cleanup, and embedded client |
| `cmd/nylzo`, `internal/cli`, `internal/client` | CLI, server commands, and HTTP client                                        |
| `web/app`                                      | Svelte 5 web client with English and Czech interfaces                        |
| `web/e2e`                                      | Playwright tests, including CLI/browser interoperability                     |
| `skills/nylzo`                                 | Agent Skills instructions for using the CLI                                  |
| `deploy/compose`                               | Self-hosting example with Caddy and Docker secrets                           |
| `assets/brand`                                 | Official light/dark SVG logos and brand colors                               |
| `.github/`, `.goreleaser.yaml`                 | CI, community forms, dependency maintenance, and release configuration       |
| `docs/`                                        | Operations, governance, release instructions, designs, and research          |

## License and name

The code and repository files are available under the [MIT license](LICENSE). That license does not grant trademark rights to the Nylzo name or logo. You may use the name to refer to this project. Do not use the name or logo to brand a different service or modified version, or imply an affiliation with nylzo.com.
