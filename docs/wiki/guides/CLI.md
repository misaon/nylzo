# Nylzo CLI

The same `nylzo` binary creates and reads messages, serves a verified web client on your computer, checks an instance's web client, and runs the server. Build it with `mise run build`, or use a [verified release](Verify-a-release.md). Run `nylzo` for command help. This guide uses `https://nylzo.example` as a placeholder for your own instance.

## Keep secrets out of arguments

Pass message links, passphrases, and management tokens through stdin, files, supported `NYLZO_*` variables, or hidden terminal prompts. Keep them out of command arguments, which can appear in process listings and shell history. Arguments resembling a secret-bearing link are rejected before any network request. Prefer private files over environment variables when other processes can inspect your environment.

The CLI requires HTTPS, except for exact localhost and loopback addresses, which bypass proxies and are checked to resolve to loopback. It does not follow redirects. Error messages do not repeat flag values, file paths, server text, or certificate text that could contain secrets. Control characters in errors are escaped.

## Create a message

Use a private directory and synthetic data while experimenting. Read real secrets from a private input file; avoid typing them into a recorded terminal.

```sh
nylzo create --server https://nylzo.example --ttl 1h \
  --manage-token-file management.txt < message.txt > link.txt
```

Create input/output files inside a directory accessible only to you, and use a restrictive umask such as `umask 077`. `create` prints only the link. It reads text from stdin (one hidden line when stdin is a terminal) and takes an attachment through `--from-file`. `NYLZO_SERVER` can select the instance instead of `--server`.

Passphrase options:

- `--generate-passphrase-file FILE` creates a strong passphrase in a new mode-0600 file and does not print it.
- `--passphrase-file FILE` or `NYLZO_PASSPHRASE_FILE` reads the first line of an existing file.
- `--ask-passphrase` prompts twice without echo.

Passphrase key derivation uses scrypt. The management token is written only when requested with `--manage-token-file` (a new mode-0600 file) or `--print-manage-token`. It has two lines: the link, then the management token. Keep both private.

## Read, inspect, cancel, or delete

```sh
nylzo read --link-file link.txt --out received
nylzo status --manage-token-file management.txt
nylzo cancel --manage-token-file management.txt
nylzo burn < link.txt
```

`read` and `burn` accept the link through stdin, `--link-file`, or `NYLZO_LINK`. Using the environment variable produces a notice on stderr. Links using a newer protocol version prompt an upgrade; links using the retired passphrase profile (`kdf_id` 2) prompt a replacement link from the sender.

`read` requires an explicit destination before making a request:

- `--stdout` prints text without writing files. If the message may carry an attachment, it requires `--out` and does not consume an open just to reject it. Control characters are escaped on a terminal; a pipe or file receives the original text.
- `--out DIRECTORY` creates a new mode-0700 subdirectory before opening the message, writes `message.txt` and any attachment using a sanitized filename with mode 0600, and prints their paths. Name collisions do not consume an open. A failed write may be retried in a second new subdirectory beside the first, never in another location.

For passphrase-protected messages, `read` prompts without echo or uses `--passphrase-file`. Ctrl-C restores terminal echo before exiting. Interrupted downloads are retried within the same invocation using the lease, without consuming another open.

`status` supports `--json`. It and `cancel` accept the management token via stdin, `--manage-token-file`, or `NYLZO_MANAGE_TOKEN_FILE`.

Downloaded files contain plaintext. Delete them when no longer needed, while recognizing that SSDs and backups may retain copies. On Windows, POSIX modes 0700 and 0600 do not restrict access: files inherit directory permissions, so choose a private destination. The CLI makes a best effort to clear keys from its own memory, but Go strings, libraries, garbage collection, terminals, and pipes may retain copies.

## Use the web client without trusting the server

`nylzo web` and `nylzo open` run the web client embedded in the binary in your own browser, so the instance sends no code to it. [Verified clients](Verified-clients.md) explains the guarantee.

```sh
nylzo web --server https://nylzo.example
nylzo open < link.txt
```

`web` serves the composer on `http://127.0.0.1` at a random port and opens it in the default browser. The links it creates point to the instance. `open` reads a link the way `read` does (stdin, a hidden prompt, `--link-file` or `NYLZO_LINK`), checks it against `--server` or `NYLZO_SERVER` when set, and opens it through a one-time address that carries no part of the link. It writes that address into a private file and opens the file, so no process argument holds it. `--no-browser` prints the address instead. Both commands run until Ctrl-C; `open` stops with an error when no browser takes the link within two minutes and warns when something asks for it a second time.

## Verify an instance's web client

```sh
nylzo verify-client --server https://nylzo.example
```

This compares the served web client with the client embedded in the binary. It fetches every page and file twice, both without compression and as requested by a browser, and checks content, the static CSP, security headers, and `/.well-known/nylzo/client.json`.

Unexpected headers are mismatches. For headers added by a proxy or CDN, explicitly allow each with `--allow-header NAME`. Behavior-changing headers such as `Refresh` or `Set-Cookie` cannot be allowed. The binary and server must run the same version; a binary without an embedded web client cannot perform this check.

The result applies only to that invocation. A malicious server can recognize the CLI and serve browsers different JavaScript. Run checks regularly and from another network; a successful check does not prove what other visitors receive. The repository's `monitor` workflow runs hourly after an operator configures `NYLZO_MONITOR_URL`, using a binary from a signature-verified release.

## Report abusive content

```sh
nylzo report --link-file link.txt --reason-file reason.txt \
  --contact reporter@example.com --good-faith
```

The report is signed with the message's read key, like the instance's `/report` page. Links use the same private input channels as `read`. The reason comes from a file so an accidentally included link does not enter the process arguments. The CLI redacts links and long tokens before submission and rejects a sanitized reason longer than 1000 bytes. `--contact` is optional; `--good-faith` declares that the report is accurate and complete to the reporter's good-faith belief. Invalid contact addresses are rejected before a request.

Reports about ended messages are accepted but not stored. When the per-message daily report allowance is exhausted, the CLI reports that no further report was stored and exits with status 1. Report vulnerabilities in Nylzo through [SECURITY.md](../community/Security-policy.md), rather than the content-reporting channel.

## Version and build identity

`nylzo version` and `nylzo --version` print the application version. `nylzo version --json` additionally prints the source commit, source commit date, and modified-source flag. Release builds derive their version from the signed tag; local builds identify development source explicitly. Version output identifies a build but does not authenticate it: verify the release signatures and provenance as described in [versioning and releases](../maintainers/Releasing.md).

## Agent use and exit codes

The [Nylzo agent skill](../../../skills/nylzo/SKILL.md) describes using private files outside the repository, keeping secrets and links out of conversations, and delivering decrypted content directly to a program rather than a language model. The skill gives instructions. The CLI, filesystem permissions, and sandbox enforce access rules.

Exit codes: `0` for success, `1` for an operational error, and `2` for invalid usage, including a secret in an argument. For `serve`, `sweep-once`, `takedown`, `report-key`, and `reports`, see the [operations guide](Operations.md).
