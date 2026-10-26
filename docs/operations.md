# Operating Nylzo

For a deployment with TLS, start with [Self-hosting](wiki/Self-hosting.md). This reference covers the server configuration, SQLite database, and file storage.

## Start a local server

`mise run dev` provisions development keys automatically. To configure a server manually after `mise run build`, generate a pepper and an operator reporting key:

```sh
export NYLZO_PEPPER="$(openssl rand -hex 32)"
bin/nylzo report-key --key-file "$HOME/nylzo-report-key"
```

Set `NYLZO_REPORT_KEY` to the public value printed by `report-key`, then run:

```sh
bin/nylzo serve
curl -fsS http://localhost:8080/api/v1/instance
```

Store the pepper securely. Losing it makes existing tokens unverifiable and live messages inaccessible. The reporting **private** key belongs on the operator's workstation, outside the server. `report-key` creates it with mode 0600 and refuses to overwrite an existing file.

The server listens on `127.0.0.1:8080` over plain HTTP and stores `nylzo.db` and `blobs/` in `./data`. Set `NYLZO_LISTEN=:8080` for a container or remote proxy and terminate TLS at a trusted proxy. The server warns with `plain-http-listener` outside loopback. Linux and macOS are supported; on Windows use Docker or WSL because the required directory fsync and free-space checks are unavailable.

The web client must be built before the Go binary to embed it. `mise run build` does both. Without the client in `internal/server/web/dist`, the server embeds a placeholder page.

## Configuration

| Variable                                                                         | Default                 | Meaning                                                                                                                                                                                       |
| -------------------------------------------------------------------------------- | ----------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `NYLZO_PEPPER`                                                                   | Required by `serve`     | 32 bytes encoded as hex or base64url; an all-equal-byte value is rejected                                                                                                                     |
| `NYLZO_PEPPER_PREVIOUS`                                                          | Empty                   | Previous pepper during rotation; retain for seven days so live tokens can still be verified                                                                                                   |
| `NYLZO_PEPPER_FILE`, `NYLZO_PEPPER_PREVIOUS_FILE`                                | Empty                   | Read the pepper from a file, such as a Docker secret; a trailing newline is ignored. Setting both a value and its file is an error                                                            |
| `NYLZO_REPORT_KEY`                                                               | Required by `serve`     | Operator public key for sealed abuse reports; generated with `nylzo report-key`                                                                                                               |
| `NYLZO_LISTEN`, `PORT`                                                           | `127.0.0.1:8080`        | Listener address; a hosting platform's `PORT` means listening on all interfaces                                                                                                               |
| `NYLZO_DATA_DIR`                                                                 | `data`                  | SQLite, blobs, and reports; the filesystem must support hard links and directory fsync                                                                                                        |
| `NYLZO_TRUSTED_PROXIES`                                                          | Empty                   | Proxy CIDRs allowed to supply `X-Forwarded-For`                                                                                                                                               |
| `NYLZO_PROXY_MODE`                                                               | Empty                   | `scaleway` reads a single `X-Real-IP`, subject to the trust rules below                                                                                                                       |
| `NYLZO_SWEEP_INTERVAL`                                                           | `5m`                    | Built-in cleanup interval; `0` disables it, negative values are invalid                                                                                                                       |
| `NYLZO_MAX_STREAMS`                                                              | `64`                    | Maximum concurrent claim/blob streams; overflow returns 503 with `Retry-After: 5` without consuming an open                                                                                   |
| `NYLZO_MIN_FREE_BYTES`                                                           | `1073741824`            | Reserved disk space (1 GiB) after pending uploads; `0` disables the check                                                                                                                     |
| `NYLZO_MAX_FILE_BYTES`                                                           | `5242880`               | File limit; may only be lowered from the protocol maximum                                                                                                                                     |
| `NYLZO_MAX_TTL`, `NYLZO_DEFAULT_TTL`                                             | `168h`, `24h`           | Maximum/default expiration; allowed range is 60 seconds to seven days                                                                                                                         |
| `NYLZO_MAX_OPENS`                                                                | `10`                    | Maximum permitted opens, capped at 100                                                                                                                                                        |
| `NYLZO_RATE_CREATE_HOUR`, `NYLZO_RATE_CREATE_DAY`, `NYLZO_RATE_CREATE_BYTES_DAY` | `20`, `100`, `50000000` | Creation limits per source address bucket; `0` disables the corresponding limit                                                                                                               |
| `NYLZO_RATE_COARSE_MULTIPLIER`                                                   | `5`                     | Multiplier for coarse address buckets; `0` disables coarse limits                                                                                                                             |
| `NYLZO_RATE_READ_HOUR`                                                           | `300`                   | Metadata, claim, and blob reads per source bucket                                                                                                                                             |
| `NYLZO_RATE_ABUSE_HOUR`                                                          | `5`                     | Abuse reports per source bucket                                                                                                                                                               |
| `NYLZO_RATE_SOURCE_RPS`, `NYLZO_RATE_SOURCE_BURST`                               | `10`, `20`              | In-memory source token bucket for `/api/*`                                                                                                                                                    |
| `NYLZO_RATE_BUCKET_RPS`, `NYLZO_RATE_BUCKET_BURST`                               | `50`, `100`             | Instance-wide in-memory token bucket for `/api/*`                                                                                                                                             |
| `NYLZO_GLOBAL_DAILY_CREATES`                                                     | `0`                     | Global daily creation cap; `0` disables it. Proof of work starts at 50%; 100% returns 503                                                                                                     |
| `NYLZO_OPERATOR`, `NYLZO_CONTACT`, `NYLZO_HOSTING`                               | Empty                   | Transparency-page operator, email or HTTPS contact, and hosting description; each is at most 200 bytes without control characters                                                             |
| `NYLZO_PUBLIC_URL`                                                               | Empty                   | Public origin for canonical URLs, language links, social previews, and sitemap; HTTPS, lowercase host, optional port, no path or trailing slash. HTTP is accepted only for localhost/loopback |
| `NYLZO_POW_DIFFICULTY`, `NYLZO_POW_CONCURRENCY`                                  | `4`, `4`                | Proof-of-work difficulty (maximum 8) and concurrent verification (maximum 4); each verification uses 16 MiB                                                                                   |

