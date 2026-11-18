# Security model

This page is for people who want to know what Nylzo protects, from whom, and where that protection ends. The [protocol](../reference/Protocol.md) has the exact rules. To report a weakness, follow the [security policy](../community/Security-policy.md).

## How a message is protected

- The sender's browser or CLI encrypts the message with AES-256-GCM before upload, with a key from a random 256-bit secret in the part of the link after `#`, which browsers never send to a server ([§2](../reference/Protocol.md#2-key-derivation-client), [§4](../reference/Protocol.md#4-link)).
- A passphrase is checked with OPAQUE (RFC 9807), so the server never receives the passphrase or anything that tests it. Each guess takes one exchange with the server, which counts it. After five failures readers wait longer between tries, and after 100 the message stays locked until it expires. Each guess also costs scrypt with 256 MiB of memory ([§2.1](../reference/Protocol.md#21-passphrase-check-opaque)).
- The key also depends on an expiry share that the server derives from a per-minute key and erases at the message's deadline. After expiry nobody can open the message, the server included ([§5.6](../reference/Protocol.md#56-expiry-shares)).
- The link carries a hash of the encrypted message, so a server cannot hand a reader different content.

## What each party can learn

| Who | Can | Cannot |
| --- | --- | --- |
| The server, run honestly | See padded sizes, request times and, in memory, client addresses for rate limits | Read the content, the passphrase or file names |
| Someone who controls the server, its hosting or a valid certificate | Send a chosen visitor a website that reads their message; keep ciphertext; ignore the open limit | Read messages sent and read with verified clients |
| A network observer | See that you use the instance, and sizes and timing | Read the content or the link key |
| Someone with the link | Open the message while opens remain, or delete it | Skip a passphrase, or open the message after expiry |
| Someone with a copy of the server's data | Count messages and see coarse times | Open any message without its link |
| The recipient | Keep, copy or forward what they read | |

## The client is part of the trust boundary

The website gets its code from the server on every visit. If the server, its hosting or a valid certificate for its name is in someone else's hands, they can send you a page that reads your message, and neither you nor your browser can notice.

A verified client runs code from a signed, publicly logged release instead: the `nylzo` CLI, or `nylzo web` and `nylzo open`, which serve the built-in web client to your own browser ([use the local web client](Verify-a-release.md#use-the-local-web-client)). A message is safe from such a server only when the sender and the reader both use one ([§14](../reference/Protocol.md#14-client-code-integrity)).

`nylzo verify-client` compares the client an instance serves with the signed release. On nylzo.com a workflow runs that check every 30 minutes; a confirmed difference puts the site into maintenance, closes its firewall and opens an incident. A server that recognizes the check can still send other code to one chosen browser; only a verified client covers that.

In the Compose and Hetzner deployment profiles, Caddy serves the web client from its own build. The API process behind it cannot set pages, cookies or redirects, or reach the internet. This only contains a compromise of that process.

## In the browser

The pages carry a strict Content Security Policy without nonces, SHA-384 integrity hashes on every script and stylesheet, Trusted Types, cross-origin isolation and a Permissions Policy that turns off every feature the client does not use. `Connection-Allowlist` limits where a page may connect or navigate in browsers that support it. No page loads third-party scripts or analytics. Messages and file names are shown as text and never parsed as HTML. These rules limit injected code; a server that sends different code bypasses them.

What the browser keeps:

- A browser records the whole link, key included, in its history, sync and session restore before the page can remove it. Anyone with that profile or synced account then holds a working link until the last open or expiry. A passphrase or `nylzo open` avoids this.
- Track in Sent keeps a message's manage key in the tab's memory only. Keep after closing this tab stores it in IndexedDB, where Chromium and Firefox write it into the browser profile. That key can check status and cancel but cannot decrypt.
- The link secret, the passphrase and the keys derived from them are never written to browser storage. A page cannot reliably erase JavaScript strings or garbage-collected memory.
- Copied links and messages can stay in clipboard history. Do not save a message passphrase in a password manager.
- Attachments download only after a warning that shows the real extension. Nylzo cannot scan encrypted files.

## In the CLI

The CLI hardens its process before it reads a secret: no core file, no debugger, no swap on Linux when the memlock limit allows it. Root and administrators can still read its memory. The [CLI guide](CLI.md#process-protection) has the details.

## Someone with the link

Anyone with the link can open the message while opens remain, with the passphrase if one is set. They can also use up the passphrase attempts and lock readers out until expiry, which costs availability and reveals nothing. A message without a passphrase can be deleted with the link alone; one with a passphrase also needs the passphrase. Only the sender's cancel is sure to end a message early.

## A copy of the server

A copy of the database, files, logs and configuration holds no key, plaintext, file name, readable message id or client address. A test on every change searches such a copy for them ([§13](../reference/Protocol.md#13-what-a-stolen-server-reveals)). Each message's metadata sits in a sealed record that opens only with the server's secret pepper together with a public key that a request about that message carries.

Limits that remain:

- Someone with the link and a copy taken before the deadline that includes the pepper and the seed file can guess the passphrase offline at the cost of scrypt per guess. So can the running server. Only the strength of the passphrase protects the message then. A generated passphrase has 80 bits and is out of reach; a chosen one must have at least 15 characters.
- A disk image or backup that includes the seed file opens, with the link, every message that was live when it was taken. The deployment profiles keep that file on a tmpfs.
- When a message ends, the server overwrites the part of its stored file that the decryption key needs before deleting the file, so a disk image taken afterwards opens nothing. This needs a file system that overwrites in place, so on Linux the server accepts only ext4, XFS and tmpfs. SSD remapping and provider snapshots can still keep old blocks, which only an encrypted volume makes unreadable.
- Restoring an older copy brings back spent opens and cancelled messages, so an operator must not restore one.

## Metadata

The server sees padded sizes and the timing of requests, keeps most times rounded to minutes and holds client addresses only in memory for rate limits. On a quiet instance, timing can still link a sender's requests to a reader's. Attachments are compressed before encryption only when the sender asks for it with `nylzo create --compress`, and the padded size then reflects the compressed size ([§3.2](../reference/Protocol.md#32-compressed-attachments)).

An operator who sets up the passkey-protected admin area at `/admin` sees aggregate counts of visits and messages, the server load and the address ranges a limit refused in the last hour. It has no way to read, open or end a message ([§6.8](../reference/Protocol.md#68-admin-session)).

An instance's `/transparency` page lists its operator, limits and logging, as the operator states them.

## Transport

The deployment profiles accept only TLS 1.3 with the hybrid post-quantum key exchange X25519MLKEM768, so traffic recorded today stays protected against a later quantum computer that breaks X25519 alone. Older clients cannot connect, among them Safari and iOS before 26, Firefox ESR 128, curl built on OpenSSL older than 3.5 and many link-preview bots and uptime monitors.

The Hetzner Flatcar profile makes one exception for search and AI crawlers, many of which do not offer ML-KEM. Its Caddy also completes a handshake with X25519 alone from the address ranges these services publish for their crawlers. Such a connection reaches only public pages such as the home pages, `robots.txt` and `sitemap.xml`. Every other request, message links and the API included, gets 403.

Behind TLS, the expiry share travels sealed to a one-time hybrid post-quantum key of the client. An attacker who controls the TLS endpoint can still swap that key while a message is created and learn its share, and could also capture what tests a passphrase during creation. A recording of requests tests no passphrase.

TLS protects only the connection: whoever holds the instance's certificate key sees what the server sees. Both profiles use short-lived certificates, and the Hetzner Flatcar profile adds CAA records that limit who may issue them.

## What no client protects against

- Malware or an administrator on the sender's or reader's device.
- Another browser extension that can read the page.
- A recipient who keeps, copies or shares the message, or a screenshot of it.
- A compromised release. Releases are signed, logged and reproducible, so such a change would reach every user and could be found; [verify a release](Verify-a-release.md) before you run it.
