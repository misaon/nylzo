# Getting started

## Use the browser

1. Open [nylzo.com](https://nylzo.com) or your own Nylzo instance.
2. Write a message or attach a file up to 5 MiB. Choose its expiry and open limit; optionally add a passphrase.
3. Create the link and send it to the recipient. Send a passphrase through a different channel.

The recipient opens the link and chooses to reveal the content. A link preview alone does not consume an open. Read the [security model](Security-model.md) before using Nylzo for sensitive information.

A message link is a bearer secret. Keep real links and passphrases out of public issues, terminal recordings, support transcripts, and agent conversations. Recipients can keep copies of content they read.

## Use the CLI

[Download and verify](Verify-a-release.md) the archive for your operating system and architecture, then extract it. The binary includes both the CLI and the web client. Run `nylzo` for help and `nylzo version --json` for its build identity.

The [CLI guide](CLI.md) covers creating and reading messages, attachments, private input files, cancellation, abuse reports, and `verify-client`. Use private input files or hidden prompts for secrets; keep links and passphrases out of command arguments.

## Run a local development instance

With a source checkout and [mise](https://mise.jdx.dev/getting-started.html) installed, inspect [mise.toml](../../../mise.toml) and [mise-tasks/](../../../mise-tasks) before trusting repository tasks. They execute code on your machine.

```sh
mise trust
mise install
mise run dev
```

Open `http://localhost:8080`, create a synthetic message, and open its link in a private browser window. This instance listens on loopback, stores local data and keys in `.dev/`, and disables creation and read rate limits. It is a development setup. Follow [Self-hosting](Self-hosting.md) for a public instance with TLS and production limits.

See [Development](Development.md) for hot reload, tests, and resetting local data.