## Proxy trust and limits

Configure `NYLZO_TRUSTED_PROXIES` or `NYLZO_PROXY_MODE` behind a reverse proxy. Otherwise clients share the proxy's address bucket. The server logs `forwarded-header-ignored` at most once an hour, naming only the ignored header. This can also be triggered by a client sending forwarding headers outside a trusted proxy.

In `scaleway` mode, `X-Real-IP` must occur exactly once. With `NYLZO_TRUSTED_PROXIES`, the connection must match one of those CIDRs; without it, only loopback, link-local, private, and CGNAT source addresses are accepted. Use this mode only when every request passes through the intended ingress.

Source buckets use IPv4 /32 or IPv6 /64; coarse buckets use /24 or /48. A request rejected by a source creation limit does not count toward the coarse limit. A coarse API token bucket has half the instance capacity. One source can hold at most two streaming slots; one coarse range can hold at most eight.

Address-based limits are in memory, keyed with a fresh random process key. Addresses are not stored in the database, backups, logs, or pepper. These limits apply per process and reset on restart. The global daily cap, proof of work, passphrase backoff, and per-message abuse-report cap are persisted and shared between instances using the database.

The daily cap reserves space atomically after proof of work and releases it on failure. A 503 may recommend retrying in 60 seconds while uploads hold reservations, including reservations left by a crashed process; that can continue until the UTC day ends. When stored messages fill the cap, the response recommends waiting until the next day. Proof-of-work challenges can be used only once.

## Storage, memory, and cleanup

Use a local filesystem with hard links and directory fsync. FAT, exFAT, and object-storage FUSE mounts do not satisfy the storage requirements. The server and cleanup command check these properties. Keep the data directory private and outside shared locations.

