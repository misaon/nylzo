# Nylzo CLI

The same `nylzo` binary creates and reads messages, serves a verified web client on your computer, checks an instance's web client, and runs the server. Run `nylzo` for command help. This guide uses `https://nylzo.example` as a placeholder for your own instance.

## Install

On macOS and Linux, use [Homebrew](https://brew.sh):

```sh
brew install misaon/nylzo/nylzo
```

On Windows, use [Scoop](https://scoop.sh):

```powershell
scoop bucket add nylzo https://github.com/misaon/homebrew-nylzo
scoop install nylzo/nylzo
```

Upgrade with `brew upgrade nylzo` or `scoop update nylzo`; only the latest release receives security fixes. The formula and the manifest in [misaon/homebrew-nylzo](https://github.com/misaon/homebrew-nylzo) pin each release archive by its SHA-256 hash. Their workflow writes a new release there only after it has verified the release's signature and build provenance and installed it on macOS, Linux and Windows. [Verify a release](Verify-a-release.md#verify-a-homebrew-or-scoop-install) shows how to check an install yourself.

The macOS and Windows binaries carry no Apple notarization or Authenticode signature. macOS refuses to run a binary from an archive that a browser downloaded. Homebrew does not mark its downloads that way, and Scoop adds no Mark of the Web on Windows. To build from source instead, run `mise run build` in a checkout.

## Keep secrets out of arguments

Pass message links, passphrases, and manage tokens through stdin, files, supported `NYLZO_*` variables, or hidden terminal prompts. Keep them out of command arguments, which can appear in process listings and shell history. Arguments resembling a secret-bearing link are rejected before any network request. Prefer private files over environment variables when other processes can inspect your environment.

The files and directories the CLI writes are new and private. It never replaces an existing file or writes through a link that has the file's name. A file gets mode 0600 and a directory mode 0700. macOS checks access control entries before the mode, so there the CLI creates each one without the entries its parent directory would pass on. Go applies no mode beyond read-only on Windows, so there the CLI creates each one with a protected access list that grants only your account. On both systems nothing is inherited from the parent directory, even when other accounts can read it (F04). The CLI then reads the owner and access back and refuses an entry that is not private. It writes only to a local disk whose file system stores and enforces the permissions itself: NTFS or ReFS on Windows, ext4, XFS, Btrfs, tmpfs and similar on Linux, and a local macOS volume that respects ownership. FAT, exFAT, network shares, FUSE and virtual machine shared folders are refused, since a mount option, server or helper decides access there whatever the file reports. `read --out` checks this before it opens the message. It opens the directory it created inside `--out` and writes every file through that handle, so renaming the directory or putting a link in its place does not redirect the files. Administrators on Windows and root elsewhere can still take ownership of any file.

The CLI requires HTTPS, except for exact localhost and loopback addresses, which bypass proxies and are checked to resolve to loopback. It does not follow redirects. Error messages do not repeat flag values, file paths, server text, or certificate text that could contain secrets. Control characters in errors are escaped.

## Process protection

Every command that may hold a link, passphrase, manage token, message, report key, pepper or epoch key protects its process before it reads anything: `create`, `read`, `open`, `web`, `status`, `cancel`, `report`, `burn`, `report-key` and `reports`, and on the server host `sweep-once`, `store-check` and `takedown`. `serve` takes the first two steps below and then locks its memory itself, refusing to start without the lock (see the [operations guide](Operations.md)). When a step fails, the command stops before it reads a secret.

First, the command sets its core file limit to 0, so a crash writes no core file.

Second, it shuts out debuggers that run as the same user, and it stops when one is already attached. On Linux the process becomes non-dumpable: other processes of the user can neither attach with ptrace nor read `/proc/<pid>/mem` and `/proc/<pid>/environ`. A tracer that attached before keeps its access, so the command refuses to run while any of its threads is traced, as under `strace`, `gdb` or `proot`, and when `/proc` cannot show its threads. A tracer outside the PID namespace that `/proc` belongs to stays invisible, and nothing on any system covers the moment between the start of the program and its protection, in which the environment already holds `NYLZO_LINK`. On macOS `PT_DENY_ATTACH` refuses ptrace, but a debugging tool that the user granted Developer Tools access can still read the memory through the task port, and every process of the user can read the environment with `ps eww`, so do not pass `NYLZO_LINK` on macOS. On Windows the process gets a new DACL: a process of the user that opens nylzo by its id can wait for it and end it, but it cannot read the memory, write a dump or change the DACL back. Windows does not separate the processes of one user, though: the handle the shell or console host opened when nylzo started keeps full access, and so do nylzo's threads. Root, an administrator with `SeDebugPrivilege` and the kernel can always read the memory. `open` and `web` start the browser without any `NYLZO_*` variable, so the browser, which nylzo does not protect, never holds `NYLZO_LINK`.

Third, on Linux the command locks all of its memory with `mlockall`, so no secret reaches swap, when the memlock limit is unlimited or the process has `CAP_IPC_LOCK` outside a user namespace. It first raises its soft limit to the hard limit. Under a finite limit it does not lock, because an allocation past the limit would later stop the process, and scrypt alone takes 256 MiB. It then reads `/proc/swaps` (or, when that is hidden, asks the kernel whether any swap is active) and warns on stderr about every swap area that dm-crypt does not encrypt on each path down to the disk. zram counts as memory unless it writes pages back to a device, and an area nylzo cannot trace counts as unencrypted. To clear the warning, encrypt swap or give your user an unlimited memlock limit (`memlock` in `/etc/security/limits.conf`, or `LimitMEMLOCK=infinity` in a systemd unit). `NYLZO_ALLOW_SWAP=1` (or `true`) hides it. macOS encrypts swap, and the CLI checks the `vm.swapusage` flag. Go cannot lock its heap on Windows, so turn on BitLocker for the system drive, which also encrypts the page file. Hibernation writes all memory to disk, locked pages included, so only disk encryption covers it.

## Create a message

Use a private directory and synthetic data while experimenting. Read real secrets from a private input file; avoid typing them into a recorded terminal.

```sh
nylzo create --server https://nylzo.example --ttl 1h \
  --manage-token-file management.txt < message.txt > link.txt
```

Create input/output files inside a directory accessible only to you, and use a restrictive umask such as `umask 077`. `--ttl` sets how long the link stays valid, up to 7 days, and `--opens` how many times it can be opened, 1 by default up to the instance's limit. `create` prints only the link. It reads text from stdin (one hidden line when stdin is a terminal) and takes an attachment through `--from-file`. `NYLZO_SERVER` can select the instance instead of `--server`.

`--compress` gzips the attachment on your device before encryption. The CLI keeps the compressed form only when it at least halves the padded size, and sends the file unchanged otherwise. The padded size of a compressed file still says something about its content, so do not compress a file that mixes a secret with bytes someone else can choose. See the [protocol](../reference/Protocol.md#32-compressed-attachments).

Passphrase options:

- `--generate-passphrase-file FILE` creates a strong passphrase in a new private file and does not print it.
- `--passphrase-file FILE` or `NYLZO_PASSPHRASE_FILE` reads the first line of an existing file.
- `--ask-passphrase` prompts twice without echo.

A passphrase you choose needs at least 15 characters and must pass the checks of [protocol section 2](../reference/Protocol.md#2-key-derivation-client); a generated one always does. Passphrase key derivation uses scrypt with `N = 2^18`. The manage token is written only when requested with `--manage-token-file` (a new private file) or `--print-manage-token`. It has two lines: the link, then the manage token. Keep both private.

## Read, inspect, cancel, or delete

```sh
nylzo read --link-file link.txt --out received
nylzo status --manage-token-file management.txt
nylzo cancel --manage-token-file management.txt
nylzo burn < link.txt
```

`read` and `burn` accept the link through stdin, `--link-file`, or `NYLZO_LINK`. Using the environment variable produces a notice on stderr. Links using a newer protocol version prompt an upgrade; links using the retired passphrase profile (`kdf_id` 2) prompt a replacement link from the sender. A link from Nylzo 0.1 or 0.2 (`/m/{id}#1.…`) reports that the instance upgrade to protocol version 2 deleted its message.

A CLI from Nylzo 0.1 or 0.2 can neither create nor read messages on an instance that runs protocol version 2: it reports a malformed response from the server, or the server answers `client_outdated`. Update it to a current release.

`read` requires an explicit destination before making a request:

- `--stdout` prints text without writing files, so it cannot deliver an attachment. A message does not show whether it holds a file until it is opened: `--stdout` then prints the text, spends the open and reports the file it did not save. Use `--out` when a file may be attached. Control characters are escaped on a terminal; a pipe or file receives the original text.
- `--out DIRECTORY` creates a new private subdirectory before opening the message, writes `message.txt` and any attachment there as private files under a sanitized filename, and prints their paths. After it saves an attachment, it warns on stderr that Nylzo cannot scan the file for malware and that it should be opened only if the sender is trusted. `read` saves the files and prints its notices about them on stderr before anything goes to stdout. A closed pipe on stdout or stderr cannot end `read` early: the files stay saved, and `read` exits with `1` when part of its output could not be written. Name collisions do not consume an open. A failed write may be retried in a second new subdirectory beside the first, never in another location.
- Every file `--out` writes gets the mark a browser gives a download before it holds any content, so Windows SmartScreen, Office Protected View and macOS Gatekeeper check an attachment when it is opened, as they check a browser download (F08). On Windows it is a `Zone.Identifier` stream with `ZoneId=3` and `HostUrl=about:internet`. On macOS it is a `com.apple.quarantine` attribute with only its flags and the time, in the form macOS writes itself; a mark the system has already set stays. The mark records neither the link nor the instance. Linux has no such mark. `read` checks that the new subdirectory can hold the mark before it opens the message.
- Opening a marked program can make SmartScreen or Gatekeeper look it up online with Microsoft or Apple, as for any download. On Windows, Microsoft Defender with its default cloud protection and automatic sample submission applies block at first sight to programs and scripts from the Internet zone: when its cloud service cannot decide on an unknown one, it uploads a copy to Microsoft for analysis. The mark does not stop a script started through an interpreter. To run an attachment you trust, clear the mark with `Unblock-File` on Windows or `xattr -d com.apple.quarantine` on macOS.
- The CLI writes the mark itself and does not call Windows Attachment Services: its scan at save time may delete a file after the message's only open was spent. Attachment Manager policies that act when a file is saved therefore do not apply; on-access antivirus scanning still does.

For passphrase-protected messages, `read` and `burn` prompt without echo or use `--passphrase-file`. The server deletes such a message only with the passphrase; a wrong one deletes nothing and counts against the same attempt limit as a wrong `read`. An older CLI sends no proof of the passphrase with `burn`, so the server refuses to delete such a message for it (`client_outdated`); update the CLI. Ctrl-C restores terminal echo before exiting. Interrupted downloads are retried within the same invocation using the lease, without consuming another open.

`status` supports `--json`. It and `cancel` accept the manage token via stdin, `--manage-token-file`, or `NYLZO_MANAGE_TOKEN_FILE`.

Downloaded files contain plaintext. Delete them when no longer needed, while recognizing that SSDs and backups may retain copies. The CLI makes a best effort to clear keys from its own memory, but Go strings, libraries, garbage collection, terminals, and pipes may retain copies.

## Use the web client without trusting the server

`nylzo web` and `nylzo open` run the web client embedded in the binary in your own browser, so the instance sends no code to it. [Verified clients](Verified-clients.md) explains the guarantee.

```sh
nylzo web --server https://nylzo.example
nylzo open --server https://nylzo.example < link.txt
```

`web` serves the composer under a new random `http://….localhost` address at a random port for every session and opens it in the default browser; on macOS before 26 it prints the address for Chrome, Edge or Firefox instead, because Safari would ask the network for that name. The links it creates point to the instance. `open` reads a link the way `read` does (stdin, a hidden prompt, `--link-file` or `NYLZO_LINK`), refuses a link of another instance than `--server` or `NYLZO_SERVER`, and opens it through a one-time address that carries no part of the link. It writes that address into a private file and opens the file, so no process argument holds it. `--no-browser` prints the address instead, and so does `open` when no browser has taken the link after ten seconds. At start both read the operator's contact from the instance's transparency document, the one outside address their pages may link. Both commands run until Ctrl-C; `open` stops with an error when no browser takes the link within two minutes and warns when something asks for it a second time.

## Verify an instance's web client

```sh
nylzo verify-client --server https://nylzo.example
```

This compares the served web client with the client embedded in the binary. It fetches every page and file twice, both without compression and as requested by a browser, and checks content, the static CSP, security headers, the `Connection-Allowlist` with the operator's contact from `/.well-known/nylzo/transparency.json`, and `/.well-known/nylzo/client.json`.

It exits with 0 when everything matches, 3 when a page, file, manifest or header differs from the release or the server answers with a redirect or an oversized response, and 1 when the check could not finish. The check compares every page and file even after an unexpected header or a failed request, and exits 1 for those only when nothing differs. A malformed transparency document counts as a difference. For headers added by a proxy or CDN, explicitly allow each with `--allow-header NAME`. Behavior-changing headers such as `Refresh` or `Set-Cookie` always exit 3 and cannot be allowed. The binary and server must run the same version; a binary without an embedded web client cannot perform this check.

The result applies only to that invocation. A malicious server can recognize the CLI and serve browsers different JavaScript. Run checks regularly and from another network; a successful check does not prove what other visitors receive. The repository's `watch.yml` runs it about every 30 minutes after an operator configures `NYLZO_MONITOR_URL`, using a binary from a signature-verified release.

## Report abusive content

```sh
nylzo report --link-file link.txt --reason-file reason.txt \
  --contact reporter@example.com --good-faith
```

The report is signed with the message's read key. Links use the same private input channels as `read`. The reason comes from a file so an accidentally included link does not enter the process arguments. The CLI redacts links and long tokens before submission and rejects a sanitized reason longer than 1000 bytes. `--contact` is optional; `--good-faith` declares that the report is accurate and complete to the reporter's good-faith belief. Invalid contact addresses are rejected before a request.

Reports about ended messages are accepted but not stored. When the per-message daily report allowance is exhausted, the CLI reports that no further report was stored and exits with status 1. Report vulnerabilities in Nylzo through [SECURITY.md](../community/Security-policy.md), rather than the content-reporting channel.

## Version and build identity

`nylzo version` and `nylzo --version` print the application version. `nylzo version --json` additionally prints the source commit, source commit date, and modified-source flag. Release builds derive their version from the signed tag; local builds identify development source explicitly. Version output identifies a build but does not authenticate it: verify the release signatures and provenance as described in [versioning and releases](../maintainers/Releasing.md).

## Agent use and exit codes

The [Nylzo agent skill](../../../skills/nylzo/SKILL.md) describes using private files outside the repository, keeping secrets and links out of conversations, and delivering decrypted content directly to a program rather than a language model. The skill gives instructions. The CLI, filesystem permissions, and sandbox enforce access rules.

Exit codes: `0` for success, `1` for an operational error, and `2` for invalid usage, including a secret in an argument. For `serve`, `sweep-once`, `store-check`, `takedown`, `report-key`, and `reports`, see the [operations guide](Operations.md).
