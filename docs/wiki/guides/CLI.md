# Nylzo CLI

This page is for people who send and read Nylzo messages from a terminal. The examples use `https://nylzo.example` for your instance, such as `https://nylzo.com`. Commands that run a server are in [Operations](Operations.md).

`nylzo` lists the commands and `nylzo <command> -h` shows the flags of one.

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

Update with `brew upgrade nylzo` or `scoop update nylzo`. Only the latest release receives security fixes. [Verify a release](Verify-a-release.md#verify-a-homebrew-or-scoop-install) shows how to check what you installed.

The macOS and Windows binaries are not notarized or Authenticode-signed, and macOS refuses to run one from an archive a browser downloaded, so install through a package manager.

## Keep secrets out of arguments

Pass links, passphrases and manage tokens on stdin, in files, through the `NYLZO_*` variables below or at a hidden prompt. Other processes and your shell history can read command arguments, so `nylzo` refuses any argument that holds a link with its key.

Files the CLI writes are new and readable only by your account. It refuses file systems that cannot keep them private, such as FAT, exFAT, network shares, FUSE mounts and virtual machine shared folders. Keep your input files private too (`umask 077`).

The CLI connects only over HTTPS, except to loopback, and follows no redirects.

## Create a message

```sh
nylzo create --server https://nylzo.example --ttl 1h --opens 2 \
  --manage-token-file manage.txt < message.txt > link.txt
```

`create` reads up to 100 KiB of UTF-8 text from stdin, or one hidden line when stdin is a terminal. It prints the link on stdout and its expiry time on stderr.

- `--server URL` names the instance; `NYLZO_SERVER` works too.
- `--ttl DURATION` sets how long the link works, such as `30m` or `24h`, from 1 minute to the instance's maximum; the instance sets the default.
- `--opens N` sets how many times the message can be opened, 1 by default, up to the instance's maximum.
- `--from-file FILE` sends this file, up to 5 MiB, instead of text.
- `--compress` gzips the attachment when that at least halves its stored size.
- `--generate-passphrase-file FILE` generates a passphrase into a new private file without printing it.
- `--passphrase-file FILE` uses the first line of the file as the passphrase; `NYLZO_PASSPHRASE_FILE` works too.
- `--ask-passphrase` asks for the passphrase twice without echo.
- `--manage-token-file FILE` writes the manage token to a new private file, and `--print-manage-token` prints it after the link.

A passphrase you choose needs at least 15 characters and must pass the checks in [protocol section 2](../reference/Protocol.md#2-key-derivation-client); the CLI warns below 16 characters or 4 words. A generated one is four groups of four random characters, 80 bits.

The manage token has two lines: the message address without the key, then the token. Whoever holds it can check the status and cancel the message.

With `--compress`, the stored size shows roughly how well the file compresses. Do not compress a file that mixes a secret with bytes someone else chose ([protocol section 3.2](../reference/Protocol.md#32-compressed-attachments)).

## Read a message

```sh
nylzo read --link-file link.txt --out received
```

`read` takes the link from `--link-file`, `NYLZO_LINK`, stdin or a hidden prompt. It asks for a passphrase when needed, or reads it from `--passphrase-file` or `NYLZO_PASSPHRASE_FILE`. It needs a destination:

- `--out DIR` creates a new private `nylzo-…` directory inside the existing directory `DIR`, saves `message.txt` and any attachment there and prints their paths. It checks that it can write there before it opens the message.
- `--stdout` prints the text and writes nothing to disk. It cannot save an attachment, and a link does not show whether one is attached: if one is, the open is spent and the file is lost. Use `--out` when a file may be attached.

On Windows and macOS each saved file gets the mark a browser gives a download, so SmartScreen, Office and Gatekeeper check it when you open it. Nylzo cannot scan attachments for malware. To run one you trust, clear the mark with `Unblock-File` on Windows or `xattr -d com.apple.quarantine` on macOS.

Saved files hold the plaintext. Delete them when you no longer need them; SSDs and backups may still keep copies.

Opening the message spends an open. Ctrl-C before that costs nothing. After it, `read` keeps going until the message is saved, and a second Ctrl-C stops it at once and loses that open. A broken download is retried without spending another open.

## Check, cancel or delete

```sh
nylzo status --manage-token-file manage.txt
nylzo cancel --manage-token-file manage.txt
nylzo burn --link-file link.txt
```

`status` shows the state, the opens used and the times, as JSON with `--json`. The state `lost` means the server can no longer open the message, for example after a host restart, so send it again. `status` and `cancel` also take the manage token on stdin or from the file that `NYLZO_MANAGE_TOKEN_FILE` names.

`burn` deletes the remaining opens using the link, as a recipient can. A message with a passphrase needs the passphrase for that.

## Use the web client from your computer

`nylzo web` writes and `nylzo open` reads messages in your browser with the web client built into `nylzo`, so the instance sends your browser no code:

```sh
nylzo web --server https://nylzo.example
nylzo open --server https://nylzo.example < link.txt
```

[Use the local web client](Verify-a-release.md#use-the-local-web-client) explains what this protects and how it works.

## Verify an instance's web client

```sh
nylzo verify-client --server https://nylzo.example
```

`verify-client` compares every page, file and security header the instance serves with the web client built into your `nylzo`, which must be of the release the instance runs. It exits with 0 when everything matches, 3 when something differs and 1 when it could not finish. Allow a header that a proxy in front of the instance adds with `--allow-header NAME`; headers that change browser behavior, such as `Set-Cookie` or `Refresh`, cannot be allowed.

A server can recognize this command and send browsers something else, so a passing check covers only what this run received.

## Process protection

Every command that handles a link, passphrase or manage token first protects its process:

- It writes no core file if it crashes.
- It refuses to run under a debugger, and other processes of your account cannot attach to it later. On macOS the release binaries also run with the hardened runtime.
- On Linux it locks its memory against swap when your memlock limit is unlimited, and otherwise warns about each swap area that is not encrypted. Encrypt swap or set `memlock` to unlimited in `/etc/security/limits.conf`; `NYLZO_ALLOW_SWAP=1` hides the warning. macOS encrypts swap itself. On Windows, turn on BitLocker, which also encrypts the page file.
- It refuses to run while `GODEBUG` contains `http2debug=1` or `http2debug=2`, which would print request signatures.

Root and administrators can still read the memory, and hibernation writes it to disk. On macOS every process of your account can read the environment with `ps eww`, so do not use `NYLZO_LINK` there.

## Exit status

`nylzo` exits with 0 on success, 1 on an error, 2 on invalid usage, including a secret in an argument, and 3 when `verify-client` finds a difference. When Ctrl-C or SIGTERM stops a command before it finished, `nylzo` cleans up and then ends by that signal, so a shell sees 130 or 143. On Windows it exits with that number instead.

## Version

`nylzo version` prints the version; `--json` adds the source commit, its date and whether the source was modified. It does not prove where the build came from; [verify the release](Verify-a-release.md) for that.

## AI agents

The [Nylzo agent skill](../../../skills/nylzo/SKILL.md) shows how an agent can use the CLI with private files, without putting links or decrypted content into a conversation.