Database files, including WAL and SHM files, are created with mode 0600. At startup, the server checks ownership and permissions of the data directory, database, blobs, and reports. It narrows its own files/directories to 0600/0700 and logs `permissions-narrowed`. It rejects foreign ownership or symbolic links. Pepper files readable by the group or others are rejected. SQLite uses `secure_delete` and checkpoints each transaction to reduce retained history in the WAL; this adds disk synchronizations.

Without an explicit `GOMEMLIMIT`, `serve` sets a soft Go memory limit of 192 MiB for a 256 MB container. SQLite's cache is outside that limit and is limited to 512 KiB per connection. Cleartext HTTP/2 buffers at most 64 KiB of unread request data per stream and 1 MiB per connection. On Linux, `serve`, `sweep-once`, and `takedown` disable core dumps and restrict memory inspection by other processes of the same user.

Cleanup runs every five minutes. For an external scheduler, set `NYLZO_SWEEP_INTERVAL=0` and run:

```sh
bin/nylzo sweep-once
```

Use an absolute `NYLZO_DATA_DIR`. `sweep-once` needs an existing database and blob directory, does not create or migrate them, and does not need the pepper. Upgrade the server before upgrading a separately scheduled cleanup image.

## Logging and upgrades

Request logs contain minute-rounded time, HTTP method, route pattern, and status. Latency is omitted for create, claim, and blob routes because it correlates with message size; response sizes are never logged. Logs omit message IDs, raw paths, blob paths, Authorization and `X-Nylzo-*` headers, bodies, and client addresses. Storage failures log only the operation and system error, while internal HTTP-server errors use categories rather than raw error text. At very low traffic, a log's rounded minute may still correlate with an individual message event.

Burn and cancel persist the intended end state before deleting a blob. A later request or cleanup completes an interrupted operation. Expiration follows `expires_at`. A missing blob for a live message without an end state returns 500 rather than deleting the message or consuming an open. Cleanup runs at startup; orphaned-blob inspection is retried after failure. `sweep-once` returns a nonzero status if any cleanup step fails.

Schema 5 stores hashed IDs and sealed message records. Upgrading an unreleased development database from an earlier schema discards its messages because they cannot be sealed without the secrets in their links; daily cleanup removes their orphaned blobs. No published release used those older schemas. Stop old server processes before opening a database with a newer server, back up according to your retention policy, and do not downgrade a migrated database. The server refuses newer schema versions. Historical token formats and retired passphrase profiles are not compatible; ask senders to create new links.

## Abuse reports and takedown

Reports are sealed to the operator's public key with HPKE (MLKEM768-X25519) and padded to a fixed size. The server cannot decrypt them. Daily UTC files are stored as `data/abuse/YYYY-MM-DD.sealed`; their timestamps are normalized to the start of the day. Legacy plaintext `.jsonl` reports are sealed and removed on startup. Reports older than seven days are removed during cleanup and before new reports.

The reason is sanitized: controls become spaces, invisible formatting characters are removed, link fragments and long tokens are redacted, and length is capped. A decrypted report contains the UTC date, message ID, sanitized reason, good-faith declaration, and an optional contact email. Reports about ended messages are accepted but not stored. At most three reports per message per day are stored; a valid additional submission receives `{"status":"limit"}`. One reporter can exhaust this message-wide allowance. The total reporting files are capped at 64 MiB; an overflowing submission returns 503 and logs `abuse-file-full`. Process and remove those files to free space.

Decrypt on the operator's workstation, using the private key kept off the server:

```sh
nylzo reports --key-file "$HOME/nylzo-report-key" < abuse/2026-10-24.sealed
```

After review, place approved message IDs in a private file, one per line, and run `nylzo takedown < approved-ids.txt` on the server with the intended `NYLZO_DATA_DIR`. This removes the blob key and blob, leaving the row for cleanup after seven days; it cannot read the message. If any line is not a message ID, the command rejects the whole input. This includes links carrying a secret. It returns a result per ID without logging it to the server log. The recipient sees a deleted message; the sender sees that a recipient or the operator removed it, without an end time.

Use a reporter's optional contact email only for acknowledgement and the moderation decision. Follow the instance's published [terms and privacy policy](legal/README.md).
