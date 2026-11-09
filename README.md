<p align="center">
  <a href="https://nylzo.com">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="assets/readme/cover-dark.svg">
      <img src="assets/readme/cover-light.svg" alt="Nylzo. Encrypted sharing. Expiring links." width="1200">
    </picture>
  </a>
</p>

<h1 align="center">Share a secret. Set its expiry.</h1>

<p align="center">
  Send a password, private note, or file through an encrypted link.<br>
  Nylzo encrypts it on your device. You choose when the link expires and how many times it opens.
</p>

<p align="center">
  <a href="https://nylzo.com"><strong>Create a link</strong></a> ·
  <a href="#install-the-cli">Install</a> ·
  <a href="https://github.com/misaon/nylzo/wiki">Documentation</a> ·
  <a href="https://github.com/misaon/nylzo/wiki/Self-hosting">Self-host</a>
</p>

<p align="center">
  <a href="https://github.com/misaon/nylzo/releases/latest"><img src="https://img.shields.io/github/v/release/misaon/nylzo?style=flat&amp;label=release&amp;color=6556AC" alt="Latest release"></a>
  <a href="https://github.com/misaon/nylzo/actions/workflows/ci.yml"><img src="https://github.com/misaon/nylzo/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI on main"></a>
  <a href="https://app.codecov.io/gh/misaon/nylzo"><img src="https://codecov.io/gh/misaon/nylzo/branch/main/graph/badge.svg" alt="Test coverage on main"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/misaon/nylzo"><img src="https://api.scorecard.dev/projects/github.com/misaon/nylzo/badge" alt="OpenSSF Scorecard"></a>
  <a href="https://www.bestpractices.dev/projects/15969"><img src="https://www.bestpractices.dev/projects/15969/baseline" alt="OpenSSF Baseline self-assessment"></a>
</p>

## Features

- Text up to 100 KiB and files up to 5 MiB, encrypted on your device before upload.
- Links expire after at most 7 days and after the number of opens you set. Add a passphrase, cancel a link you sent, or let the recipient delete the message.
- One Go binary holds the `nylzo` CLI and the web client, and runs the server on Linux and macOS with SQLite and file storage.
- Signed releases with build provenance and reproducible archives. `nylzo verify-client` compares the web client an instance serves with the signed release.

## How it works

1. Your browser or the CLI generates a random secret and encrypts the message with AES-256-GCM.
2. Only the ciphertext goes to the server. The secret travels in the link after `#`, the part of a URL that browsers do not send to servers.
3. The server counts opens and enforces the expiry. When the hour in which a link expires ends, the server erases the key it needs to produce the link's decryption share, so a copy of the stored data stops opening too.
4. The recipient's browser or CLI decrypts the message locally.

The [protocol](https://github.com/misaon/nylzo/wiki/Protocol) defines the cryptography. The [security model](https://github.com/misaon/nylzo/wiki/Security-model) describes each layer of protection and the threat model behind it.

## Get started

| Use Nylzo          | Start here                                                                                                                                                    |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| In your browser    | [Create a link at nylzo.com](https://nylzo.com). No account required. Its limits and operator are on its [transparency page](https://nylzo.com/transparency). |
| From your terminal | [Install the CLI](#install-the-cli) with Homebrew on macOS and Linux or with Scoop on Windows, on x64 or ARM64.                                               |
| On your own server | [Deploy with Docker Compose and Caddy](https://github.com/misaon/nylzo/wiki/Self-hosting), or run the binary behind a TLS proxy.                              |

### Install the CLI

On macOS and Linux:

```sh
brew install misaon/nylzo/nylzo
```

On Windows:

```powershell
scoop bucket add nylzo https://github.com/misaon/homebrew-nylzo
scoop install nylzo/nylzo
```

The [tap](https://github.com/misaon/homebrew-nylzo) offers a release only after it has verified the release's signature and build provenance. [Verify a release](https://github.com/misaon/nylzo/wiki/Verify-a-release#verify-a-homebrew-or-scoop-install) shows how to check an install yourself. Then create a link from a file and read it back:

```sh
nylzo create --server https://nylzo.com --ttl 1h --opens 1 < message.txt > link.txt
nylzo read --link-file link.txt --out received
```

The [CLI guide](https://github.com/misaon/nylzo/wiki/CLI) covers passphrases, attachments, and `nylzo web`, which runs the web client from the binary so the instance sends no code to your browser.

## Security

- Your device encrypts the message with AES-256-GCM. The 256-bit key stays in the link after `#`, so the server stores only ciphertext.
- A passphrase goes through scrypt with 256 MiB of memory per guess, and the server locks a message after 100 wrong guesses.
- Every change runs a stolen-server test. It searches a copy of the database, files and logs for keys, content, file names, message IDs and IP addresses, and must find none.
- nylzo.com carries messages only over TLS 1.3 with the post-quantum key exchange X25519MLKEM768.
- Every 30 minutes a workflow compares the web client nylzo.com serves with the signed release. On a mismatch it puts the instance into maintenance and closes it at the Hetzner Cloud Firewall.
- The host runs Flatcar Container Linux in Finland. SSH accepts only hardware keys from one administrator network, with post-quantum key exchange.
- Renovate looks for dependency updates every six hours. It merges a security fix once every check passes, and nylzo.com installs the signed patch release within ten minutes.
- Releases are signed and reproducible, with build provenance.

Independent tests of nylzo.com: [securityheaders.com](https://securityheaders.com/?q=https%3A%2F%2Fnylzo.com%2F&followRedirects=on) and [Mozilla Observatory](https://developer.mozilla.org/en-US/observatory/analyze?host=nylzo.com) rate it A+, and [PageSpeed Insights](https://pagespeed.web.dev/analysis?url=https%3A%2F%2Fnylzo.com%2F) gives it 100 for accessibility, best practices and SEO.

The [security model](https://github.com/misaon/nylzo/wiki/Security-model) describes each layer and the threat model behind it. Report vulnerabilities [privately](https://github.com/misaon/nylzo/security/advisories/new), as [SECURITY.md](SECURITY.md) describes.

## Built with

| Layer | Technology |
| --- | --- |
| Encryption | AES-256-GCM, scrypt, HKDF-SHA-256, Ed25519, TLS 1.3 with X25519MLKEM768 |
| Server | Go and its standard library with six direct dependencies, SQLite, Caddy |
| Web | Svelte 5, TypeScript, Bun, Web Crypto, Subresource Integrity, Trusted Types, a strict CSP |
| Hosting | Hetzner Cloud in Finland, Flatcar Container Linux, OpenTofu, Let's Encrypt |
| Supply chain | Signed commits, CodeQL, govulncheck, OSV-Scanner, Grype, golangci-lint, Renovate, Sigstore, SLSA provenance, release-please changelogs |

## Contributing

Read the [contributing guide](CONTRIBUTING.md), pick an [issue](https://github.com/misaon/nylzo/issues), or start a [discussion](https://github.com/misaon/nylzo/discussions). [SUPPORT.md](SUPPORT.md) says where to ask for help.

[MIT license](LICENSE) · [Name and logo](https://github.com/misaon/nylzo/wiki/Name-and-logo) · [Changelog](https://github.com/misaon/nylzo/releases)
